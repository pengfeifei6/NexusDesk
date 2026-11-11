#!/usr/bin/env python3
"""Local foreground launcher. Reads .env as data; never evaluates shell code."""
import argparse
import json
import ipaddress
import os
from pathlib import Path
import re
import shlex
import socket
import sys
import urllib.request
from urllib.parse import urlsplit

ROOT = Path(__file__).resolve().parents[1]
BASE_KEYS = {'PATH', 'HOME', 'USER', 'LOGNAME', 'LANG', 'LC_ALL', 'TERM', 'TMPDIR', 'GOPATH', 'GOCACHE', 'GOMODCACHE', 'GOTOOLCHAIN', 'XDG_CACHE_HOME'}
COMMON_KEYS = set('APP_ENV DB_HOST DB_PORT DB_USER DB_PASSWORD DB_NAME DB_SSLMODE STORAGE_ROOT UPLOAD_MAX_FILE_BYTES UPLOAD_MAX_REQUEST_BYTES TEXT_EXTRACT_MAX_BYTES TEXT_EXTRACT_TIMEOUT_SECONDS RAG_ENABLED OLLAMA_BASE_URL EMBEDDING_MODEL EMBEDDING_DIM RAG_MIN_SCORE ASYNC_ENABLED REDIS_ADDR REDIS_DB REDIS_PASSWORD WORKER_CONCURRENCY'.split())
API_KEYS = set('OBSERVABILITY_ADDR AI_RATE_LIMIT_ENABLED AI_RATE_LIMIT_REQUESTS AI_RATE_LIMIT_WINDOW_SECONDS CACHE_ENABLED CACHE_TTL_SECONDS REDIS_OPERATION_TIMEOUT_MS CACHE_MAX_BYTES HTTP_ADDR JWT_ISSUER JWT_AUDIENCE JWT_TTL_MINUTES JWT_SECRET_BASE64 CHAT_PROVIDER CHAT_MODEL DEEPSEEK_API_KEY'.split())


def read_env(path):
    try:
        text = Path(path).read_text(encoding='utf-8')
    except (OSError, UnicodeError):
        raise ValueError('无法读取根目录 .env；请按 .env.example 配置。') from None
    values = {}
    for number, line in enumerate(text.splitlines(), 1):
        line = line.strip()
        if not line or line.startswith('#'):
            continue
        if line.startswith('export '):
            line = line[7:]
        key, sep, raw = line.partition('=')
        if not sep or not re.fullmatch(r'[A-Za-z_][A-Za-z0-9_]*', key) or key in values:
            raise ValueError('.env 第 %d 行格式错误或变量重复。' % number)
        try:
            parts = shlex.split(raw, comments=True, posix=True)
        except ValueError:
            raise ValueError('.env 第 %d 行引号不完整。' % number) from None
        if len(parts) > 1 or '\x00' in raw:
            raise ValueError('.env 第 %d 行需要单个值；有空格时请加引号。' % number)
        values[key] = parts[0] if parts else ''
    return values


def child_env(component, values, parent=None, diagnostics_address=None):
    parent = os.environ if parent is None else parent
    result = {k: v for k, v in parent.items() if k in BASE_KEYS}
    keys = COMMON_KEYS | API_KEYS if component == 'api' else COMMON_KEYS | {'WORKER_OBSERVABILITY_ADDR'} if component == 'worker' else set()
    result.update({k: v for k, v in values.items() if k in keys})
    if diagnostics_address is not None:
        if component not in ('api', 'worker'):
            raise ValueError('--diagnostics-address 仅支持 api/worker。')
        validate_diagnostics_address(diagnostics_address)
        result['OBSERVABILITY_ADDR' if component == 'api' else 'WORKER_OBSERVABILITY_ADDR'] = diagnostics_address
    return result


def validate_diagnostics_address(raw):
    try:
        host, sep, port = raw.rpartition(':')
        if not sep or not re.fullmatch(r'[0-9]+', port) or not 0 <= int(port) <= 65535:
            raise ValueError()
        if host.startswith('[') and host.endswith(']'):
            host = host[1:-1]
        elif ':' in host:
            raise ValueError()
        if not ipaddress.ip_address(host).is_loopback:
            raise ValueError()
    except ValueError:
        raise ValueError('诊断地址必须是显式回环IP和0..65535端口。') from None


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *args, **kwargs):
        return None


def local_http():
    return urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())


def loopback_origin(raw):
    try:
        url = urlsplit(raw)
        if url.scheme != 'http' or url.username or url.password or url.query or url.fragment or url.path not in ('', '/') or not ipaddress.ip_address(url.hostname).is_loopback or not url.port:
            raise ValueError()
        return url
    except (ValueError, TypeError):
        raise ValueError('服务地址必须是显式回环IP和端口的HTTP地址。') from None


