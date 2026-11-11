#!/usr/bin/env python3
"""Actual dependency/process recovery in an owned lab; no daily service actions."""
import json
import re
import tempfile
import time
import uuid
from datetime import datetime, timezone
from pathlib import Path

from week11_model import LocalModel
from week11_runtime import OUT, RUNS, Week11Lab, application, fetch, wait_for


def stamp():
    return datetime.now(timezone.utc).isoformat()


def counter(raw, name):
    match = re.search(r'^' + re.escape(name) + r' ([0-9.eE+\-]+)$', raw.decode(), re.M)
    assert match, 'expected metric missing: ' + name
    return float(match.group(1))


def correlated_error(status, headers, raw, expected, code=None):
    assert status == expected, 'unexpected error status'
    data = json.loads(raw)['error']
    assert re.fullmatch(r'[a-f0-9]{32}', data['request_id'])
    assert headers.get('X-Request-Id', headers.get('X-Request-ID')) == data['request_id']
    if code:
        assert data['code'] == code
    return {'status': status, 'code': data['code'], 'request_id': data['request_id']}


def main():
    OUT.mkdir(parents=True, exist_ok=True)
    RUNS.mkdir(parents=True, exist_ok=True)
    evidence = {'status': 'INCOMPLETE_OR_FAIL', 'started_at': stamp(), 'synthetic_only': True,
                'daily_services_modified': False, 'cloud_calls': 0, 'model': 'local deterministic fixture', 'drills': []}
    step = None
    try:
        with Week11Lab() as lab, LocalModel() as model, tempfile.TemporaryDirectory(prefix='faults-', dir=RUNS) as tmp:
            lab.migrate('up')
            directory = Path(tmp)
            with application(lab, directory, {'OLLAMA_BASE_URL': model.origin}, worker=True) as app:
                api = app.api
                token, org, user = api.account('faults')
                base = '/organizations/%s' % org
                path = base + '/tickets'
                ticket = api.request(path, 'POST', {'title': 'Recovery invariant', 'body': 'Synthetic fixture only.', 'priority': 2}, token, 201)
                conversation = api.request(base + '/ai/conversations', 'POST', {'title': 'Redis recovery'}, token, 201)
                chat_path = '/api/v1' + base + '/ai/conversations/' + conversation['id'] + '/messages'
                for _ in range(2):
                    assert api.request(path, token=token)['items'][0]['id'] == ticket['id']
                metrics_before = fetch(app.diagnostic_url + '/metrics')
                (OUT / 'faults-metrics-before.txt').write_bytes(metrics_before)
                assert counter(metrics_before, 'nexusdesk_cache_hits_total') >= 1
                failures_before = counter(metrics_before, 'nexusdesk_cache_failures_total')

                step = {'name': 'redis_outage', 'injected_at': stamp()}
                before = lab.sql('SELECT count(*) FROM ai_messages; SELECT count(*) FROM ai_tool_actions;').strip()
                started = time.monotonic()
                lab.service('redis', 'stop')
                try:
                    rows = api.request(path, token=token)['items']
                    assert len(rows) == 1 and rows[0]['id'] == ticket['id']
                    step['list_fallback_status'] = 200
                    step['ai_rejection'] = correlated_error(*api.raw(chat_path, 'POST', {'content': 'Recovery check', 'client_request_id': str(uuid.uuid4())}, token), 503, 'RATE_LIMIT_UNAVAILABLE')
                    assert lab.sql('SELECT count(*) FROM ai_messages; SELECT count(*) FROM ai_tool_actions;').strip() == before
                    assert model.calls['stream'] == 0
                    metrics_down = fetch(app.diagnostic_url + '/metrics')
                    assert counter(metrics_down, 'nexusdesk_cache_failures_total') > failures_before
                    step['cache_failure_metric_increased'] = True
                    step['no_message_action_or_model_call'] = True
                    step['failure_observed_seconds'] = time.monotonic() - started
                finally:
                    lab.service('redis', 'start')
                recovered = time.monotonic()
                wait_for(lambda: lab.run(lab.compose + ['exec', '-T', 'redis', 'redis-cli', 'PING']).strip() == 'PONG', description='owned Redis')
                status, headers, raw = api.raw(chat_path, 'POST', {'content': 'Recovery check', 'client_request_id': str(uuid.uuid4())}, token)
                assert status == 200 and b'event: message_end' in raw, 'AI did not recover'
                assert lab.sql('SELECT count(*) FROM ai_messages;').strip() == '2'
                for _ in range(2):
                    assert api.request(path, token=token)['items'][0]['id'] == ticket['id']
                step.update(recovered_at=stamp(), recovery_seconds=time.monotonic() - recovered, status='PASS')
                evidence['drills'].append(step)
                print('PASS Redis outage: DB fallback, AI safely rejected without writes, then recovery', flush=True)

                step = {'name': 'postgres_outage', 'injected_at': stamp()}
                started = time.monotonic()
                lab.service('db', 'stop')
                try:
                    assert api.raw('/healthz')[0] == 200
                    step['health_status'] = 200
                    step['business_failure'] = correlated_error(*api.raw('/api/v1' + path, token=token), 500)
                    step['no_cached_authorization_bypass'] = True
                    step['failure_observed_seconds'] = time.monotonic() - started
                finally:
                    lab.service('db', 'start')
                recovered = time.monotonic()
                def database_ready():
                    status, _, raw = api.raw('/api/v1' + path, token=token)
                    return status == 200 and json.loads(raw)['data']['items'][0]['id'] == ticket['id']
                wait_for(database_ready, timeout=40, description='original ticket after PostgreSQL restart')
                step.update(recovered_at=stamp(), recovery_seconds=time.monotonic() - recovered, original_record_preserved=True, status='PASS')
                evidence['drills'].append(step)
                print('PASS PostgreSQL outage: liveness distinct from business availability; original data recovered', flush=True)

                step = {'name': 'worker_graceful_restart', 'injected_at': stamp()}
                step['shutdown'] = app.stop_worker()
                assert step['shutdown']['exit_code'] == 0
                doc = api.upload(org, token, 'graceful-recovery.md', '# Graceful restart\n\nSynthetic restart check.')
                job = api.request(base + '/knowledge/documents/%s/jobs' % doc['id'], 'POST', token=token, expected=202)
                assert api.request(base + '/jobs/%s' % job['id'], token=token)['status'] == 'PENDING'
                app.start_worker()
                def finished(job_id):
                    current = api.request(base + '/jobs/%s' % job_id, token=token)
                    if current['status'] in ('FAILED', 'CANCELED'):
                        raise AssertionError('job entered unexpected terminal state: ' + current['status'])
                    return current if current['status'] == 'SUCCEEDED' else None
                final = wait_for(lambda: finished(job['id']), timeout=45, description='pending work after restart')
                step.update(recovered_at=stamp(), job_id=job['id'], final_status=final['status'], attempts=final['total_attempts'], status='PASS')
                evidence['drills'].append(step)
                print('PASS graceful Worker shutdown and persisted pending work recovery', flush=True)

                step = {'name': 'worker_crash_lease_recovery', 'started_at': stamp()}
                model.block_embedding_once()
                doc = api.upload(org, token, 'crash-recovery.md', '# Crash recovery\n\nSynthetic document with one chunk. Recover using the current lease version.')
                job = api.request(base + '/knowledge/documents/%s/jobs' % doc['id'], 'POST', token=token, expected=202)
                assert model.blocked.wait(20), 'Worker did not reach model fixture'
                running = api.request(base + '/jobs/%s' % job['id'], token=token)
                assert running['status'] == 'RUNNING' and running['phase'] == 'INDEXING'
                step.update(injected_at=stamp(), phase_before_crash=running['phase'], status_before_crash=running['status'], attempts_before=running['total_attempts'])
                lease = float(lab.sql('SELECT EXTRACT(EPOCH FROM (lease_until-clock_timestamp())) FROM knowledge_jobs WHERE id=%d;' % job['id']).strip())
                assert 0 < lease <= 30
                step['remaining_lease_seconds_at_kill'] = lease
                killed_at = time.monotonic()
                step['kill'] = app.stop_worker(kill=True)
                assert step['kill']['exit_code'] == -9
                model.release.set()
                # No SQL state/time edits: the new Worker must respect the real lease.
                app.start_worker()
                initial = api.request(base + '/jobs/%s' % job['id'], token=token)
                assert initial['status'] == 'RUNNING' and initial['total_attempts'] == running['total_attempts']
                print('Worker killed during INDEXING; waiting for the actual 30s lease and retry', flush=True)
                final = wait_for(lambda: finished(job['id']), timeout=80, interval=.5, description='real lease expiry recovery')
                elapsed = time.monotonic() - killed_at
                assert elapsed >= lease - 1 and final['total_attempts'] == running['total_attempts'] + 1
                invariant = json.loads(lab.sql('''SELECT json_build_object('index_status',i.status,'chunk_count',i.chunk_count,'actual_chunks',count(c.id),'current_chunks',count(c.id) FILTER(WHERE c.index_version=i.index_version),'distinct_positions',count(DISTINCT c.chunk_index)) FROM knowledge_document_indexes i LEFT JOIN knowledge_chunks c ON c.org_id=i.org_id AND c.document_id=i.document_id WHERE i.document_id=%d GROUP BY i.status,i.chunk_count,i.index_version;''' % doc['id']))
                assert invariant['index_status'] == 'READY'
                assert invariant['chunk_count'] == invariant['actual_chunks'] == invariant['current_chunks'] == invariant['distinct_positions'] == 1
                step.update(recovered_at=stamp(), recovery_seconds=elapsed, job_id=job['id'], final_status=final['status'], attempts_after=final['total_attempts'], index_invariants=invariant, lease_shortened_by_test=False, status='PASS')
                evidence['drills'].append(step)
                (OUT / 'faults-metrics-after.txt').write_bytes(fetch(app.diagnostic_url + '/metrics'))
                (OUT / 'faults-worker-metrics.txt').write_bytes(fetch(app.worker_diagnostic_url + '/metrics'))
                logs = app.safe_logs()
                for name, content in logs.items():
                    assert token not in content
                    (OUT / ('faults-' + name + '.jsonl')).write_text(content)
                # Both observed failures must be locatable by their server-generated IDs.
                for name, key in [('redis_outage', 'ai_rejection'), ('postgres_outage', 'business_failure')]:
                    item = next(x for x in evidence['drills'] if x['name'] == name)
                    assert item[key]['request_id'] in logs['api']
                evidence['model_fixture_calls'] = model.calls
                print('PASS real Worker crash/lease recovery; one current index, correlated logs', flush=True)
            evidence['status'] = 'PASS'
        evidence['owned_resources_removed'] = True
    finally:
        if step is not None and step not in evidence['drills']:
            step['status'] = 'INCOMPLETE_OR_FAIL'
            evidence['drills'].append(step)
        evidence['completed_at'] = stamp()
        (OUT / 'faults.json').write_text(json.dumps(evidence, ensure_ascii=False, indent=2) + '\n')


if __name__ == '__main__':
    main()
