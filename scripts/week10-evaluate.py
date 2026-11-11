#!/usr/bin/env python3
"""--live: real BGE + DeepSeek on frozen synthetic corpus and owned disposable DB."""
import argparse
import hashlib
import json
import math
import platform
import tempfile
import time
import urllib.request
from pathlib import Path
from dev import ROOT, read_env, local_http, warmup
from week10_runtime import OUT, Week10Lab, application, RequestError
from week10_metrics import load_corpus, covers, retrieval_metrics, citation_metrics, cost_range, summarize, classify_failures, PRICE_SNAPSHOT


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--live', action='store_true', required=True, help='allows paid cloud generation using .env key; sends only frozen synthetic corpus')
    parser.parse_args()
    manifest, labels, texts = load_corpus(ROOT / 'requests/testdata/rag-week10')
    values = read_env(ROOT / '.env')
    key, model = values.get('DEEPSEEK_API_KEY', ''), values.get('CHAT_MODEL', 'deepseek-flash')
    if not key or model not in ('deepseek-flash', 'deepseek-v4-pro'):
        raise RuntimeError('configure DeepSeek model and key before explicit live evaluation')
    OUT.mkdir(parents=True, exist_ok=True)
    (ROOT / '.local/week10-runs').mkdir(parents=True, exist_ok=True)
    report = {'status': 'FAIL', 'generated_at_utc': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()),
              'synthetic_data_only': True, 'daily_database_modified': False, 'semantic_mock': False,
              'manifest': manifest, 'model': model, 'model_documented_version': PRICE_SNAPSHOT[model]['version'],
              'price_snapshot': PRICE_SNAPSHOT, 'label_provenance': 'AI-prepared exact source spans; human review pending',
              'embedding_model': 'bge-m3:567m', 'embedding_dimension': 1024, 'min_score': .40, 'top_k': 5,
              'chunker': 'rune-500-80-v1', 'candidate_limit_per_branch': 20, 'rrf_constant': 60, 'lexical_max_terms': 32,
              'machine': {'system': platform.system(), 'machine': platform.machine(), 'python': platform.python_version()},
              'queries': [], 'asks': [], 'indexes': [], 'isolation_checks': [], 'limitations': [
                  'Small synthetic corpus and AI labels awaiting human review; not production accuracy.',
                  'Single sequential interleaved vector/hybrid trial; no significance or concurrency claim.',
                  'Span-based citation relevance is not claim entailment or answer correctness.',
                  'No retries or label changes after observing results. Failed requests stay in denominators.',
                  'Cost range uses frozen historical off-peak/peak prices, not the provider bill; failed request usage unknown.']}
    def save():
        report['failure_cases'] = classify_failures(report['queries'], report['asks'])
        (OUT / 'evaluation.json').write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n')
    try:
        # Local warmup is explicit, outside measured request latency; never sends cloud key.
        warmup({'OLLAMA_BASE_URL': 'http://127.0.0.1:11434'})
        with local_http().open('http://127.0.0.1:11434/api/tags', timeout=5) as r:
            tags = json.load(r)
        report['embedding_model_digest'] = next((m['digest'] for m in tags.get('models', []) if m.get('name') == 'bge-m3:567m'), None)
        with Week10Lab() as lab, tempfile.TemporaryDirectory(prefix='evaluation-', dir=ROOT / '.local/week10-runs') as tmp:
            lab.migrate('up')
            report['database_versions'] = lab.sql("SELECT version(); SELECT extversion FROM pg_extension WHERE extname='vector';").strip().splitlines()
            with application(lab, Path(tmp), {'CHAT_PROVIDER': 'deepseek', 'CHAT_MODEL': model, 'DEEPSEEK_API_KEY': key}) as (api, _, env):
                tokens, orgs, ids = {}, {}, {}
                for slot in ('A', 'B'):
                    tokens[slot], orgs[slot] = api.account(slot)
                for doc in manifest['documents']:
                    slot, name = doc['organization_slot'], doc['file']
                    uploaded = api.upload(orgs[slot], tokens[slot], name, texts[name])
                    ids[name] = uploaded['id']
                    path = f'/organizations/{orgs[slot]}/knowledge/documents/{uploaded["id"]}'
                    assert api.request(path + '/process', 'POST', token=tokens[slot])['status'] == 'READY'
                    info = api.request(path + '/index', 'POST', token=tokens[slot])
                    assert info['status'] == 'READY' and info['chunk_count'] > 0
                    report['indexes'].append({'document': name, **info})
                    print('PASS real BGE index ' + name, flush=True)
                by_id = {v: k for k, v in ids.items()}
                slots = {d['file']: d['organization_slot'] for d in manifest['documents']}
                def validate(items, slot):
                    for source in items:
                        name = by_id[source['document_id']]
                        assert slots[name] == slot, 'cross-organization source leak'
                        assert source['source_name'] == name and source['chunk_id'] > 0
                        assert source['excerpt'] == texts[name][source['start_rune']:source['end_rune']]
                        assert math.isfinite(source['score']) and -1 <= source['score'] <= 1
                for mode in ('vector', 'hybrid'):
                    api.request(f'/organizations/{orgs["B"]}/knowledge/search', 'POST', {'query': '玻璃港', 'retrieval_mode': mode}, tokens['A'], expected=404)
                    report['isolation_checks'].append({'mode': mode, 'nonmember_status': 404})
                for n, label in enumerate(labels):
                    expected = [dict(e, document_id=ids[e['document']]) for e in label['expected_evidence']]
                    modes = ('vector', 'hybrid') if n % 2 == 0 else ('hybrid', 'vector')
                    for mode in modes:
                        path = f'/organizations/{orgs["A"]}/knowledge'
                        payload = {'query': label['query'], 'top_k': 5, 'retrieval_mode': mode}
                        row = {'id': label['id'], 'category': label['category'], 'query': label['query'], 'mode': mode, 'expected_answerable': label['expected_answerable']}
                        start = time.perf_counter()
                        try:
                            result = api.request(path + '/search', 'POST', payload, tokens['A'])
                            validate(result['items'], 'A')
                            ranks = [next((rank for rank, item in enumerate(result['items'], 1) if covers(item, e)), None) for e in expected]
                            row.update(result, evidence_ranks=ranks, duration_ms=round((time.perf_counter() - start) * 1000, 3))
                            for k in (1, 3, 5):
                                row['at_' + str(k)] = retrieval_metrics(ranks, k)
                        except RequestError as error:
                            row['error'] = {'http_status': error.status, 'code': error.code}
                        report['queries'].append(row)
                        if label['id'] in manifest['ask_ids']:
                            ask = {k: row[k] for k in ('id', 'category', 'mode', 'expected_answerable')}
                            start = time.perf_counter()
                            try:
                                result = api.request(path + '/ask', 'POST', payload, tokens['A'])
                                validate(result['sources'], 'A')
                                assert 'ORGB-ONLY-SAMPLE-GLASSHARBOR' not in result['answer'], 'private marker in answer'
                                if result['insufficient_evidence']:
                                    assert result['sources'] == [] and result['answer'] == '知识库未检索到足够依据。'
                                else:
                                    assert result['answer'].strip() and result['sources']
                                ask.update(result, duration_ms=round((time.perf_counter() - start) * 1000, 3),
                                           citation_metrics=citation_metrics(result['sources'], expected),
                                           cost_estimate=cost_range(result['usage'], result['generation_model']))
                            except RequestError as error:
                                ask['error'] = {'http_status': error.status, 'code': error.code}
                            report['asks'].append(ask)
                        print('MEASURE ' + label['id'] + ' ' + mode + (' error=' + row['error']['code'] if 'error' in row else ' recall@5=' + str(row['at_5']['recall'])), flush=True)
                        save()
                report['metrics'] = {m: summarize([r for r in report['queries'] if r['mode'] == m], [a for a in report['asks'] if a['mode'] == m]) for m in ('vector', 'hybrid')}
                report['cross_organization_source_leaks'] = 0
                report['status'] = 'PASS' if all(m['search_errors'] == 0 and m['ask_errors'] == 0 for m in report['metrics'].values()) else 'COMPLETED_WITH_MODEL_ERRORS'
    finally:
        save()
    print('Evaluation complete: ' + report['status'] + '; owned resources removed', flush=True)


if __name__ == '__main__':
    main()
