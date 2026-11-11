# 第十一周运行与维护

继续使用原项目目录和现有.env，W11没有新增数据库迁移。已有服务运行时不要重复启动。查看状态：

```bash
cd /media/yjz/pzp/Go/projects/NexusDesk
python3 scripts/dev.py status
```

交付时已实际验证API8080、前端5173、API诊断6060、Worker诊断6061可用，旧API/Worker进程组已退出，见[运行记录](../testing/evidence/week11/runtime.json)。这是一份交付时快照；日后重启机器后仍需按下述步骤启动。

## 启动与查看

需要重新启动时，在三个终端分别执行：

```bash
python3 scripts/dev.py api --diagnostics-address 127.0.0.1:6060
```

```bash
python3 scripts/dev.py worker --diagnostics-address 127.0.0.1:6061
```

```bash
python3 scripts/dev.py web
```

页面地址http://127.0.0.1:5173。前端保持W10企业工作台，W11主要增强后端诊断与可靠性。API业务地址8080；上面两个可选参数分别启用API6060和Worker6061的诊断端口。省略参数且.env没有配置诊断地址时，诊断监听关闭。

```bash
curl -i http://127.0.0.1:8080/healthz
curl http://127.0.0.1:6060/metrics
curl http://127.0.0.1:6061/metrics
```

X-Request-ID会出现在API响应头；失败响应JSON也带同一个ID。按该ID查API启动终端的JSON日志。指标只表示当前进程，自上次启动计数；API与Worker需分别查看。

## 查看性能与演练成果

优先阅读[性能报告](../performance-report.md)和[故障报告](../failure-drills.md)，再查看docs/testing/evidence/week11里的原始数据。profile来自隔离合成实验；不要为了学习采集或传播含真实私有业务的诊断文件。

```bash
go tool pprof -top docs/testing/evidence/week11/performance-reviewed-baseline-profile.pprof
go tool pprof -sample_index=alloc_space -top docs/testing/evidence/week11/performance-reviewed-baseline-heap-after.pprof
```

具体profile文件名和复现命令以性能报告为准；Go trace可用 `go tool trace <文件>` 在本机查看。runtime trace不是分布式追踪。

## 修改后执行哪些检查

```bash
python3 scripts/week11-checks.py
python3 scripts/week11-smoke.py --backend-only
python3 scripts/week11-smoke.py --web-only
```

需要重新测量或演练时：

```bash
python3 scripts/week11-performance.py --stage baseline --replay-label local-1
python3 scripts/week11-performance.py --stage after --replay-label local-1
python3 scripts/week11-faults.py
```

`local-1`是这次复测的标签；下一次换新标签，避免覆盖已保存结果。baseline使用保留的旧缓存源码通过Go overlay构建，不会修改主目录源码；其他产品源码必须与记录的基线一致。源码已变更时按性能报告重新建立实验，不能把新版本冒充旧基线。

这些脚本只启动自己的随机PG/Redis项目，使用合成数据和本地确定性模型，结束后清理自己的资源，不需要真实DeepSeek Key。压测会消耗本机CPU/IO，结果会受同时运行的程序影响；故障演练需要等待真实Worker租约，不能用手工改DB时间代替。

## 常见处理

- **诊断端口连不上**：检查启动参数和进程是否为新版；默认禁用不是故障。端口占用时选择另一个本机端口，不终止未知进程。
- **healthz正常但业务500**：按响应ID查日志，检查PG连通和池指标；healthz是存活信号。
- **Redis不可用时AI503**：恢复Redis后重新确认当前操作；不要自动重放未完成AI消息，也不要关闭限流掩盖问题。
- **任务长时间RUNNING**：先检查Worker进程、租约与当前任务状态；使用既有恢复/重试路径，不直接修改lease_token或任务终态。
- **CPU高**：先确认是否登录散列、模型计算或普通业务负载，使用同类请求的profile定位；不要混合比较。

不运行清库、FLUSHDB、删除日常卷或重写历史命令。开发改动保留在主目录工作区，用户审阅后自行提交。
