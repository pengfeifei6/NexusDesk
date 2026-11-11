#!/usr/bin/env python3
"""Reproducible SQL plans on synthetic owned schema12, then current migrations."""
import json
from pathlib import Path
import statistics
import sys
from week09_lab import Lab
from dev import ROOT

TICKET_COLUMNS = 'id,org_id,creator_id,assignee_id,title,body,priority,status,version,created_at,updated_at,deleted_at'
DOC_COLUMNS = 'id,org_id,uploaded_by,original_filename,storage_key,media_type,size_bytes,sha256_hex,status,failure_code,extracted_chars,created_at,updated_at'
OLD_T = f'SELECT {TICKET_COLUMNS} FROM tickets WHERE org_id=$1 AND deleted_at IS NULL AND ($2::text IS NULL OR status=$2) AND ($3::smallint IS NULL OR priority=$3) ORDER BY created_at DESC,id DESC LIMIT $4 OFFSET $5'
NEW_T = f'SELECT {TICKET_COLUMNS} FROM tickets WHERE org_id=$1 AND deleted_at IS NULL AND priority=$2 ORDER BY created_at DESC,id DESC LIMIT $3 OFFSET $4'
OLD_D = f"SELECT {DOC_COLUMNS} FROM knowledge_documents WHERE org_id=$1 AND deleted_at IS NULL AND ($2='' OR status=$2) ORDER BY created_at DESC,id DESC LIMIT $3 OFFSET $4"
NEW_D = f'SELECT {DOC_COLUMNS} FROM knowledge_documents WHERE org_id=$1 AND deleted_at IS NULL AND status=$2 ORDER BY created_at DESC,id DESC LIMIT $3 OFFSET $4'

SEED = """
INSERT INTO users(email,display_name,password_hash) VALUES('perf@example.test','Synthetic benchmark','test-only');
INSERT INTO organizations(name) VALUES('Perf A'),('Perf B'),('Perf C');
INSERT INTO organization_members(org_id,user_id,role,status) SELECT id,1,'admin','active' FROM organizations;
INSERT INTO tickets(org_id,creator_id,title,body,priority,status,created_at,updated_at,deleted_at)
SELECT CASE WHEN n<=100000 THEN 1 WHEN n<=110000 THEN 2 ELSE 3 END,1,'Synthetic ticket '||n,repeat('Synthetic body. ',20),
CASE WHEN n%101=0 THEN 3 ELSE 2 END,'OPEN',t,t,CASE WHEN n%17=0 THEN t ELSE NULL END
FROM generate_series(1,120000) n CROSS JOIN LATERAL (SELECT '2026-01-01'::timestamptz+(n/3)*interval '1 second' AS t) dt;
INSERT INTO knowledge_documents(org_id,uploaded_by,original_filename,storage_key,media_type,size_bytes,sha256_hex,status,extracted_chars,created_at,updated_at,deleted_at)
SELECT CASE WHEN n<=100000 THEN 1 WHEN n<=110000 THEN 2 ELSE 3 END,1,'synthetic-'||n||'.txt',lpad(to_hex(n),64,'0'),'text/plain',128,repeat('a',64),
CASE WHEN n%1009=0 THEN 'READY' ELSE 'UPLOADED' END,CASE WHEN n%1009=0 THEN 128 ELSE 0 END,t,t,CASE WHEN n%17=0 THEN t ELSE NULL END
FROM generate_series(1,120000) n CROSS JOIN LATERAL (SELECT '2026-01-01'::timestamptz+(n/3)*interval '1 second' AS t) dt;
ANALYZE tickets; ANALYZE knowledge_documents;
"""


def decode_many(raw):
    decoder = json.JSONDecoder()
    values = []
    while raw.strip():
        raw = raw.lstrip()
        val, end = decoder.raw_decode(raw)
        values.append(val)
        raw = raw[end:]
    return values


