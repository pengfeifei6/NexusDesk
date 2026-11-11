#!/usr/bin/env python3
"""W05 isolated database/filesystem and real-browser acceptance; never reads .env."""
import os
from pathlib import Path
import sys

args = sys.argv[1:]
mode = ["--knowledge", "--web"]
if "--backend-only" in args:
    args.remove("--backend-only")
    mode = ["--knowledge"]
os.execv(sys.executable, [sys.executable, str(Path(__file__).with_name("week02-day2-smoke.py")), *mode, *args])
