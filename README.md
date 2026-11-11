# NexusDesk

以 Go 后端为核心的组织工单、知识库与受控 AI 工作台。十二周本地 MVP 交付版：39 个业务 API、14 个迁移、10 个主要页面；支持组织隔离、工单协作、异步索引、带引用的问答和人工确认的 AI 工具调用。

本项目由开发者定义需求并借助 AI 逐周实现、测试和整理，不将代码生成等同于个人独立掌握。当前是可在本机运行、维护和演示的求职作品，公网生产部署未交付。

## 当前 API 文档与新版界面

完整接口说明见[API 中文手册](docs/api/NexusDesk_API完整文档.md)，可导入接口工具的契约见[OpenAPI 3.1](docs/api/openapi.json)。覆盖39个业务操作及独立健康检查，说明字段、权限、分页、错误、上传下载和SSE；运行 `python3 scripts/check_api_reference.py` 做只读静态核验。

前端已完成新的工作台视觉优化，包含登录/注册、组织入口、工单当前页概览、知识库、成员、问答与AI会话，见[优化说明和实际截图](docs/frontend-polish.md)。当前页概览随筛选更新；后端代码与接口保持原实现。W12源码包/录屏是当时交付快照，新界面以当前主目录源码和新截图为准。

## 启动现有项目

在项目根目录执行 `python3 scripts/dev.py status`。已有服务正常时直接打开 **http://127.0.0.1:5173**。需要重启时，在三个终端分别运行：

```bash
python3 scripts/dev.py api --diagnostics-address 127.0.0.1:6060
```

```bash
python3 scripts/dev.py worker --diagnostics-address 127.0.0.1:6061
```

```bash
python3 scripts/dev.py web
```

启动器自动读取根目录 `.env`，无需 `source`；直接 `go run ./cmd/api` 不会自动加载 `.env`。已有配置不要重新初始化。首次获取源码、启动数据库及模型，按[完整运行指南](docs/release/quickstart.md)操作。

## 可以操作什么

注册 → 登录 → 创建组织 → 创建工单 → 编辑、指派或领取 → 状态流转与评论。管理员从“组织成员”添加已注册同事；同事可在组织工作台查看账号编号。工单删除需要再次确认；版本冲突会刷新并要求重新操作。

知识库上传 TXT/Markdown 后可异步提取、索引、预览和下载；检索页可选择精确向量或混合检索并查看引用。AI 对话支持 SSE 输出、有限工具查询及创建工单提议，只有用户确认才写入工单。生成模型可用已配置的 DeepSeek，向量仍由本机 BGE-M3 提供。

| 技术 | 用途 |
|---|---|
| Go / Gin / pgx / PostgreSQL + pgvector | HTTP、业务事务、组织隔离和向量检索 |
| Redis / Asynq / 独立 Worker | 持久任务通知、重试与租约恢复、元数据缓存和 AI 配额 |
| Vue / TypeScript / Pinia / Router | 桌面与移动工作台，无损 bigint，内存会话 |
| Ollama / DeepSeek 适配器 | 本机嵌入与可切换生成模型 |
| Python / Playwright / Go race | 隔离验收、演示、初始化与交付工具 |

## 验证和阅读

```bash
python3 scripts/week12-checks.py
python3 scripts/week12-smoke.py --backend-only
python3 scripts/week12-smoke.py --web-only
```

全量真实数据库验收使用随机独立 PG/Redis 和合成数据，不操作日常数据库；浏览器模型是确定性本地夹具，不消耗云模型 Key。普通 `go test ./...` 和现有 GitHub CI 会跳过集成门禁，不能替代上面的 smoke。前提、命令及证据见[维护手册](docs/release/runbook.md)。

- [最终项目介绍](docs/项目最终交付介绍_第十二周.md) · [第十二周任务](项目开发文件/每周开发手册/15_NexusDesk_第十二周详细开发手册.md) · [完成记录](docs/weekly/week12.md)
- [架构与代码导航](docs/release/architecture.md) · [39 个 API 索引](docs/release/api-index.md) · [数据库设计](docs/database.md) · [功能与边界](docs/release/acceptance.md)
- [实际演示与复现](docs/demo/week12-demo.md) · [简历素材](docs/release/resume.md) · [深入面试问题](docs/release/interview.md) · [本周常见问题](docs/weekly/week12-answers.md)
- [RAG 实测报告](docs/rag-evaluation.md) · [性能报告](docs/performance-report.md) · [故障恢复](docs/failure-drills.md) · [交付清单](docs/release/checklist.md)

`python3 scripts/package_release.py --version v1.0.0` 可生成本地源码包及 SHA256/逐文件清单，已有同名文件拒绝覆盖。包不含 `.env`、上传、备份、依赖缓存及原始视频/测试证据；后者保留在主仓库 `docs/testing/evidence/`。这不是 GitHub 发布命令。

PDF 文本提取、角色修改/停用、AI 历史分页归档、SSO、长期指标告警和公网高可用仍不在此 MVP 中。历史周报按当时范围理解；原 W11 README 原文保存在 [历史快照](docs/history/week11-readme.md)，其中相对路径按原根目录解释。当前状态以本 README 和 W12 文档为准。
