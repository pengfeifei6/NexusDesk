#!/usr/bin/env python3
"""Local static/unit gates using a supported Node; no private config, DB, or cloud."""
import json
import os
import subprocess
import time
from dev import ROOT, child_env
from week12_runtime import OUT, configure_node
NODE = configure_node()


def main():
    OUT.mkdir(parents=True, exist_ok=True)
    env = child_env('web', {})
    env['PATH'] = str(NODE.parent) + os.pathsep + env.get('PATH', '')
    checks = [
        ('python unit', ['python3', '-m', 'unittest', 'discover', '-s', 'scripts', '-p', 'test_*.py'], ROOT),
        ('go vet', ['go', 'vet', './...'], ROOT),
        ('go build', ['go', 'build', './...'], ROOT),
        ('frontend unit', ['npm', 'run', 'test:unit', '--', '--run'], ROOT / 'web'),
        ('frontend typecheck/build', ['npm', 'run', 'build'], ROOT / 'web'),
        ('frontend lint', ['npm', 'run', 'lint'], ROOT / 'web'),
        ('frontend format', ['npm', 'run', 'format:check'], ROOT / 'web'),
    ]
    rows, logs = [], []
    for name, command, cwd in checks:
        start = time.perf_counter()
        result = subprocess.run(command, cwd=cwd, env=env, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=300)
        rows.append({'name': name, 'command': command, 'exit_code': result.returncode, 'seconds': round(time.perf_counter() - start, 3)})
        logs.append(name + '\n' + result.stdout.decode(errors='replace'))
        print(('PASS ' if result.returncode == 0 else 'FAIL ') + name, flush=True)
        (OUT / 'checks.log').write_text('\n\n'.join(logs))
        (OUT / 'checks.json').write_text(json.dumps({'status': 'PASS' if all(r['exit_code'] == 0 for r in rows) and len(rows) == len(checks) else 'INCOMPLETE_OR_FAIL', 'node': str(NODE), 'checks': rows}, indent=2) + '\n')
    if any(row['exit_code'] for row in rows):
        raise SystemExit(1)


if __name__ == '__main__':
    main()
