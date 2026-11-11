#!/usr/bin/env python3
"""W04 isolated real PostgreSQL, claim concurrency and browser acceptance.

Use --backend-only to omit browser; --web-only to iterate browser checks.
Never reads .env. The common runner owns and cleans only its random resources.
"""
import os
from pathlib import Path
import sys

args = sys.argv[1:]
mode = "--web"
if "--backend-only" in args:
    args.remove("--backend-only")
    mode = "--claims"
os.execv(sys.executable, [sys.executable, str(Path(__file__).with_name("week02-day2-smoke.py")), mode, *args])
