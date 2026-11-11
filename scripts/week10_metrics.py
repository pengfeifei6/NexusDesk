"""Metrics over frozen evidence spans; no model calls or private configuration."""
import hashlib
import json
import math

PRICE_SNAPSHOT = {
    'source': 'https://api-docs.deepseek.com/quick_start/pricing/',
    'checked_on': '2026-11-11', 'currency': 'USD', 'unit': 'per million tokens',
    'deepseek-flash': {'version': 'DeepSeek-V4.1-Flash-1017', 'offpeak': [.003, .15, .6], 'peak': [.006, .3, 1.2]},
    'deepseek-v4-pro': {'version': 'DeepSeek-V4-Pro-0813', 'offpeak': [.022, .66, 1.98], 'peak': [.044, 1.32, 3.96]},
    'note': 'Rates ordered cache hit, cache miss, output. Range spans off-peak/peak; not a provider invoice. Frozen historical rates, verify before future use.'
}


def load_corpus(directory):
    manifest = json.loads((directory / 'manifest.json').read_text())
    raw = (directory / 'rag-eval.jsonl').read_bytes()
    assert hashlib.sha256(raw).hexdigest() == manifest['label_sha256'], 'frozen labels changed'
    labels = [json.loads(line) for line in raw.splitlines()]
    assert len(labels) == manifest['question_count'] == 60
    assert len({x['id'] for x in labels}) == len(labels)
    texts = {}
    slots = {}
    for doc in manifest['documents']:
        data = (directory / doc['file']).read_bytes()
        assert hashlib.sha256(data).hexdigest() == doc['sha256'], 'corpus hash mismatch'
        texts[doc['file']] = data.decode('utf-8')
        slots[doc['file']] = doc['organization_slot']
    for label in labels:
        assert bool(label['expected_evidence']) == label['expected_answerable']
        for e in label['expected_evidence']:
            assert slots[e['document']] == label['organization_slot']
            assert e['start_rune'] >= 0 and e['end_rune'] > e['start_rune']
            assert texts[e['document']][e['start_rune']:e['end_rune']] == e['quote']
    assert set(manifest['ask_ids']) <= {x['id'] for x in labels}
    return manifest, labels, texts


def retrieval_metrics(ranks, k):
    if not ranks:
        return {'recall': None, 'reciprocal_rank': None, 'hit': None}
    found = [r for r in ranks if r is not None and r <= k]
    return {'recall': len(found) / len(ranks), 'reciprocal_rank': 1 / min(found) if found else 0, 'hit': bool(found)}


def covers(source, expected):
    return source['document_id'] == expected['document_id'] and source['start_rune'] <= expected['start_rune'] and source['end_rune'] >= expected['end_rune']


def citation_metrics(sources, expected):
    return {'precision': sum(any(covers(s, e) for e in expected) for s in sources) / len(sources) if sources else None,
            'recall': sum(any(covers(s, e) for s in sources) for e in expected) / len(expected) if expected else None}


def cost_range(usage, model):
    if not usage.get('known') or model not in PRICE_SNAPSHOT:
        return None
    p, c, t = [usage.get(k) for k in ('prompt_tokens', 'completion_tokens', 'total_tokens')]
    if any(type(x) is not int or x < 0 for x in (p, c, t)) or p + c != t:
        return None
    hit, miss = usage.get('cache_hit_tokens'), usage.get('cache_miss_tokens')
    exact_cache = all(type(x) is int and x >= 0 for x in (hit, miss)) and hit + miss == p
    low, high = PRICE_SNAPSHOT[model]['offpeak'], PRICE_SNAPSHOT[model]['peak']
    return {'usd_min': ((hit * low[0] + miss * low[1]) if exact_cache else p * low[0]) / 1e6 + c * low[2] / 1e6,
            'usd_max': ((hit * high[0] + miss * high[1]) if exact_cache else p * high[1]) / 1e6 + c * high[2] / 1e6,
            'cache_split_known': exact_cache, 'basis': 'historical price range, not billed amount'}


