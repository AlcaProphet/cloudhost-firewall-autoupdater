#!/usr/bin/env python3
"""使用临时 Go overlay 缩短预算，执行并核验 I8-R06 真实调用链专项。"""
import argparse
from collections import Counter
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile


MODULE = 'github.com/alcaprophet/cloudhost-firewall-autoupdater'
TARGET_TEST = 'TestReadBudgetTargetSafety'
SCAN_TEST = 'TestReadBudgetScanCache'
# 独立列出必需场景，防止夹具删掉场景后连同验收清单一起缩减。
EXPECTED_TESTS = {
    MODULE + '/syncer': [TARGET_TEST] + [TARGET_TEST + '/' + case for case in (
        's0_exhaust', 's1_exhaust', 's2_exhaust',
        's0_recovers', 's1_recovers', 's2_recovers', 'normal',
    )],
    MODULE + '/webui/api': [SCAN_TEST] + [
        SCAN_TEST + '/' + cloud + '/later=' + later
        for cloud in ('ali_ecs', 'ali_swas', 'tc_lighthouse', 'tc_cvm')
        for later in ('false', 'true')
    ],
}


def validate_results(events, count, returncode):
    """包成功不能代替各项测试按次数实际运行并通过。"""
    seen = Counter((event.get('Package'), event.get('Test', ''), event.get('Action'))
                   for event in events)
    errors = []
    if returncode:
        errors.append(f'go test exited with status {returncode}')
    for event in events:
        if event.get('Action') in ('fail', 'skip'):
            errors.append(f"{event['Action']}: {event.get('Package', '')}/{event.get('Test', '')}")
    for package, tests in EXPECTED_TESTS.items():
        if seen[package, '', 'pass'] != 1:
            errors.append(f'{package}: expected one package pass')
        for test in tests:
            for action in ('run', 'pass'):
                actual = seen[package, test, action]
                if actual != count:
                    errors.append(f'{package}/{test}: {action}={actual}, expected {count}')
    return errors


def run_tests(command, repo, count):
    env = os.environ.copy()
    # Go 的 os.Getwd 会优先采用仍指向同一目录的逻辑 PWD；cwd 本身不足以消除别名。
    env['PWD'] = str(repo)
    events, errors = [], []
    with subprocess.Popen(command, cwd=repo, env=env, stdout=subprocess.PIPE, text=True) as process:
        for line in process.stdout:
            try:
                event = json.loads(line)
                if not isinstance(event, dict):
                    raise ValueError('expected a JSON object')
            except ValueError:
                errors.append('invalid go test JSON event')
                print(line, end='', flush=True)
                continue
            if event.get('Output'):
                print(event['Output'], end='', flush=True)
            if event.get('Action') in ('run', 'pass', 'fail', 'skip'):
                events.append(event)
        returncode = process.wait()
    errors.extend(validate_results(events, count, returncode))
    if errors:
        for error in errors:
            print('I9-R05: ' + error, file=sys.stderr)
        return returncode if returncode > 0 else 1
    print(f'I9-R05: verified 2 top-level tests and 15 subtests, each run/passed {count} time(s).')
    return 0


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--count', type=int, default=3, help='repeat each integration test (default: 3)')
    args = parser.parse_args()
    if args.count < 1:
        parser.error('--count must be positive')

    repo = Path(__file__).resolve().parents[2]
    fixtures = Path(__file__).resolve().parent
    source = repo / 'provider/read_budget.go'
    expected = 'const cloudReadBudget = 120 * time.Second'
    text = source.read_text()
    if text.count(expected) != 1:
        raise SystemExit('Expected fixed production budget declaration was not found once')
    for name in ('syncer_budget_test.go', 'api_budget_test.go'):
        if not (fixtures / name).is_file():
            raise SystemExit('Missing integration fixture: ' + str(fixtures / name))

    with tempfile.TemporaryDirectory(prefix='i8-r06-integration-') as temporary:
        temp = Path(temporary).resolve()
        short = temp / 'read_budget.go'
        short.write_text(text.replace(expected, 'const cloudReadBudget = 150 * time.Millisecond', 1))
        overlay = temp / 'overlay.json'
        overlay.write_text(json.dumps({'Replace': {
            str(source): str(short),
            str(repo / 'syncer/read_budget_integration_test.go'): str(fixtures / 'syncer_budget_test.go'),
            str(repo / 'webui/api/read_budget_integration_test.go'): str(fixtures / 'api_budget_test.go'),
        }}))
        return run_tests([
            'go', 'test', '-json', '-overlay=' + str(overlay), './syncer', './webui/api',
            '-race', '-run', '^TestReadBudget(TargetSafety|ScanCache)$',
            '-count=' + str(args.count), '-timeout=2m',
        ], repo, args.count)


if __name__ == '__main__':
    raise SystemExit(main())
