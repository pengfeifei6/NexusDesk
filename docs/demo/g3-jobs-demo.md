# 第七周运行与演示

> 后续已按用户要求配置 DeepSeek。当前本机启动请优先阅读 [DeepSeek 运行指南](local-deepseek-run.md)；下文保留 W07 原始本地 Qwen 验收方式。

本周新增独立 Worker 和 Redis；API、Worker 必须使用同一个数据库和同一个私有 STORAGE_ROOT。使用本工作区新版 API，避免旧 W06 二进制绕过任务所有权。本机日常库已在不透明0600备份后只Up到migration11，独立Redis6380已healthy/PONG；备份在ignored `.local/week07-backups/`，没有读取内容。详细证据见[第七周记录](../weekly/week07.md)。

## 1. 启动依赖

本机 6379 已有其他服务，NexusDesk 使用独立 Compose 项目和 6380 端口，不清空原 Redis：

```bash
cd /media/yjz/pzp/Go/projects/NexusDesk
docker compose --env-file /dev/null -p nexusdesk-queue -f deploy/compose.redis.yaml up -d redis
# 容器内6379映射到宿主机127.0.0.1:6380，持久卷nexusdesk-queue_queue-data。
python3 scripts/week06-model-warmup.py
```

本机已安装 bge-m3:567m 与 qwen3.5:35b。预热只发送公开短句；长时间闲置后首次模型请求可能仍需重新预热。新的独立 Redis 默认仅绑定回环；没有公网部署。

## 2. API终端

停止旧 API 后，在你自己的终端加载已有配置；程序不会自动读取 .env，AI 没有读取或改写真实文件：

```bash
cd /media/yjz/pzp/Go/projects/NexusDesk
set -a
source .env
set +a
export ASYNC_ENABLED=true REDIS_ADDR=127.0.0.1:6380 REDIS_DB=6 WORKER_CONCURRENCY=2
# 新建的本地专用Redis无密码；若继承了其他Redis配置，应在此终端自行纠正。
go run ./cmd/api
```

JWT_ISSUER/JWT_AUDIENCE/JWT_TTL_MINUTES/JWT_SECRET_BASE64 仍是 API 必需项。新任务的默认值使旧 .env 无需增加字段即可读取，但显式 export 更便于核对运行配置。

## 3. Worker终端

```bash
cd /media/yjz/pzp/Go/projects/NexusDesk
set -a
source .env
set +a
export ASYNC_ENABLED=true REDIS_ADDR=127.0.0.1:6380 REDIS_DB=6 WORKER_CONCURRENCY=2
# Worker不需要JWT签发配置；不把它传给子进程。
unset JWT_ISSUER JWT_AUDIENCE JWT_TTL_MINUTES JWT_SECRET_BASE64 HTTP_ADDR
go run ./cmd/worker
```

看到 Worker started 表示独立进程已启动；Ctrl+C 正常停止。API 可以在 Redis 不可用时继续启动/保存任务，Worker 启动时会检查 Redis。模型调用不在启动路径中。

## 4. 前端终端与演示

```bash
cd /media/yjz/pzp/Go/projects/NexusDesk
export PATH="$PWD/.local/week04-tools/node_modules/node/bin:$PATH"
npm --prefix web run dev -- --host 127.0.0.1
```

打开 http://127.0.0.1:5173，登录并选择组织。

1. 知识库选一个 TXT/MD，点击“上传并异步处理”。先显示等待/处理中，再显示处理完成、正文预览和已索引。
2. 点击“检索问答”，输入文档中的问题，核对召回片段及回答来源。
3. 停止 Worker，再上传一个新文档，观察 PENDING；取消或重试只对创建者/管理员显示。
4. 恢复 Worker，确认等待任务继续执行。PDF 会得到明确失败码，不假装支持解析。
5. 切组织或退出登录，旧任务内容应消失。隐藏页面暂停轮询，五分钟后可点“刷新任务”；后台任务仍继续。

上传成功但任务提交失败时，页面保留文档并提示再次提交，无需重新上传。旧“上传文档”“提取正文”“建立索引”仍可用，有活动异步任务时返回冲突。

## 维护与故障定位

| 现象 | 优先检查 |
| --- | --- |
| 新任务404 | 是否重启了本工作区API；组织/文档是否可见 |
| 长期PENDING | Worker是否运行、REDIS_ADDR/DB是否一致、日志是否QUEUE_UNAVAILABLE |
| Worker启动失败 | DB连通性、6380专用Redis、私有目录权限；无需补JWT |
| RETRYING | 等待5/30秒预算，不反复提交绕过退避 |
| FAILED | 查看固定failure_code；永久错误先修正原因，创建者/admin再重试 |
| 老文档长期PROCESSING/INDEXING | 可能是W05/W06无token旧中间态；查明原进程，不直接批量重置 |
| 任务完成但其他账号找不到 | 组织隔离、成员状态、软删除；不会自动把测试语料导入日常账号 |
| 模型冷启动超时 | 独立预热，不取消服务端时间限制 |

不要把 `down -v`、FLUSHALL、批量改任务状态当作日常修复。备份 PostgreSQL 与私有文件需要配套；Redis AOF 不等于业务备份。日常迁移只 Up；Down 在隔离验收中执行。重新创建 PostgreSQL 容器仍必须使用 deploy/compose.pgvector.yaml 并保持原项目/卷，详见[第六周说明](g2-rag-demo.md)。

## 完整隔离验收

```bash
python3 scripts/week07-smoke.py --backend-only  # 真实PG/Redis、Go race与故障进程
python3 scripts/week07-smoke.py               # 加真实模型与W04～W07浏览器
```

脚本建立随机专用库、Redis和虚构账号，只清理本次资源；不加载 .env。`--db-only` 仅跑schema/Go，`--skip-go` 只用于已通过Go后的进程/浏览器迭代，不能替代完整验收。实际结果见周报。
