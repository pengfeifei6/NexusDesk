# 第九周运行与查看指南

第八周依赖与第九周完整代码均在原主目录，后续开发、测试和启动使用同一个位置：

```bash
cd /media/yjz/pzp/Go/projects/NexusDesk
git branch --show-current
# main（第八周/第九周成果保留为未提交工作区改动）
python3 scripts/dev.py status
```

直接在本目录查看代码和运行程序即可。需要保存 Git 版本时先检查改动，再自行提交；代理没有自动提交或推送。`.env`、本机 Node 依赖与私有存储继续使用原主目录中的文件，秘密不写入源码。

## 启动

日常依赖是原 PostgreSQL（pgvector）、专用 Redis 6380/DB6、本地 Ollama/BGE。最新迁移 14。日常库只备份后 Up，不能用测试脚本对日常库 Down/删卷。实际本机升级和启动状态见 `../testing/evidence/week09/runtime.json`。

三个终端分别执行，已有对应进程在运行时不要再重复占端口：

```bash
python3 scripts/dev.py api
python3 scripts/dev.py worker
python3 scripts/dev.py web
```

浏览器访问 `http://127.0.0.1:5173`，API 为 `http://127.0.0.1:8080`。此启动器安全读取 .env；直接 `go run ./cmd/api` 仍不会自动读取它。AI 继续用已有 DeepSeek 配置，向量检索继续本地 BGE。

## 看什么

1. 登录并选择组织，打开工单/知识列表，刷新相同分页和筛选即可使用缓存；界面无需新增“开启缓存”按钮。
2. 新建、领取或修改工单后刷新，看到最新数据；知识文档异步处理完成后看到新状态。底层版本表覆盖 API、Worker、AI 确认写入。
3. AI 对话默认同用户同组织 60 秒最多 10 次发送。超过时显示限流提示和等待秒数，保留输入；确认/拒绝、历史不受发送额度限制。
4. Redis 中断时列表可回源，AI 新发送返回配额服务不可用的 503。不要为了演示停止日常 Redis；下一节脚本会在隔离环境自动验证。

## 可重跑验收（不花 DeepSeek 调用费用）

```bash
python3 scripts/week09-smoke.py --backend-only
python3 scripts/week09-smoke.py
python3 scripts/week09-performance.py
```

完整烟测包含真实浏览器、API、PG、Redis，并实际停/启它自己创建的 Redis；模型传输使用明确标记的本地确定性夹具。它验证 200/SSE、429/Retry-After、503、输入保留和恢复，不能替代真实模型质量评测。结果在 `docs/testing/evidence/week09/`。

## 配置和维护

| 设置 | 默认值 | 用途 |
|---|---:|---|
| CACHE_ENABLED | true | 关闭时列表直接查库 |
| CACHE_TTL_SECONDS | 30 | 另加 0～20% 正抖动 |
| CACHE_MAX_BYTES | 1048576 | 超大列表不入缓存 |
| REDIS_OPERATION_TIMEOUT_MS | 100 | API 缓存/限流 Redis 操作界限 |
| AI_RATE_LIMIT_ENABLED | true | AI 发消息限流开关 |
| AI_RATE_LIMIT_REQUESTS | 10 | 窗口额度 |
| AI_RATE_LIMIT_WINDOW_SECONDS | 60 | 窗口秒数 |

新设置复用原 REDIS_ADDR/DB/PASSWORD，不需要新的 AI Key。改 .env 后重启 API；Worker 无需这些新开关。Redis 恢复会自动重新使用，无需重启 API。若 Redis OOM，检查内存/队列/缓存 TTL，禁止通过 FLUSHDB 清掉任务队列来处理缓存问题。

代码从 main 的 redisClient/listCache/limiter 注入开始，再看 `internal/platform/cache`、`ratelimit`、两个 Service.List、migration13 和 AI handler。问题参考答案见 `../weekly/week09-answers.md`。
