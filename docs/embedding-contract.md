# W06 模型契约

本周使用本机 Ollama，Embedding 固定 `bge-m3:567m`、1024 维，生成固定 `qwen3.5:35b`。选择本机已具备的 GPU 与生成模型，避免将私有文档发给外部服务。BGE-M3 的维度和模型说明见 [BAAI 模型卡](https://huggingface.co/BAAI/bge-m3)；接口遵循 [Ollama Embed](https://docs.ollama.com/api/embed) 和 [Chat](https://docs.ollama.com/api/chat)。没有 API Key，没有生产 Mock 降级。

[Embedder/Generator](../internal/rag/models.go) 只暴露业务需要的能力；[适配器](../internal/platform/llm/client.go) 只接收 HTTP 显式回环 IP，禁用代理与重定向。API 启动不下载、不连接模型；模型不可用只影响索引、Search、Ask。`RAG_ENABLED=false` 时仍可读索引状态，旧业务可用。

| 边界 | 本周限制 |
| --- | --- |
| Embedding | 每批 1～8 条，每条最多 4000 UTF-8 字节，总计 32000；truncate=false |
| 向量校验 | 数量和维度匹配；每项有限；非零；float64 累计范数后归一化为 float32 |
| 查询 | 去空白后 1～1000 码点且不超过 4000 字节；top_k 默认 5，范围 1～10 |
| 响应体 | Embedding 2 MiB，Chat 64 KiB；错误也受大小上限控制 |
| 模型调用 | Embedding 20 秒，Chat 35 秒；共享最多 2 个进行中请求，满额立即 503，无排队和自动重试 |
| 完整流程 | Index 60 秒，Search 25 秒，Ask 60 秒；浏览器分别 70/30/70 秒 |
| 生成 | stream=false、think=false、temperature=0、num_ctx=16384、num_predict=512 |
| 输入预算 | 所有消息序列化后最多 12000 UTF-8 字节；最多 5000 码点的候选证据；按排名丢弃整个末尾片段 |
| 输出 | 纯文本最多 4000 码点；JSON schema；非空答案必须至少引用一个真实来源 |

12000 字节是保守输入预算，包含系统提示和转义后的来源元数据，**不是已经用该模型分词器证明的精确 Token 数**。供应方没有返回 usage 时不虚构费用或 Token 统计。被预算移除的片段不能被模型引用。模型若输出未知 ID、截断 JSON、工具调用或不合法答案，返回固定安全错误，不透传原文。

配置只允许本版固定模型/维度；改模型须评审迁移与重建策略，不能仅换环境变量。冷启动和预热的实际验证见 [本周记录](weekly/week06.md)。
