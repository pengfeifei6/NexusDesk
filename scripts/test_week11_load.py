import unittest
from week11_load import metric_delta, reset_sql, workload

class LoadTests(unittest.TestCase):
    def test_metrics_delta_retains_allocation_counts_and_rejects_restart(self):
        self.assertEqual(metric_delta('runtime_alloc_bytes_total 12\nruntime_alloc_objects_total 3\n', 'runtime_alloc_bytes_total 22\nruntime_alloc_objects_total 9\n'), {'runtime_alloc_bytes_total': 10, 'runtime_alloc_objects_total': 6})
        with self.assertRaises(ValueError):
            metric_delta('runtime_alloc_bytes_total 20\n', 'runtime_alloc_bytes_total 1\n')

    def test_reset_rejects_unowned_project(self):
        with self.assertRaises(ValueError):
            reset_sql('daily')

    def test_workload_does_not_serialize_token_or_vary_body(self):
        paths = workload(1)
        self.assertEqual(len(paths), 4)
        self.assertEqual(paths['ticket_list']['path'], '/api/v1/organizations/1/tickets?page=1&page_size=100')
        self.assertEqual(paths['ticket_create']['method'], 'POST')
        self.assertNotIn('token', str(paths))


class FrozenBaselineTests(unittest.TestCase):
    def test_overlay_requires_frozen_matching_sources(self):
        import hashlib
        import tempfile
        from pathlib import Path
        from week11_load import frozen_overlay
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'cache.go').write_text('current')
            (root / 'other.go').write_text('unchanged')
            snapshot = root / 'old.txt'
            snapshot.write_text('baseline')
            manifest = {'cache.go': hashlib.sha256(b'baseline').hexdigest(),
                        'other.go': hashlib.sha256(b'unchanged').hexdigest()}
            self.assertEqual(frozen_overlay(root, manifest, snapshot, 'cache.go'),
                             {'Replace': {str(root / 'cache.go'): str(snapshot)}})
            (root / 'other.go').write_text('changed')
            with self.assertRaises(ValueError):
                frozen_overlay(root, manifest, snapshot, 'cache.go')
            (root / 'other.go').write_text('unchanged')
            snapshot.unlink()
            with self.assertRaises(ValueError):
                frozen_overlay(root, manifest, snapshot, 'cache.go')

if __name__ == '__main__':
    unittest.main()
