import hashlib
import json
import shutil
from pathlib import Path
import tarfile
import tempfile
import unittest
from dev import ROOT
from package_release import build
from week10_metrics import load_corpus


class PackageTest(unittest.TestCase):
    def setUp(self):
        directory = ROOT / '.local/week12-unit'; directory.mkdir(parents=True, exist_ok=True)
        self.tmp = tempfile.TemporaryDirectory(dir=directory); self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        for name, content in {'README.md': '# example', 'go.mod': 'module example', 'go.sum': '', '.env.example': 'DB_PASSWORD=REPLACE_WITH_LOCAL_PASSWORD\n', 'cmd/api/main.go': 'package main\nfunc main() {}', 'web/package.json': '{}', 'web/package-lock.json': '{}', 'web/src/main.ts': 'export {}', 'migrations/00001.sql': 'SELECT 1;', 'docs/release/quickstart.md': '# Start', '.env': 'private', 'var/storage/private.md': 'private', '.local/backup.sql': 'private', 'web/node_modules/pkg/key.js': 'private', 'web/dist/index.html': 'generated', 'docs/testing/evidence/week11/private.json': 'private'}.items():
            p = self.root/name; p.parent.mkdir(parents=True, exist_ok=True); p.write_text(content)
        self.output = self.root/'.local/releases/nexusdesk-v1.0.0-source.tar.gz'

    def test_archive_contains_uncommitted_source_and_hashes_but_no_private_or_generated_files(self):
        manifest = build(self.root, self.output, 'v1.0.0')
        with tarfile.open(self.output) as archive:
            names = archive.getnames()
            self.assertIn('NexusDesk-v1.0.0/cmd/api/main.go', names)
            self.assertIn('NexusDesk-v1.0.0/.env.example', names)
            self.assertIn('NexusDesk-v1.0.0/RELEASE-MANIFEST.json', names)
            self.assertFalse(any('/.env' == n[-5:] or any(s in n for s in ['/var/', '/.local/', '/node_modules/', '/dist/', '/testing/evidence/']) for n in names))
            for item in manifest['files']:
                data = archive.extractfile('NexusDesk-v1.0.0/' + item['path']).read()
                self.assertEqual(hashlib.sha256(data).hexdigest(), item['sha256'])
        before = self.output.read_bytes()
        with self.assertRaises(FileExistsError): build(self.root, self.output, 'v1.0.0')
        self.assertEqual(self.output.read_bytes(), before)
        self.assertEqual(json.loads(Path(str(self.output)+'.manifest.json').read_text()), manifest)
        self.assertIn(hashlib.sha256(before).hexdigest(), Path(str(self.output)+'.sha256').read_text())

    def test_archive_preserves_explicit_web_format_configuration_without_following_links(self):
        for name, text in [('.prettierrc.json', '{"semi":false}'), ('.editorconfig', 'indent_size = 2')]:
            (self.root/'web'/name).write_text(text)
        (self.root/'web/.env.production').write_text('must-not-copy')
        manifest = build(self.root, self.output, 'v1.0.0')
        paths = {f['path'] for f in manifest['files']}
        self.assertIn('web/.prettierrc.json', paths)
        self.assertIn('web/.editorconfig', paths)
        self.assertNotIn('web/.env.production', paths)
        config = self.root/'web/.prettierrc.json'; config.unlink(); config.symlink_to(self.root/'.env')
        with self.assertRaises(ValueError): build(self.root, self.output.parent/'symlink.tar.gz', 'v1.0.1')

    def test_extracted_archive_keeps_the_frozen_corpus_needed_by_unit_checks(self):
        source = ROOT/'requests/testdata/rag-week10'
        target = self.root/'requests/testdata/rag-week10'
        shutil.copytree(source, target)
        build(self.root, self.output, 'v1.0.0')
        extracted = self.root/'extracted'
        with tarfile.open(self.output) as archive:
            for item in archive.getmembers():
                self.assertTrue(item.isfile())
                destination = extracted/item.name
                destination.parent.mkdir(parents=True, exist_ok=True)
                destination.write_bytes(archive.extractfile(item).read())
        _, labels, texts = load_corpus(extracted/'NexusDesk-v1.0.0/requests/testdata/rag-week10')
        self.assertEqual(len(labels), 60)
        self.assertEqual(len(texts), 8)

    def test_symlink_sources_and_outside_outputs_are_rejected(self):
        (self.root/'cmd/api/link.go').symlink_to(self.root/'.env')
        with self.assertRaises(ValueError): build(self.root, self.output, 'v1.0.0')
        self.assertFalse(self.output.exists())
        (self.root/'cmd/api/link.go').unlink()
        with self.assertRaises(ValueError): build(self.root, self.root/'outside.tar.gz', 'v1.0.0')
        with self.assertRaises(ValueError): build(self.root, self.output, '../bad')

    def test_manual_directory_symlink_cannot_escape_source_allowlist(self):
        outside = self.root / '.local/private-notes/每周开发手册'
        outside.mkdir(parents=True)
        (outside / '15_NexusDesk_第十二周详细开发手册.md').write_text('must-not-copy')
        (self.root / '项目开发文件').symlink_to(outside.parent, target_is_directory=True)
        with self.assertRaises(ValueError): build(self.root, self.output, 'v1.0.0')
        self.assertFalse(self.output.exists())

    def test_secret_detection_fails_closed_without_copying_the_value(self):
        secret = 'sk-' + 'a1' * 16
        (self.root/'cmd/api/main.go').write_text(secret)
        with self.assertRaises(ValueError) as error: build(self.root, self.output, 'v1.0.0')
        self.assertNotIn(secret, str(error.exception)); self.assertFalse(self.output.exists())


if __name__ == '__main__': unittest.main()
