#!/usr/bin/env python3
"""Create a local reviewable source archive; never publishes or reads private data."""
import argparse
import gzip
import hashlib
import io
import json
from pathlib import Path
import re
import tarfile
from dev import ROOT

TOP = {'README.md', 'go.mod', 'go.sum', '.env.example', '.gitignore'}
EXTENSIONS = {
    'cmd': {'.go'}, 'internal': {'.go'}, 'tests': {'.go'}, 'migrations': {'.sql'},
    'scripts': {'.py', '.mjs'}, 'deploy': {'.yaml', '.yml'},
    'requests': {'.http', '.md', '.json', '.jsonl', '.txt'}, '.github': {'.yaml', '.yml'},
    'docs': {'.md'}, 'web': {'.vue', '.ts', '.js', '.mjs', '.cjs', '.css', '.html', '.json', '.svg', '.md'},
}
PUBLIC_FILES = {'web/.prettierrc.json', 'web/.editorconfig'}
EXCLUDED = {'.local', '.git', '.superpowers', 'node_modules', 'dist', '__pycache__', 'var', 'backups', 'coverage', 'evidence'}
SECRET = re.compile(rb'sk-[a-zA-Z0-9]{24,}|-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----')


def selected_files(root):
    """Traverse only source allowlists, without following any directory symlink."""
    def walk(directory):
        for p in sorted(directory.iterdir()):
            if p.name in EXCLUDED or (p.name.startswith('.') and p.name not in TOP and p.name != '.github' and p.relative_to(root).as_posix() not in PUBLIC_FILES):
                continue
            if p.is_symlink():
                raise ValueError('源码目录含符号链接，拒绝打包：' + str(p.relative_to(root)))
            if p.is_dir():
                yield from walk(p)
            elif p.is_file():
                yield p
    for name in sorted(TOP | set(EXTENSIONS)):
        path = root / name
        if path.is_symlink(): raise ValueError('源码入口含符号链接，拒绝打包。')
        if path.is_file() and name in TOP: yield path
        elif path.is_dir():
            for p in walk(path):
                if p.suffix in EXTENSIONS[name] or p.relative_to(root).as_posix() in PUBLIC_FILES: yield p
    manual = root/'项目开发文件/每周开发手册/15_NexusDesk_第十二周详细开发手册.md'
    if any(part.is_symlink() for part in [manual, manual.parent, manual.parent.parent]):
        raise ValueError('手册及其目录不可为符号链接。')
    if manual.is_file(): yield manual


def build(root, output, version):
    root, output = Path(root).resolve(), Path(output)
    if not re.fullmatch(r'v[0-9]+\.[0-9]+\.[0-9]+(?:-[a-z0-9-]+)?', version):
        raise ValueError('版本格式应为v1.0.0或v1.0.0-preview。')
    # Output may not escape the original project or follow an output symlink.
    allowed = root / '.local/releases'
    try:
        output.resolve().relative_to(allowed)
        allowed.resolve().relative_to(root / '.local')
    except ValueError:
        raise ValueError('源码包只能写入主目录.local/releases。') from None
    paths = [output, Path(str(output)+'.manifest.json'), Path(str(output)+'.sha256')]
    if any(p.exists() or p.is_symlink() for p in paths): raise FileExistsError('已有发布产物，拒绝覆盖。')
    files, payloads = [], []
    for p in sorted(selected_files(root)):
        name = p.relative_to(root).as_posix()
        if any(ord(c) < 32 for c in name): raise ValueError('源码文件名含控制字符。')
        data = p.read_bytes()
        if SECRET.search(data): raise ValueError('疑似凭据，拒绝打包：' + name)
        files.append({'path': name, 'bytes': len(data), 'sha256': hashlib.sha256(data).hexdigest()})
        payloads.append((name, data))
    manifest = {'version': version, 'scope': 'source and Markdown documentation; runtime data and raw evidence excluded', 'files': files}
    payloads.append(('RELEASE-MANIFEST.json', (json.dumps(manifest, ensure_ascii=False, indent=2)+'\n').encode()))
    output.parent.mkdir(parents=True, exist_ok=True)
    created = []
    try:
        with output.open('xb') as stream:
            created.append(output)
            with gzip.GzipFile(filename='', fileobj=stream, mode='wb', mtime=0) as compressed:
                with tarfile.open(fileobj=compressed, mode='w|', format=tarfile.PAX_FORMAT) as archive:
                    for name, data in payloads:
                        item = tarfile.TarInfo('NexusDesk-' + version + '/' + name)
                        item.size = len(data); item.mode = 0o644; item.mtime = 0
                        archive.addfile(item, io.BytesIO(data))
        with paths[1].open('x') as stream:
            created.append(paths[1]); json.dump(manifest, stream, ensure_ascii=False, indent=2); stream.write('\n')
        with paths[2].open('x') as stream:
            created.append(paths[2]); stream.write(hashlib.sha256(output.read_bytes()).hexdigest() + '  ' + output.name + '\n')
    except BaseException:
        for p in created: p.unlink()
        raise
    return manifest


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--version', default='v1.0.0')
    args = parser.parse_args()
    try:
        output = ROOT / '.local/releases' / ('nexusdesk-' + args.version + '-source.tar.gz')
        manifest = build(ROOT, output, args.version)
        print('本地源码包已生成：' + str(output.relative_to(ROOT)))
        print('包含%d个文件；未发布；原始测试证据/视频保留在主目录docs/testing/evidence。' % len(manifest['files']))
    except (ValueError, OSError) as error:
        parser.exit(1, str(error) + '\n')


if __name__ == '__main__': main()
