#!/usr/bin/env python3
"""Build and run the standalone desktop GitHub catalog with illustrative data."""
import argparse
import json
import os
from pathlib import Path
import secrets
import socket
import subprocess

ROOT = Path(__file__).resolve().parents[1]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--port', type=int, default=18091)
    parser.add_argument('--navigation-url', default='', help='optional link back to navigation')
    args = parser.parse_args()
    if not 1024 <= args.port <= 65535:
        parser.error('port must be between 1024 and 65535')
    try:
        with socket.socket() as probe:
            probe.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
            probe.bind(('127.0.0.1', args.port))
    except OSError:
        parser.error(f'port {args.port} is already in use; stop the existing demo or choose another port')
    data = ROOT / 'data/demo'
    data.mkdir(parents=True, exist_ok=True, mode=0o700)
    account = data / 'dev-account.json'
    if not account.exists():
        with os.fdopen(os.open(account, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600), 'w') as out:
            json.dump({'username': 'admin', 'password': secrets.token_urlsafe(24)}, out)
    credentials = json.loads(account.read_text())
    binary = ROOT / 'bin/github-catalog'
    binary.parent.mkdir(exist_ok=True)
    subprocess.run(['go', 'build', '-mod=readonly', '-o', str(binary), '.'], cwd=ROOT, check=True)
    print(f'GitHub catalog demo port: {args.port}; browse without signing in.', flush=True)
    print('Optional administrator: admin; private password file: ' + str(account), flush=True)
    origin = f'http://127.0.0.1:{args.port}'
    command = [str(binary), '-addr', f'127.0.0.1:{args.port}', '-public-url', origin, '-demo']
    if args.navigation_url:
        command += ['-navigation-url', args.navigation_url]
    os.chdir(ROOT)
    os.execve(str(binary), command, {**os.environ, 'GITHUB_CATALOG_ADMIN_PASSWORD': credentials['password']})


if __name__ == '__main__':
    main()
