#!/usr/bin/env python3
"""Owned W07 process fault contracts; fake vectors are NOT semantic evidence.

Requires the disposable runner's gate, loopback API/DB/Redis, compose project,
private synthetic storage and already-running API. Never reads .env or flushes
Redis. Actual OS signals kill only child Worker PIDs created by this script.
SQL lease/attempt time injection accelerates recovery after those real kills.
"""
import http.server
import ipaddress
import json
import os
from pathlib import Path
import re
import secrets
import signal
import subprocess
import sys
import tempfile
import threading
import time
import urllib.error
import urllib.parse
import urllib.request

ROOT = Path(__file__).resolve().parents[1]
EVIDENCE = ROOT / "docs/weekly/evidence/week07-process.json"


class ContractError(RuntimeError):
    """Fixed safe diagnostic; never wraps library error text."""


def require(value, message):
    if not value:
        raise ContractError(message)


def failure_location(error):
    # Report code location only: library exception text can contain credentials.
    trace = error.__traceback__
    while trace is not None and trace.tb_next is not None:
        trace = trace.tb_next
    return {"function": trace.tb_frame.f_code.co_name, "line": trace.tb_lineno} if trace else None


def run_suite(evidence):
    env = dict(os.environ)
    require(env.get("NEXUSDESK_TEST_W07_JOBS") == "1", "owned W07 gate required")
    project = env.get("NEXUSDESK_TEST_COMPOSE_PROJECT", "")
    require(re.fullmatch(r"nexusdesk-w02d2-[a-f0-9]{12}", project), "owned project guard failed")
    # The full project regex above verifies this prefix before slicing (Python 3.8).
    suffix = project[len("nexusdesk-w02d2-"):]
    require(env.get("DB_NAME") == "nexusdesk_w02d2_test_" + suffix, "disposable database guard failed")
    require(ipaddress.ip_address(env.get("DB_HOST", "0.0.0.0")).is_loopback, "database must be loopback")
    base = env.get("NEXUSDESK_BROWSER_API", "")
    parsed = urllib.parse.urlparse(base)
    require(parsed.scheme == "http" and parsed.hostname and ipaddress.ip_address(parsed.hostname).is_loopback
            and parsed.port and parsed.path in ("", "/") and not parsed.username
            and not parsed.query and not parsed.fragment, "explicit owned API origin required")
    base = base.rstrip("/")
    redis = urllib.parse.urlparse("redis://" + env.get("REDIS_ADDR", ""))
    require(redis.hostname and ipaddress.ip_address(redis.hostname).is_loopback and redis.port
            and env.get("REDIS_DB") == "6" and not env.get("REDIS_PASSWORD"), "owned Redis guard failed")
    storage = Path(env.get("STORAGE_ROOT", "/not-owned")).resolve()
    require(storage.name == "storage" and storage.parent.name.startswith("nexusdesk-w02d3-api-")
            and storage.is_dir() and storage.stat().st_mode & 0o077 == 0, "private synthetic storage guard failed")
    compose = ["docker", "compose", "--env-file", "/dev/null", "-p", project,
               "-f", str(ROOT / "deploy/compose.yaml"),
               "-f", str(ROOT / "deploy/compose.pgvector.yaml"),
               "-f", str(ROOT / "deploy/compose.redis.yaml")]

    def run(command, timeout=20):
        result = subprocess.run(command, cwd=ROOT, env=env, capture_output=True, text=True, timeout=timeout)
        require(result.returncode == 0, "owned subprocess operation failed")
        return result.stdout.strip()

    def sql(statement):
        return run(compose + ["exec", "-T", "db", "psql", "-U", env["DB_USER"], "-d", env["DB_NAME"],
                              "-v", "ON_ERROR_STOP=1", "-Atqc", statement])

    require(sql("SELECT version_id FROM goose_db_version WHERE is_applied ORDER BY id DESC LIMIT 1") == "11",
            "process tests require schema 11")
    redis_id = run(compose + ["ps", "-q", "redis"])
    require(re.fullmatch(r"[a-f0-9]{64}", redis_id), "owned Redis container identity missing")

    def redis_action(action):
        inspected = json.loads(run(["docker", "inspect", redis_id]))
        labels = inspected[0]["Config"]["Labels"]
        require(inspected[0]["Id"] == redis_id and labels.get("com.docker.compose.project") == project
                and labels.get("com.docker.compose.service") == "redis", "Redis mutation ownership guard failed")
        run(["docker", action, redis_id], timeout=20)

    def restore_redis():
        redis_action("start")
        deadline = time.monotonic() + 10
        while True:
            try:
                inspected = json.loads(run(["docker", "inspect", redis_id]))
                require(inspected[0]["Id"] == redis_id and inspected[0]["State"]["Running"],
                        "owned Redis is not running")
                require(run(["docker", "exec", redis_id, "redis-cli", "PING"], timeout=2) == "PONG",
                        "owned Redis is not ready")
                return
            except (ContractError, subprocess.TimeoutExpired):
                require(time.monotonic() < deadline, "owned Redis restart deadline exceeded")
                time.sleep(0.1)

    http_client = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    token = ""
    password = secrets.token_urlsafe(32)

    def request(method, path, payload=None, expected=200, raw=None, content_type=None):
        headers = {}
        if token:
            headers["Authorization"] = "Bearer " + token
        if payload is not None:
            raw = json.dumps(payload).encode()
            content_type = "application/json"
        if content_type:
            headers["Content-Type"] = content_type
        req = urllib.request.Request(base + path, data=raw, headers=headers, method=method)
        try:
            response = http_client.open(req, timeout=8)
        except urllib.error.HTTPError as error:
            response = error
        with response:
            body = response.read()
            require(response.status == expected, "API contract status mismatch")
            require(password.encode() not in body, "API leaked synthetic password")
            return json.loads(body) if body else None

    email = "w07-process-" + secrets.token_hex(6) + "@example.test"
    request("POST", "/api/v1/auth/register", {"email": email, "display_name": "W07 synthetic process", "password": password}, 201)
    token = request("POST", "/api/v1/auth/login", {"email": email, "password": password})["data"]["access_token"]
    org = request("POST", "/api/v1/organizations", {"name": "W07 owned process fixture"}, 201)["data"]["id"]
    prefix = "/api/v1/organizations/" + str(org)
    records = evidence["cases"]
    model_states = {}
    state_lock = threading.Lock()

    class Model(http.server.BaseHTTPRequestHandler):
        def log_message(self, *_args):
            pass

        def do_POST(self):
            if self.path != "/api/embed":
                self.send_error(404)
                return
            try:
                payload = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
                marker = payload["input"][0].split()[0]
                with state_lock:
                    state = model_states[marker]
                    state["calls"] += 1
                    state["call_times"].append(time.monotonic())
                    call = state["calls"]
                    state["entered"].set()
                if not state["release"].wait(45):
                    return
                code = 503 if call <= state["failures"] else 200
                body = {"error": "synthetic unavailable"} if code != 200 else {
                    "model": "bge-m3:567m", "embeddings": [[1.0] + [0.0] * 1023 for _ in payload["input"]]}
                data = json.dumps(body).encode()
                self.send_response(code)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(data)))
                self.end_headers()
                self.wfile.write(data)
            except (BrokenPipeError, ConnectionResetError):
                pass
            except Exception:
                # Never print input text, tokens, bodies, or upstream details.
                self.close_connection = True

    model = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Model)
    model.daemon_threads = True
    model_thread = threading.Thread(target=model.serve_forever, daemon=True)
    model_thread.start()
    worker = None
    worker_log = None
    stopped_redis = False
    trigger_name = None

    def fixture(case, blocked=False, failures=0):
        evidence["current_case"] = case
        marker = "W07" + secrets.token_hex(8)
        state = {"calls": 0, "call_times": [], "entered": threading.Event(),
                 "release": threading.Event(), "failures": failures}
        if not blocked:
            state["release"].set()
        with state_lock:
            model_states[marker] = state
        boundary = "w07" + secrets.token_hex(12)
        body = ("--" + boundary + '\r\nContent-Disposition: form-data; name="file"; filename="synthetic.md"\r\n'
                "Content-Type: text/markdown\r\n\r\n" + marker + " synthetic process " + case + "\r\n--" + boundary + "--\r\n").encode()
        doc = request("POST", prefix + "/knowledge/documents", expected=201, raw=body,
                      content_type="multipart/form-data; boundary=" + boundary)["data"]
        job = request("POST", prefix + "/knowledge/documents/" + str(doc["id"]) + "/jobs", expected=202)["data"]
        return doc, job, state

    def get(job):
        return request("GET", prefix + "/jobs/" + str(job["id"]))["data"]

    def await_job(job, expected="SUCCEEDED", timeout=65):
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            value = get(job)
            if value["status"] == expected:
                return value
            require(value["status"] not in ("FAILED", "CANCELED"), "process job unexpectedly terminal")
            require(worker is None or worker.poll() is None, "owned Worker exited early")
            time.sleep(0.12)
        raise ContractError("process job convergence deadline exceeded")

    def published_once(doc):
        docid = int(doc["id"])
        require(sql(f"SELECT count(*) FROM knowledge_document_texts WHERE org_id={int(org)} AND document_id={docid}") == "1", "text publication count mismatch")
        require(sql(f"SELECT count(*) FROM knowledge_chunks WHERE org_id={int(org)} AND document_id={docid}") == "1", "chunk publication count mismatch")
        require(sql(f"SELECT count(*) FROM knowledge_document_indexes WHERE org_id={int(org)} AND document_id={docid} AND status='READY' AND chunk_count=1") == "1", "index publication count mismatch")

    def pass_case(case, started, **counts):
        records.append({"case": case, "status": "PASS", "seconds": round(time.monotonic()-started, 3), **counts})
        print("PASS W07 process " + case, flush=True)

    evidence["current_case"] = "build_worker"
    try:
        with tempfile.TemporaryDirectory(prefix="nexusdesk-week07-worker-", dir="/var/tmp") as temporary:
            binary = str(Path(temporary) / "worker")
            run(["go", "build", "-o", binary, "./cmd/worker"], timeout=120)
            worker_env = {key: value for key, value in env.items() if not key.startswith("JWT") and key != "HTTP_ADDR"}
            worker_app = "nexusdesk-week07-process-" + suffix
            worker_env.update(PGAPPNAME=worker_app, OLLAMA_BASE_URL="http://127.0.0.1:" + str(model.server_port), WORKER_CONCURRENCY="1")
            require(not any(key.startswith("JWT") or key == "HTTP_ADDR" for key in worker_env), "Worker signing configuration isolation failed")
            worker_log = open(Path(temporary) / "worker.log", "w+b")

            def start_worker():
                nonlocal worker
                require(worker is None, "owned Worker already active")
                worker = subprocess.Popen([binary], cwd=ROOT, env=worker_env, stdout=worker_log, stderr=subprocess.STDOUT)
                require(worker.pid != os.getpid(), "Worker was not an independent PID")

            def stop_worker(sig=signal.SIGTERM):
                nonlocal worker
                if worker is None:
                    return 0.0
                started = time.monotonic()
                worker.send_signal(sig)
                try:
                    worker.wait(timeout=12)
                except subprocess.TimeoutExpired:
                    worker.kill()
                    worker.wait(timeout=5)
                    worker = None
                    raise ContractError("graceful Worker shutdown exceeded 12 seconds")
                if sig == signal.SIGKILL:
                    require(worker.returncode == -signal.SIGKILL, "actual SIGKILL not observed")
                else:
                    require(worker.returncode == 0, "Worker graceful exit failed")
                observed = signal.Signals(sig).name
                if observed not in evidence["actual_signals"]:
                    evidence["actual_signals"].append(observed)
                worker = None
                return round(time.monotonic()-started, 3)

            # No Worker exists: durable acceptance is observable before any model request.
            started = time.monotonic()
            doc, job, state = fixture("absent_worker")
            time.sleep(0.25)
            pending = get(job)
            require(pending["status"] == "PENDING" and pending["attempts"] == 0 and state["calls"] == 0,
                    "HTTP submission launched background execution")
            start_worker()
            final = await_job(job)
            published_once(doc)
            require(final["attempts"] == 1 and state["calls"] == 1, "first process delivery duplicated")
            pass_case("accepted_without_worker_then_independent_no_jwt_worker", started, attempts=1, model_calls=1)
            stop_worker()

            started = time.monotonic()
            doc, job, state = fixture("sigkill_embed", blocked=True)
            start_worker()
            require(state["entered"].wait(15), "SIGKILL model barrier not reached")
            require(get(job)["status"] == "RUNNING", "Worker did not claim before embedding")
            stop_worker(signal.SIGKILL)
            state["release"].set()
            # Actual killed process, accelerated lease clock only (not a simulated kill).
            require(sql(f"UPDATE knowledge_jobs SET lease_until=clock_timestamp()-interval '1 second' WHERE id={int(job['id'])} AND status='RUNNING' RETURNING id") == str(job["id"]),
                    "killed attempt lease injection did not update one row")
            evidence["fixture_injection"].append("expired lease time after embedding SIGKILL")
            start_worker()
            final = await_job(job)
            published_once(doc)
            require(final["attempts"] == 2 and state["calls"] == 2, "SIGKILL recovery attempt count mismatch")
            pass_case("sigkill_during_embedding_restart", started, attempts=final["attempts"], model_calls=state["calls"], published_chunks=1)
            stop_worker()

            started = time.monotonic()
            doc, job, state = fixture("sigterm_embed", blocked=True)
            start_worker()
            require(state["entered"].wait(15), "SIGTERM model barrier not reached")
            shutdown_seconds = stop_worker()
            state["release"].set()
            start_worker()
            final = await_job(job)
            published_once(doc)
            require(final["attempts"] == 2 and state["calls"] == 2, "SIGTERM recovery attempt count mismatch")
            pass_case("sigterm_bounded_then_restart", started, shutdown_seconds=shutdown_seconds, attempts=2, model_calls=2)
            stop_worker()

            started = time.monotonic()
            doc, job, state = fixture("transient_model", failures=2)
            start_worker()
            final = await_job(job, timeout=65)
            published_once(doc)
            require(final["attempts"] == 3 and state["calls"] == 3, "transient retries exceeded DB attempt limit")
            with state_lock:
                call_times = list(state["call_times"])
            require(len(call_times) == 3, "transient model timing sample count mismatch")
            intervals = [later - earlier for earlier, later in zip(call_times, call_times[1:])]
            # Allow at most 200 ms at the external HTTP observation boundary;
            # immediate retries must fail even when total attempt counts are correct.
            require(intervals[0] >= 5.0 - 0.2 and intervals[1] >= 30.0 - 0.2,
                    "transient model retries bypassed 5s/30s backoff")
            pass_case("transient_model_real_5s_30s_backoff", started, attempts=3, model_calls=3,
                      intercall_seconds=[round(interval, 3) for interval in intervals],
                      timing_tolerance_seconds=0.2)
            stop_worker()

            started = time.monotonic()
            evidence["current_case"] = "redis_outage"
            stopped_redis = True  # Intent precedes stop: timeout can follow a successful stop.
            redis_action("stop")
            doc, job, state = fixture("redis_outage")
            require(get(job)["status"] == "PENDING", "Redis outage lost durable accepted job")
            request("GET", "/api/v1/organizations")
            request("GET", prefix + "/knowledge/documents")
            restore_redis()
            stopped_redis = False
            start_worker()
            final = await_job(job)
            published_once(doc)
            pass_case("redis_outage_api_202_business_alive_restart", started, attempts=final["attempts"], model_calls=state["calls"])
            stop_worker()

            # Persisted READY publication followed by a real kill before job acknowledgement.
            # The SQL trigger only widens the acknowledgement window. Attempts=2 is
            # deterministic fixture injection so the killed actual attempt is exactly 3.
            started = time.monotonic()
            doc, job, state = fixture("postpublication_attempt3")
            jobid = int(job["id"])
            trigger_name = "nexusdesk_w07_ack_" + str(jobid)
            require(sql(f"UPDATE knowledge_jobs SET attempts=2,total_attempts=2 WHERE id={jobid} RETURNING id") == str(jobid),
                    "attempt count injection did not update one row")
            evidence["fixture_injection"].append("attempts=2 before actual third attempt")
            sql(f"CREATE FUNCTION {trigger_name}() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_sleep(30); RETURN NEW; END $$; CREATE TRIGGER {trigger_name} BEFORE UPDATE ON knowledge_jobs FOR EACH ROW WHEN (NEW.id={jobid} AND NEW.status='SUCCEEDED') EXECUTE FUNCTION {trigger_name}()")
            evidence["fixture_injection"].append("scoped acknowledgement delay trigger")
            start_worker()
            deadline = time.monotonic() + 15
            while sql(f"SELECT count(*) FROM knowledge_document_indexes WHERE document_id={int(doc['id'])} AND status='READY'") != "1":
                require(time.monotonic() < deadline, "READY pre-ack barrier not reached")
                time.sleep(0.08)
            require(get(job)["status"] == "RUNNING", "postpublication acknowledgement window missed")
            stop_worker(signal.SIGKILL)
            # PostgreSQL notices the dead client; bound DDL to avoid hiding a stuck session.
            # pg_sleep may continue after client death, so cancel ONLY the owned fixture
            # statement for this function/job using backend query text and database guard.
            sql(f"SELECT pg_cancel_backend(pid) FROM pg_stat_activity WHERE datname=current_database() AND application_name='{worker_app}' AND pid<>pg_backend_pid() AND query LIKE '%UPDATE knowledge_jobs SET status=$3%' AND state='active'")
            sql(f"SET lock_timeout='5s'; DROP TRIGGER {trigger_name} ON knowledge_jobs; DROP FUNCTION {trigger_name}()")
            trigger_name = None
            require(sql(f"UPDATE knowledge_jobs SET lease_until=clock_timestamp()-interval '1 second' WHERE id={jobid} AND status='RUNNING' RETURNING id") == str(jobid),
                    "postpublication lease injection did not update one row")
            evidence["fixture_injection"].append("expired lease time after postpublication SIGKILL")
            start_worker()
            final = await_job(job)
            published_once(doc)
            require(final["attempts"] == 3 and final["total_attempts"] == 3 and state["calls"] == 1,
                    "postpublication recovery performed fourth attempt/model call")
            pass_case("sigkill_after_ready_attempt3_reconciles_without_fourth", started, attempts=3, model_calls=1, published_chunks=1)
            stop_worker()

            evidence["current_case"] = "worker_log_privacy"
            worker_log.flush()
            worker_log.seek(0)
            logs = worker_log.read()
            for secret in (password, token, env.get("DB_PASSWORD", ""), env.get("JWT_SECRET_BASE64", "")):
                if secret:
                    require(secret.encode() not in logs, "Worker log secret disclosure")
            for marker in model_states:
                require(marker.encode() not in logs, "Worker log source disclosure")
            pass_case("worker_logs_no_credentials_or_document_text", time.monotonic(), checked_workers=9)
    finally:
        primary_failure = sys.exc_info()[0] is not None
        cleanup_failures = []

        def cleanup(label, action):
            try:
                action()
            except Exception:
                # Labels are fixed locally; library details may contain credentials.
                cleanup_failures.append(label)
                evidence["cases"].append({"case": "cleanup_" + label, "status": "FAIL",
                                          "failure": "owned cleanup failed"})
                print("FAIL W07 cleanup " + label, flush=True)

        def kill_child():
            if worker is not None and worker.poll() is None:
                worker.kill()
            if worker is not None:
                worker.wait(timeout=5)

        cleanup("worker", kill_child)
        if stopped_redis:
            cleanup("redis_restore_and_readiness", restore_redis)
        for state in model_states.values():
            cleanup("model_release", state["release"].set)
        cleanup("model_shutdown", model.shutdown)
        cleanup("model_close", model.server_close)
        cleanup("model_thread", lambda: model_thread.join(timeout=3))
        if worker_log:
            cleanup("worker_log", worker_log.close)
        if trigger_name:
            cleanup("acknowledgement_fixture", lambda: sql(
                f"SET lock_timeout='2s'; DROP TRIGGER IF EXISTS {trigger_name} ON knowledge_jobs; DROP FUNCTION IF EXISTS {trigger_name}()"))
        evidence["cleanup_failures"] = cleanup_failures
        if cleanup_failures and not primary_failure:
            evidence["current_case"] = "cleanup"
            raise ContractError("owned process cleanup failed")


