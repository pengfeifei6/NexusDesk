"""Owned W11 fixtures. No daily configuration, private data or cloud calls."""
import json
import re
import secrets
import subprocess
import time
import urllib.error
import urllib.request
from contextlib import contextmanager
from pathlib import Path

from dev import ROOT, child_env, local_http
from week09_lab import Lab, free_port
from week10_runtime import API as PreviousAPI, NODE

OUT = ROOT / 'docs/testing/evidence/week11'
RUNS = ROOT / '.local/week11-runs'


def validate_fixture(lab):
    if (not re.fullmatch(r'nexusdesk-week11-[a-f0-9]{12}', lab.project)
            or lab.env.get('DB_NAME') != 'nexusdesk_w02d2_test_' + lab.project[-12:]
            or lab.env.get('DB_HOST') != '127.0.0.1'
            or not re.fullmatch(r'127\.0\.0\.1:[0-9]+', lab.env.get('REDIS_ADDR', ''))
            or lab.env.get('CHAT_PROVIDER') != 'ollama'
            or lab.env.get('DEEPSEEK_API_KEY')):
        raise ValueError('requires an owned synthetic W11 fixture without cloud credentials')


class Week11Lab(Lab):
    def __init__(self):
        super().__init__()
        old = self.project
        self.project = old.replace('week09', 'week11', 1)
        self.compose = [self.project if part == old else part for part in self.compose]
        validate_fixture(self)

    def run(self, command, env=None, timeout=300, input=None):
        env = self.env if env is None else env
        result = subprocess.run(command, cwd=ROOT, env=env, input=input,
                                stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=timeout)
        text = result.stdout.decode(errors='replace')
        for key in ('DB_PASSWORD', 'JWT_SECRET_BASE64', 'DEEPSEEK_API_KEY', 'REDIS_PASSWORD'):
            if env.get(key):
                text = text.replace(env[key], '[redacted]')
        if result.returncode:
            path = ROOT / '.local/week11-last-failure.log'
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(text)
            path.chmod(0o600)
            raise RuntimeError('fixture command failed: ' + Path(command[0]).name)
        return text

    def service(self, name, action):
        """Only stop/start an inspected container belonging to this exact lab."""
        validate_fixture(self)
        if not self.created or name not in ('db', 'redis') or action not in ('stop', 'start'):
            raise ValueError('invalid owned service action')
        container = self.run(self.compose + ['ps', '-aq', name]).strip()
        if not re.fullmatch(r'[a-f0-9]{12,64}', container):
            raise RuntimeError('service ownership identity missing')
        labels = json.loads(self.run(['docker', 'inspect', '--format', '{{json .Config.Labels}}', container]))
        if (labels.get('com.docker.compose.project') != self.project
                or labels.get('com.docker.compose.service') != name):
            raise RuntimeError('service ownership mismatch')
        self.run(['docker', action, container], timeout=45)
        return container


class API(PreviousAPI):
    def account(self, slot):
        email = 'week11-' + slot.lower() + '-' + secrets.token_hex(6) + '@example.test'
        password = secrets.token_urlsafe(24)
        user = self.request('/auth/register', 'POST', {'email': email, 'display_name': 'W11 synthetic ' + slot, 'password': password}, expected=201)
        token = self.request('/auth/login', 'POST', {'email': email, 'password': password})['access_token']
        org = self.request('/organizations', 'POST', {'name': 'W11 synthetic ' + slot}, token, 201)['id']
        return token, org, user['id']

    def raw(self, path, method='GET', data=None, token=None):
        body = json.dumps(data).encode() if data is not None else None
        headers = {'Content-Type': 'application/json'} if body is not None else {}
        if token:
            headers['Authorization'] = 'Bearer ' + token
        request = urllib.request.Request(self.origin + path, data=body, headers=headers, method=method)
        try:
            response = self.http.open(request, timeout=20)
        except urllib.error.HTTPError as error:
            response = error
        with response:
            raw = response.read(2 * 1024 * 1024 + 1)
            if len(raw) > 2 * 1024 * 1024:
                raise ValueError('fixture response exceeds bound')
            return response.status, dict(response.headers), raw


def wait_for(check, timeout=30, interval=.2, description='condition'):
    end = time.monotonic() + timeout
    while time.monotonic() < end:
        value = check()
        if value:
            return value
        time.sleep(interval)
    raise RuntimeError('timed out waiting for ' + description)


def fetch(url, timeout=40):
    with local_http().open(url, timeout=timeout) as response:
        if response.status != 200:
            raise RuntimeError('diagnostic request failed')
        raw = response.read(32 * 1024 * 1024 + 1)
        if len(raw) > 32 * 1024 * 1024:
            raise RuntimeError('diagnostic response exceeds bound')
        return raw


