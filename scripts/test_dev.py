import importlib.util
import tempfile
import unittest
from pathlib import Path

spec = importlib.util.spec_from_file_location('dev', Path(__file__).with_name('dev.py'))
dev = importlib.util.module_from_spec(spec)
spec.loader.exec_module(dev)

class DevTest(unittest.TestCase):
    def test_env_values_are_literal_not_shell(self):
        with tempfile.TemporaryDirectory() as tmp:
            marker = Path(tmp) / 'should-not-exist'
            p = Path(tmp) / '.env'
            p.write_text("export DB_PASSWORD='space # $value'\nDEEPSEEK_API_KEY='$(touch " + str(marker) + ")'\nREDIS_PASSWORD=\n")
            values = dev.read_env(p)
            self.assertEqual(values['DB_PASSWORD'], 'space # $value')
            self.assertEqual(values['REDIS_PASSWORD'], '')
            self.assertFalse(marker.exists())
            self.assertTrue(values['DEEPSEEK_API_KEY'].startswith('$(touch'))
    def test_bad_lines_do_not_print_secret(self):
        with tempfile.TemporaryDirectory() as tmp:
            p = Path(tmp) / '.env'
            for value in ["SECRET='private-value", 'SECRET=private-value two', 'BAD KEY=private-value', 'DB_HOST=a\nDB_HOST=b']:
                p.write_text(value)
                with self.assertRaises(ValueError) as cm:
                    dev.read_env(p)
                self.assertNotIn('private-value', str(cm.exception))
    def test_child_environment_is_scoped(self):
        values = {'DB_PASSWORD':'db-secret','JWT_SECRET_BASE64':'jwt-secret','DEEPSEEK_API_KEY':'cloud-secret','CHAT_PROVIDER':'deepseek','CHAT_MODEL':'deepseek-flash','REDIS_PASSWORD':'redis-secret','APP_ENV':'development','OLLAMA_BASE_URL':'http://127.0.0.1:11434'}
        parent = {'PATH':'/bin','HOME':'/home/test','DEEPSEEK_API_KEY':'inherited-secret','VITE_SECRET':'not-public','AWS_SECRET_ACCESS_KEY':'unrelated'}
        api = dev.child_env('api', values, parent)
        self.assertEqual(api['DEEPSEEK_API_KEY'], 'cloud-secret')
        worker = dev.child_env('worker', values, parent)
        self.assertEqual(worker['DB_PASSWORD'], 'db-secret')
        for key in ['JWT_SECRET_BASE64','DEEPSEEK_API_KEY','CHAT_PROVIDER','CHAT_MODEL','AWS_SECRET_ACCESS_KEY','VITE_SECRET']:
            self.assertNotIn(key, worker)
        web = dev.child_env('web', values, parent)
        self.assertEqual(web['PATH'], '/bin')
        for key in values:
            self.assertNotIn(key, web)
    def test_rate_config_is_api_only(self):
        values = {'AI_RATE_LIMIT_ENABLED':'true','AI_RATE_LIMIT_REQUESTS':'1','AI_RATE_LIMIT_WINDOW_SECONDS':'60'}
        self.assertEqual(dev.child_env('api', values, {}), values)
        self.assertEqual(dev.child_env('worker', values, {}), {})
        self.assertEqual(dev.child_env('web', values, {}), {})

    def test_diagnostics_environment_is_scoped_and_explicit_override(self):
        values = {'OBSERVABILITY_ADDR': '127.0.0.1:6060', 'WORKER_OBSERVABILITY_ADDR': '127.0.0.1:6061'}
        parent = {'OBSERVABILITY_ADDR': '127.0.0.1:9999', 'WORKER_OBSERVABILITY_ADDR': '127.0.0.1:9998'}
        self.assertEqual(dev.child_env('api', values, parent), {'OBSERVABILITY_ADDR': '127.0.0.1:6060'})
        self.assertEqual(dev.child_env('worker', values, parent), {'WORKER_OBSERVABILITY_ADDR': '127.0.0.1:6061'})
        self.assertEqual(dev.child_env('web', values, parent), {})
        self.assertEqual(dev.child_env('api', values, parent, diagnostics_address='127.0.0.1:0'), {'OBSERVABILITY_ADDR': '127.0.0.1:0'})
        for bad in ['localhost:6060', ':6060', '0.0.0.0:6060', '127.0.0.1:65536', '127.0.0.1:+1']:
            with self.assertRaises(ValueError):
                dev.child_env('api', values, {}, diagnostics_address=bad)
        with self.assertRaises(ValueError):
            dev.child_env('web', {}, {}, diagnostics_address='127.0.0.1:6060')

    def test_diagnostics_cli_reaches_only_the_selected_backend(self):
        import contextlib
        import io
        import sys
        from unittest.mock import patch
        for component, key in [('api', 'OBSERVABILITY_ADDR'), ('worker', 'WORKER_OBSERVABILITY_ADDR')]:
            with patch.object(sys, 'argv', ['dev.py', component, '--diagnostics-address', '127.0.0.1:0']), patch.object(dev, 'read_env', return_value={}), patch.object(dev.os, 'chdir'), patch.object(dev.os, 'execvpe') as execute:
                dev.main()
            command, args, env = execute.call_args.args
            self.assertEqual(command, 'go')
            self.assertEqual(args, ['go', 'run', './cmd/' + component])
            self.assertEqual(env[key], '127.0.0.1:0')
            self.assertNotIn('WORKER_OBSERVABILITY_ADDR' if component == 'api' else 'OBSERVABILITY_ADDR', env)
        diagnostic = io.StringIO()
        with contextlib.redirect_stderr(diagnostic), patch.object(sys, 'argv', ['dev.py', 'web', '--diagnostics-address', '127.0.0.1:6060']), patch.object(dev, 'read_env') as read, patch.object(dev.os, 'execvpe') as execute:
            with self.assertRaises(SystemExit):
                dev.main()
            read.assert_not_called()
            execute.assert_not_called()
        self.assertIn('--diagnostics-address 仅支持 api/worker。', diagnostic.getvalue())

    def test_status_uses_configured_ports_and_provider_default(self):
        import contextlib
        import io
        import threading
        from http.server import ThreadingHTTPServer, BaseHTTPRequestHandler
        class Handler(BaseHTTPRequestHandler):
            def do_GET(self):
                self.send_response(200)
                self.end_headers()
            def log_message(self, *args):
                pass
        server = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        address = '127.0.0.1:' + str(server.server_port)
        values = {'HTTP_ADDR':address, 'REDIS_ADDR':address, 'DB_HOST':'127.0.0.1', 'DB_PORT':str(server.server_port), 'OLLAMA_BASE_URL':'http://' + address, 'CHAT_PROVIDER':'deepseek'}
        try:
            output = io.StringIO()
            with contextlib.redirect_stdout(output):
                dev.status(values)
            self.assertIn('API: HTTP 200 http://' + address + '/healthz', output.getvalue())
            self.assertIn('deepseek-flash', output.getvalue())
        finally:
            server.shutdown()
            server.server_close()
            thread.join()

    def test_missing_env_has_helpful_error(self):
        with tempfile.TemporaryDirectory() as tmp:
            with self.assertRaises(ValueError):
                dev.read_env(Path(tmp) / '.env')

if __name__ == '__main__':
    unittest.main()