def warmup(values):
    # Same literal loopback validation as the application; no key is sent.
    url = loopback_origin(values.get('OLLAMA_BASE_URL', 'http://127.0.0.1:11434'))
    payload = {'model': 'bge-m3:567m', 'input': ['公开演示：数据库连接端口为5432。'], 'truncate': False, 'keep_alive': '30m'}
    req = urllib.request.Request(url.geturl().rstrip('/') + '/api/embed', data=json.dumps(payload).encode(), headers={'Content-Type': 'application/json'})
    with local_http().open(req, timeout=180) as response:
        raw = response.read(2 * 1024 * 1024 + 1)
        if len(raw) > 2 * 1024 * 1024:
            raise ValueError('本地向量模型响应过大。')
        result = json.loads(raw)
    if result.get('model') != 'bge-m3:567m' or len(result.get('embeddings', [])) != 1 or len(result['embeddings'][0]) != 1024:
        raise ValueError('本地模型未返回1024维向量。')
    print('BGE-M3 已预热：1024维；DeepSeek 接入不需要启动本地 Qwen。')


def status(values):
    redis = loopback_origin('http://' + values.get('REDIS_ADDR', '127.0.0.1:6380'))
    ollama = loopback_origin(values.get('OLLAMA_BASE_URL', 'http://127.0.0.1:11434'))
    api = loopback_origin('http://' + values.get('HTTP_ADDR', '127.0.0.1:8080'))
    checks = [('PostgreSQL', values.get('DB_HOST', '127.0.0.1'), int(values.get('DB_PORT', '5432'))),
              ('Redis', redis.hostname, redis.port), ('Ollama', ollama.hostname, ollama.port)]
    for name, host, port in checks:
        try:
            with socket.create_connection((host, port), timeout=2):
                print('%s: 端口可连接（非功能验收）' % name)
        except OSError:
            print('%s: 无法连接' % name)
    for name, url in [('API', api.geturl().rstrip('/') + '/healthz'), ('前端', 'http://127.0.0.1:5173')]:
        try:
            with local_http().open(url, timeout=3) as response:
                print('%s: HTTP %d %s' % (name, response.status, url))
        except Exception:
            print('%s: 未就绪 %s' % (name, url))
    print('Worker 请检查启动终端的 Worker started；任务完成状态才证明处理成功。')
    print('问答提供方: %s；模型: %s；Key: %s' % (values.get('CHAT_PROVIDER', 'ollama'), values.get('CHAT_MODEL', 'deepseek-flash' if values.get('CHAT_PROVIDER') == 'deepseek' else 'qwen3.5:35b'), '已配置' if values.get('DEEPSEEK_API_KEY') else '未配置'))


def main():
    parser = argparse.ArgumentParser(description='NexusDesk 本机启动：自动读取根目录 .env，不需要 source。')
    parser.add_argument('component', choices=['api', 'worker', 'web', 'status', 'warmup'])
    parser.add_argument('--diagnostics-address', help='仅 api/worker；显式回环IP与端口；默认禁用')
    args = parser.parse_args()
    if args.diagnostics_address is not None:
        if args.component not in ('api', 'worker'):
            parser.error('--diagnostics-address 仅支持 api/worker。')
        try:
            validate_diagnostics_address(args.diagnostics_address)
        except ValueError as error:
            parser.error(str(error))
    os.chdir(ROOT)
    try:
        # The frontend never reads backend credentials, even temporarily.
        values = {} if args.component == 'web' else read_env(ROOT / '.env')
        if args.component == 'status':
            status(values)
            return
        if args.component == 'warmup':
            warmup(values)
            return
        env = child_env(args.component, values, diagnostics_address=args.diagnostics_address)
        if args.component == 'web':
            node_dir = ROOT / '.local/week04-tools/node_modules/node/bin'
            if (node_dir / 'node').is_file():
                env['PATH'] = str(node_dir) + os.pathsep + env.get('PATH', os.defpath)
            os.chdir(ROOT / 'web')
            cmd = ['npm', 'run', 'dev', '--', '--host', '127.0.0.1', '--port', '5173', '--strictPort']
        else:
            cmd = ['go', 'run', './cmd/' + args.component]
        print('启动 %s；Ctrl+C 停止。配置值不输出。' % args.component, flush=True)
        os.execvpe(cmd[0], cmd, env)
    except ValueError as error:
        print(str(error), file=sys.stderr)
        sys.exit(1)
    except Exception as error:
        print('启动或连接失败 (%s)；检查依赖和本地配置。' % type(error).__name__, file=sys.stderr)
        sys.exit(1)


if __name__ == '__main__':
    main()
