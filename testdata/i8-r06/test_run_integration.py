#!/usr/bin/env python3
"""独立入口回归：使用隔离仓库与 Go 命令替身，不代替真实 SDK 专项。

运行：python3 -m unittest discover -s testdata/i8-r06 -p 'test_run_integration.py' -v
"""
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest


FIXTURES = Path(__file__).resolve().parent
MODULE = 'github.com/alcaprophet/cloudhost-firewall-autoupdater'
SYNCER = MODULE + '/syncer'
API = MODULE + '/webui/api'
TARGET = 'TestReadBudgetTargetSafety'
SCAN = 'TestReadBudgetScanCache'


def passing_events(count):
    # 与脚本验收清单独立的固定输入，覆盖两包和重复执行。
    names = {
        SYNCER: [TARGET] + [TARGET + '/' + case for case in (
            's0_exhaust', 's1_exhaust', 's2_exhaust',
            's0_recovers', 's1_recovers', 's2_recovers', 'normal',
        )],
        API: [SCAN] + [SCAN + '/' + cloud + '/later=' + later
                       for cloud in ('ali_ecs', 'ali_swas', 'tc_lighthouse', 'tc_cvm')
                       for later in ('false', 'true')],
    }
    events = []
    for package, tests in names.items():
        events.append(dict(Package=package, Action='start'))
        for _ in range(count):
            for name in tests:
                events.append(dict(Package=package, Test=name, Action='run'))
                events.append(dict(Package=package, Test=name, Action='pass'))
        events.append(dict(Package=package, Action='pass'))
    return events


