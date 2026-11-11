# 第六周运行与演示

当前本机日常数据库已经只向上升级到 migration10，vector扩展0.8.7已安装；已有数据未回滚，备份保存在被Git忽略的 `.local/week06-backups/`，权限0600。旧Go进程需要重启才能加载4个新接口。

## 当前机器启动

在项目根目录开启终端。使用你原先已加载数据库/JWT配置的终端；如果新开终端，自行加载现有配置（不要把内容发到聊天或日志）：

```bash
cd /media/yjz/pzp/Go/projects/NexusDesk
set -a
source .env
set +a

# 先启动已安装的 Ollama 服务；若它已经运行，无需再启动一份。
ollama list
# 当前已安装 bge-m3:567m 与 qwen3.5:35b。
python3 scripts/week06-model-warmup.py
# 大模型首次加载实测约两分钟；此步骤与普通API请求超时分开。
go run ./cmd/api
```

前端另开终端：

```bash
cd /media/yjz/pzp/Go/projects/NexusDesk
export PATH="$PWD/.local/week04-tools/node_modules/node/bin:$PATH"
npm --prefix web run dev -- --host 127.0.0.1
```

打开 `http://127.0.0.1:5173`。API通常是8080；不同地址按已有 `HTTP_ADDR` 和 `NEXUSDESK_API_TARGET` 配置，服务仅监听回环。网页刷新会退出登录，这是现有Token只放内存的设计。

1. 注册/登录，选择或创建组织。
2. 进入“知识库”，上传TXT或Markdown，点击“提取正文”。PDF暂只上传/下载，不支持提取/索引。
3. 正文READY后点击“建立索引”，确认“已索引”和块数；重复点击会复用相同索引。
4. 点“检索问答”，输入文档能回答的问题，先“检索文档”核对片段，再“基于知识库回答”。
5. 点击来源文件名回看原文；切换到空组织重复查询，应显示依据不足。

模型预热只用公开短句，默认保留15分钟；后续Ollama普通调用的默认驻留时间可能改变，长时间空闲后首次请求仍可能超时，重新预热即可。不要通过取消服务端超时或换生产Mock来掩盖冷启动。`RAG_ENABLED=false` 可关闭新AI操作，旧业务和索引状态读取继续可用。

## 重建数据库容器与其他机器

当前容器仍沿用原来的 `postgres:17` 配置，已复制与其完全相同PostgreSQL17.11发行版的官方vector扩展文件，未重启数据库。**今后重新创建容器必须带pgvector覆盖文件**，否则新容器缺少扩展二进制。保持原Compose项目名和数据卷，不执行down -v：

```bash
# 先按你的备份策略保存日常库；以下由你在已加载配置的终端执行。
docker compose --env-file /dev/null -p deploy \
  -f deploy/compose.yaml -f deploy/compose.pgvector.yaml up -d db
```

新机器应使用该overlay启动PG17，并按README配置Goose环境执行 `goose -env=none -dir migrations up` 和 `status`。现有本机已经10，无需再次手工改表。正常联网的机器可先 `ollama pull bge-m3:567m`、`ollama pull qwen3.5:35b`；本机曾遇到守护进程下载EOF，已用公开官方制品校验后导入，详见[环境报告](../reviews/week06-environment.md)。这不是全新机器一键安装已验收的承诺。

## 故障定位

| 现象 | 排查 |
| --- | --- |
| 登录成功但没有“检索问答” | 重启旧API、确认前端从本工作区启动，刷新后重新登录 |
| POST索引404 | 检查是否新API、组织成员/文档可见性；直接GET不存在的旧路径不能测试POST |
| vector类型不存在/扩展文件缺失 | 确认库migration10和overlay；不要删除数据卷重试 |
| 模型503 | Ollama11434、两个固定模型、RAG_ENABLED；先运行预热脚本 |
| 模型504 | 冷启动或资源拥挤，查看预热耗时；API仍保留35秒生成预算 |
| 文档READY但检索为空 | 查看独立索引状态/hash；未点击建立索引不会自动向量化 |
| 长时间INDEXING | 当前无租约恢复，先确认进程/任务是否已中断，不直接抢占或手改数据库 |

完整验收命令与证据：[week06](../weekly/week06.md)。演示语料只进入隔离库，不会自动出现在你的日常账号里。
