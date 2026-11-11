import base64
import os
from pathlib import Path
import tempfile
import unittest
from urllib.parse import urlsplit, parse_qs, unquote
from dev import ROOT, read_env
from local_setup import init_env, migration_env


class LocalSetupTest(unittest.TestCase):
    def setUp(self):
        directory = ROOT / '.local/week12-unit'; directory.mkdir(parents=True, exist_ok=True)
        self.tmp = tempfile.TemporaryDirectory(dir=directory); self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        (self.root / '.env.example').write_bytes((ROOT / '.env.example').read_bytes())

    def test_init_is_private_random_and_never_overwrites_existing_configuration(self):
        init_env(self.root)
        path = self.root / '.env'; first = path.read_bytes(); values = read_env(path)
        self.assertEqual(path.stat().st_mode & 0o777, 0o600)
        self.assertGreaterEqual(len(base64.b64decode(values['JWT_SECRET_BASE64'], validate=True)), 32)
        self.assertGreaterEqual(len(values['DB_PASSWORD']), 24)
        self.assertEqual(values['DEEPSEEK_API_KEY'], '')
        with self.assertRaises(FileExistsError): init_env(self.root)
        self.assertEqual(path.read_bytes(), first)
        another = self.root / 'another'; another.mkdir(); (another / '.env.example').write_bytes((ROOT / '.env.example').read_bytes())
        init_env(another); self.assertNotEqual(read_env(another / '.env')['JWT_SECRET_BASE64'], values['JWT_SECRET_BASE64'])

    def test_init_does_not_follow_a_dangling_configuration_symlink(self):
        target = self.root / 'private-target'; (self.root / '.env').symlink_to(target)
        with self.assertRaises(FileExistsError): init_env(self.root)
        self.assertFalse(target.exists())

    def test_migration_encodes_secrets_and_strips_unrelated_environment(self):
        values = dict(DB_HOST='127.0.0.1', DB_PORT='5432', DB_USER='user name', DB_PASSWORD='a:@/?#% secret', DB_NAME='owned db', DB_SSLMODE='disable')
        env = migration_env(values, {'PATH': '/bin', 'HTTP_PROXY': 'external', 'DEEPSEEK_API_KEY': 'private', 'GOOSE_DBSTRING': 'wrong', 'PGHOST': 'external'})
        parsed = urlsplit(env['GOOSE_DBSTRING'])
        self.assertEqual(unquote(parsed.password), values['DB_PASSWORD']); self.assertEqual(unquote(parsed.path), '/owned db')
        self.assertEqual(parsed.hostname, '127.0.0.1'); self.assertEqual(parse_qs(parsed.query)['connect_timeout'], ['5'])
        self.assertEqual(env['GOOSE_DRIVER'], 'postgres')
        for key in ['DEEPSEEK_API_KEY', 'HTTP_PROXY', 'PGHOST', 'DB_PASSWORD']: self.assertNotIn(key, env)
        for key, value in [('DB_HOST', 'db.example.test'), ('DB_HOST', '0.0.0.0'), ('DB_PORT', '65536'), ('DB_PASSWORD', 'REPLACE_WITH_LOCAL_PASSWORD'), ('DB_NAME', ''), ('DB_USER', 'bad\0user')]:
            with self.subTest(key=key, value=value), self.assertRaises(ValueError): migration_env(dict(values, **{key: value}), {})


if __name__ == '__main__': unittest.main()
