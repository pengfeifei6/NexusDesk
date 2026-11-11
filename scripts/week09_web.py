"""Browser fixture: real application/PG/Redis, deterministic local model transport."""
import json
import subprocess
import tempfile
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from dev import ROOT, child_env, local_http
from week09_lab import free_port


def run_browser(lab, env, directory):
    calls=[]
    class Fixture(BaseHTTPRequestHandler):
        def log_message(self,*args): pass
        def do_POST(self):
            size=int(self.headers.get('Content-Length','0'))
            self.rfile.read(size)
            if self.path=='/api/chat':
                calls.append(1)
                payload='\n'.join(json.dumps(x,ensure_ascii=False) for x in [
                    {'model':'qwen3.5:35b','message':{'role':'assistant','content':'第九周合成流式响应。'},'done':False},
                    {'model':'qwen3.5:35b','message':{'role':'assistant','content':''},'done':True,'done_reason':'stop','prompt_eval_count':3,'eval_count':4}])+'\n'
                self.send_response(200); self.send_header('Content-Type','application/x-ndjson'); self.end_headers()
                self.wfile.write(payload.encode()); self.wfile.flush(); return
            if self.path=='/redis/stop': lab.run(lab.compose+['stop','redis'])
            elif self.path=='/redis/start': lab.run(lab.compose+['start','--wait','redis'])
            else: self.send_error(404); return
            self.send_response(200); self.end_headers(); self.wfile.write(b'{}')

    server=ThreadingHTTPServer(('127.0.0.1',0),Fixture)
    thread=threading.Thread(target=server.serve_forever,daemon=True);thread.start()
    api,web=free_port(),free_port()
    fixture='http://127.0.0.1:'+str(server.server_port)
    processes=[]; logs=[]
    node=ROOT/'.local/week04-tools/node_modules/node/bin/node'
    def ready(url,p):
        for _ in range(200):
            if p.poll() is not None: raise RuntimeError('application exited')
            try:
                with local_http().open(url,timeout=.3) as r:
                    if r.status==200:return
            except Exception: pass
            time.sleep(.1)
        raise RuntimeError('application readiness timeout')
    try:
        lab.run(['go','build','-o',str(directory/'api'),'./cmd/api'])
        api_env=dict(env,HTTP_ADDR='127.0.0.1:'+str(api),OLLAMA_BASE_URL=fixture,
                     AI_RATE_LIMIT_ENABLED='true',AI_RATE_LIMIT_REQUESTS='1',AI_RATE_LIMIT_WINDOW_SECONDS='60')
        log=tempfile.TemporaryFile();logs.append(log)
        p=subprocess.Popen([str(directory/'api')],cwd=ROOT,env=api_env,stdout=log,stderr=subprocess.STDOUT);processes.append(p)
        ready('http://127.0.0.1:'+str(api)+'/healthz',p)
        base=child_env('web',{})
        log=tempfile.TemporaryFile();logs.append(log)
        p=subprocess.Popen([str(node),str(ROOT/'web/node_modules/vite/bin/vite.js'),'--host','127.0.0.1','--port',str(web),'--strictPort'],cwd=ROOT/'web',env=dict(base,NEXUSDESK_API_TARGET='http://127.0.0.1:'+str(api)),stdout=log,stderr=subprocess.STDOUT);processes.append(p)
        ready('http://127.0.0.1:'+str(web)+'/healthz',p)
        browser_env=dict(base,NEXUSDESK_LIVE_TEST_ORIGIN='http://127.0.0.1:'+str(web),NEXUSDESK_LIVE_TEST_API='http://127.0.0.1:'+str(api),NEXUSDESK_LIVE_TEST_DATABASE=env['DB_NAME'],NEXUSDESK_FIXTURE_CONTROL=fixture)
        print(lab.run([str(node),'scripts/week09-browser.mjs'],browser_env,180),flush=True)
        assert len(calls)==1, 'throttled/unavailable request reached the model'
        for log in logs:
            log.seek(0); raw=log.read()
            assert all(env[k].encode() not in raw for k in ('DB_PASSWORD','JWT_SECRET_BASE64'))
        return {'status':'PASS','model':'deterministic local HTTP fixture','model_calls':len(calls),
                'browser':json.loads((ROOT/'docs/testing/evidence/week09/browser.json').read_text())}
    finally:
        for p in reversed(processes):
            if p.poll() is None:
                p.terminate()
                try:p.wait(timeout=10)
                except subprocess.TimeoutExpired:p.kill();p.wait(timeout=5)
        for log in logs:log.close()
        server.shutdown();server.server_close();thread.join(timeout=2)
