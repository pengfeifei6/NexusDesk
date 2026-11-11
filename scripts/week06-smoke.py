#!/usr/bin/env python3
"""W06 isolated pgvector, real model and browser acceptance; never reads .env."""
import os
from pathlib import Path
import sys

args = sys.argv[1:]
mode = ["--rag", "--web", "--rag-evaluation"]
if "--backend-only" in args:
    args.remove("--backend-only")
    mode = ["--rag"]
if "--evaluation-only" in args:
    args.remove("--evaluation-only")
    mode = ["--rag", "--rag-evaluation", "--skip-go"]
os.execv(sys.executable, [sys.executable, str(Path(__file__).with_name("week02-day2-smoke.py")), *mode, *args])
