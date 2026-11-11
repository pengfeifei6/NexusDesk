#!/usr/bin/env python3
"""Finite local-only API experiment; baseline must finish before cache edits."""
import argparse
from concurrent.futures import ThreadPoolExecutor
import hashlib
import json
import os
from pathlib import Path
import platform
import subprocess
import time

from dev import ROOT
from week11_runtime import Week11Lab, RUNS, OUT, application, fetch, validate_fixture
from week11_load import metric_delta, reset_sql, workload, frozen_overlay

SEED = '''INSERT INTO tickets(org_id,creator_id,title,body,priority,status,created_at,updated_at)
SELECT CASE WHEN n<=10000 THEN 1 ELSE 2 END,CASE WHEN n<=10000 THEN 1 ELSE 2 END,
'Synthetic ticket '||n,repeat('Synthetic body. ',20),2,'OPEN',
'2026-01-01'::timestamptz+n*interval '1 second','2026-01-01'::timestamptz+n*interval '1 second'
FROM generate_series(1,20000) n;
INSERT INTO knowledge_documents(org_id,uploaded_by,original_filename,storage_key,media_type,size_bytes,sha256_hex,status,created_at,updated_at)
SELECT CASE WHEN n<=10000 THEN 1 ELSE 2 END,CASE WHEN n<=10000 THEN 1 ELSE 2 END,
'synthetic-'||n||'.txt',lpad(to_hex(n),64,'0'),'text/plain',128,repeat('a',64),'UPLOADED',
'2026-01-01'::timestamptz+n*interval '1 second','2026-01-01'::timestamptz+n*interval '1 second'
FROM generate_series(1,20000) n;
ANALYZE tickets; ANALYZE knowledge_documents;'''


def save(path, value):
    path.write_text(json.dumps(value, indent=2) + '\n')


def counts(lab):
    return json.loads(lab.sql("SELECT json_build_object('tickets',(SELECT count(*) FROM tickets),'documents',(SELECT count(*) FROM knowledge_documents),'events',(SELECT count(*) FROM ticket_events),'org_rows',(SELECT json_agg(c) FROM (SELECT org_id,count(*) FROM tickets GROUP BY org_id ORDER BY org_id)c));"))


def clean(lab):
    validate_fixture(lab)
    lab.sql(reset_sql(lab.project))
    # Exact lab has its own Redis container and DB; never addresses daily Redis.
    lab.run(lab.compose + ['exec', '-T', 'redis', 'redis-cli', '-n', '6', 'FLUSHDB'])
    assert counts(lab)['tickets'] == counts(lab)['documents'] == 20000


def client(binary, lab, app, token, scenario, concurrency, seconds):
    cfg = dict(project=lab.project, environment='test', origin=app.api.origin,
               token=token, concurrency=concurrency, seconds=seconds,
               timeout_seconds=3, max_bytes=1048576, **scenario)
    result = subprocess.run([str(binary)], cwd=ROOT, env=lab.env,
                            input=json.dumps(cfg).encode(), stdout=subprocess.PIPE,
                            stderr=subprocess.PIPE, timeout=seconds+10)
    if result.returncode:
        raise RuntimeError('bounded generator failed')
    return json.loads(result.stdout)


