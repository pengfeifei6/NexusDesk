#!/usr/bin/env python3
"""Explicit local setup: create missing config, inspect schema, or apply Up only."""
import argparse
import base64
import ipaddress
import os
from pathlib import Path
import re
import secrets
import shutil
import subprocess
from urllib.parse import quote, urlencode
from dev import ROOT, child_env, read_env


def init_env(root):
    root = Path(root)
    target = root / '.env'
    # Exclusive creation also refuses dangling symlinks; existing files stay intact.
    content = (root / '.env.example').read_text(encoding='utf-8')
    content = content.replace('REPLACE_WITH_LOCAL_PASSWORD', secrets.token_urlsafe(32))
    content = content.replace('REPLACE_WITH_LOCAL_BASE64_SECRET', base64.b64encode(secrets.token_bytes(32)).decode())
    descriptor = os.open(str(target), os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
    with os.fdopen(descriptor, 'w', encoding='utf-8') as stream:
        stream.write(content); stream.flush(); os.fsync(stream.fileno())
    return target


def migration_env(values, parent=None):
    required = ['DB_HOST', 'DB_PORT', 'DB_USER', 'DB_PASSWORD', 'DB_NAME', 'DB_SSLMODE']
    if any(not values.get(key) or '\0' in values[key] for key in required):
        raise ValueError('数据库配置缺失或格式不合法。')
    try:
        host = ipaddress.ip_address(values['DB_HOST'])
        if not host.is_loopback or not re.fullmatch(r'[0-9]{1,5}', values['DB_PORT']) or not 1 <= int(values['DB_PORT']) <= 65535:
            raise ValueError()
    except ValueError:
        raise ValueError('迁移工具只接受显式回环数据库地址和有效端口。') from None
    if values['DB_PASSWORD'].startswith('REPLACE_') or values['DB_SSLMODE'] not in ('disable', 'require', 'verify-ca', 'verify-full'):
        raise ValueError('请填写本地数据库密码和合法SSL模式。')
    address = '[' + str(host) + ']' if host.version == 6 else str(host)
    url = 'postgres://' + quote(values['DB_USER'], safe='') + ':' + quote(values['DB_PASSWORD'], safe='') + '@' + address + ':' + values['DB_PORT'] + '/' + quote(values['DB_NAME'], safe='')
    url += '?' + urlencode({'sslmode': values['DB_SSLMODE'], 'connect_timeout': 5})
    env = child_env('web', {}, parent=parent)
    env.update(GOOSE_DRIVER='postgres', GOOSE_DBSTRING=url)
    return env


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=['init', 'status', 'up'])
    args = parser.parse_args()
    try:
        if args.action == 'init':
            init_env(ROOT)
            print('已创建权限0600的本地.env；凭据未输出。已有环境不要重新初始化。')
            return
        values = read_env(ROOT / '.env')
        env = migration_env(values)
        executable = shutil.which('goose', path=env.get('PATH'))
        if not executable:
            raise ValueError('未找到goose，请按quickstart安装固定版本。')
        result = subprocess.run([executable, '-env=none', '-dir', str(ROOT / 'migrations'), args.action],
                                cwd=ROOT, env=env, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=120)
        if result.returncode:
            # Goose connection errors may include DSNs. Never forward raw failure text.
            raise ValueError('迁移命令失败；请检查本地数据库连通性、配置和迁移版本。配置值未输出。')
        text = result.stdout.decode(errors='replace')
        for value in (env['GOOSE_DBSTRING'], values['DB_PASSWORD']):
            text = text.replace(value, '[redacted]')
        print(text, end='')
    except FileExistsError:
        parser.exit(1, '.env已经存在（或为符号链接），未覆盖；现有项目直接运行status。\n')
    except (ValueError, OSError, subprocess.TimeoutExpired) as error:
        message = str(error) if isinstance(error, ValueError) else '本地操作未完成，请检查工具、文件权限和服务状态。'
        parser.exit(1, message + '\n')


if __name__ == '__main__': main()
