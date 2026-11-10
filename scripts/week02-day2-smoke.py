#!/usr/bin/env python3
"""Exercise W02 migrations in an owned disposable database, without .env."""

import argparse
import base64
import json
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


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--baseline", action="store_true",
                        help="apply only W1 schema, then run W02 tests (expected failure)")
    parser.add_argument("--registration", action="store_true",
                        help="also run W02 Day 3 registration integration and process HTTP smoke")
    parser.add_argument("--login", action="store_true",
                        help="also run W02 Day 4 login/JWT integration and process smoke (includes registration)")
    parser.add_argument("--organizations", action="store_true",
                        help="also run W02 Day 5 protected identity/organization tests (includes login)")
    parser.add_argument("--members", action="store_true",
                        help="also run W02 Day 6 member/RBAC tests (includes organizations)")
    parser.add_argument("--closeout", action="store_true",
                        help="run full W02 suite and ordered Day 7 Alice/Bob/expired-token demo")
    parser.add_argument("--tickets", action="store_true", help="run W03 schema/business/TCP suite (includes W02 closeout)")
    parser.add_argument("--ticket-schema-baseline", action="store_true", help="expected failure: W03 schema checks on W02 version 3")
    options = parser.parse_args()
    if options.ticket_schema_baseline:
        options.tickets = True
    if options.tickets:
        options.closeout = True
    baseline = options.baseline
    if baseline and (options.registration or options.login or options.organizations or options.members or options.closeout or options.tickets):
        parser.error("--baseline cannot be combined with registration, login or organization mode")
    if options.closeout:
        options.members = True
    if options.members:
        options.organizations = True
    if options.organizations:
        options.login = True
    if options.login:
        options.registration = True
    goose = shutil.which("goose")
    if not goose:
        raise RuntimeError("goose is not in PATH; use the version pinned in README")
    env = {key: value for key, value in os.environ.items()
           if not key.startswith(("PG", "GOOSE_", "NEXUSDESK_TEST_"))}
    suffix = secrets.token_hex(6)
    project = "nexusdesk-w02d2-" + suffix
    volume = project + "_nexusdesk_pgdata"
    password = secrets.token_hex(16) + " spaces ' \\ fixture"
    with socket.socket() as listener:
        listener.bind(("127.0.0.1", 0))
        port = listener.getsockname()[1]
    env.update(APP_ENV="test", HTTP_ADDR="127.0.0.1:0", DB_HOST="127.0.0.1",
               DB_PORT=str(port), DB_USER="nexusdesk_test", DB_PASSWORD=password,
               DB_NAME="nexusdesk_w02d2_test_" + suffix, DB_SSLMODE="disable",
               GIN_MODE="test", NEXUSDESK_TEST_POSTGRES="1",
               NEXUSDESK_TEST_W02_SCHEMA="1", NO_COLOR="1")
    env.update(JWT_ISSUER="nexusdesk", JWT_AUDIENCE="nexusdesk-api", JWT_TTL_MINUTES="15",
               JWT_SECRET_BASE64=base64.b64encode(secrets.token_bytes(32)).decode("ascii"))
    if options.registration:
        env["NEXUSDESK_TEST_W02_REGISTER"] = "1"
    if options.login:
        env["NEXUSDESK_TEST_W02_LOGIN"] = "1"
    if options.organizations:
        env["NEXUSDESK_TEST_W02_ORGANIZATIONS"] = "1"
    if options.members:
        env["NEXUSDESK_TEST_W02_MEMBERS"] = "1"
    if options.tickets:
        env["NEXUSDESK_TEST_W03_TICKETS"] = "1"
    connection_url = ("postgres://" + env["DB_USER"] + ":"
                      + urllib.parse.quote(password, safe="") + "@127.0.0.1:"
                      + str(port) + "/" + env["DB_NAME"] + "?sslmode=disable")
    env.update(GOOSE_DRIVER="postgres", GOOSE_DBSTRING=connection_url)
    compose = ["docker", "compose", "--env-file", "/dev/null", "-p", project,
               "-f", str(ROOT / "deploy/compose.yaml")]

    def redacted(text):
        return text.replace(env["JWT_SECRET_BASE64"], "[fixture JWT key]").replace(connection_url, "[fixture database URL]").replace(
            urllib.parse.quote(password, safe=""), "[fixture password]"
        ).replace(password, "[fixture password]")

    def run(args, *, check=True, timeout=120):
        result = subprocess.run(args, cwd=ROOT, env=env, capture_output=True,
                                text=True, timeout=timeout)
        if check and result.returncode:
            raise RuntimeError("command failed: " + " ".join(args) + "\n"
                               + redacted(result.stdout + result.stderr))
        return result

    def sql(statement):
        return run(compose + ["exec", "-T", "db", "psql", "-U", env["DB_USER"],
                              "-d", env["DB_NAME"], "-v", "ON_ERROR_STOP=1",
                              "-Atqc", statement]).stdout.strip()

    def migrate(*args):
        result = run([goose, "-env=none", "-dir", str(ROOT / "migrations")] + list(args))
        print(redacted(result.stdout + result.stderr).strip(), flush=True)

    def version():
        return sql("SELECT version_id FROM goose_db_version WHERE is_applied ORDER BY id DESC LIMIT 1")

    def registration_process_smoke():
        with tempfile.TemporaryDirectory(prefix="nexusdesk-w02d3-api-", dir="/var/tmp") as directory:
            binary = str(Path(directory) / "api")
            run(["go", "build", "-o", binary, "./cmd/api"])
            with socket.socket() as listener:
                listener.bind(("127.0.0.1", 0))
                api_port = listener.getsockname()[1]
            process_env = dict(env, HTTP_ADDR="127.0.0.1:" + str(api_port))
            process = subprocess.Popen([binary], cwd=ROOT, env=process_env,
                                       stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
            fixture_password = "ProcessFixture-Only-LongRegistrationPassword"
            observed_tokens = []
            http = urllib.request.build_opener(urllib.request.ProxyHandler({}))
            base = "http://" + process_env["HTTP_ADDR"]

            def demo(step, summary):
                if options.closeout:
                    print("DEMO step " + str(step) + ": " + summary, flush=True)

            def auth_request(path, payload, expected_status):
                request = urllib.request.Request(base + path,
                                                 data=json.dumps(payload).encode(),
                                                 headers={"Content-Type": "application/json"})
                try:
                    response = http.open(request, timeout=8)
                except urllib.error.HTTPError as error:
                    response = error
                with response:
                    body = response.read()
                    assert response.status == expected_status
                    assert response.headers.get("Cache-Control") == "no-store"
                    assert fixture_password.encode() not in body and b"$argon2id$" not in body
                    result = json.loads(body)
                    if path.endswith("/login") and expected_status == 200:
                        observed_tokens.append(result["data"]["access_token"])
                    return result

            try:
                deadline = time.monotonic() + 12
                while True:
                    if process.poll() is not None:
                        raise RuntimeError("API exited before health check")
                    try:
                        with http.open(base + "/healthz", timeout=0.3) as response:
                            assert response.status == 200 and json.load(response) == {"status": "ok"}
                        break
                    except urllib.error.URLError:
                        if time.monotonic() >= deadline:
                            raise RuntimeError("registration API failed to start")
                        time.sleep(0.03)
                before = sql("SELECT count(*) FROM organizations")
                payload = {"email": "ProcessFixture@example.test", "display_name": "Process fixture",
                           "password": fixture_password}
                created = auth_request("/api/v1/auth/register", payload, 201)
                assert set(created["data"]) == {"id", "email", "display_name"}
                assert created["data"]["email"] == "processfixture@example.test"
                payload["email"] = payload["email"].upper()
                assert auth_request("/api/v1/auth/register", payload, 409)["error"]["code"] == "EMAIL_ALREADY_EXISTS"
                assert auth_request("/api/v1/auth/register", dict(payload, role="admin"), 400)["error"]["code"] == "INVALID_REQUEST"
                assert sql("SELECT count(*) FROM organizations") == before
                assert sql("SELECT count(*) FROM organization_members WHERE user_id=" + str(created["data"]["id"])) == "0"
                assert sql("SELECT password_hash LIKE '$argon2id$%' FROM users WHERE id=" + str(created["data"]["id"])) == "t"
                if options.login:
                    login_payload = {"email": payload["email"], "password": fixture_password}
                    result = auth_request("/api/v1/auth/login", login_payload, 200)
                    assert set(result["data"]) == {"access_token", "token_type", "expires_in"}
                    assert result["data"]["token_type"] == "Bearer" and result["data"]["expires_in"] == 900
                    token = result["data"]["access_token"]
                    # Verify the real process token without printing or persisting it.
                    import hashlib
                    import hmac
                    parts = token.split(".")
                    assert len(parts) == 3
                    decode_segment = lambda value: base64.urlsafe_b64decode(value + "=" * (-len(value) % 4))
                    header, claims = json.loads(decode_segment(parts[0])), json.loads(decode_segment(parts[1]))
                    assert header["alg"] == "HS256" and set(claims) == {"sub", "iss", "aud", "iat", "exp"}
                    assert claims["sub"] == str(created["data"]["id"])
                    assert claims["iss"] == env["JWT_ISSUER"] and env["JWT_AUDIENCE"] in claims["aud"]
                    assert claims["exp"] - claims["iat"] == 900 and claims["iat"] <= time.time() < claims["exp"]
                    expected = hmac.new(base64.b64decode(env["JWT_SECRET_BASE64"]), (parts[0] + "." + parts[1]).encode("ascii"), hashlib.sha256).digest()
                    assert hmac.compare_digest(expected, decode_segment(parts[2]))
                    wrong = auth_request("/api/v1/auth/login", dict(login_payload, password="Wrong-Process-Fixture-Password"), 401)
                    unknown = auth_request("/api/v1/auth/login", {"email": "unknown-process@example.test", "password": "Wrong-Process-Fixture-Password"}, 401)
                    assert wrong == unknown and wrong["error"]["code"] == "INVALID_CREDENTIALS"
                    assert auth_request("/api/v1/auth/login", dict(login_payload, password=""), 400)["error"]["code"] == "INVALID_REQUEST"
                    print("PASS real API login 200, independently verified HS256/claims, uniform credential 401 and empty-password 400; Token kept in memory only", flush=True)
                    demo(1, "Alice registered 201 and logged in 200; identity and HS256 claims verified")
                if options.organizations:
                    def protected_request(method, path, payload=None, credential=token, expected_status=200):
                        headers = {"Content-Type": "application/json"}
                        if credential:
                            headers["Authorization"] = "Bearer " + credential
                        request = urllib.request.Request(base + path, method=method,
                                                         data=None if payload is None else json.dumps(payload).encode(),
                                                         headers=headers)
                        try:
                            response = http.open(request, timeout=8)
                        except urllib.error.HTTPError as error:
                            response = error
                        with response:
                            assert response.status == expected_status
                            assert response.headers.get("Cache-Control") == "no-store"
                            if expected_status == 401:
                                assert response.headers.get("WWW-Authenticate") == "Bearer"
                            if expected_status == 204:
                                assert response.read() == b""
                                return None
                            result = json.load(response)
                            assert token not in json.dumps(result) and fixture_password not in json.dumps(result)
                            return result
                    assert protected_request("GET", "/api/v1/me")["data"] == created["data"]
                    assert protected_request("GET", "/api/v1/organizations")["data"] == {"items": [], "page": 1, "page_size": 20}
                    org = protected_request("POST", "/api/v1/organizations", {"name": "  Process organization  "}, expected_status=201)["data"]
                    assert set(org) == {"id", "name", "my_role"} and org["name"] == "Process organization" and org["my_role"] == "admin"
                    assert protected_request("GET", "/api/v1/organizations")["data"]["items"] == [org]
                    assert sql("SELECT role,status FROM organization_members WHERE org_id=" + str(org["id"]) + " AND user_id=" + str(created["data"]["id"])) == "admin|active"
                    demo(2, "Alice created organization A 201; database creator relation is admin/active")
                    for field in ("owner_id", "admin_user_id", "user_id", "role", "status"):
                        protected_request("POST", "/api/v1/organizations", {"name": "Forged process org", field: 999}, expected_status=400)
                    assert sql("SELECT count(*) FROM organizations WHERE name='Forged process org'") == "0"
                    protected_request("GET", "/api/v1/me", credential="", expected_status=401)
                    protected_request("GET", "/api/v1/organizations", credential=token + "x", expected_status=401)
                    protected_request("POST", "/api/v1/organizations", {"name": "Unauthorized"}, credential="", expected_status=401)
                    protected_request("GET", "/api/v1/organizations?user_id=999", expected_status=400)
                    print("PASS real TCP /me, empty list, atomic organization/admin creation, own list, forged fields 400 and protected routes 401", flush=True)
                    if options.members:
                        bob = auth_request("/api/v1/auth/register", {"email": "bob-process@example.test", "display_name": "Bob fixture", "password": fixture_password}, 201)["data"]
                        bob_token = auth_request("/api/v1/auth/login", {"email": bob["email"], "password": fixture_password}, 200)["data"]["access_token"]
                        demo(3, "Bob registered 201 and logged in 200")
                        member_path = "/api/v1/organizations/" + str(org["id"]) + "/members"
                        initial = protected_request("GET", member_path)["data"]["items"]
                        assert len(initial) == 1 and set(initial[0]) == {"user_id", "display_name", "role", "status", "created_at"}
                        bob_org = protected_request("POST", "/api/v1/organizations", {"name": "Bob process org"}, credential=bob_token, expected_status=201)["data"]
                        assert sql("SELECT role,status FROM organization_members WHERE org_id=" + str(bob_org["id"]) + " AND user_id=" + str(bob["id"])) == "admin|active"
                        demo(4, "Bob created organization B 201; database creator relation is admin/active")
                        hidden = protected_request("GET", member_path, credential=bob_token, expected_status=404)
                        assert sql("SELECT count(*) FROM organization_members WHERE org_id=" + str(org["id"]) + " AND user_id=" + str(bob["id"])) == "0"
                        demo(5, "Bob reading A members returned 404; no membership in A exists")
                        bob_path = "/api/v1/organizations/" + str(bob_org["id"]) + "/members"
                        assert protected_request("GET", bob_path, expected_status=404) == hidden
                        protected_request("POST", bob_path, {"user_id": created["data"]["id"], "role": "admin"}, expected_status=404)
                        added = protected_request("POST", member_path, {"user_id": bob["id"], "role": "member"}, expected_status=201)["data"]
                        assert set(added) == {"user_id", "display_name", "role", "status", "created_at"} and added["role"] == "member" and added["status"] == "active"
                        assert sql("SELECT role,status FROM organization_members WHERE org_id=" + str(org["id"]) + " AND user_id=" + str(bob["id"])) == "member|active"
                        demo(6, "Alice added Bob to A 201; database relation is member/active")
                        assert len(protected_request("GET", member_path, credential=bob_token)["data"]["items"]) == 2
                        protected_request("POST", member_path, {"user_id": created["data"]["id"], "role": "admin"}, credential=bob_token, expected_status=403)
                        demo(7, "Bob read A members 200 but adding a member returned 403")
                        if options.closeout:
                            # This credential uses only this run's synthetic key and stays in memory.
                            encode_segment = lambda value: base64.urlsafe_b64encode(value).rstrip(b"=").decode("ascii")
                            expired_at = int(time.time()) - 900
                            expired_claims = dict(claims, iat=expired_at - 900, exp=expired_at)
                            assert expired_claims["exp"] - expired_claims["iat"] == 900 and expired_claims["exp"] < time.time()
                            signing_input = encode_segment(json.dumps({"alg": "HS256", "typ": "JWT"}).encode()) + "." + encode_segment(json.dumps(expired_claims).encode())
                            signature = hmac.new(base64.b64decode(env["JWT_SECRET_BASE64"]), signing_input.encode("ascii"), hashlib.sha256).digest()
                            expired_token = signing_input + "." + encode_segment(signature)
                            observed_tokens.append(expired_token)
                            protected_request("GET", "/api/v1/me", credential=expired_token, expected_status=401)
                            protected_request("GET", member_path, credential=expired_token, expected_status=401)
                            demo(8, "Expired HS256 credential rejected with 401 on /me and A members; no production clock change")
                        assert protected_request("POST", member_path, {"user_id": bob["id"], "role": "admin"}, expected_status=409)["error"]["code"] == "MEMBER_ALREADY_EXISTS"
                        protected_request("POST", member_path, {"user_id": bob["id"], "role": "admin", "status": "active"}, expected_status=400)
                        sql("UPDATE organization_members SET status='disabled' WHERE org_id=" + str(org["id"]) + " AND user_id=" + str(bob["id"]))
                        assert protected_request("GET", member_path, credential=bob_token, expected_status=404) == hidden
                        protected_request("POST", member_path, {"user_id": bob["id"], "role": "member"}, credential=bob_token, expected_status=404)
                        assert protected_request("GET", bob_path, credential=bob_token)["data"]["items"][0]["role"] == "admin"
                        protected_request("POST", member_path, {"user_id": bob["id"], "role": "member"}, expected_status=409)
                        assert sql("SELECT role,status FROM organization_members WHERE org_id=" + str(org["id"]) + " AND user_id=" + str(bob["id"])) == "member|disabled"
                        print("PASS real TCP Alice/Bob member read/add, cross-org 404, member 403, admin 201, duplicate 409, disabled old-token 404 and other-org access retained", flush=True)
                        if options.tickets:
                            sql("UPDATE organization_members SET status='active' WHERE org_id=" + str(org["id"]) + " AND user_id=" + str(bob["id"]))
                            tickets_path = "/api/v1/organizations/" + str(org["id"]) + "/tickets"
                            made = protected_request("POST", tickets_path, {"title": "Process W03 ticket", "body": "Fictional body", "priority": 2}, expected_status=201)["data"]
                            ticket_path = tickets_path + "/" + str(made["id"])
                            assert made["status"] == "OPEN" and made["version"] == 0
                            assert protected_request("GET", ticket_path)["data"]["id"] == made["id"]
                            assert len(protected_request("GET", ticket_path + "/events")["data"]["items"]) == 1
                            print("W03 DEMO 1: create/detail/initial event and scoped filtered list", flush=True)
                            assert protected_request("GET", tickets_path + "?status=OPEN&priority=2")["data"]["items"][0]["id"] == made["id"]
                            assert protected_request("PATCH", ticket_path, {"expected_version": 0, "priority": 3})["data"]["version"] == 1
                            assert protected_request("PATCH", ticket_path, {"expected_version": 1, "assignee_id": bob["id"]})["data"]["version"] == 2
                            assert len(protected_request("GET", ticket_path + "/events")["data"]["items"]) == 3
                            print("W03 DEMO 2: admin update and assignment, two audited changes", flush=True)
                            protected_request("POST", ticket_path + "/transition", {"expected_version": 2, "to_status": "IN_PROGRESS"}, credential=bob_token)
                            protected_request("POST", ticket_path + "/comments", {"content": "Bob process comment"}, credential=bob_token, expected_status=201)
                            assert protected_request("GET", ticket_path)["data"]["version"] == 3
                            protected_request("POST", ticket_path + "/transition", {"expected_version": 3, "to_status": "RESOLVED"}, credential=bob_token)
                            protected_request("POST", ticket_path + "/transition", {"expected_version": 4, "to_status": "CLOSED"})
                            protected_request("POST", ticket_path + "/transition", {"expected_version": 5, "to_status": "OPEN"}, expected_status=409)
                            print("W03 DEMO 3: Bob starts/comments/resolves, admin closes, CLOSED reopen rejected", flush=True)
                            visitor = auth_request("/api/v1/auth/register", {"email": "w03-visitor@example.test", "display_name": "Visitor", "password": fixture_password}, 201)["data"]
                            visitor_token = auth_request("/api/v1/auth/login", {"email": visitor["email"], "password": fixture_password}, 200)["data"]["access_token"]
                            protected_request("POST", bob_path, {"user_id": visitor["id"], "role": "member"}, credential=bob_token, expected_status=201)
                            for suffix in ("", "/comments", "/events"):
                                protected_request("GET", ticket_path + suffix, credential=visitor_token, expected_status=404)
                            protected_request("POST", tickets_path, {"title": "forged", "body": "B"}, credential=visitor_token, expected_status=404)
                            protected_request("PATCH", ticket_path, {"expected_version": 5, "title": "forged"}, credential=visitor_token, expected_status=404)
                            print("W03 DEMO 4: organization B visitor cannot read/write A ticket or children", flush=True)
                            deleted = protected_request("POST", tickets_path, {"title": "Process delete", "body": "B"}, expected_status=201)["data"]
                            deleted_path = tickets_path + "/" + str(deleted["id"])
                            protected_request("DELETE", deleted_path, expected_status=204)
                            for suffix in ("", "/comments", "/events"):
                                protected_request("GET", deleted_path + suffix, expected_status=404)
                            assert sql("SELECT count(*) FROM ticket_events WHERE ticket_id=" + str(deleted["id"]) + " AND event_type='TICKET_DELETED'") == "1"
                            print("W03 DEMO 5: soft delete 204, ordinary parent/children hidden, database audit retained", flush=True)
                            rollback = protected_request("POST", tickets_path, {"title": "Process rollback", "body": "B"}, expected_status=201)["data"]
                            rollback_path = tickets_path + "/" + str(rollback["id"])
                            protected_request("PATCH", rollback_path, {"expected_version": 0, "assignee_id": bob["id"]})
                            sql("CREATE FUNCTION nexusdesk_w03_process_fail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'controlled fixture'; END $$")
                            sql("CREATE TRIGGER nexusdesk_w03_process_failure BEFORE INSERT ON ticket_events FOR EACH ROW EXECUTE FUNCTION nexusdesk_w03_process_fail()")
                            try:
                                protected_request("POST", rollback_path + "/transition", {"expected_version": 1, "to_status": "IN_PROGRESS"}, credential=bob_token, expected_status=500)
                                protected_request("POST", rollback_path + "/comments", {"content": "must roll back"}, credential=bob_token, expected_status=500)
                            finally:
                                sql("DROP TRIGGER nexusdesk_w03_process_failure ON ticket_events")
                                sql("DROP FUNCTION nexusdesk_w03_process_fail()")
                            state = protected_request("GET", rollback_path)["data"]
                            assert state["status"] == "OPEN" and state["version"] == 1
                            assert protected_request("GET", rollback_path + "/comments")["data"]["items"] == []
                            print("W03 DEMO 6: real event failure rolls back state/version/comment; restored database usable", flush=True)

                process.send_signal(signal.SIGTERM)
                stdout, stderr = process.communicate(timeout=8)
                assert process.returncode == 0
                assert password not in stdout + stderr and fixture_password not in stdout + stderr
                assert "$argon2id$" not in stdout + stderr
                assert env["JWT_SECRET_BASE64"] not in stdout + stderr
                assert all(token not in stdout + stderr for token in observed_tokens)
                print("PASS real API registration 201, duplicate 409, forged role 400, no organization side effects, clean logs and SIGTERM", flush=True)
            finally:
                if process.poll() is None:
                    process.kill()
                    process.communicate()

    run(["docker", "compose", "version"])
    if run(["docker", "volume", "inspect", volume], check=False).returncode == 0:
        raise RuntimeError("refusing an existing test volume")
    if run(["docker", "ps", "-aq", "--filter", "label=com.docker.compose.project=" + project]).stdout.strip():
        raise RuntimeError("refusing an existing Compose project")

    cleanup = False
    try:
        run(compose + ["config", "--quiet"])
        cleanup = True
        run(compose + ["up", "-d", "--wait", "--wait-timeout", "90", "--pull", "never", "db"])
        print("PASS isolated PostgreSQL 17 healthy; project=" + project, flush=True)
        migrate("up-to", "1")
        assert version() == "1"
        sql("INSERT INTO organizations (name) VALUES ('W1 upgrade sentinel')")
        if baseline:
            result = run(["go", "test", "-count=1", "-v", "./tests/integration", "-run", "^TestWeek02Schema$"], check=False)
            print(redacted(result.stdout + result.stderr), flush=True)
            # Baseline is intentionally a failing invocation, not a passing test.
            if result.returncode == 0 or "W02 schema missing" not in result.stdout + result.stderr:
                raise RuntimeError("baseline did not fail on the missing W02 schema as expected")
            return result.returncode

        migrate("up-to", "3")
        migrate("status")
        assert version() == "3"
        assert sql("SELECT count(*) FROM organizations WHERE name='W1 upgrade sentinel'") == "1"
        assert sql("SELECT to_regclass('public.users'), to_regclass('public.organization_members')") == "users|organization_members"
        migrate("up-to", "3")
        print("PASS W1 to W02 upgrade preserves existing organization; repeated Up is stable", flush=True)

        # Only this run's empty/fixture database may be rolled back.
        migrate("down")
        assert version() == "2"
        assert sql("SELECT to_regclass('public.organization_members') IS NULL, to_regclass('public.users') IS NOT NULL") == "t|t"
        migrate("down")
        assert version() == "1"
        assert sql("SELECT to_regclass('public.users') IS NULL") == "t"
        assert sql("SELECT count(*) FROM organizations WHERE name='W1 upgrade sentinel'") == "1"
        migrate("up-to", "3")
        assert version() == "3"
        print("PASS reverse Down 3 to 2 to 1, then Up to 3; W1 data retained", flush=True)
        if options.tickets:
            if options.ticket_schema_baseline:
                result = run(["go", "test", "-count=1", "-v", "./tests/integration", "-run", "^TestWeek03Schema$"], check=False)
                print(redacted(result.stdout + result.stderr), flush=True)
                if result.returncode == 0 or "W03 schema missing" not in result.stdout + result.stderr:
                    raise RuntimeError("W03 baseline did not fail for missing tables")
                return result.returncode
            migrate("up-to", "6")
            migrate("status")
            assert version() == "6"
            migrate("up-to", "6")
            for expected in ("5", "4", "3"):
                migrate("down")
                assert version() == expected
            assert sql("SELECT count(*) FROM organizations WHERE name='W1 upgrade sentinel'") == "1"
            migrate("up-to", "6")
            assert version() == "6"
            print("PASS W03 3 -> 6, repeated Up, reverse Down to 3, Up to 6; old organization retained", flush=True)
        result = run(["go", "test", "-race", "-count=1", "-v", "./..."], timeout=240)
        print(redacted(result.stdout + result.stderr), flush=True)
        print("PASS full race suite including real W1 connection and W02 schema tests", flush=True)
        if options.registration:
            registration_process_smoke()
        return 0
    finally:
        if cleanup:
            run(compose + ["down", "--timeout", "5"], timeout=40)
            label = run(["docker", "volume", "inspect", "--format",
                         '{{index .Labels "com.docker.compose.project"}}', volume], check=False)
            if label.returncode == 0:
                if label.stdout.strip() != project:
                    raise RuntimeError("refusing to remove a volume owned by another project")
                run(["docker", "volume", "rm", volume])
            print("CLEANUP only this run's test containers and labeled volume", flush=True)


if __name__ == "__main__":
    raise SystemExit(main())
