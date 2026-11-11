# 运行 NexusDesk 与查看现有功能

> 本文保留第七周接入时的说明；当前启动与新增功能请看 [第九周运行指南](week09-demo.md)。

当前是第七周后的可运行版本：注册/登录、组织、工单、知识库、检索问答、异步文档处理。SSE流式对话和工具调用留在第八周。

## 已配置内容

项目根目录 `.env` 已按本次明确授权配置：保留原数据库账号与JWT，设置 `CHAT_PROVIDER=deepseek`、`CHAT_MODEL=deepseek-flash`、本地BGE-M3与Redis6380。真实Key只存在本地0600文件，不进入Git、前端或Worker。

DeepSeek负责生成回答；`bge-m3:567m`负责本地1024维向量，已有索引可继续用。不必运行本地Qwen。云端问答会向DeepSeek发送问题及当前组织检索出的相关片段，原文件仍在本机私有目录。使用官方 [Chat Completions](https://api-docs.deepseek.com/api/create-chat-completion/) 和 [JSON Output](https://api-docs.deepseek.com/guides/json_mode/)，不经过代理；生成仍受超时及引用校验限制。

## 本次已经启动

本次已在后台启动日常 API8080、Worker、前端5173；可直接打开页面，无需再启动一遍。API健康和前端均返回HTTP200，Worker已报告启动。日志在 `.local/run/api.log`、`worker.log`、`web.log`，目录与文件不进Git。

如需停止本次后台进程，先在项目根目录执行：

```bash
python3 .local/run/stop.py
```

该本机辅助脚本核对PID及启动时间，只停止本次启动的进程组，不停止数据库、Redis或Ollama。重启后按下文三个终端命令运行；Ctrl+C分别停止。端口被本次后台进程占用时，先停止它们再运行终端命令。

## 每次开发如何启动

所有命令先进入项目目录：

```bash
cd /media/yjz/pzp/Go/projects/NexusDesk
python3 scripts/dev.py status
```

数据库和Redis目前已安装。若重启电脑后容器没有运行，启动已有容器即可（保留原卷和数据）：

```bash
docker start deploy-db-1 nexusdesk-queue-redis-1
```

Ollama通常由现有服务运行；如果11434无法连接，先恢复本机Ollama服务。若是手动安装，可在独立终端执行 `ollama serve`；端口已监听时不要重复启动。

```bash
python3 scripts/dev.py warmup
```

该命令只预热本地BGE-M3，首次冷启动可能较慢。然后分别打开三个终端，每个终端都进入项目根目录：

```bash
# 终端一：API，保持运行
python3 scripts/dev.py api
```

```bash
# 终端二：Worker，保持运行
python3 scripts/dev.py worker
```

```bash
# 终端三：Vue前端，保持运行
python3 scripts/dev.py web
```

看到 `HTTP server started`、`Worker started` 和 Vite 地址即表示进程已启动。脚本自动加载根目录 `.env`，无需手动source；修改配置后重启API，修改队列/数据库/存储配置时同时重启Worker。各终端按Ctrl+C正常停止。前端自动复用仓库已安装的Node24，不把后端凭据传入Node。

浏览器打开 **http://127.0.0.1:5173**；API健康检查是 **http://127.0.0.1:8080/healthz**。不要把8080当作页面地址。

如果浏览器在另一台电脑，本机回环地址不可直接远程访问。在那台电脑建立SSH转发：

```bash
ssh -L 5173:127.0.0.1:5173 yjz@bigdata1
```

保持SSH连接，再用那台电脑的浏览器打开 http://127.0.0.1:5173。使用你实际能连接的服务器名/IP；无需向公网开放API。

## 按这个顺序查看功能

1. **注册/登录**：已有账号直接登录；没有账号点“注册”。账号不自动加入组织。
2. **组织与工作台**：创建组织，创建者自动成为管理员；选择组织进入对应业务。组织为空时不会自动出现演示工单或文档。
3. **工单**：创建工单，查看详情，领取，评论，按页面按钮推进状态。当前角色和状态会影响按钮；所有后端接口的完整管理UI尚未全部补齐。
4. **知识库**：上传TXT或MD，点击“上传并异步处理”。查看等待→执行→成功、正文和索引状态。PDF目前可保存，解析尚不支持。
5. **检索问答**：打开“检索问答”，先查询已上传资料中的问题，再点“基于知识库回答”。回答下方显示 `deepseek-flash` 及来源。没有相关片段时会拒答，不是通用闲聊入口。
6. **异步任务**：可以停止Worker后上传一个新的演示文档观察等待，再恢复Worker；失败或取消任务按权限显示重试入口。不要批量改数据库任务状态。

可自行新建 `运行演示.md`，内容为：

```text
NexusDesk运行说明：前端端口为5173，API端口为8080。
数据库连接超时时先检查DB_HOST和DB_PORT，再检查数据库服务是否正常。
知识库异步处理需要独立Worker运行，Redis地址为127.0.0.1:6380。
```

上传并完成索引后提问：“数据库连接超时应该先检查什么？”或“前端和API端口分别是多少？”；不要把API Key填入测试文档。

## 排错与日常维护

- `JWT_ISSUER is required`：使用上面的启动脚本；直接 `go run` 不会自动读取 `.env`。
- `address already in use`：已有进程占用端口，停止对应旧进程后重启；不要重复启动。`ss -ltnp` 可查看端口占用。
- 任务一直等待：查看Worker终端，以及 `python3 scripts/dev.py status`。端口可连接只代表依赖在线，不代表任务成功。
- 提示模型超时：先执行 `warmup`；DeepSeek端故障或网络问题也可能触发，不必重新上传已有文档。
- DeepSeek 401/402/429：分别检查Key、API账户余额、请求限制。应用不返回上游原文和Key；不要开启带Authorization的HTTP调试日志。
- 刷新页面后回登录：当前Token只放内存，这是现有设计。
- 不要重复复制 `.env.example` 覆盖真实 `.env`，不要把Key放入 `VITE_*`。密钥若需轮换，在DeepSeek控制台生成新Key后只更新根目录对应字段并重启API。
- 保留数据库卷与 `var/storage`；不要使用 `down -v`、FLUSHALL 或数据库Down清理日常数据。

## 本次检查记录

- 配置/适配器/服务先RED后GREEN，`go test -race -count=1 ./...`、`go vet ./...`、`go build ./...`均通过；普通Go调用中的数据库门禁随后由隔离实际库补验。
- 5项启动器测试通过，包括Python3.8、不执行shell、子进程凭据隔离、读取实际配置端口诊断。
- 独立临时PostgreSQL schema11 + Redis：285个集成PASS节点，0跳过。日常库未写测试账号或数据。
- 真实Chromium：上传→202任务→Worker本地BGE索引→DeepSeek回答→正确文档引用，桌面和移动视口均通过。虚构问题“数据库连接超时应该先检查什么？”的真实回答是“数据库连接超时首先检查 DB_HOST 和 DB_PORT。”
- 日常API8080/前端5173 HTTP200，Worker已启动；实际Worker/前端环境没有DeepSeek/JWT Key，前端也没有数据库/Redis密码。新改文件与运行日志未出现云Key/JWT秘密。
- [实际链路结果](../testing/evidence/deepseek/result.json)、[本机运行结果](../testing/evidence/deepseek/runtime.json)、[桌面截图](../testing/evidence/deepseek/answer-desktop.png)、[移动截图](../testing/evidence/deepseek/answer-mobile.png)。

需要重做真实云验收时，运行 `python3 scripts/deepseek-smoke.py --live`；这会创建并清理自己拥有的临时PG/Redis，使用虚构资料实际调用一次DeepSeek问答，可能产生API费用。该脚本不在普通单元测试中自动调用。历史W07完整结果仍见 [第七周记录](../weekly/week07.md)。