def warm(app, token, scenario):
    for _ in range(20):
        status, _, _ = app.api.raw(scenario['path'], scenario['method'],
                                 json.loads(scenario['body']) if 'body' in scenario else None, token)
        if status not in (200, 201):
            raise RuntimeError('warmup failed: HTTP ' + str(status))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--stage', choices=['baseline', 'after'], required=True)
    parser.add_argument('--replay-label', help='new retained experiment label, e.g. replay-1')
    args = parser.parse_args()
    stage = args.stage
    import re
    if args.replay_label and not re.fullmatch(r'[a-z0-9][a-z0-9-]{0,39}', args.replay_label):
        raise ValueError('invalid replay label')
    prefix = 'performance-' + (args.replay_label + '-' if args.replay_label else '') + stage
    summary_path = OUT / ('performance-' + (args.replay_label + '-' if args.replay_label else '') + 'summary.json')
    OUT.mkdir(parents=True, exist_ok=True)
    directory = RUNS / prefix
    directory.mkdir(parents=True, exist_ok=True)
    evidence_path = OUT / (prefix + '.json')
    if evidence_path.exists():
        raise ValueError('retained stage evidence already exists; do not silently overwrite runs')
    source = ROOT / 'internal/platform/cache/cache.go'
    save(summary_path, dict(status='INCOMPLETE', active_stage=stage))
    manifest_path = OUT / 'performance-baseline-build-manifest.json'
    if not manifest_path.is_file():
        raise ValueError('frozen baseline manifest missing; restore retained W11 evidence before replay')
    manifest = json.loads(manifest_path.read_text())['sources']
    paths = [ROOT/'go.mod', ROOT/'go.sum'] + sorted((ROOT/'cmd/api').rglob('*.go')) + sorted((ROOT/'internal').rglob('*.go'))
    if {str(p.relative_to(ROOT)) for p in paths if not p.name.endswith('_test.go')} != set(manifest):
        raise ValueError('product source set differs from frozen baseline')
    snapshot = OUT / 'performance-baseline-cache.go.txt'
    overlay = frozen_overlay(ROOT, manifest, snapshot)
    source_copy = OUT / (prefix + '-cache.go.txt')
    if stage == 'baseline':
        source_bytes = snapshot.read_bytes()
    else:
        paired = OUT / (prefix[:-len('after')] + 'baseline.json')
        if not paired.is_file() or json.loads(paired.read_text())['status'] != 'PASS':
            raise ValueError('complete paired baseline required before after stage')
        canonical_after = OUT/'performance-reviewed-after-cache.go.txt'
        if not canonical_after.is_file() or source.read_bytes() != canonical_after.read_bytes():
            raise ValueError('current optimized source differs from frozen after snapshot')
        source_bytes = source.read_bytes()
    source_copy.write_bytes(source_bytes)
    frozen_sources = directory / "frozen-source"
    frozen_cache = frozen_sources / "internal/platform/cache/cache.go"
    frozen_cache.parent.mkdir(parents=True, exist_ok=True)
    frozen_cache.write_bytes(source_bytes)
    api_binary = directory / 'api'
    generator = directory / 'loadtest'
    build = ['go', 'build']
    if stage == 'baseline':
        overlay_path = directory / 'baseline-overlay.json'
        save(overlay_path, overlay)
        build += ['-overlay', str(overlay_path)]
    subprocess.run(build + ['-o', str(api_binary), './cmd/api'], cwd=ROOT, check=True)
    subprocess.run(['go', 'build', '-o', str(generator), './cmd/loadtest'], cwd=ROOT, check=True)
    evidence = dict(status='RUNNING', stage=stage, started_utc=time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()),
                    source_sha256=hashlib.sha256(source_bytes).hexdigest(),
                    source_manifest=str(manifest_path.relative_to(ROOT)),
                    rebuild='frozen cache via Go overlay' if stage == 'baseline' else 'verified frozen optimized cache',
                    binary_sha256=hashlib.sha256(api_binary.read_bytes()).hexdigest(),
                    retained_binary=str(api_binary.relative_to(ROOT)),
                    machine=dict(platform=platform.platform(), cpus=os.cpu_count(),
                                 cpu=next((line.split(':',1)[1].strip() for line in Path('/proc/cpuinfo').read_text().splitlines() if line.startswith('model name')), 'unknown'),
                                 memory=Path('/proc/meminfo').read_text().splitlines()[0],
                                 go=subprocess.check_output(['go', 'version'], text=True).strip(),
                                 loadavg=os.getloadavg()),
                    method=dict(mode='Go standard-library closed-loop HTTP/1.1 keepalive, one transport per worker',
                                concurrency=[1,8,16], seconds=3, rounds=2, warmup_requests=20,
                                cache='enabled; flushed before each case; warm before reads',
                                observability='enabled identically; no CPU/trace during latency matrix',
                                response_bound_bytes=1048576, request_timeout_seconds=3,
                                initial_rows_per_table=20000, org_rows=[10000,10000],
                                create_reset='remove all synthetic id>20000 and their events, restart IDs, reset generations, VACUUM ANALYZE and flush owned Redis before every case'),
                    runs=[], profiles={})
    save(evidence_path, evidence)
    try:
        with Week11Lab() as lab:
            lab.migrate('up')
            evidence['postgres'] = lab.sql('SELECT version();').strip()
            evidence['redis'] = lab.run(lab.compose + ['exec','-T','redis','redis-server','--version']).strip()
            setup = directory/'setup'; setup.mkdir(exist_ok=True)
            with application(lab, setup, api_binary=api_binary) as app:
                token, org, _ = app.api.account('perf-a')
                app.api.account('perf-b')
                assert org == 1
            lab.sql(SEED)
            evidence['seed_sha256'] = hashlib.sha256(SEED.encode()).hexdigest()
            evidence['initial_counts'] = counts(lab)
            scenarios = workload(org)
            evidence['scenarios'] = scenarios
            for round_number in (1,2):
                run_dir = directory/('round-'+str(round_number));run_dir.mkdir(exist_ok=True)
                with application(lab, run_dir, api_binary=api_binary) as app:
                    for name, scenario in scenarios.items():
                        for concurrency in (1,8,16):
                            clean(lab)
                            warm(app, token, scenario)
                            if name == 'ticket_create':
                                clean(lab)  # writes begin at exactly 20,000 after auth/pool warmup
                            start_counts = counts(lab)
                            before = fetch(app.diagnostic_url+'/metrics').decode()
                            measured = client(generator, lab, app, token, scenario, concurrency, 3)
                            after = fetch(app.diagnostic_url+'/metrics').decode()
                            row = dict(round=round_number, scenario=name, concurrency=concurrency,
                                       profiled=False, start_counts=start_counts, end_counts=counts(lab),
                                       metrics_before=before, metrics_after=after,
                                       metric_delta=metric_delta(before,after), **measured)
                            evidence['runs'].append(row)
                            save(evidence_path,evidence)
                            print(stage, round_number,name,concurrency,measured['requests'],measured['failures'],flush=True)
            clean(lab)
            profile_dir = directory/'profile';profile_dir.mkdir(exist_ok=True)
            with application(lab,profile_dir,api_binary=api_binary) as app:
                warm(app,token,scenarios['ticket_list'])
                for kind in ('heap','allocs'):
                    (OUT/(prefix+'-'+kind+'-before.pprof')).write_bytes(fetch(app.diagnostic_url+'/debug/pprof/'+kind))
                with ThreadPoolExecutor(max_workers=1) as pool:
                    pending=pool.submit(client,generator,lab,app,token,scenarios['ticket_list'],8,12)
                    time.sleep(.3)
                    for kind,seconds in [('profile',5),('trace',1)]:
                        (OUT/(prefix+'-'+kind+'.'+('trace' if kind=='trace' else 'pprof'))).write_bytes(fetch(app.diagnostic_url+'/debug/pprof/'+kind+'?seconds='+str(seconds)))
                    evidence['profiles']['business_load']=pending.result()
                for kind in ('heap','allocs'):
                    (OUT/(prefix+'-'+kind+'-after.pprof')).write_bytes(fetch(app.diagnostic_url+'/debug/pprof/'+kind))
                evidence['profiles']['metrics_after']=fetch(app.diagnostic_url+'/metrics').decode()
                for sample in ('alloc_space','alloc_objects'):
                    command=['go','tool','pprof','-top','-nodecount=40','-sample_index='+sample,
                             '-base',str(OUT/(prefix+'-allocs-before.pprof')),str(api_binary),str(OUT/(prefix+'-allocs-after.pprof'))]
                    (OUT/(prefix+'-'+sample+'-top.txt')).write_text(subprocess.check_output(command,cwd=ROOT,text=True))
                for kind in ('profile','heap-after'):
                    (OUT/(prefix+'-'+kind+'-top.txt')).write_text(subprocess.check_output(['go','tool','pprof','-top','-nodecount=50',str(api_binary),str(OUT/(prefix+'-'+kind+'.pprof'))],cwd=ROOT,text=True))
                for function in ('cache.*get','encoding/json.Unmarshal'):
                    label='cache-get' if function.startswith('cache') else 'json-unmarshal'
                    command=['go','tool','pprof','-source_path='+str(frozen_sources),'-trim_path='+str(ROOT),'-list='+function,str(api_binary),str(OUT/(prefix+'-profile.pprof'))]
                    (OUT/(prefix+'-'+label+'-list.txt')).write_text(subprocess.check_output(command,cwd=ROOT,text=True,stderr=subprocess.STDOUT))
                with (OUT/(prefix+'-trace-sched.pprof')).open('wb') as stream:
                    subprocess.run(['go','tool','trace','-pprof=sched',str(OUT/(prefix+'-trace.trace'))],cwd=ROOT,stdout=stream,check=True)
                evidence['profiles']['parse']='CPU/heap/allocs parsed with go tool pprof; trace parsed with go tool trace -pprof=sched'
                evidence['profiles']['scope']='Separate 12-second concurrency8 warm ticket-list load; 5s CPU and 1s trace sequential, never latency matrix'
            evidence['final_counts'] = counts(lab)
        evidence['status']='PASS' if all(r['failures']==0 for r in evidence['runs']) else 'COMPLETED_WITH_ERRORS'
    except BaseException as error:
        evidence['status']='FAIL'
        evidence['failure_type']=type(error).__name__
        raise
    finally:
        evidence['finished_utc']=time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime())
        save(evidence_path,evidence)
    baseline_path = OUT / (prefix[:-len(stage)] + 'baseline.json')
    save(summary_path, dict(status='PASS' if stage == 'after' and evidence['status'] == 'PASS' and json.loads(baseline_path.read_text())['status'] == 'PASS' else 'INCOMPLETE', baseline=baseline_path.name, latest=evidence_path.name))
    print(stage, evidence['status'],flush=True)


if __name__ == '__main__':
    main()
