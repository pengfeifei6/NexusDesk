# W06 切块策略

实现：[chunker.go](../internal/knowledge/chunker.go)。输入为 W05 已保存的 `content_text`，不是原始文件字节。TXT/Markdown 提取阶段处理 BOM/换行后的文本是偏移基准。

固定版本 `rune-500-80-v1`：窗口 500 个 Unicode 码点，重叠 80，步长 420。偏移 `[start_rune,end_rune)`；`[]rune(content_text)[start:end]` 必须与片段完全相等。汉字和 Emoji 不拆字节；组合字符/字素簇不是本周的计数单位。尾窗保留，不为整齐丢弃末尾内容。

空输入、纯空白、无效 UTF-8、NUL 和超过 1 MiB 的输入拒绝。纯空白窗口跳过，剩余 chunk_index 从 0 连续编号，偏移仍指向原正文。最多 200 个实际片段，超出整体失败，绝不截断后标记成功。C01～C08 的边界与确定性测试在 [chunker_test.go](../internal/knowledge/chunker_test.go)。

选择固定窗口便于建立可核验基线；80 字重叠减少跨边界事实丢失，但不能保证每个完整事实都落入单个片段。后续可对比标题/段落切块，但必须升级版本、重建索引和使用相同评测集比较。
