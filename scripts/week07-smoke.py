#!/usr/bin/env python3
"""W07 owned PostgreSQL/Redis/Worker and browser acceptance. Never reads .env."""
import os
from pathlib import Path
import sys

args = sys.argv[1:]
mode = ["--jobs", "--web"]
if "--backend-only" in args:
    args.remove("--backend-only")
    mode = ["--jobs"]
os.execv(sys.executable, [sys.executable, str(Path(__file__).with_name("week02-day2-smoke.py")), *mode, *args])
