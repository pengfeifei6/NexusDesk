#!/usr/bin/env python3
"""Verify Day 5 against a disposable database; never load the developer's .env."""

import json
import base64
import os
from pathlib import Path
import secrets
import shutil
import signal
import socket
import subprocess
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request


ROOT = Path(__file__).resolve().parents[1]


def free_port():
    with socket.socket() as listener:
        listener.bind(("127.0.0.1", 0))
        return listener.getsockname()[1]


def main():
    goose = shutil.which("goose")
    if not goose:
        raise RuntimeError("goose is not in PATH; see README for its pinned install command")
    subprocess.run(["docker", "compose", "version"], check=True, cwd=ROOT)
    # Remove inherited PostgreSQL / Goose options so the test uses only its fixture.
    env = {key: value for key, value in os.environ.items()
           if not key.startswith(("PG", "GOOSE_", "NEXUSDESK_TEST_"))}
    password = secrets.token_hex(16) + " spaces ' \\ test"
    env.update(
        APP_ENV="test", HTTP_ADDR="127.0.0.1:0", DB_HOST="127.0.0.1",
        DB_PORT=str(free_port()), DB_USER="nexusdesk_test", DB_PASSWORD=password,
        DB_NAME="nexusdesk_day5_test", DB_SSLMODE="disable", GIN_MODE="release",
        NEXUSDESK_TEST_POSTGRES="1", NO_COLOR="1",
        JWT_ISSUER="nexusdesk", JWT_AUDIENCE="nexusdesk-api", JWT_TTL_MINUTES="15",
        JWT_SECRET_BASE64=base64.b64encode(secrets.token_bytes(32)).decode("ascii"),
    )
    connection_url = (
        "postgres://" + urllib.parse.quote(env["DB_USER"], safe="") + ":"
        + urllib.parse.quote(password, safe="") + "@127.0.0.1:" + env["DB_PORT"]
        + "/" + env["DB_NAME"] + "?sslmode=disable"
    )
    env.update(GOOSE_DRIVER="postgres", GOOSE_DBSTRING=connection_url)
    project = "nexusdesk-day5-" + secrets.token_hex(6)
    volume = project + "_nexusdesk_pgdata"
    compose = ["docker", "compose", "--env-file", "/dev/null", "-p", project,
               "-f", str(ROOT / "deploy/compose.yaml")]

    def redacted(text):
        return text.replace(env["JWT_SECRET_BASE64"], "[test JWT key]").replace(connection_url, "[test database URL]").replace(
            urllib.parse.quote(password, safe=""), "[test password]"
        ).replace(password, "[test password]")

    def command(args, *, timeout=120, check=True, child_env=None):
        result = subprocess.run(args, cwd=ROOT, env=child_env or env,
                                capture_output=True, text=True, timeout=timeout)
        if check and result.returncode:
            raise RuntimeError("command failed: " + " ".join(args)
                               + "\n" + redacted(result.stdout + result.stderr))
        return result

    def sql(statement):
        return command(compose + ["exec", "-T", "db", "psql", "-U", env["DB_USER"],
                                  "-d", env["DB_NAME"], "-Atqc", statement]).stdout.strip()

    def migrate(action):
        # Keep this historical W1 smoke independent of later schema additions.
        migration_args = ["up-to", "1"] if action == "up" else [action]
        result = command([goose, "-env=none", "-dir", str(ROOT / "migrations")] + migration_args)
        print(redacted(result.stdout + result.stderr).strip(), flush=True)

    existing = command(["docker", "volume", "inspect", "--format", "{{.Name}}", volume], check=False)
    if existing.returncode == 0:
        raise RuntimeError("refusing to use an existing test volume")

    cleanup = False
    try:
        command(compose + ["config", "--quiet"])
        cleanup = True
        command(compose + ["up", "-d", "--wait", "--wait-timeout", "90", "--pull", "never", "db"])
        print("PASS PostgreSQL container is healthy", flush=True)
        print(command(compose + ["ps"]).stdout.strip(), flush=True)
        migrate("up")
        migrate("status")
        assert sql("SELECT to_regclass('public.organizations')") == "organizations"
        # Rollback is limited to this freshly created, empty, disposable database.
        migrate("down")
        assert sql("SELECT to_regclass('public.organizations') IS NULL") == "t"
        migrate("up")
        assert sql("SELECT version_id FROM goose_db_version WHERE is_applied ORDER BY id DESC LIMIT 1") == "1"
        assert sql("SELECT count(*) FROM organizations") == "0"
        print("PASS Goose Up / Status / Down / Up and version 1", flush=True)

        # Day 7: reproduce a README restart while preserving this test volume.
        command(compose + ["down", "--timeout", "5"])
        command(compose + ["up", "-d", "--wait", "--wait-timeout", "90", "--pull", "never", "db"])
        assert sql("SELECT to_regclass('public.organizations')") == "organizations"
        assert sql("SELECT version_id FROM goose_db_version WHERE is_applied ORDER BY id DESC LIMIT 1") == "1"
        migrate("up")
        print("PASS README restart preserves the volume, organizations table and migration version", flush=True)

        command(["go", "test", "-race", "./..."], timeout=180)
        print("PASS full race suite including PostgreSQL integration and SQL defaults", flush=True)
        null_result = command(compose + ["exec", "-T", "db", "psql", "-U", env["DB_USER"],
                                        "-d", env["DB_NAME"], "-v", "ON_ERROR_STOP=1", "-c",
                                        "INSERT INTO organizations (name) VALUES (NULL)"], check=False)
        assert null_result.returncode != 0 and "not-null" in null_result.stderr
        print("PASS organizations name NOT NULL constraint", flush=True)

        with tempfile.TemporaryDirectory(prefix="nexusdesk-day5-api-") as directory:
            binary = str(Path(directory) / "api")
            command(["go", "build", "-o", binary, "./cmd/api"])
            http = urllib.request.build_opener(urllib.request.ProxyHandler({}))
            for stop_signal in (signal.SIGTERM, signal.SIGINT):
                api_env = dict(env, HTTP_ADDR="127.0.0.1:" + str(free_port()))
                process = subprocess.Popen([binary], cwd=ROOT, env=api_env,
                                           stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
                try:
                    deadline = time.monotonic() + 12
                    while True:
                        if process.poll() is not None:
                            raise RuntimeError("API exited before the health request")
                        try:
                            with http.open("http://" + api_env["HTTP_ADDR"] + "/healthz", timeout=0.3) as response:
                                assert response.status == 200 and json.load(response) == {"status": "ok"}
                            break
                        except urllib.error.URLError:
                            if time.monotonic() >= deadline:
                                raise RuntimeError("API did not become ready")
                            time.sleep(0.03)
                    process.send_signal(stop_signal)
                    stdout, stderr = process.communicate(timeout=8)
                    assert process.returncode == 0
                    assert password not in stdout + stderr
                    assert "PostgreSQL connection verified" in stderr
                    print("PASS database-backed API /healthz and " + stop_signal.name + " exit 0", flush=True)
                finally:
                    if process.poll() is None:
                        process.kill()
                        process.communicate()

            for overrides, expected in [
                ({"DB_PASSWORD": "wrong-test-password"}, "authentication failed"),
                ({"DB_NAME": "database_that_does_not_exist"}, "database does not exist"),
                ({"DB_PORT": str(free_port())}, "connect PostgreSQL"),
            ]:
                result = command([binary], child_env=dict(env, **overrides), timeout=10, check=False)
                assert result.returncode == 1 and expected in result.stderr
                assert "HTTP server started" not in result.stderr
                assert password not in result.stdout + result.stderr
            print("PASS authentication / missing database / unreachable database fail before HTTP", flush=True)
    finally:
        if cleanup:
            result = command(compose + ["down", "--timeout", "5"], timeout=30, check=False)
            if result.returncode:
                raise RuntimeError("test container cleanup failed; project: " + project)
            label = command(["docker", "volume", "inspect", "--format",
                             '{{index .Labels "com.docker.compose.project"}}', volume], check=False)
            if label.returncode == 0:
                if label.stdout.strip() != project:
                    raise RuntimeError("refusing to remove a volume owned by another project")
                command(["docker", "volume", "rm", volume])
            print("Cleaned up only this run's test containers and labeled volume", flush=True)


if __name__ == "__main__":
    main()
