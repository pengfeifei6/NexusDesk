"""W10-owned application fixture. Reuses W09 resource guards; never daily DB."""
import json
import secrets
import subprocess
import tempfile
import time
import urllib.error
import urllib.request
from contextlib import contextmanager
from pathlib import Path
from dev import ROOT, child_env, local_http
from week09_lab import Lab, free_port

OUT = ROOT / 'docs/testing/evidence/week10'
NODE = ROOT / '.local/week04-tools/node_modules/node/bin/node'


class Week10Lab(Lab):
    def __init__(self):
        super().__init__()
        old = self.project
        self.project = old.replace('week09', 'week10', 1)
        self.compose = [self.project if x == old else x for x in self.compose]

    def run(self, command, env=None, timeout=300, input=None):
        env = self.env if env is None else env
        result = subprocess.run(command, cwd=ROOT, env=env, input=input,
                                stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=timeout)
        text = result.stdout.decode(errors='replace')
        for key in ('DB_PASSWORD', 'JWT_SECRET_BASE64', 'DEEPSEEK_API_KEY'):
            if env.get(key):
                text = text.replace(env[key], '[redacted]')
        if result.returncode:
            path = ROOT / '.local/week10-last-failure.log'
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(text)
            path.chmod(0o600)
            raise RuntimeError('fixture command failed: ' + Path(command[0]).name)
        return text


class RequestError(Exception):
    def __init__(self, status, code):
        self.status, self.code = status, code


class API:
    def __init__(self, origin):
        self.origin = origin
        self.http = local_http()

    def request(self, path, method='GET', data=None, token=None, expected=200, content_type='application/json'):
        body = data if isinstance(data, bytes) else json.dumps(data, ensure_ascii=False).encode() if data is not None else None
        headers = {'Content-Type': content_type} if body is not None else {}
        if token:
            headers['Authorization'] = 'Bearer ' + token
        req = urllib.request.Request(self.origin + '/api/v1' + path, data=body, method=method, headers=headers)
        try:
            response = self.http.open(req, timeout=75)
        except urllib.error.HTTPError as error:
            response = error
        with response:
            raw = response.read(2 * 1024 * 1024 + 1)
            if len(raw) > 2 * 1024 * 1024:
                raise RequestError(response.status, 'RESPONSE_TOO_LARGE')
            if response.status != expected:
                try:
                    code = json.loads(raw).get('error', {}).get('code', 'UNKNOWN')
                except (ValueError, AttributeError):
                    code = 'INVALID_RESPONSE'
                # Only application error identifiers, never arbitrary upstream text.
                code = code if isinstance(code, str) and code.replace('_', '').isupper() and len(code) < 80 else 'UNKNOWN'
                raise RequestError(response.status, code)
            if expected >= 400:
                return None
            return json.loads(raw)['data'] if raw else None

    def account(self, slot):
        email = 'week10-' + slot.lower() + '-' + secrets.token_hex(6) + '@example.test'
        password = secrets.token_urlsafe(24)
        self.request('/auth/register', 'POST', {'email': email, 'display_name': 'W10 合成用户 ' + slot, 'password': password}, expected=201)
        token = self.request('/auth/login', 'POST', {'email': email, 'password': password})['access_token']
        org = self.request('/organizations', 'POST', {'name': 'W10 隔离实验 ' + slot}, token, 201)['id']
        return token, org

    def upload(self, org, token, name, content):
        boundary = 'W10' + secrets.token_hex(12)
        body = (f'--{boundary}\r\nContent-Disposition: form-data; name="file"; filename="{name}"\r\nContent-Type: text/markdown\r\n\r\n'.encode()
                + content.encode() + f'\r\n--{boundary}--\r\n'.encode())
        return self.request(f'/organizations/{org}/knowledge/documents', 'POST', body, token, 201, 'multipart/form-data; boundary=' + boundary)


@contextmanager
def application(lab, directory, overrides=None, web=False, worker=False):
    env = dict(lab.env, STORAGE_ROOT=str(directory / 'storage'), HTTP_ADDR='127.0.0.1:' + str(free_port()))
    env.update(overrides or {})
    base = 'http://' + env['HTTP_ADDR']
    processes, logs = [], []

    def start(command, values, cwd=ROOT):
        log = tempfile.TemporaryFile(dir=directory)
        logs.append(log)
        p = subprocess.Popen(command, cwd=cwd, env=values, stdout=log, stderr=subprocess.STDOUT)
        processes.append(p)
        return p

    def ready(url, process):
        for _ in range(200):
            if process.poll() is not None:
                raise RuntimeError('fixture process exited before readiness')
            try:
                with local_http().open(url, timeout=.3) as r:
                    if r.status == 200:
                        return
            except Exception:
                pass
            time.sleep(.1)
        raise RuntimeError('fixture readiness timeout')

    try:
        lab.run(['go', 'build', '-o', str(directory / 'api'), './cmd/api'])
        p = start([str(directory / 'api')], env)
        ready(base + '/healthz', p)
        if worker:
            lab.run(['go', 'build', '-o', str(directory / 'worker'), './cmd/worker'])
            start([str(directory / 'worker')], child_env('worker', env))
        origin = None
        if web:
            port = free_port()
            origin = 'http://127.0.0.1:' + str(port)
            p = start([str(NODE), str(ROOT / 'web/node_modules/vite/bin/vite.js'), '--host', '127.0.0.1', '--port', str(port), '--strictPort'],
                      dict(child_env('web', {}), NEXUSDESK_API_TARGET=base), ROOT / 'web')
            ready(origin + '/healthz', p)
        yield API(base), origin, env
    finally:
        for p in reversed(processes):
            if p.poll() is None:
                p.terminate()
                try:
                    p.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    p.kill()
                    p.wait(timeout=5)
        leaked = False
        for log in logs:
            log.seek(0)
            raw = log.read()
            leaked |= any(env[k].encode() in raw for k in ('DB_PASSWORD', 'JWT_SECRET_BASE64', 'DEEPSEEK_API_KEY') if env.get(k))
            log.close()
        if leaked:
            raise RuntimeError('fixture output failed credential guard')
