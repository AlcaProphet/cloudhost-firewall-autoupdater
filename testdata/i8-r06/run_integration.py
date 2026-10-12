#!/usr/bin/env python3
"""使用临时 Go overlay 缩短预算，执行 I8-R06 真实调用链专项。"""
import argparse
import json
from pathlib import Path
import subprocess
import tempfile

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
with tempfile.TemporaryDirectory(prefix='i8-r06-integration-') as temporary:
    temp = Path(temporary)
    short = temp / 'read_budget.go'
    short.write_text(text.replace(expected, 'const cloudReadBudget = 150 * time.Millisecond', 1))
    overlay = temp / 'overlay.json'
    overlay.write_text(json.dumps({'Replace': {
        str(source): str(short),
        str(repo / 'syncer/read_budget_integration_test.go'): str(fixtures / 'syncer_budget_test.go'),
        str(repo / 'webui/api/read_budget_integration_test.go'): str(fixtures / 'api_budget_test.go'),
    }}))
    result = subprocess.run(['go', 'test', '-overlay=' + str(overlay), './syncer', './webui/api', '-race', '-run', '^TestReadBudget(TargetSafety|ScanCache)$', '-count=' + str(args.count), '-timeout=2m'], cwd=repo)
    raise SystemExit(result.returncode)
