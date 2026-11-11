#!/usr/bin/env python3
"""Week08 owned PostgreSQL/Redis integration; --live additionally exercises synthetic DeepSeek/browser flow."""
import argparse
import base64
import json
import os
from pathlib import Path
import secrets
import socket
import subprocess
import sys
import tempfile
import time
import urllib.request
from urllib.parse import quote
from dev import ROOT, read_env, child_env, local_http


def free_port():
    with socket.socket() as listener:
        listener.bind(('127.0.0.1', 0))
        return listener.getsockname()[1]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--live', action='store_true')
    parser.add_argument('--backend-only', action='store_true')
    args = parser.parse_args()
    values = read_env(ROOT / '.env') if args.live else {}
    key = values.get('DEEPSEEK_API_KEY', '')
    if args.live and not key:
        raise ValueError('DEEPSEEK_API_KEY is not configured')
    suffix = secrets.token_hex(6)
    project = 'nexusdesk-week08-' + suffix
    pg_port, redis_port, api_port, web_port = [free_port() for _ in range(4)]
    base = child_env('web', {})
    env = dict(base, APP_ENV='test', HTTP_ADDR='127.0.0.1:' + str(api_port),
               DB_HOST='127.0.0.1', DB_PORT=str(pg_port), DB_USER='nexusdesk_test',
               DB_PASSWORD=secrets.token_hex(24), DB_NAME='nexusdesk_w02d2_test_' + suffix,
               DB_SSLMODE='disable', JWT_ISSUER='nexusdesk', JWT_AUDIENCE='nexusdesk-api', JWT_TTL_MINUTES='15',
               JWT_SECRET_BASE64=base64.b64encode(secrets.token_bytes(32)).decode(),
               RAG_ENABLED='true', OLLAMA_BASE_URL='http://127.0.0.1:11434',
               EMBEDDING_MODEL='bge-m3:567m', EMBEDDING_DIM='1024', RAG_MIN_SCORE='0.40',
               CHAT_PROVIDER='ollama', CHAT_MODEL='qwen3.5:35b', ASYNC_ENABLED='true',
               REDIS_ADDR='127.0.0.1:' + str(redis_port), REDIS_DB='6', REDIS_PASSWORD='',
               WORKER_CONCURRENCY='2', NEXUSDESK_REDIS_PORT=str(redis_port))
    compose = ['docker', 'compose', '--env-file', '/dev/null', '-p', project,
               '-f', str(ROOT / 'deploy/compose.yaml'), '-f', str(ROOT / 'deploy/compose.pgvector.yaml'),
               '-f', str(ROOT / 'deploy/compose.redis.yaml')]
    processes = []
    created = False
    out = ROOT / 'docs/testing/evidence/week08'
    out.mkdir(parents=True, exist_ok=True)
    result = {'status':'FAIL', 'synthetic_data_only':True, 'daily_database_modified':False}
    logs = []

    def run(command, process_env=env, timeout=180):
        done = subprocess.run(command, cwd=ROOT, env=process_env, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=timeout)
        if done.returncode:
            # Fixture logs contain no private inputs; keep failure output in owned ignored scratch.
            failure=ROOT/'.local/week08-last-failure.log'; failure.parent.mkdir(parents=True,exist_ok=True)
            output=done.stdout.decode(errors='replace')
            for secret in [key, env['DB_PASSWORD'], env['JWT_SECRET_BASE64']]:
                if secret: output=output.replace(secret,'[redacted]')
            failure.write_text(output)
            # Never reflect subprocess arguments, environment, connection URLs or response bodies.
            raise RuntimeError('subprocess failed: ' + Path(command[0]).name + ', exit=' + str(done.returncode))
        return done.stdout

    def ready(url, process):
        for _ in range(100):
            if process.poll() is not None:
                raise RuntimeError('application exited before readiness')
            try:
                with local_http().open(url, timeout=.5) as response:
                    if response.status == 200:
                        return
            except Exception:
                pass
            time.sleep(.1)
        raise RuntimeError('readiness timeout')

    try:
        if run(['docker', 'ps', '-aq', '--filter', 'label=com.docker.compose.project=' + project]).strip():
            raise RuntimeError('test project already exists')
        for volume in [project + '_nexusdesk_pgdata', project + '_queue-data']:
            if subprocess.run(['docker','volume','inspect',volume],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode == 0:
                raise RuntimeError('test volume already exists')
        created = True
        run(compose + ['up', '-d', '--wait', '--wait-timeout', '90', '--pull', 'never', 'db', 'redis'])
        migration_env = dict(env, GOOSE_DRIVER='postgres', GOOSE_DBSTRING='postgres://'+env['DB_USER']+':'+quote(env['DB_PASSWORD'],safe='')+'@127.0.0.1:'+str(pg_port)+'/'+env['DB_NAME']+'?sslmode=disable')
        run(['goose','-env=none','-dir',str(ROOT/'migrations'),'up'], migration_env)
        # Down/Up ONLY this owned disposable database, before creating fixtures.
        run(['goose','-env=none','-dir',str(ROOT/'migrations'),'down'], migration_env)
        run(['goose','-env=none','-dir',str(ROOT/'migrations'),'up'], migration_env)
        result['migration_12_up_down_up']='PASS'
        print('PASS owned PostgreSQL schema12 Up/Down/Up + Redis', flush=True)
        with tempfile.TemporaryDirectory(prefix='nexusdesk-week08-') as tmp:
            directory = Path(tmp)
            env['STORAGE_ROOT'] = str(directory / 'storage')
            tests_env = dict(env)
            for flag in ['POSTGRES','W02_SCHEMA','W02_REGISTER','W02_LOGIN','W02_ORGANIZATIONS','W02_MEMBERS','W03_TICKETS','W04_CLAIM','W05_KNOWLEDGE','W06_RAG','W07_JOBS','W08_AI']:
                tests_env['NEXUSDESK_TEST_' + flag] = '1'
            test_output = run(['go','test','-race','-count=1','-v','./...'], tests_env, 300).decode()
            # These tests use generated fixture credentials only. Guard the artifact anyway.
            for secret in [key, env['DB_PASSWORD'], env['JWT_SECRET_BASE64']]:
                if secret: test_output = test_output.replace(secret, '[redacted]')
            (out / 'integration.log').write_text(test_output)
            if '--- FAIL:' in test_output or '--- SKIP:' in test_output:
                raise RuntimeError('full gated suite contains failures or skips')
            result['full_go_pass_nodes'] = sum(line.lstrip().startswith('--- PASS:') for line in test_output.splitlines())
            result['full_go_skip_nodes'] = sum(line.lstrip().startswith('--- SKIP:') for line in test_output.splitlines())
            print('PASS full Go race suite with real PG/Redis: %d checks, %d skips' % (result['full_go_pass_nodes'], result['full_go_skip_nodes']), flush=True)
            if not args.live or args.backend_only:
                result['status']='PASS'
                return
            for name in ['api','worker']:
                run(['go','build','-o',str(directory/name),'./cmd/'+name])
            api_env = dict(env, CHAT_PROVIDER='deepseek', CHAT_MODEL='deepseek-flash', DEEPSEEK_API_KEY=key)
            worker_env = child_env('worker',env,base)
            for name, process_env in [('api',api_env),('worker',worker_env)]:
                log = tempfile.TemporaryFile(); logs.append(log)
                p = subprocess.Popen([str(directory/name)],cwd=ROOT,env=process_env,stdout=log,stderr=subprocess.STDOUT)
                processes.append(p)
            ready('http://127.0.0.1:%d/healthz' % api_port, processes[0])
            node = ROOT / '.local/week04-tools/node_modules/node/bin/node'
            vite_env = dict(base, NEXUSDESK_API_TARGET='http://127.0.0.1:'+str(api_port))
            vite_log = tempfile.TemporaryFile(); logs.append(vite_log)
            vite = subprocess.Popen([str(node),str(ROOT/'web/node_modules/vite/bin/vite.js'),'--host','127.0.0.1','--port',str(web_port),'--strictPort'],cwd=ROOT/'web',env=vite_env,stdout=vite_log,stderr=subprocess.STDOUT)
            processes.append(vite)
            ready('http://127.0.0.1:%d/healthz' % web_port,vite)
            browser_env = dict(base,NEXUSDESK_LIVE_TEST_ORIGIN='http://127.0.0.1:'+str(web_port),NEXUSDESK_LIVE_TEST_API='http://127.0.0.1:'+str(api_port),NEXUSDESK_LIVE_TEST_DATABASE=env['DB_NAME'])
            print(run([str(node),'scripts/week08-browser.mjs'],browser_env,600).decode().strip(),flush=True)
            for log in logs:
                log.seek(0)
                logged=log.read()
                if any(secret.encode() in logged for secret in [key,env['DB_PASSWORD'],env['JWT_SECRET_BASE64']] if secret):
                    raise RuntimeError('credential appeared in application output')
            result['status']='PASS'
            result['model']='deepseek-flash'
            result['browser']=json.loads((out/'browser.json').read_text())
    finally:
        for p in reversed(processes):
            if p.poll() is None:
                p.terminate()
                try: p.wait(timeout=15)
                except subprocess.TimeoutExpired: p.kill(); p.wait(timeout=5)
        for log in logs:
            log.close()
        if created:
            run(compose+['down','--volumes','--remove-orphans'],timeout=60)
        (out/'result.json').write_text(json.dumps(result,ensure_ascii=False,indent=2)+'\n')
    print('PASS isolated end-to-end; owned resources removed',flush=True)

if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        # Known controlled failures may describe a stage; never expose HTTP error bodies/credentials.
        print('Live check failed (%s); inspect stage/status and local setup.' % type(error).__name__,file=sys.stderr)
        sys.exit(1)
