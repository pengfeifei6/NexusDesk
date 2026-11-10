#!/usr/bin/env python3
"""W03 disposable PostgreSQL and complete prior-week regression entry point."""
import os
from pathlib import Path
import sys
os.execv(sys.executable, [sys.executable, str(Path(__file__).with_name("week02-day2-smoke.py")), "--tickets", *sys.argv[1:]])
