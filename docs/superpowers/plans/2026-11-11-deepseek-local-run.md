# DeepSeek Local Run Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans. Steps use checkbox syntax for tracking.

**Goal:** 真实.env配好DeepSeek，并能启动、查看现有项目。
**Architecture:** 保留Ollama embedding，新增严格DeepSeek generator与本地启动器。
**Tech Stack:** Go标准库HTTP / Vue现有页面 / Python3.8。
**Spec:** ../specs/2026-11-11-deepseek-local-run-design.md

## Global Constraints

固定官方HTTPS，不代理/重定向；35秒、两并发、64KiB；向量模型/迁移不改；秘密不输出、不提交；实际测试只用虚构资料；工作区直接可运行，feature branch，无自动commit。

## Review Focus

凭据不传给web/worker；无Key的Worker仍运行；错误不泄露上游响应；云返回恶意引用拒绝；启动器不执行.env中的命令替换。

### Task 1: 云生成适配器

Files: internal/platform/config/rag*.go, async.go; internal/platform/llm/deepseek*.go, chat_client.go; internal/rag/models.go, service*.go; cmd/api/main.go。
Interfaces: NewDeepSeek(apiKey, model string) (rag.Generator,error); RAGConfig.ChatProvider/ChatModel/DeepSeekAPIKey; Worker只加载本地模型设置。
- [x] RED: 配置选择/无KeyWorker、固定TLS目的地/鉴权/JSON请求/错误/取消/大小/并发、服务generation_model与引用测试。
- [x] GREEN: 实现选择、复用prompt构建和parseAnswer、动态generation_model。
- [x] go test -race ./internal/platform/config ./internal/platform/llm ./internal/rag; go test ./...; go vet/build ./...。

### Task 2: 本地启动和真实验收

Files: scripts/dev.py, scripts/test_dev.py, .env（ignored）, .env.example, docs/demo/local-deepseek-run.md, README.md, AGENTS.md。
Interfaces: python3 scripts/dev.py api|worker|web|status|warmup。
- [x] RED→GREEN: 配置解析不执行、错误不含值、不同子进程环境隔离。
- [x] 保留原数据库/JWT配置，补齐DeepSeek/队列/存储，chmod0600；真实官方API请求只用虚构资料。
- [x] 独立验收库验证上传→Worker→Search→DeepSeek Ask；启动日常API/Worker/web并检查健康。
- [x] 更新运行/查看功能/日志与停止操作说明；独立review后修正问题，保留工作区不提交。