class RunningApp:
    def __init__(self, lab, directory, overrides=None, api_binary=None):
        validate_fixture(lab)
        self.lab, self.directory = lab, directory.resolve()
        if RUNS.resolve() not in self.directory.parents:
            raise ValueError('application scratch must be inside .local/week11-runs')
        if overrides and any(k not in {'CACHE_ENABLED', 'CACHE_MAX_BYTES', 'AI_RATE_LIMIT_REQUESTS', 'AI_RATE_LIMIT_WINDOW_SECONDS', 'OLLAMA_BASE_URL', 'ASYNC_ENABLED'} for k in overrides):
            raise ValueError('fixture override outside supported runtime controls')
        self.env = dict(lab.env, STORAGE_ROOT=str(directory / 'storage'), HTTP_ADDR='127.0.0.1:' + str(free_port()),
                        OBSERVABILITY_ADDR='127.0.0.1:' + str(free_port()), WORKER_OBSERVABILITY_ADDR='127.0.0.1:' + str(free_port()))
        self.env.update(overrides or {})
        self.api = API('http://' + self.env['HTTP_ADDR'])
        self.diagnostic_url = 'http://' + self.env['OBSERVABILITY_ADDR']
        self.worker_diagnostic_url = 'http://' + self.env['WORKER_OBSERVABILITY_ADDR']
        self.api_binary = Path(api_binary) if api_binary else directory / 'api'
        if RUNS.resolve() not in self.api_binary.resolve().parents:
            raise ValueError('API binary must belong to W11 scratch')
        self.worker_binary = directory / 'worker'
        self.processes, self.log_paths = [], []
        self.api_process = self.worker_process = None
        self.origin = None

    def start(self, name, command, values, cwd=ROOT):
        path = self.directory / (name + '-' + str(len(self.processes)) + '.log')
        with path.open('wb') as stream:
            process = subprocess.Popen(command, cwd=cwd, env=values, stdout=stream, stderr=subprocess.STDOUT)
        self.processes.append(process)
        self.log_paths.append((name, path))
        return process

    def ready(self, url, process):
        def check():
            if process.poll() is not None:
                raise RuntimeError('owned process exited before readiness')
            try:
                fetch(url, timeout=.4)
                return True
            except (OSError, urllib.error.URLError):
                return False
        wait_for(check, timeout=25, description='owned application readiness')

    def start_api(self):
        if not self.api_binary.exists():
            self.lab.run(['go', 'build', '-o', str(self.api_binary), './cmd/api'])
        self.api_process = self.start('api', [str(self.api_binary)], child_env('api', self.env))
        self.ready(self.api.origin + '/healthz', self.api_process)
        self.ready(self.diagnostic_url + '/metrics', self.api_process)

    def start_worker(self):
        if self.worker_process is not None and self.worker_process.poll() is None:
            raise ValueError('owned worker is already running')
        if not self.worker_binary.exists():
            self.lab.run(['go', 'build', '-o', str(self.worker_binary), './cmd/worker'])
        self.worker_process = self.start('worker', [str(self.worker_binary)], child_env('worker', self.env))
        log = self.log_paths[-1][1]
        self.ready(self.worker_diagnostic_url + '/metrics', self.worker_process)
        wait_for(lambda: b'Worker started' in log.read_bytes(), timeout=20, description='worker started')
        return self.worker_process

    def stop_worker(self, kill=False):
        process = self.worker_process
        if process is None or process not in self.processes or process.poll() is not None:
            raise ValueError('no running owned worker')
        started = time.monotonic()
        process.kill() if kill else process.terminate()
        code = process.wait(timeout=20)
        return {'exit_code': code, 'seconds': time.monotonic() - started}

    def start_web(self):
        self.origin = 'http://127.0.0.1:' + str(free_port())
        process = self.start('web', [str(NODE), str(ROOT / 'web/node_modules/vite/bin/vite.js'), '--host', '127.0.0.1', '--port', self.origin.rsplit(':', 1)[1], '--strictPort'],
                             dict(child_env('web', {}), NEXUSDESK_API_TARGET=self.api.origin), ROOT / 'web')
        self.ready(self.origin, process)

    def safe_logs(self):
        logs = {}
        for name, path in self.log_paths:
            raw = path.read_text(errors='replace')
            if any(self.env[k] in raw for k in ('DB_PASSWORD', 'JWT_SECRET_BASE64', 'DEEPSEEK_API_KEY') if self.env.get(k)):
                raise RuntimeError('fixture output failed credential guard')
            logs.setdefault(name, '')
            logs[name] += raw
        return logs

    def close(self):
        for process in reversed(self.processes):
            if process.poll() is None:
                process.terminate()
                try:
                    process.wait(timeout=15)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait(timeout=5)
        self.safe_logs()


@contextmanager
def application(lab, directory, overrides=None, worker=False, web=False, api_binary=None):
    app = RunningApp(lab, directory, overrides, api_binary)
    try:
        app.start_api()
        if worker:
            app.start_worker()
        if web:
            app.start_web()
        yield app
    finally:
        app.close()
