import tempfile
import unittest
from pathlib import Path
from dev import ROOT
from week12_runtime import resolve_node


class NodePortabilityTest(unittest.TestCase):
    def test_existing_cache_or_supported_system_node_and_invalid_versions(self):
        base = ROOT / '.local/week12-unit'; base.mkdir(parents=True, exist_ok=True)
        with tempfile.TemporaryDirectory(dir=base) as tmp:
            directory = Path(tmp)
            def executable(name, version):
                path = directory / name
                path.write_text('#!/bin/sh\nprintf "%s\\n" "' + version + '"\n')
                path.chmod(0o700)
                return path
            cached = executable('cached', 'v24.12.0')
            system = executable('system', 'v22.18.0')
            self.assertEqual(resolve_node(cached, system), cached)
            self.assertEqual(resolve_node(directory / 'missing', system), system)
            old = executable('old', 'v18.20.0')
            self.assertEqual(resolve_node(old, system), system)
            for version in ['v22.17.0', 'v23.9.0', 'v24.11.0', 'invalid']:
                invalid = executable('invalid', version)
                with self.subTest(version=version), self.assertRaises(RuntimeError):
                    resolve_node(invalid, directory / 'missing')


if __name__ == '__main__': unittest.main()
