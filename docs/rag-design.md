# W06 组织内检索与问答设计

业务链路：私有文件上传 → W05 提取正文 READY → W06 同步建立索引 → 组织内精确余弦检索 → 使用召回证据生成非流式回答 → 服务端核对引用与权限后返回。

四个新增接口见 [API 契约](api/rag.md)，切块与模型边界见 [切块策略](chunking-policy.md) 和 [模型契约](embedding-contract.md)。本周不加入 Redis、Worker、SSE、工具调用、ANN 或跨组织去重。

文档的 UPLOADED/PROCESSING/READY/FAILED 表示正文提取；独立索引的 ABSENT/INDEXING/READY/FAILED 表示向量就绪。ABSENT 只是没有索引行时的 API 展示值。READY 正文必须由用户点击建立索引；模型错误不会把正文变成失败，原文预览/下载继续可用。

索引先在短事务内按父文档、正文、索引行顺序加锁，检查组织/可见性/READY，领取 INDEXING 并递增 index_version。相同正文 hash、模型、维度、chunker 版本的 READY 行直接复用。INDEXING 拒绝重复请求。分块与模型请求在事务外执行。最后重新锁定同一范围，核对当前 hash、状态和本次 version，同一事务删除旧片段、插入全部新片段、发布 READY。失败回写使用独立短 Context，只更新本次版本的 INDEXING，不能覆盖别人或 READY。未知 claim 提交结果不贸然写失败。

`knowledge_chunks.content_sha256` 保存**完整正文的 hash**，并非该片段单独的 hash；真实片段由 rune 偏移核验。检索同时连接组织 ID、父文档未删除/READY、索引 READY、正文 hash、模型/维度、chunker 和 index_version，使用参数化 `$n::vector`，按 `<=>` 距离升序与 chunk ID 升序排序。阈值默认 0.40，相似度不是正确率。

每次入口使用数据库 active 成员关系授权，JWT 只提供用户 ID。非成员、disabled、跨组织或已删除对象返回 404。Search 在 Embedding 后复查成员和来源；Ask 在生成前后复查全部召回来源，包括模型未引用或因预算未发送的来源。撤权/删除/替换在检查前完成即拒绝输出；不声称响应发出后的撤权具有线性化保证。

模型接收的文档名、片段、用户问题均是不可信数据，不能授权、执行 SQL、创建工单或读其他组织。没有证据时不调用生成模型，直接固定拒答。模型返回的 citation_ids 先通过实际发送片段白名单，再由 Service 映射真实召回行，去重后输出来源；不存在把模型编造的文件名/URL当作证据的路径。引用真实不等于答案必然正确，仍需检查答案与原文是否一致。

已知同步限制：进程崩溃可能留下 INDEXING，没有租约/自动恢复；大文档可能超过 60 秒而失败；请求取消影响运行。不要在页面假装完成，也不要直接抢占仍运行的任务。W07 的队列、租约与重试应解决这些真实问题。
