"""Owned, disposable W09 PostgreSQL/Redis fixture. Never reads the daily .env."""
import base64
import json
import os
from pathlib import Path
import secrets
import socket
import subprocess
from urllib.parse import quote
from dev import ROOT, child_env


def free_port():
    with socket.socket() as s:
        s.bind(('127.0.0.1', 0))
        return s.getsockname()[1]


class Lab:
    def __init__(self):
        suffix = secrets.token_hex(6)
        self.project = 'nexusdesk-week09-' + suffix
        pg, redis = free_port(), free_port()
        self.env = dict(child_env('web', {}), APP_ENV='test', GIN_MODE='release', HTTP_ADDR='127.0.0.1:0',
                        DB_HOST='127.0.0.1', DB_PORT=str(pg), DB_USER='nexusdesk_test',
                        DB_PASSWORD=secrets.token_hex(24), DB_NAME='nexusdesk_w02d2_test_' + suffix,
                        DB_SSLMODE='disable', JWT_ISSUER='nexusdesk', JWT_AUDIENCE='nexusdesk-api',
                        JWT_TTL_MINUTES='15', JWT_SECRET_BASE64=base64.b64encode(secrets.token_bytes(32)).decode(),
                        RAG_ENABLED='true', OLLAMA_BASE_URL='http://127.0.0.1:11434', EMBEDDING_MODEL='bge-m3:567m',
                        EMBEDDING_DIM='1024', RAG_MIN_SCORE='0.40', CHAT_PROVIDER='ollama', CHAT_MODEL='qwen3.5:35b',
                        ASYNC_ENABLED='true', REDIS_ADDR='127.0.0.1:' + str(redis), REDIS_DB='6',
                        REDIS_PASSWORD='', WORKER_CONCURRENCY='2', NEXUSDESK_REDIS_PORT=str(redis))
        goose = ROOT.parents[1] / 'bin/goose'
        self.goose = str(goose) if goose.exists() else 'goose'
        self.compose = ['docker', 'compose', '--env-file', '/dev/null', '-p', self.project,
                        '-f', str(ROOT/'deploy/compose.yaml'), '-f', str(ROOT/'deploy/compose.pgvector.yaml'),
                        '-f', str(ROOT/'deploy/compose.redis.yaml')]
        self.created = False

    def run(self, command, env=None, timeout=300, input=None):
        r = subprocess.run(command, cwd=ROOT, env=env or self.env, input=input,
                           stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=timeout)
        output = r.stdout.decode(errors='replace')
        for key in ('DB_PASSWORD', 'JWT_SECRET_BASE64'):
            output = output.replace(self.env[key], '[redacted]')
        if r.returncode:
            path = ROOT/'.local/week09-last-failure.log'
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(output)
            raise RuntimeError('fixture command failed: ' + Path(command[0]).name)
        return output

    def __enter__(self):
        if self.run(['docker', 'ps', '-aq', '--filter', 'label=com.docker.compose.project='+self.project]).strip():
            raise RuntimeError('fixture project already exists')
        for suffix in ('_nexusdesk_pgdata', '_queue-data'):
            if subprocess.run(['docker', 'volume', 'inspect', self.project+suffix], stdout=subprocess.DEVNULL,
                              stderr=subprocess.DEVNULL).returncode == 0:
                raise RuntimeError('fixture volume already exists')
        self.created = True
        try:
            self.run(self.compose+['up','-d','--wait','--wait-timeout','90','--pull','never','db','redis'])
        except BaseException:
            self.__exit__(None, None, None)
            raise
        return self

    def migrate(self, *args):
        e = self.env
        url = 'postgres://'+e['DB_USER']+':'+quote(e['DB_PASSWORD'],safe='')+'@127.0.0.1:'+e['DB_PORT']+'/'+e['DB_NAME']+'?sslmode=disable'
        return self.run([self.goose,'-env=none','-dir',str(ROOT/'migrations'),*args],
                        dict(e, GOOSE_DRIVER='postgres', GOOSE_DBSTRING=url))

    def sql(self, sql):
        return self.run(self.compose+['exec','-T','db','psql','-X','-qAt','-v','ON_ERROR_STOP=1',
                                     '-U',self.env['DB_USER'],'-d',self.env['DB_NAME']], input=sql.encode())

    def __exit__(self, *unused):
        if self.created:
            self.run(self.compose+['down','--volumes','--remove-orphans'], timeout=60)
            self.created = False
