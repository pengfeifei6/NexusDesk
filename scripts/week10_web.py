"""Real product browser paths with explicitly synthetic local model transport."""
import json
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from dev import ROOT, child_env
from week10_runtime import OUT, NODE, application


def run_browser(lab, directory):
    calls = {'embedding': 0, 'generation': 0, 'stream': 0}
    class Fixture(BaseHTTPRequestHandler):
        def log_message(self, *unused):
            pass

        def do_POST(self):
            payload = json.loads(self.rfile.read(int(self.headers.get('Content-Length', '0'))))
            if self.path == '/api/embed':
                calls['embedding'] += 1
                value = {'model': 'bge-m3:567m', 'embeddings': [[1.] + [0.] * 1023 for _ in payload['input']]}
                self.send_response(200)
                self.send_header('Content-Type', 'application/json')
                self.end_headers()
                self.wfile.write(json.dumps(value).encode())
            elif self.path == '/api/chat' and not payload.get('stream'):
                calls['generation'] += 1
                data = json.loads(payload['messages'][-1]['content'])
                answer = {'answer': '先核对数据库连接地址与端口，再检查网络连通性。', 'citation_ids': [str(data['sources'][0]['chunk_id'])], 'insufficient_evidence': False}
                value = {'model': 'qwen3.5:35b', 'message': {'role': 'assistant', 'content': json.dumps(answer, ensure_ascii=False)}, 'done': True, 'done_reason': 'stop', 'prompt_eval_count': 128, 'eval_count': 32}
                self.send_response(200)
                self.send_header('Content-Type', 'application/json')
                self.end_headers()
                self.wfile.write(json.dumps(value).encode())
            elif self.path == '/api/chat':
                calls['stream'] += 1
                messages = payload['messages']
                proposal = messages[-1]['role'] != 'tool' and '提议' in messages[-1].get('content', '')
                message = {'role': 'assistant', 'content': '已整理处理建议，请核对下方工单提议后确认。' if proposal else '可以先核对连接配置，再检查网络和服务状态。'}
                if proposal:
                    message['tool_calls'] = [{'function': {'name': 'propose_create_ticket', 'arguments': {'title': '排查数据库连接超时', 'body': '核对 DB_HOST 与 DB_PORT，检查网络连通性并记录复验结果。', 'priority': 3}}}]
                rows = [{'model': 'qwen3.5:35b', 'message': message, 'done': False},
                        {'model': 'qwen3.5:35b', 'message': {'role': 'assistant', 'content': ''}, 'done': True, 'done_reason': 'stop', 'prompt_eval_count': 128, 'eval_count': 32}]
                self.send_response(200)
                self.send_header('Content-Type', 'application/x-ndjson')
                self.end_headers()
                for row in rows:
                    self.wfile.write((json.dumps(row, ensure_ascii=False) + '\n').encode())
                    self.wfile.flush()
            else:
                self.send_error(404)

    server = ThreadingHTTPServer(('127.0.0.1', 0), Fixture)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    try:
        overrides = {'OLLAMA_BASE_URL': 'http://127.0.0.1:' + str(server.server_port), 'AI_RATE_LIMIT_REQUESTS': '10'}
        with application(lab, directory, overrides, web=True, worker=True) as (api, origin, env):
            browser_env = dict(child_env('web', {}), NEXUSDESK_LIVE_TEST_ORIGIN=origin,
                               NEXUSDESK_LIVE_TEST_API=api.origin, NEXUSDESK_LIVE_TEST_DATABASE=env['DB_NAME'])
            output = lab.run([str(NODE), 'scripts/week10-browser.mjs'], browser_env, 240)
            (OUT / 'browser.log').write_text(output)
            print(output, flush=True)
            result = json.loads((OUT / 'browser.json').read_text())
            result['model_fixture_calls'] = calls
            return result
    finally:
        server.shutdown()
        server.server_close()
        thread.join(timeout=2)
