# 从零运行与现有环境启动

命令在 NexusDesk 根目录执行。已有安装走第一节；解压源码的新用户走第二节。日常配置、已有卷和私有上传必须沿用，不能因升级 README 重新建库。

## 1. 你目前这台机器

```bash
cd /media/yjz/pzp/Go/projects/NexusDesk
python3 scripts/dev.py status
```

API 和前端正常时打开 http://127.0.0.1:5173。如果未启动，在三个终端分别执行：

```bash
python3 scripts/dev.py api --diagnostics-address 127.0.0.1:6060
```

```bash
python3 scripts/dev.py worker --diagnostics-address 127.0.0.1:6061
```

```bash
python3 scripts/dev.py web
```

启动器按白名单传递配置，前端不读取后端 `.env`。不要直接依赖 `go run` 自动读文件。端口已占用时先识别已有服务，避免重复运行。数据库容器沿用原 Compose project 和 volume，不能套用下一节的全新安装项目名。

W12 没有新增迁移，当前最新版为 14。`python3 scripts/local_setup.py status` 仅查看迁移；只有明确需要升级且已按自己的维护流程备份，才执行 `up`。工具不提供 Down 或清库命令。

## 2. 全新源码安装

需要 Go 1.26.1（见 go.mod）、Node 24.12.0 或满足 web/package.json engines 的版本、npm、Python 3.8+、Docker Engine + Compose、Goose v3.29.0。这些是本项目验证组合，不表示组件的最新版本。命令需能从 PATH 找到；宿主机运行 API/Worker，容器只运行数据库和 Redis。

```bash
go mod download
go install github.com/pressly/goose/v3/cmd/goose@v3.29.0
export PATH="$(go env GOPATH)/bin:$PATH"
npm --prefix web ci --no-audit --no-fund
python3 scripts/local_setup.py init
```

`init` 只在没有 `.env` 时创建权限 0600 的文件，随机生成本地数据库密码及 JWT 密钥；不输出秘密，不申请或生成 DeepSeek Key，已有文件或同名符号链接均拒绝覆盖。不要将 `.env` 提交。数据库密码只在首次创建空卷时初始化；之后改文件不会自动修改库内密码。

检查 5432、6380、8080、5173 是否可用。以下 **仅适用于全新安装**：

```bash
docker compose --env-file .env -p nexusdesk -f deploy/compose.yaml -f deploy/compose.pgvector.yaml up -d --wait
docker compose --env-file .env -f deploy/compose.redis.yaml up -d --wait
python3 scripts/local_setup.py up
python3 scripts/local_setup.py status
```

预期迁移为 1～14 全部 Applied。数据库是 pgvector/pgvector:0.8.7-pg17-trixie；Redis 使用部署文件中的固定 digest。Redis 默认只绑定本机 6380，应用用逻辑库 6。若更改 Redis 端口，Compose 的 `NEXUSDESK_REDIS_PORT` 与 `.env` 的 `REDIS_ADDR` 必须一致；修改 DB_PORT 同样要同步本地配置。不要用公开 IP 绕过回环绑定。

然后按第一节的三个启动命令启动。验证：

```bash
curl -i http://127.0.0.1:8080/healthz
```

健康响应只证明 HTTP 存活；用实际登录、创建组织和工单确认业务可用。注册是 `POST /api/v1/auth/register`，浏览器访问地址栏发送 GET 不能注册。登录页有“注册账号”入口；注册成功返回登录，不自动建立组织。

## 3. AI 与向量模型

先验证普通工单流程，再启动模型。安装并启动本机 Ollama 后，下载项目固定的向量模型：

```bash
ollama pull bge-m3:567m
python3 scripts/dev.py warmup
```

向量始终使用本机 BGE-M3 1024 维。生成模型在自己的 `.env` 配置：DeepSeek 使用 `CHAT_PROVIDER=deepseek`、`CHAT_MODEL=deepseek-flash` 和自己持有的 `DEEPSEEK_API_KEY`；官方 API 地址由后端适配器限定。已有 DeepSeek 配置保留即可。重新启动 API 生效，Worker 只需要本地嵌入。任何 Key 都不要放入前端、VITE 变量或截图。

若继续使用示例默认 `CHAT_PROVIDER=ollama`，还需自行提供 `.env` 指定的本机聊天模型；默认 qwen3.5:35b 有较高资源要求。没有模型时普通工单仍可使用，但知识索引、问答或 AI 对话会失败，不能用 healthz 推断模型已就绪。这里的模型名称沿用项目已验证配置；真实供应商可用性、余额和语义质量要独立验证。

## 4. 首次演示

1. 注册自己的账号，登录后创建一个组织；此时你是该组织管理员。
2. 创建工单，打开详情尝试编辑、指派或领取，再开始处理、发表评论。
3. 同事自行注册并在组织工作台提供账号编号；管理员在“组织成员”添加。
4. 上传原创 Markdown，选择“上传并异步处理”，等待完成后预览和检索。
5. AI 对话中提出创建工单请求，先查看提议内容，再确认；确认前没有新增工单。

页面刷新会清空内存 Token，需要重新登录；这是当前会话设计，不是登录故障。

## 5. 验收工具与源码包

如需执行真实浏览器 smoke，先安装浏览器（下载动作在 web 下执行）：

```bash
cd web
npx playwright install chromium
cd ..
```

Linux 主机还需 Playwright 所要求的系统库；缺失时按安装提示处理。测试夹具使用 `--pull never`，因此第一次运行前需要拉取部署文件指定的 PostgreSQL/Redis 镜像（上面的 Compose 启动会完成）。W12 运行器优先使用现有本机 Node 缓存，否则选择 PATH 中受支持的 Node；不要求源码包含缓存。

```bash
python3 scripts/week12-checks.py
python3 scripts/week12-smoke.py --backend-only
python3 scripts/week12-smoke.py --web-only
```

源码包包含锁文件、迁移、前后端源码与 Markdown 文档；依赖需联网下载。原始证据、截图、视频、profile 不在精简源码包中，相关链接需要主仓库文件；当前 W12 检查可重新生成。历史 W11 性能基线复跑还需要主仓库保留的证据文件，不能用缺失文件冒充原实验。