def mean(values):
    values = [v for v in values if v is not None]
    return sum(values) / len(values) if values else None


def percentile(values, p):
    return sorted(values)[max(0, math.ceil(len(values) * p) - 1)] if values else None


def classify_failures(rows, asks):
    """Mechanical failure categories, not semantic judging or relabeling."""
    cases = []
    for stage, records in [('search', rows), ('ask', asks)]:
        for row in records:
            category = None
            if 'error' in row:
                code = row['error'].get('code', '')
                category = 'model_error' if code.startswith(('MODEL_', 'EMBEDDING_')) else 'request_error'
            elif stage == 'search' and row['expected_answerable'] and row['at_5']['recall'] < 1:
                category = 'retrieval_miss'
            elif stage == 'ask':
                if row['expected_answerable'] and row['insufficient_evidence']:
                    category = 'unexpected_refusal'
                elif not row['expected_answerable'] and not row['insufficient_evidence']:
                    category = 'negative_answered'
                elif row['expected_answerable'] and any(row['citation_metrics'].get(k) != 1 for k in ('precision', 'recall')):
                    category = 'citation_span_incomplete'
            if category:
                cases.append({'id': row['id'], 'mode': row['mode'], 'stage': stage, 'category': category,
                              'error': row.get('error'), 'semantic_judgment': 'not evaluated; human review pending'})
    return cases


def summarize(rows, asks):
    positives = [r for r in rows if r['expected_answerable']]
    negatives = [r for r in rows if not r['expected_answerable']]
    measured = [r for r in rows if 'error' not in r]
    good_asks = [a for a in asks if 'error' not in a]
    negative_asks = [a for a in asks if not a['expected_answerable']]
    result = {'queries': len(rows), 'answerable': len(positives), 'negative': len(negatives),
              'search_errors': len(rows) - len(measured), 'ask_errors': len(asks) - len(good_asks)}
    # Errors remain in denominators: failing queries are zero retrieval hits, not silently dropped.
    for k in (1, 3, 5):
        result['recall_at_' + str(k)] = mean([r.get('at_' + str(k), {}).get('recall', 0) for r in positives])
        result['hit_at_' + str(k)] = mean([int(r.get('at_' + str(k), {}).get('hit', False)) for r in positives])
    result['mrr_at_5'] = mean([r.get('at_5', {}).get('reciprocal_rank', 0) for r in positives])
    result['negative_refusal_rate'] = mean([int(a.get('insufficient_evidence', False)) for a in negative_asks])
    result['negative_empty_search_rate'] = mean([int('error' not in r and not r['items']) for r in negatives])
    result['positive_ask_refusal_rate'] = mean([int(a.get('insufficient_evidence', False)) for a in asks if a['expected_answerable']])
    result['citation_label_precision'] = mean([a['citation_metrics']['precision'] for a in good_asks if a['expected_answerable']])
    result['citation_label_recall'] = mean([a['citation_metrics']['recall'] for a in good_asks if a['expected_answerable']])
    for field in ('embedding_ms', 'search_ms', 'duration_ms'):
        for p in (.5, .95):
            result[field + '_p' + str(int(p * 100))] = percentile([r[field] for r in measured], p)
    for p in (.5, .95):
        result['ask_ms_p' + str(int(p * 100))] = percentile([r['duration_ms'] for r in good_asks], p)
    costs = [a['cost_estimate'] for a in good_asks if a['cost_estimate'] is not None]
    result['cost_unknown_calls'] = len(asks) - len(costs)
    result['known_usage_cost_usd_range'] = [sum(c['usd_min'] for c in costs), sum(c['usd_max'] for c in costs)]
    for field in ('prompt_tokens', 'completion_tokens', 'total_tokens'):
        result['known_' + field] = sum(a['usage'][field] for a in good_asks if a['usage']['known'])
    result['model_calls'] = sum(a.get('usage', {}).get('model_called', False) for a in good_asks)
    return result
