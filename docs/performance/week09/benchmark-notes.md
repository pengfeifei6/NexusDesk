# 可复现的查询测量记录

命令：`python3 scripts/week09-performance.py`。随机独立 PG/Redis，schema12 造数，记录 before 后 Up 最新；结束只移除自己创建的资源。无日常数据、无云调用。

| 模式 | 查询 | 原中位数 ms | 改后中位数 ms | 原/新 shared hit blocks（首个测量） |
|---|---|---:|---:|---:|
|auto|tickets|0.453|0.021|133 / 23|
|auto|documents|0.072|0.033|23 / 23|
|force_generic_plan|tickets|0.561|0.021|133 / 23|
|force_generic_plan|documents|4.185|0.029|848 / 23|

运行环境：PostgreSQL 17.11 (Debian 17.11-1.pgdg13+2) on x86_64-pc-linux-gnu, compiled by gcc (Debian 14.2.0-19) 14.2.0, 64-bit.

上述为本机热缓存合成负载；知识列表 auto 本来已命中正确索引，两个很小的数值差异不能作为稳定倍数收益。其明确收益是 generic plan 对状态条件的索引利用。工单 priority 条件的无关扫描从约 1891 行降至 0。

每表 120000 行、3 个组织，完整分布和固定参数见 baseline.md 与 JSON；9 次测量全部保存，没有挑选单次最快值。三个连续分页完整结果一致，固定数据下没有重复 ID。

深页 OFFSET=80000 执行中位数：16.600 ms。

新增 priority 索引大小：5636096 bytes。

不将本实验外推为生产延迟 SLA；未测冷盘、跨网络、写入吞吐、持续高并发或多实例容量。
