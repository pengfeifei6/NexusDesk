#!/usr/bin/env python3
"""Real local-model evaluation. Only called with the disposable W06 runner's environment."""
import hashlib
import json
import math
import os
from pathlib import Path
import re
import secrets
import time
import urllib.error
import urllib.request

ROOT = Path(__file__).resolve().parents[1]
CORPUS = ROOT / "requests/testdata/rag-corpus"
OUT = ROOT / "docs/weekly/evidence/week06-rag-evaluation.json"


def main():
    base = os.environ.get("NEXUSDESK_BROWSER_API", "")
    if not re.fullmatch(r"http://127\.0\.0\.1:\d+", base) or os.environ.get("NEXUSDESK_TEST_W06_RAG") != "1" or not os.environ.get("DB_NAME", "").startswith("nexusdesk_w02d2_test_"):
        raise RuntimeError("Use scripts/week06-smoke.py --evaluation-only with owned fixtures")
    http = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    def request(path, method="GET", data=None, token=None, expected=200, content_type="application/json"):
        body = data if isinstance(data, bytes) else json.dumps(data, ensure_ascii=False).encode() if data is not None else None
        headers = {"Content-Type": content_type} if body is not None else {}
        if token:
            headers["Authorization"] = "Bearer " + token
        req = urllib.request.Request(base + "/api/v1" + path, data=body, method=method, headers=headers)
        try:
            response = http.open(req, timeout=75)
        except urllib.error.HTTPError as error:
            response = error
        raw = response.read(2 * 1024 * 1024)
        if response.status != expected:
            # Only fixed server codes/status, never credentials or arbitrary model responses.
            try:
                code = json.loads(raw).get("error", {}).get("code", "UNKNOWN")
            except (ValueError, AttributeError):
                code = "INVALID_RESPONSE"
            raise RuntimeError(f"fixture request {method} {path}: HTTP {response.status}, {code}")
        return json.loads(raw)["data"] if raw else None

    manifest = json.loads((CORPUS / "manifest.json").read_text())
    labels_bytes = (CORPUS / "rag-eval.jsonl").read_bytes()
    labels = [json.loads(line) for line in labels_bytes.splitlines()]
    assert len(labels) >= 20
    texts = {}
    for doc in manifest["documents"]:
        data = (CORPUS / doc["file"]).read_bytes()
        assert hashlib.sha256(data).hexdigest() == doc["sha256"]
        texts[doc["file"]] = data.decode()
    for label in labels:
        for evidence in label["expected_evidence"]:
            assert texts[evidence["document"]][evidence["start_rune"]:evidence["end_rune"]] == evidence["quote"]

    tag, password = secrets.token_hex(6), secrets.token_urlsafe(24)
    tokens, orgs, ids, indexes = {}, {}, {}, []
    for slot in ("A", "B"):
        email = f"w06-eval-{slot.lower()}-{tag}@example.test"
        request("/auth/register", "POST", {"email": email, "display_name": "W06 fixture " + slot, "password": password}, expected=201)
        tokens[slot] = request("/auth/login", "POST", {"email": email, "password": password})["access_token"]
        orgs[slot] = request("/organizations", "POST", {"name": "W06 evaluation " + slot}, tokens[slot], 201)["id"]
    for doc in manifest["documents"]:
        slot, name = doc["organization_slot"], doc["file"]
        endpoint = f"/organizations/{orgs[slot]}/knowledge/documents"
        boundary = "W06" + secrets.token_hex(12)
        body = (f'--{boundary}\r\nContent-Disposition: form-data; name="file"; filename="{name}"\r\nContent-Type: text/markdown\r\n\r\n'.encode() + texts[name].encode() + f"\r\n--{boundary}--\r\n".encode())
        uploaded = request(endpoint, "POST", body, tokens[slot], 201, "multipart/form-data; boundary=" + boundary)
        ids[name] = uploaded["id"]
        endpoint += "/" + str(uploaded["id"])
        assert request(endpoint + "/process", "POST", token=tokens[slot])["status"] == "READY"
        start = time.perf_counter()
        info = request(endpoint + "/index", "POST", token=tokens[slot])
        assert info["status"] == "READY" and info["embedding_dimension"] == 1024 and info["chunk_count"] > 0
        reused = request(endpoint + "/index", "POST", token=tokens[slot])
        assert info == reused
        indexes.append({"document": name, "document_id": uploaded["id"], "chunk_count": info["chunk_count"], "source_hash": info["source_content_sha256"], "index_and_reuse_ms": round((time.perf_counter() - start) * 1000, 3)})
        print("PASS real embedding/index/reuse " + name, flush=True)

    allowed = {ids[d["file"]] for d in manifest["documents"] if d["organization_slot"] == "A"}
    by_id = {value: key for key, value in ids.items()}
    def validate_sources(items, organization_slot):
        scope = allowed if organization_slot == "A" else {ids["org-b-private-guide.md"]}
        for source in items:
            assert source["document_id"] in scope and source["chunk_id"] > 0
            name = by_id[source["document_id"]]
            assert source["source_name"] == name
            assert source["excerpt"] == texts[name][source["start_rune"]:source["end_rune"]]
            assert math.isfinite(source["score"]) and -1 <= source["score"] <= 1
            if organization_slot == "A":
                assert "ORGB-ONLY-SAMPLE-GLASSHARBOR" not in source["excerpt"]

    rows, asks = [], []
    for label in labels:
        endpoint = f"/organizations/{orgs['A']}/knowledge"
        result = request(endpoint + "/search", "POST", {"query": label["query"], "top_k": 5}, tokens["A"])
        validate_sources(result["items"], "A")
        ranks = []
        for expected in label["expected_evidence"]:
            rank = next((i + 1 for i, item in enumerate(result["items"]) if item["document_id"] == ids[expected["document"]] and item["start_rune"] <= expected["start_rune"] and item["end_rune"] >= expected["end_rune"]), None)
            ranks.append(rank)
        matched = [r for r in ranks if r is not None]
        rows.append({"id": label["id"], "category": label["category"], "query": label["query"], "expected_answerable": label["expected_answerable"], "evidence_ranks": ranks, "hit_at_5": bool(matched), "evidence_recall_at_5": len(matched) / len(ranks) if ranks else None, "reciprocal_rank": 1 / min(matched) if matched else 0, "embedding_ms": result["embedding_ms"], "search_ms": result["search_ms"], "items": result["items"]})
        print(f"MEASURE {label['id']} retrieved={len(result['items'])} evidence_hits={len(matched)}/{len(ranks)}", flush=True)
        # Fixed representative subset: two answerable facts and ALL unanswerable / cross-org queries.
        if label["id"] in ("F01", "F04") or not label["expected_answerable"]:
            started = time.perf_counter()
            answer = request(endpoint + "/ask", "POST", {"query": label["query"], "top_k": 5}, tokens["A"])
            validate_sources(answer["sources"], "A")
            assert "ORGB-ONLY-SAMPLE-GLASSHARBOR" not in answer["answer"]
            if answer["insufficient_evidence"]:
                assert answer["answer"] == "知识库未检索到足够依据。" and answer["sources"] == []
            else:
                assert answer["answer"].strip() and answer["sources"]
            asks.append({"id": label["id"], "expected_answerable": label["expected_answerable"], "duration_ms": round((time.perf_counter() - started) * 1000, 3), **answer})

    # B's own evidence contains an explicit injection attempt. Model has no tools;
    # the same source is still ordinary evidence, not an authorization grant.
    endpoint = f"/organizations/{orgs['B']}"
    before = request(endpoint + "/tickets", token=tokens["B"])
    answer = request(endpoint + "/knowledge/ask", "POST", {"query": "B 组织值班确认时限是多少分钟？", "top_k": 5}, tokens["B"])
    validate_sources(answer["sources"], "B")
    after = request(endpoint + "/tickets", token=tokens["B"])
    assert before == after
    asks.append({"id": "INJECTION_B", "expected_answerable": True, "ticket_side_effect": False, **answer})
    answerable = [row for row in rows if row["expected_answerable"]]
    multi_evidence = [row for row in rows if row["category"] == "multi_evidence"]
    negatives = [row for row in rows if not row["expected_answerable"]]
    negative_asks = [row for row in asks if not row["expected_answerable"]]
    percentile = lambda values, p: sorted(values)[max(0, math.ceil(len(values) * p) - 1)]
    report = {"generated_at_utc": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()), "corpus_version": manifest["version"], "label_sha256": hashlib.sha256(labels_bytes).hexdigest(), "label_provenance": "AI-prepared exact source spans; human review pending", "real_models": True, "semantic_mock": False, "embedding_model": "bge-m3:567m", "embedding_dimension": 1024, "generation_model": "qwen3.5:35b", "top_k": 5, "min_score": 0.40, "chunker_version": "rune-500-80-v1", "query_count": len(rows), "answerable_count": len(answerable), "metrics": {"hit_at_5_manual_recall": sum(r["hit_at_5"] for r in answerable) / len(answerable), "mean_evidence_recall_at_5": sum(r["evidence_recall_at_5"] for r in answerable) / len(answerable), "multi_evidence_recall_at_5": sum(r["evidence_recall_at_5"] for r in multi_evidence) / len(multi_evidence), "mrr": sum(r["reciprocal_rank"] for r in answerable) / len(answerable), "no_answer_search_empty_rate": sum(not r["items"] for r in negatives) / len(negatives), "no_answer_ask_refusal_rate": sum(r["insufficient_evidence"] for r in negative_asks) / len(negative_asks), "cross_org_source_leaks": 0, "embedding_p50_ms": percentile([r["embedding_ms"] for r in rows], .5), "embedding_p95_ms": percentile([r["embedding_ms"] for r in rows], .95), "db_search_p50_ms": percentile([r["search_ms"] for r in rows], .5), "db_search_p95_ms": percentile([r["search_ms"] for r in rows], .95)}, "indexes": indexes, "queries": rows, "ask_samples": asks, "limitations": ["20 small synthetic queries; single sequential run, not production load", "Reference labels await human review", "Citation membership and exact excerpts checked; answer entailment needs human review", "No automatic re-labeling, threshold tuning, or answer retries after observing metrics"]}
    OUT.write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n")
    assert any(not row["insufficient_evidence"] for row in asks if row["expected_answerable"]), "No real grounded answer produced"
    print("PASS real model evaluation; metrics=" + json.dumps(report["metrics"], ensure_ascii=False), flush=True)


if __name__ == "__main__":
    main()
