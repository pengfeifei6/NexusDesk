#!/usr/bin/env python3
"""Full real PG/Redis regression and browser; owned lab, deterministic model."""
import argparse
import json
import tempfile
from pathlib import Path

from dev import ROOT, child_env
from week11_model import LocalModel
from week12_runtime import OUT, RUNS, Week11Lab, application, fetch, configure_node


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    group = parser.add_mutually_exclusive_group()
    group.add_argument('--backend-only', action='store_true')
    group.add_argument('--web-only', action='store_true')
    parser.add_argument('--demo', action='store_true', help='Record a 5–8 minute captioned synthetic walkthrough')
    args = parser.parse_args()
    if args.demo and args.backend_only:
        parser.error('--demo requires browser execution')
    OUT.mkdir(parents=True, exist_ok=True)
    RUNS.mkdir(parents=True, exist_ok=True)
    result = {'status': 'INCOMPLETE_OR_FAIL', 'synthetic_only': True, 'daily_database_modified': False, 'cloud_calls': 0, 'migration': 14}
    name = 'demo' if args.demo else 'backend' if args.backend_only else 'web' if args.web_only else 'smoke'
    try:
        with Week11Lab() as lab, tempfile.TemporaryDirectory(prefix='smoke-', dir=RUNS) as tmp:
            lab.migrate('up')
            directory = Path(tmp)
            if not args.web_only:
                env = dict(lab.env, STORAGE_ROOT=str(directory / 'storage'))
                for flag in ['POSTGRES', 'W02_SCHEMA', 'W02_REGISTER', 'W02_LOGIN', 'W02_ORGANIZATIONS', 'W02_MEMBERS',
                             'W03_TICKETS', 'W04_CLAIM', 'W05_KNOWLEDGE', 'W06_RAG', 'W07_JOBS', 'W08_AI', 'W09_CACHE', 'W09_RATE', 'W10_RAG']:
                    env['NEXUSDESK_TEST_' + flag] = '1'
                output = lab.run(['go', 'test', '-race', '-count=1', '-v', './...'], env, 600)
                (OUT / 'integration.log').write_text(output)
                assert '--- FAIL:' not in output and '--- SKIP:' not in output
                result.update(go_pass_nodes=sum(line.lstrip().startswith('--- PASS:') for line in output.splitlines()),
                              go_top_level_tests=sum(line.startswith('--- PASS:') for line in output.splitlines()), go_skips=0)
                print('PASS all real PG/Redis race gates: %d checks, 0 skips' % result['go_pass_nodes'], flush=True)
            if not args.backend_only:
                node = configure_node()
                with LocalModel() as model, application(lab, directory, {'OLLAMA_BASE_URL': model.origin}, worker=True, web=True) as app:
                    env = dict(child_env('web', {}), NEXUSDESK_LIVE_TEST_ORIGIN=app.origin,
                               NEXUSDESK_LIVE_TEST_API=app.api.origin, NEXUSDESK_LIVE_TEST_DATABASE=app.env['DB_NAME'])
                    command = [str(node), 'scripts/week12-browser.mjs'] + (['--demo'] if args.demo else [])
                    output = lab.run(command, env, 600 if args.demo else 240)
                    (OUT / ('demo-browser.log' if args.demo else 'browser.log')).write_text(output)
                    print(output, flush=True)
                    result['browser'] = json.loads((OUT / ('demo-browser.json' if args.demo else 'browser.json')).read_text())
                    result['browser']['model_fixture_calls'] = model.calls
                    (OUT / ('demo-metrics.txt' if args.demo else 'browser-metrics.txt')).write_bytes(fetch(app.diagnostic_url + '/metrics'))
            result['status'] = 'PASS'
        result['owned_resources_removed'] = True
    finally:
        (OUT / (name + '.json')).write_text(json.dumps(result, ensure_ascii=False, indent=2) + '\n')


if __name__ == '__main__':
    main()
