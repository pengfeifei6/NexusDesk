# 第一周可复现演示记录

- 日期：2026-11-09，Asia/Shanghai。
- 执行者：AI 根据开发者委托完成工程验证。
- 记录类型：真实终端演示摘要，未录制视频，不表示开发者已经独立演示。

## 1. 本次验证环境

将当前项目的运行源码、模块文件、Compose、迁移和脚本复制到 `/var/tmp` 下的新目录，未复制真实 `.env`、`.git` 或生成缓存。先执行 `go mod download`，再执行 `python3 scripts/day5-smoke.py`。

复现使用当前机器已安装的 Go、Docker、Compose、Goose、模块下载缓存和本地 `postgres:17` 镜像。这验证了干净项目目录的运行流程，不等同于在一台全新机器重新安装全部工具。

## 2. 真实结果摘要

以下摘录来自 [完整复现输出](evidence/week01-clean-reproduction.log)：

```text
PASS PostgreSQL container is healthy
PASS Goose Up / Status / Down / Up and version 1
PASS README restart preserves the volume, organizations table and migration version
PASS full race suite including PostgreSQL integration and SQL defaults
PASS organizations name NOT NULL constraint
PASS database-backed API /healthz and SIGTERM exit 0
PASS database-backed API /healthz and SIGINT exit 0
PASS authentication / missing database / unreachable database fail before HTTP
Cleaned up only this run's test containers and labeled volume
```

这里的 authentication 指 PostgreSQL 连接密码验证，不是尚未实现的用户登录功能。

数据库停止后重新启动时保留同一个测试卷，组织表和版本记录仍然存在，再次 Up 提示当前版本为 1、没有迁移需要执行。最终仅清理本次创建的容器和核对过归属的卷。

## 3. 自己复现时的顺序

1. 在项目根目录按 [README](../../README.md) 准备并加载本地环境变量。
2. 用 Compose 启动数据库，等待 healthy。
3. 执行 Goose Up 与 Status，查询 `organizations`。
4. 运行 `go run ./cmd/api`，观察数据库连接通过后再启动 HTTP。
5. 另开终端访问 `/healthz`，查看 200 和 `{"status":"ok"}`。
6. Ctrl+C 停止 API；用 Compose `down` 停止数据库并保留卷，再次启动检查迁移版本。

不希望使用自己的本地数据库时，可直接执行独立烟测。它会建立并清理自己的测试环境，不加载真实 `.env`：

```bash
python3 scripts/day5-smoke.py
```

## 4. 展示边界

本周可以展示：配置、启动、连接池、健康检查、组织表迁移、失败处理、测试与复现证据。

用户登录、组织授权、工单 CRUD、前端和 AI 功能都尚未实现，不能在项目介绍中写成已完成。
