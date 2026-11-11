# NexusDesk W06 environment preparation

Completed 2026-11-11 01:10 Asia/Shanghai (2026-11-10 17:10 UTC). Scope: public artifact downloads, official PostgreSQL/pgvector image import, a newly imported BGE-M3 model, and an isolated environment probe. No production source edits, daily DB migration/backup, existing model pull/change/unload, Docker/Ollama restart, or global configuration changes.

## Official references used before import

- [Ollama v0.17.5 API documentation: GGUF create and blob upload](https://github.com/ollama/ollama/blob/v0.17.5/docs/api.md#create-a-model): upload each GGUF with `POST /api/blobs/:digest`, then create using a filename-to-SHA256 `files` map. Blob upload verifies the expected SHA256. The same document specifies `/api/embed`.
- [Current Ollama create API](https://docs.ollama.com/api/create): `files`, `license`, and `stream` request fields.
- [Official bge-m3:567m library entry](https://ollama.com/library/bge-m3:567m): BAAI BGE-M3, embedding, 567M, F16.
- [Official pgvector v0.8.7 release](https://github.com/pgvector/pgvector/releases/tag/v0.8.7) and [official version-pinned Dockerfile](https://github.com/pgvector/pgvector/blob/v0.8.7/Dockerfile): PostgreSQL image, v0.8.7 source, portable `OPTFLAGS=""`. A custom build was unnecessary because the official image was downloadable from the host.

## Ready: official pgvector Docker image

Use the **local tag** `pgvector/pgvector:0.8.7-pg17-trixie` or the image ID below. The official registry image was reconstructed as a Docker archive and loaded, so Docker may not list a RepoDigest; do not assume `tag@registry-digest` resolves locally without another network pull.

| Artifact | SHA-256 |
| --- | --- |
| Official multi-platform index | `01215e09bd4bd582d20230e025b6c971deeeffee49ceef1aae167e839cb62a7e` |
| Official linux/amd64 manifest | `5811bccb0b1bc9d8016bb9fe49c7fac1a96770005cc6d1a044f538009bdf2342` |
| Official config / loaded image ID | `0cbe3b07df64744bcaca35dbe785ec4ddca5b1d9023b5c0b31075280dec10ba6` |
| Saved Docker archive | `1f9f506d94da13a1b794b183649800b93e513f6745c91fc885ae8200081abe67` |

Archive: `/tmp/nexusdesk-week06-environment/pgvector-0.8.7-pg17-trixie-docker.tar`.

Every compressed layer/config was verified against the official manifest SHA256. Every uncompressed layer was verified against the config's rootfs diff ID before packing. Archive contains the original config and layers; no server binaries were rebuilt or replaced.

Actual isolated runtime verification (`postgres-verification.txt`):

- PostgreSQL `17.11 (Debian 17.11-1.pgdg13+2)`, linux/amd64.
- `CREATE EXTENSION vector` succeeded; installed extension version `0.8.7`.
- `vector(1024)` accepted two 1024-dimensional unit vectors.
- HNSW index with `vector_cosine_ops` created successfully.
- Cosine distance results `0` and `1`; EXPLAIN confirmed `Index Scan using embedding_probe_hnsw`.
- Probe used a uniquely named container, `--network none`, and tmpfs data. Container was removed in `finally`; no persistent DB volume was used.

## Ready: local BAAI BGE-M3

Ollama server remains `0.17.5`, `http://127.0.0.1:11434`. New model available as **`bge-m3:567m`**. Target was absent (`/api/show` HTTP 404) before creation. Import preserved the exact official F16 weights and MIT license; no quantization or model substitution.

| Artifact | SHA-256 |
| --- | --- |
| Official bge-m3:567m registry manifest | `7907646426070047a77226ac3e684fbbe8410524f7b4a74d02837e43f2146bab` |
| Official GGUF, 1,157,671,200 bytes | `daec91ffb5dd0c27411bd71f29932917c49cf529a641d0168496c3a501e3062c` |
| Official MIT license, 1,068 bytes | `a406579cd136771c705c521db86ca7d60a6f3de7c9b5460e6193a2df27861bde` |
| Official original model config | `0c4c9c2a325fb1cdafec606e6809cb745f1cb26a6d919994400d27372303e276` |
| Locally created model manifest | `eaba75fd71625c18c596bc988afb2bc654a3fdcc08b890eed0abcd642787ce24` |

The local manifest differs because `/api/create` emits a fresh config. Its weights are the exact verified official GGUF. Model metadata reports `bert`, `566.70M`, `F16`, and `bert.embedding_length=1024`.

Actual `/api/embed` input was three public, synthetic Chinese sentences:

1. `如何重置我的登录密码？`
2. `忘记密码时，可以在登录页面选择重置密码。`
3. `今天上海天气晴朗，适合出门散步。`

Results: 3 vectors, each 1024 dimensions; all components finite. Norms `1.0000004519459536`, `0.9999999973127311`, `0.999999970367737`. Related cosine similarity `0.8099245067525224`, unrelated `0.36549234231588473`. Total duration `4,285,348,803 ns`, load `3,999,724,864 ns`, prompt tokens `37`. These are real BGE-M3 outputs, not mocks. Application retrieval, authorization, chunking, generation, and the existing qwen model remain the root task's responsibility.

## Reproduction and retained artifacts

All scripts and evidence are under `/tmp/nexusdesk-week06-environment/`; no credentials were written to those files. Host Python/urllib inherited its normal networking behavior without printing/inspecting proxy values. Anonymous public Docker bearer tokens existed only in script memory.

```bash
# Fresh public downloads: complete-file SHA verification included.
python3 /tmp/nexusdesk-week06-environment/download.py docker
python3 /tmp/nexusdesk-week06-environment/download.py model

# Alternative faster/resumable model download: range chunks + full GGUF SHA verification.
python3 /tmp/nexusdesk-week06-environment/download_model_parts.py

docker load -i /tmp/nexusdesk-week06-environment/pgvector-0.8.7-pg17-trixie-docker.tar
python3 /tmp/nexusdesk-week06-environment/verify_image.py
python3 /tmp/nexusdesk-week06-environment/import_model.py

# Minimal real embedding API probe.
curl --fail --silent --show-error http://127.0.0.1:11434/api/embed \
  -H 'Content-Type: application/json' \
  -d '{"model":"bge-m3:567m","input":"如何重置登录密码？","truncate":false}'
```

`download.py` requires the retained official manifests `docker-manifest.json` and `ollama-manifest.json` in its directory; their fixed versions/digests are above. It obtains a fresh anonymous token itself. The range downloader needs no manifest input and validates its pinned full GGUF digest. `import_model.py` verifies GGUF size/SHA before upload, preserves an already existing target model rather than overwriting it, and probes its dimensions/semantic ordering.

Additional evidence: `docker-amd64.json`, original layer `.blob` files, `docker-archive/`, `bge-m3-f16.gguf`, `bge-m3-LICENSE`, `ollama-original-config.json`, `ollama-create-request.json`, `ollama-create-response.json`, `ollama-model-info.json`, `ollama-local-model-metadata.json`, `ollama-embedding-probe.json`, `public-probe-embeddings.json`, and `postgres-verification.txt`. The last JSON contains only the three synthetic sentences above and their real embeddings.

## Actual failures and limits

The root agent reported native Docker pull and native Ollama pull failed with TLS unexpected EOF. Those daemon paths were not retried or reconfigured here. Host public downloads succeeded, with transient urllib network errors handled through bounded retries/ranged resume. The initial scratch download script used Python 3.11 `hashlib.file_digest`, unavailable in local Python 3.10; it was replaced with streaming SHA256, the archive and model were fully reverified, and the corrected scripts completed. No success is claimed for the failing native pull path.

Only environment preparation was verified. No application-level W06 acceptance or daily DB modification is claimed by this report. The official source tarball v0.8.7 was downloaded as an unused build fallback (`pgvector.tar.gz`, SHA256 `cac0b10c360f05b2d521200105ba3697e773d4cd3731f5a915a7e37ebe0bea85`). It was not built or installed.
