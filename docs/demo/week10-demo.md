# 第十周：运行与查看

开发始终在原主目录，前后端路由与配置保持兼容。第十周没有新增数据库迁移，日常库保持14，不需要重建或清空数据库。

## 启动

先查看现有进程和依赖，已经运行的服务不要重复启动：

```bash
cd /media/yjz/pzp/Go/projects/NexusDesk
python3 scripts/dev.py status
```

需要启动时，分别在三个终端运行：

```bash
cd /media/yjz/pzp/Go/projects/NexusDesk
python3 scripts/dev.py api
```

```bash
cd /media/yjz/pzp/Go/projects/NexusDesk
python3 scripts/dev.py worker
```

```bash
cd /media/yjz/pzp/Go/projects/NexusDesk
python3 scripts/dev.py web
```

浏览器打开 **http://127.0.0.1:5173**。API健康检查是 `curl http://127.0.0.1:8080/healthz`。启动器自动解析现有 `.env`，直接 `go run ./cmd/api` 则不会自动加载文件；无需重新把密钥粘贴到终端或文档。

依赖沿用原PG/pgvector、独立Redis6380、Ollama11434与BGE；DeepSeek用于生成。重启依赖/首次环境配置见 [现有本地运行指南](local-deepseek-run.md)。不要误用Redis6379，也不要通过删除数据库卷解决启动错误。模型冷启动可先运行 `python3 scripts/dev.py warmup`。

## 看哪些变化

1. **登录/注册**：统一品牌与表单风格；新账号注册后登录。现有账号继续使用。
2. **组织与工单**：侧栏选择组织，创建组织仍在可见入口；工单列表、筛选、创建和详情保持功能。创建→领取→开始处理→评论→解决→管理员关闭是一次完整路径。
3. **知识库**：上传TXT/MD后选“上传并异步处理”，Worker完成后可预览、下载、检索。PDF仍只支持上传下载。
4. **检索问答**：选择“精确向量”或“混合检索”，检索或生成回答。比较排序、阶段耗时、实际Token；点击来源文件核对原文。切换问题或策略会清空旧结果。
5. **AI对话**：创建私人会话，输入问题；需要工单时让AI提出建议。必须看见“工单提议”卡并明确点击确认后才会创建；模型说“已创建”不等于真实写入。可以拒绝提议或停止生成。
6. **手机布局**：窄屏通过导航按钮进入组织工作区，检查表格/表单/消息可读。刷新页面仍需重新登录，这是既有内存身份设计。

## 自己维护

- 修改页面前先看 `web/src/App.vue`、`web/src/assets/main.css` 及对应view；行为状态在composables，API边界在lib。
- 修改检索看 `internal/rag/retriever.go` → `hybrid.go` / `index_repository.go`。不要移除组织与来源版本过滤。
- 修改生成用量看 `internal/platform/llm/chat_client.go`、`deepseek.go`；成本计算在 `scripts/week10_metrics.py`，不要把价目硬写成永远准确的产品收费。
- 改完运行 `python3 scripts/week10-checks.py`；涉及后端数据路径再运行 `python3 scripts/week10-smoke.py --backend-only`，涉及交互运行 `--web-only`。
- 效果实验单独运行 `python3 scripts/week10-evaluate.py --live`，会调用付费DeepSeek；只使用合成数据。默认测试不会调用云模型。
- 用 `git diff` 查看本次变更；工作区保留之前各周尚未提交的代码，提交前自行审阅归组，不要执行清空工作区命令。

评测结论见 [RAG评测报告](../rag-evaluation.md)，完成情况见 [W10周报](../weekly/week10.md)，复习见 [常见问题](../weekly/week10-answers.md)。
