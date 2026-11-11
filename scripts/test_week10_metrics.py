import unittest
from week10_metrics import retrieval_metrics, citation_metrics, cost_range, load_corpus, classify_failures
from dev import ROOT


class EvaluationMetricsTest(unittest.TestCase):
    def test_recall_uses_all_relevant_evidence_and_mrr_first_rank(self):
        result = retrieval_metrics([2, None, 5], 3)
        self.assertAlmostEqual(result['recall'], 1 / 3)
        self.assertEqual(result['reciprocal_rank'], .5)
        self.assertTrue(result['hit'])

    def test_negative_query_is_not_a_zero_recall_positive(self):
        self.assertEqual(retrieval_metrics([], 5), {'recall': None, 'reciprocal_rank': None, 'hit': None})

    def test_citations_distinguish_legality_from_label_relevance(self):
        sources = [{'document_id': 1, 'start_rune': 0, 'end_rune': 50}, {'document_id': 2, 'start_rune': 0, 'end_rune': 20}]
        expected = [{'document_id': 1, 'start_rune': 10, 'end_rune': 20}, {'document_id': 3, 'start_rune': 0, 'end_rune': 5}]
        self.assertEqual(citation_metrics(sources, expected), {'precision': .5, 'recall': .5})

    def test_no_citations_and_negative_labels_are_undefined(self):
        self.assertEqual(citation_metrics([], []), {'precision': None, 'recall': None})

    def test_unknown_usage_never_becomes_zero_cost(self):
        self.assertIsNone(cost_range({'known': False, 'model_called': True}, 'deepseek-flash'))
        self.assertIsNone(cost_range({'known': True, 'prompt_tokens': -1}, 'deepseek-flash'))

    def test_known_usage_cost_bounds_include_peak_offpeak(self):
        result = cost_range({'known': True, 'model_called': True, 'prompt_tokens': 1000, 'completion_tokens': 100, 'total_tokens': 1100, 'cache_hit_tokens': 800, 'cache_miss_tokens': 200}, 'deepseek-flash')
        self.assertAlmostEqual(result['usd_min'], .0000924)
        self.assertAlmostEqual(result['usd_max'], .0001848)

    def test_missing_cache_counts_has_conservative_bound(self):
        result = cost_range({'known': True, 'model_called': True, 'prompt_tokens': 1000, 'completion_tokens': 100, 'total_tokens': 1100}, 'deepseek-flash')
        self.assertAlmostEqual(result['usd_min'], .000063)
        self.assertAlmostEqual(result['usd_max'], .00042)

    def test_frozen_labels_and_evidence_are_consistent(self):
        manifest, labels, texts = load_corpus(ROOT / 'requests/testdata/rag-week10')
        self.assertEqual(len(labels), 60)
        self.assertEqual(len(texts), 8)
        self.assertEqual(len(set(x['id'] for x in labels)), 60)
        self.assertEqual(len(manifest['ask_ids']), 16)

    def test_failure_classification_keeps_missing_evidence_separate_from_model_errors(self):
        rows = [{'id': 'M1', 'mode': 'vector', 'expected_answerable': True, 'at_5': {'recall': .5}}]
        asks = [
            {'id': 'N1', 'mode': 'hybrid', 'expected_answerable': False, 'insufficient_evidence': False},
            {'id': 'M1', 'mode': 'vector', 'expected_answerable': True, 'insufficient_evidence': False, 'citation_metrics': {'precision': .5, 'recall': .5}},
            {'id': 'F1', 'mode': 'hybrid', 'expected_answerable': True, 'error': {'code': 'MODEL_TIMEOUT', 'http_status': 504}},
        ]
        categories = [c['category'] for c in classify_failures(rows, asks)]
        self.assertEqual(categories, ['retrieval_miss', 'negative_answered', 'citation_span_incomplete', 'model_error'])


if __name__ == '__main__':
    unittest.main()
