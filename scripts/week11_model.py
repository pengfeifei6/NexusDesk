"""Local deterministic model for recovery/UI mechanics, never quality metrics."""
import json
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


class LocalModel:
    def __init__(self):
        self.calls = {'embedding': 0, 'generation': 0, 'stream': 0}
        self.block_next = False
        self.blocked = threading.Event()
        self.release = threading.Event()
        self.lock = threading.Lock()
        owner = self
        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *unused):
                pass

            def do_POST(self):
                length = int(self.headers.get('Content-Length', '0'))
                if not 0 < length <= 2 * 1024 * 1024:
                    self.send_error(413)
                    return
                payload = json.loads(self.rfile.read(length))
                try:
                    if self.path == '/api/embed':
                        with owner.lock:
                            owner.calls['embedding'] += 1
                            block = owner.block_next
                            owner.block_next = False
                        if block:
                            owner.blocked.set()
                            owner.release.wait(60)
                        self.respond({'model': 'bge-m3:567m', 'embeddings': [[1.] + [0.] * 1023 for _ in payload['input']]})
                    elif self.path == '/api/chat' and not payload.get('stream'):
                        with owner.lock:
                            owner.calls['generation'] += 1
                        data = json.loads(payload['messages'][-1]['content'])
                        answer = {'answer': '先核对数据库连接地址与端口，再检查网络连通性。', 'citation_ids': [str(data['sources'][0]['chunk_id'])], 'insufficient_evidence': False}
                        self.respond({'model': 'qwen3.5:35b', 'message': {'role': 'assistant', 'content': json.dumps(answer, ensure_ascii=False)}, 'done': True, 'done_reason': 'stop', 'prompt_eval_count': 128, 'eval_count': 32})
                    elif self.path == '/api/chat':
                        with owner.lock:
                            owner.calls['stream'] += 1
                        last = payload['messages'][-1]
                        proposal = last['role'] != 'tool' and '提议' in last.get('content', '')
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
                except (BrokenPipeError, ConnectionResetError):
                    pass  # Expected when the crash drill kills its waiting Worker.

            def respond(self, value):
                self.send_response(200)
                self.send_header('Content-Type', 'application/json')
                self.end_headers()
                self.wfile.write(json.dumps(value, ensure_ascii=False).encode())

        self.server = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.origin = 'http://127.0.0.1:' + str(self.server.server_port)

    def __enter__(self):
        self.thread.start()
        return self

    def __exit__(self, *unused):
        self.release.set()
        self.server.shutdown()
        self.server.server_close()
        self.thread.join(timeout=2)

    def block_embedding_once(self):
        self.blocked.clear()
        self.release.clear()
        with self.lock:
            self.block_next = True
