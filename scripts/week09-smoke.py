#!/usr/bin/env python3
"""Full gated PG/Redis regression in an owned disposable database; no cloud API."""
import argparse
import json
import tempfile
from pathlib import Path
from dev import ROOT
from week09_lab import Lab


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--backend-only',action='store_true',help='run real PG/Redis suite without browser')
    args=parser.parse_args()
    out=ROOT/'docs/testing/evidence/week09'
    out.mkdir(parents=True,exist_ok=True)
    result={'status':'FAIL','synthetic_data_only':True,'daily_database_modified':False,'real_cloud_model_called':False}
    try:
        with Lab() as lab, tempfile.TemporaryDirectory(prefix='nexusdesk-week09-') as tmp:
            lab.migrate('up-to','12')
            lab.migrate('up')
            lab.migrate('down-to','12')
            lab.migrate('up')
            result['migration_13_14_up_down_up']='PASS'
            print('PASS owned schema 12→14→12→14 and Redis',flush=True)
            env=dict(lab.env, STORAGE_ROOT=str(Path(tmp)/'storage'))
            for flag in ['POSTGRES','W02_SCHEMA','W02_REGISTER','W02_LOGIN','W02_ORGANIZATIONS','W02_MEMBERS',
                         'W03_TICKETS','W04_CLAIM','W05_KNOWLEDGE','W06_RAG','W07_JOBS','W08_AI','W09_CACHE','W09_RATE']:
                env['NEXUSDESK_TEST_'+flag]='1'
            output=lab.run(['go','test','-race','-count=1','-v','./...'],env,600)
            (out/'integration.log').write_text(output)
            if '--- FAIL:' in output or '--- SKIP:' in output:
                raise RuntimeError('full gated suite contains failures or skips')
            result['go_pass_nodes']=sum(x.lstrip().startswith('--- PASS:') for x in output.splitlines())
            result['go_top_level_tests']=sum(x.startswith('--- PASS:') for x in output.splitlines())
            result['go_skip_nodes']=0
            print('PASS full Go race real PG/Redis: '+str(result['go_pass_nodes'])+' checks, 0 skips',flush=True)
            if not args.backend_only:
                from week09_web import run_browser
                result['browser']=run_browser(lab,env,Path(tmp))
            result['status']='PASS'
    finally:
        (out/'result.json').write_text(json.dumps(result,ensure_ascii=False,indent=2)+'\n')
    print('PASS owned resources removed',flush=True)


if __name__=='__main__':
    main()
