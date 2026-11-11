#!/usr/bin/env python3
"""Preload this release's local models; no credentials, documents or daemon changes."""
import json
import time
import urllib.request

http = urllib.request.build_opener(urllib.request.ProxyHandler({}))
requests = [
    ("embed", {"model": "bge-m3:567m", "input": ["公开测试：准备知识库检索。"], "truncate": False, "keep_alive": "15m"}),
    ("chat", {"model": "qwen3.5:35b", "messages": [{"role": "user", "content": "公开测试：请只回答准备好了。"}], "stream": False, "think": False, "keep_alive": "15m", "options": {"num_ctx": 16384, "num_predict": 16, "temperature": 0}}),
]
for path, payload in requests:
    start = time.monotonic()
    req = urllib.request.Request("http://127.0.0.1:11434/api/" + path, data=json.dumps(payload).encode(), headers={"Content-Type": "application/json"})
    try:
        with http.open(req, timeout=180) as response:
            raw = response.read(2 * 1024 * 1024 + 1)
            assert len(raw) <= 2 * 1024 * 1024
            result = json.loads(raw)
        assert result["model"] == payload["model"]
        if path == "embed":
            assert len(result["embeddings"]) == 1 and len(result["embeddings"][0]) == 1024
        else:
            assert result["done"] and result["message"]["role"] == "assistant"
    except Exception as error:
        raise SystemExit("Local model warmup failed: " + payload["model"] + " (" + type(error).__name__ + "); check Ollama and installed models") from None
    print(json.dumps({"model": payload["model"], "elapsed_seconds": round(time.monotonic() - start, 3), "load_duration_ns": result.get("load_duration"), "prompt_eval_count": result.get("prompt_eval_count"), "eval_count": result.get("eval_count"), "warmup_excluded_from_retrieval_metrics": True}), flush=True)