def main():
    evidence = {"suite": "W07 owned process fault contracts", "status": "RUNNING",
                "fake_model": True, "semantic_evidence": False, "actual_signals": [],
                "fixture_injection": [], "cases": [], "current_case": "setup",
                "failed_case": None, "cleanup_failures": []}
    try:
        run_suite(evidence)
        evidence["status"] = "PASS"
        evidence["current_case"] = None
    except Exception as error:
        evidence["status"] = "FAIL"
        evidence["failed_case"] = evidence["current_case"]
        detail = str(error) if isinstance(error, ContractError) else type(error).__name__
        evidence["cases"].append({"case": evidence["current_case"], "status": "FAIL", "failure": detail,
                                  "location": failure_location(error)})
        raise
    finally:
        primary_failure = sys.exc_info()[0] is not None
        try:
            EVIDENCE.parent.mkdir(parents=True, exist_ok=True)
            EVIDENCE.write_text(json.dumps(evidence, indent=2) + "\n")
        except Exception:
            print("FAIL W07 evidence write", flush=True)
            if not primary_failure:
                raise ContractError("safe process evidence could not be written")


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        # Only our fixed ContractError messages are safe; library text is omitted.
        detail = str(error) if isinstance(error, ContractError) else type(error).__name__
        location = failure_location(error)
        where = " at " + location["function"] + ":" + str(location["line"]) if location else ""
        print("FAIL W07 process contract: " + detail + where, flush=True)
        raise SystemExit(1)