class IntegrationRunnerTest(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix='i9-r05-runner-test-')
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name).resolve()
        self.repo = self.root / 'repo'
        self.fixtures = self.repo / 'testdata/i8-r06'
        self.fixtures.mkdir(parents=True)
        (self.repo / 'provider').mkdir()
        (self.repo / 'provider/read_budget.go').write_text(
            'package provider\nimport "time"\nconst cloudReadBudget = 120 * time.Second\n')
        source = Path(os.environ.get('I9_R05_RUNNER_UNDER_TEST', FIXTURES / 'run_integration.py'))
        self.runner = self.fixtures / 'run_integration.py'
        shutil.copyfile(source, self.runner)
        for name in ('syncer_budget_test.go', 'api_budget_test.go'):
            shutil.copyfile(FIXTURES / name, self.fixtures / name)
        self.link = self.root / 'repo-link'
        self.link.symlink_to(self.repo, target_is_directory=True)
        self.bin = self.root / 'bin'
        self.bin.mkdir()
        fake_go = self.bin / 'go'
        fake_go.write_text('#!' + sys.executable + '\n' + '''
import json, os, pathlib, sys
repo = pathlib.Path(os.environ['EXPECTED_REPO'])
pathlib.Path(os.environ['TRACE']).write_text('started')
assert pathlib.Path.cwd() == repo
assert os.environ['PWD'] == str(repo), 'logical PWD was inherited'
args = sys.argv[1:]
assert args[0] == 'test' and '-json' in args and '-race' in args
assert './syncer' in args and './webui/api' in args
assert '-timeout=2m' in args
assert args[args.index('-run') + 1] == '^TestReadBudget(TargetSafety|ScanCache)$'
assert '-count=' + os.environ['EXPECTED_COUNT'] in args
mapping = json.loads(pathlib.Path(next(a.split('=', 1)[1] for a in args if a.startswith('-overlay='))).read_text())['Replace']
assert set(mapping) == {str(repo / name) for name in (
    'provider/read_budget.go', 'syncer/read_budget_integration_test.go', 'webui/api/read_budget_integration_test.go')}
for key, value in mapping.items():
    assert pathlib.Path(key).is_absolute() and pathlib.Path(value).is_absolute()
    assert pathlib.Path(value).is_file()
assert 'const cloudReadBudget = 120 * time.Second' in (repo / 'provider/read_budget.go').read_text()
assert 'const cloudReadBudget = 150 * time.Millisecond' in pathlib.Path(mapping[str(repo / 'provider/read_budget.go')]).read_text()
sys.stdout.write(pathlib.Path(os.environ['EVENTS']).read_text())
raise SystemExit(int(os.environ['GO_EXIT']))
''')
        fake_go.chmod(0o700)

    def invoke(self, events, *, count=1, location='physical', raw=None, go_exit=0):
        event_file = self.root / 'events.jsonl'
        event_file.write_text(raw if raw is not None else ''.join(json.dumps(e) + '\n' for e in events))
        cwd = {'physical': self.repo, 'symlink': self.link, 'unrelated': self.root}[location]
        env = dict(os.environ, PATH=str(self.bin) + os.pathsep + os.environ['PATH'],
                   PWD=str(cwd), EXPECTED_REPO=str(self.repo), EXPECTED_COUNT=str(count),
                   EVENTS=str(event_file), TRACE=str(self.root / 'trace'), GO_EXIT=str(go_exit))
        return subprocess.run([sys.executable, str(self.runner), '--count', str(count)],
                              cwd=cwd, env=env, text=True, capture_output=True, timeout=15)

    def assert_rejected(self, events, diagnostic, **kwargs):
        result = self.invoke(events, **kwargs)
        self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn(diagnostic, result.stderr)
        return result

    def test_paths_and_repeated_execution(self):
        for location in ('physical', 'symlink', 'unrelated'):
            with self.subTest(location=location):
                result = self.invoke(passing_events(2), count=2, location=location)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                self.assertIn('2 top-level tests and 15 subtests', result.stdout)
                self.assertIn('2 time(s)', result.stdout)

    def test_zero_matches_despite_package_pass(self):
        self.assert_rejected([dict(Package=p, Action='pass') for p in (SYNCER, API)], 'run=0')

    def test_missing_subtest_despite_top_level_pass(self):
        events = [e for e in passing_events(1) if e.get('Test') != TARGET + '/normal']
        self.assert_rejected(events, TARGET + '/normal: run=0')

    def test_skip_is_failure(self):
        events = passing_events(1)
        for e in events:
            if e.get('Test') == TARGET + '/normal' and e['Action'] == 'pass':
                e['Action'] = 'skip'
        self.assert_rejected(events, 'skip: ' + SYNCER + '/' + TARGET + '/normal')

    def test_each_repeat_is_required(self):
        events = passing_events(1)
        self.assert_rejected(events, 'run=1, expected 2', count=2)

    def test_correct_name_in_wrong_package_is_insufficient(self):
        events = passing_events(1)
        for e in events:
            if e.get('Test') == TARGET + '/normal':
                e['Package'] = API
        self.assert_rejected(events, SYNCER + '/' + TARGET + '/normal: run=0')

    def test_package_pass_is_required(self):
        events = [e for e in passing_events(1) if e.get('Test') or e['Action'] != 'pass']
        self.assert_rejected(events, 'expected one package pass')

    def test_go_failure_is_preserved(self):
        result = self.assert_rejected(passing_events(1), 'status 7', go_exit=7)
        self.assertEqual(result.returncode, 7)

    def test_fail_event_cannot_be_hidden_by_exit_zero(self):
        events = passing_events(1) + [dict(Package=SYNCER, Test=TARGET, Action='fail')]
        self.assert_rejected(events, 'fail: ' + SYNCER)

    def test_invalid_json_is_failure(self):
        for raw in ('not-json\n', '[]\n'):
            with self.subTest(raw=raw):
                self.assert_rejected([], 'invalid go test JSON event', raw=raw)

    def test_missing_fixture_fails_before_go(self):
        for name in ('syncer_budget_test.go', 'api_budget_test.go'):
            with self.subTest(name=name):
                path = self.fixtures / name
                original = path.read_text()
                path.unlink()
                try:
                    self.assert_rejected(passing_events(1), 'Missing integration fixture:')
                    self.assertFalse((self.root / 'trace').exists())
                finally:
                    path.write_text(original)

    def test_budget_anchor_must_be_unique(self):
        path = self.repo / 'provider/read_budget.go'
        path.write_text(path.read_text() * 2)
        self.assert_rejected(passing_events(1), 'not found once')
        self.assertFalse((self.root / 'trace').exists())

    def test_count_must_be_positive(self):
        self.assert_rejected([], '--count must be positive', count=0)
        self.assertFalse((self.root / 'trace').exists())


if __name__ == '__main__':
    unittest.main()
