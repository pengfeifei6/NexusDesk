# W01-REVIEW-01：第一周收尾审查

- 日期：2026-11-09，Asia/Shanghai。
- 模式：独立审查方只读；修订由主执行方在本次授权范围内完成。
- 范围：`cmd/api/**`、`internal/platform/**`、`migrations/**`、`deploy/compose.yaml`、`scripts/day5-smoke.py`、`.github/workflows/ci.yml`、`go.mod`，并对照需求、架构与 README。
- 审查方法：直接读取当前文件，包括未跟踪文件；审查方没有读取真实 `.env`、修改文件、运行测试或执行 Docker 操作。测试证据由主执行方提供。

## 1. 结论

未发现当前 W1 范围内的阻塞性运行缺陷。这个结论限于已实现的工程基础，不代表认证、组织授权、工单业务或生产部署已经实现或经过安全验收。

确认的关键路径：配置错误不直接输出密码；数据库 Ping 成功后才监听 HTTP；连接失败和应用退出会释放连接池；HTTP 退出使用独立的超时 Context；健康检查断言覆盖状态码、JSON 类型和固定响应。

组织表 migration 符合首周三字段范围。回滚只由独立烟测对新建测试库执行，清理使用随机项目名、精确卷名和项目归属标签。

## 2. 发现与处置

| 编号 | 级别 / 性质 | 位置与证据 | 处置 | 验证 |
| --- | --- | --- | --- | --- |
| R-01 | P3，检查有效性问题 | 原 CI 的 `git diff --check` 在正常干净 checkout 中通常没有工作区差异，不能证明本次提交或 PR 的已有空白错误被检查 | 移除该 CI 步骤，保留 Go 格式、race、vet、build；本地 Diff 检查单独记录 | 更新后的 YAML 与所有 Shell 块解析通过；本地质量命令通过 |
| R-02 | 先前审查的测试缺口，已处理 | 数据库原测试只有预取消和连接拒绝，未覆盖 TCP 接受后不回应 | Day 5 已增加 `TestOpenBoundsUnresponsiveDatabaseStartup` 与 `TestOpenCancelsPendingConnection` | 移除启动上限的临时变异会失败，恢复后通过；Day 6 真实测试与 race 再次通过 |
| R-03 | 先前回归建议，已有进程验证 | HTTP 单元测试在启动附近立即取消，不能单独证明已响应后的退出 | 保留针对启动竞争的单测，另由持久化烟测在收到真实 200 后发送退出信号 | 干净目录烟测中 SIGTERM 与 SIGINT 均退出 0 |

没有为了满足“必须发现问题”而制造代码缺陷。R-01 是本次审查新发现，R-02 / R-03 是已有反馈的处置追踪。

## 3. 已验证与尚未验证

- 本地普通测试、竞态检查、静态检查和构建通过，原始结果见 [质量日志](../weekly/evidence/week01-quality.log)。
- 干净项目目录中，数据库与 API 的完整验证通过，见 [复现日志](../weekly/evidence/week01-clean-reproduction.log)。
- GitHub Actions 文件已准备并做 YAML / Shell 静态检查，GitHub 远程执行尚未触发。
- 数据库集成项在普通测试中明确跳过，在独立数据库烟测中实际执行通过，不能把 Skip 写成 Pass。
- 尚未实施对象级权限、JWT、用户与成员表；这属于后续周次，不列为 W1 已实现功能的缺陷。
- 依赖使用固定版本和 `go.sum`，本次未执行外部漏洞数据库审计，不将普通代码审查称为完整供应链安全审计。

## 4. 复现环境问题记录

第一次干净快照位于共享 `/tmp` 下，数据库、迁移和重启验证通过，但 API 二进制构建报 `error obtaining VCS status`。检查发现 `/tmp/.git` 存在，而 `git -C /tmp rev-parse --show-toplevel` 失败；这会干扰临时目录内的 Go VCS 检测。

没有修改他人的 Git 标记，也没有通过禁用生产构建检查来掩盖失败。将快照放到祖先路径没有 Git 标记的 `/var/tmp` 后，相同源代码完整通过。首次失败保留为 [attempt1 日志](../weekly/evidence/week01-clean-reproduction-attempt1.log)。

该项是验证环境问题，不是开发者独立解决的产品 Bug。若在其他环境出现类似现象，应检查快照祖先目录的 Git 状态，而不是删除现有仓库。

## 5. 参考与交接

CI 使用 GitHub 官方 [checkout](https://github.com/actions/checkout) 和 [setup-go](https://github.com/actions/setup-go)。工作流仅做不依赖真实配置的检查，完整数据库验证通过独立脚本运行。

后续改动需保留健康检查、连接失败、取消、超时和迁移验证。Week 2 安全 Review 应基于届时实际实现重新开展，本记录不能替代认证和授权审查。
