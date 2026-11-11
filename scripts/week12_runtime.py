"""Portable W12 entry into the unchanged, ownership-checked W11 lab."""
import re
import shutil
import subprocess
from pathlib import Path
from dev import ROOT, child_env
import week11_runtime as runtime
from week11_runtime import RUNS, Week11Lab, application, fetch

OUT = ROOT / 'docs/testing/evidence/week12'


def resolve_node(cached, system):
    for candidate in (cached, system):
        if not candidate:
            continue
        path = Path(candidate)
        try:
            result = subprocess.run([str(path), '--version'], env=child_env('web', {}),
                                    stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=5)
            match = re.fullmatch(r'v(\d+)\.(\d+)\.(\d+)\s*', result.stdout.decode('ascii'))
            version = tuple(map(int, match.groups())) if match else (0, 0, 0)
            if result.returncode == 0 and ((version[0] == 22 and version >= (22, 18, 0)) or version >= (24, 12, 0)):
                return path
        except (OSError, UnicodeError, subprocess.TimeoutExpired):
            continue
    raise RuntimeError('Node 22.18+ (22.x) or 24.12+ is required; install Node and add it to PATH')


def configure_node():
    node = resolve_node(ROOT / '.local/week04-tools/node_modules/node/bin/node', shutil.which('node'))
    runtime.NODE = node
    return node
