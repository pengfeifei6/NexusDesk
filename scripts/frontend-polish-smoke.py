#!/usr/bin/env python3
"""Before/after visual and full UI acceptance against owned synthetic fixtures."""
import argparse
import json
import tempfile
from pathlib import Path
from dev import ROOT, child_env
from week11_model import LocalModel
from week12_runtime import RUNS, Week11Lab, application, configure_node


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--stage', choices=['before', 'after'], required=True)
    args = parser.parse_args()
    out = ROOT/'docs/testing/evidence/frontend-polish'/args.stage
    out.mkdir(parents=True, exist_ok=True); RUNS.mkdir(parents=True, exist_ok=True)
    result = dict(status='INCOMPLETE_OR_FAIL', synthetic_only=True, backend_modified=False, daily_database_modified=False, cloud_calls=0)
    try:
        node = configure_node()
        with Week11Lab() as lab, tempfile.TemporaryDirectory(prefix='frontend-polish-', dir=RUNS) as tmp:
            lab.migrate('up')
            with LocalModel() as model, application(lab, Path(tmp), {'OLLAMA_BASE_URL': model.origin}, worker=True, web=True) as app:
                env = dict(child_env('web', {}), NEXUSDESK_LIVE_TEST_ORIGIN=app.origin, NEXUSDESK_LIVE_TEST_API=app.api.origin,
                           NEXUSDESK_LIVE_TEST_DATABASE=app.env['DB_NAME'], NEXUSDESK_UI_CAPTURE_STAGE=args.stage)
                output = lab.run([str(node), 'scripts/frontend-polish-browser.mjs'], env, 240)
                (out/'browser.txt').write_text(output); print(output, flush=True)
                result['browser'] = json.loads((out/'browser.json').read_text())
                result['model_fixture_calls'] = model.calls
        result.update(status='PASS', owned_resources_removed=True)
    finally:
        (out/'smoke.json').write_text(json.dumps(result, ensure_ascii=False, indent=2)+'\n')


if __name__ == '__main__': main()