def measure(lab, query, params, mode):
    statement = 'EXECUTE bench('+params+')'
    # Two warmups then nine measurements. No forced index or disabled scan method.
    sql = 'SET plan_cache_mode='+mode+'; PREPARE bench AS '+query+';'
    sql += '\n'.join('EXPLAIN (ANALYZE,BUFFERS,FORMAT JSON) '+statement+';' for _ in range(11))
    plans = [x[0] for x in decode_many(lab.sql(sql))]
    return {'mode':mode, 'parameters':params, 'sql':query, 'warmups':2,
            'execution_ms_median':statistics.median(x['Execution Time'] for x in plans[2:]),
            'planning_ms_median':statistics.median(x['Planning Time'] for x in plans[2:]),
            'plans':plans[2:]}


def ids(lab, query, params):
    return lab.sql('PREPARE bench AS '+query+'; EXECUTE bench('+params+');')


def main():
    out = ROOT/'docs/testing/evidence/week09'
    out.mkdir(parents=True, exist_ok=True)
    evidence = {'status':'FAIL','synthetic_only':True,'rows_per_table':120000,
                'org_rows':[100000,10000,10000],'runs':9,'warmups':2,'daily_database_modified':False}
    with Lab() as lab:
        lab.migrate('up-to','12')
        lab.sql(SEED)
        evidence['postgres'] = lab.sql('SELECT version();').strip()
        evidence['before'] = {}
        before_results = {}
        for mode in ('auto','force_generic_plan'):
            evidence['before'][mode] = {'tickets':measure(lab,OLD_T,'1,NULL,3,20,0',mode),
                                        'documents':measure(lab,OLD_D,"1,'READY',20,0",mode)}
        for page in range(3):
            before_results[('t',page)] = ids(lab,OLD_T,f'1,NULL,3,20,{page*20}')
            before_results[('d',page)] = ids(lab,OLD_D,f"1,'READY',20,{page*20}")
        evidence['before_saved_before_migration'] = True
        (out/'sql-plans.json').write_text(json.dumps(evidence,indent=2)+'\n')
        lab.migrate('up')
        lab.sql('ANALYZE tickets; ANALYZE knowledge_documents;')
        evidence['after'] = {}
        for mode in ('auto','force_generic_plan'):
            evidence['after'][mode] = {'tickets':measure(lab,NEW_T,'1,3,20,0',mode),
                                       'documents':measure(lab,NEW_D,"1,'READY',20,0",mode)}
        all_ids = {'t':[], 'd':[]}
        for page in range(3):
            for domain,query,params in [('t',NEW_T,f'1,3,20,{page*20}'),('d',NEW_D,f"1,'READY',20,{page*20}")]:
                actual = ids(lab,query,params)
                assert actual == before_results[(domain,page)], 'query result changed'
                all_ids[domain].extend(line.split('|')[0] for line in actual.splitlines())
        assert all(len(v)==len(set(v))==60 for v in all_ids.values()), 'pagination duplicate or missing row'
        evidence['three_pages_full_rows_equal_and_unique'] = True
        assert all(p['Plan']['Plans'][0].get('Rows Removed by Filter',0)==0
                   for p in evidence['after']['auto']['tickets']['plans']), 'priority query scans unrelated priorities'
        evidence['deep_offset'] = measure(lab,f'SELECT {TICKET_COLUMNS} FROM tickets WHERE org_id=$1 AND deleted_at IS NULL ORDER BY created_at DESC,id DESC LIMIT $2 OFFSET $3','1,20,80000','auto')
        evidence['indexes_bytes'] = json.loads(lab.sql("SELECT json_object_agg(indexrelname,pg_relation_size(indexrelid)) FROM pg_stat_user_indexes WHERE relname IN ('tickets','knowledge_documents');"))
        evidence['status']='PASS'
    (out/'sql-plans.json').write_text(json.dumps(evidence,indent=2)+'\n')
    for mode in ('auto','force_generic_plan'):
        for domain in ('tickets','documents'):
            print(mode,domain,evidence['before'][mode][domain]['execution_ms_median'],'->',evidence['after'][mode][domain]['execution_ms_median'],'ms')
    print('PASS SQL equivalence and stable pagination; owned resources removed')


if __name__ == '__main__':
    main()
