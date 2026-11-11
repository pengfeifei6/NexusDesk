#!/usr/bin/env python3
"""Owned PG/Redis regression and browser acceptance; no paid cloud calls."""
import argparse
import json
import tempfile
from pathlib import Path
from dev import ROOT
from week10_runtime import OUT, Week10Lab


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--backend-only', action='store_true')
    parser.add_argument('--web-only', action='store_true')
    args = parser.parse_args()
    if args.backend_only and args.web_only:
        parser.error('choose at most one scope')
    OUT.mkdir(parents=True, exist_ok=True)
    (ROOT / '.local/week10-runs').mkdir(parents=True, exist_ok=True)
    result = {'status': 'FAIL', 'synthetic_data_only': True, 'daily_database_modified': False, 'cloud_model_called': False}
    name = 'backend' if args.backend_only else 'web' if args.web_only else 'smoke'
    try:
        with Week10Lab() as lab, tempfile.TemporaryDirectory(prefix='smoke-', dir=ROOT / '.local/week10-runs') as tmp:
            lab.migrate('up')
            result['migration'] = 'latest 14 (W10 adds no schema)'
            env = dict(lab.env, STORAGE_ROOT=str(Path(tmp) / 'storage'))
            if not args.web_only:
                for flag in ['POSTGRES', 'W02_SCHEMA', 'W02_REGISTER', 'W02_LOGIN', 'W02_ORGANIZATIONS', 'W02_MEMBERS',
                             'W03_TICKETS', 'W04_CLAIM', 'W05_KNOWLEDGE', 'W06_RAG', 'W07_JOBS', 'W08_AI', 'W09_CACHE', 'W09_RATE', 'W10_RAG']:
                    env['NEXUSDESK_TEST_' + flag] = '1'
                output = lab.run(['go', 'test', '-race', '-count=1', '-v', './...'], env, 600)
                (OUT / 'integration.log').write_text(output)
                assert '--- FAIL:' not in output and '--- SKIP:' not in output
                result.update(go_pass_nodes=sum(l.lstrip().startswith('--- PASS:') for l in output.splitlines()),
                              go_top_level_tests=sum(l.startswith('--- PASS:') for l in output.splitlines()), go_skips=0)
                print('PASS all real PG/Redis Go race gates; ' + str(result['go_pass_nodes']) + ' checks, 0 skips', flush=True)
            if not args.backend_only:
                from week10_web import run_browser
                result['browser'] = run_browser(lab, Path(tmp))
            result['status'] = 'PASS'
    finally:
        (OUT / (name + '.json')).write_text(json.dumps(result, ensure_ascii=False, indent=2) + '\n')
    print('PASS owned fixture resources removed', flush=True)


if __name__ == '__main__':
    main()
