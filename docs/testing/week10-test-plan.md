# W10 测试矩阵

对象：第十周混合检索、实际用量观测、企业工作台 UI；保持 W01～W09 回归。结果以 `evidence/week10/` 的实际日志为准。

| 范围 | 核验要点 | 证据/命令 |
|---|---|---|
| HTTP模式 | 缺省vector；精确接受vector/hybrid；拒空/null/别名/重复字段 | Go rag单测 |
| 词项与融合 | 中文bigram、标识符、32上限、字面标点、RRF60、去重与同分稳定 | hybrid_test.go |
| 真实SQL | 两路当前组织、hash/version、软删、状态、模型及阈值；保留余弦分 | week10_rag_test.go |
| 来源复查 | 生成后未引用来源变化、成员禁用必须拒绝发布旧答案 | 同上，真实PG |
| 真实Token | DeepSeek/Ollama；整数、总数、缓存拆分；缺失/null/负值/溢出unknown | usage_test.go、ollama_usage_test.go |
| 指标 | 多证据分母、负例null、首位MRR、引用范围、费用unknown | test_week10_metrics.py |
| 全部后端 | JWT/组织/工单/文件/RAG/队列/AI/cache/rate真实PG/Redis全门禁，race | week10-smoke.py --backend-only |
| 前端 | 策略切换清空并取消旧响应、未知/零/已知用量、融合分精确解析、菜单焦点返回、真实评论事件中文，原141测试回归 | week10-checks.py：149测试 |
| 浏览器 | 注册→登录→组织→工单→评论→上传/异步入库→下载→混合问答→AI提议→确认→手机导航→退出 | week10-smoke.py --web-only |
| 真实效果 | 固定60题×2策略，固定16题问答×2，BGE/DeepSeek真实模型，跨组织404及来源核验 | week10-evaluate.py --live |
| 视觉 | 1440px桌面、390px手机、无全页横向溢出、长标题两行且保留完整可访问名称、空态/有数据状态截图 | browser.json及PNG |

## 证据解释

`backend.json` / `integration.log` 来自真实隔离数据库和Redis，不将普通Go默认Skip当成验收。`backend-fix.log` 验证共享SQL来源解析器调整后的真实W10数据库路径。`checks.json` / `checks.log` 是前端与静态检查记录。`browser.json` 的模型由本地确定性HTTP夹具提供，仅验证业务流转、实际SSE、引用展示、人工确认和响应式布局；不声称该夹具测了语义质量。

`evaluation.json` 是单独的真实模型效果实验，记录预冻结标签、每条rank、实际usage/耗时、模型参数和费用区间。32次问答中实际调用生成30次，2次没有召回而跳过生成。失败/拒答/unknown均保留。

只测试脚本拥有的随机 `nexusdesk-week10-*` Compose资源和 `nexusdesk_w02d2_test_<12hex>` 库；不回滚、删卷或更改日常数据。测试私有文件只在主目录ignored `.local/week10-runs/` 生命周期内存在。云key只在API进程环境，不传浏览器或Worker。
