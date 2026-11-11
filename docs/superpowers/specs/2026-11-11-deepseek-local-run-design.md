# DeepSeek 接入与本机启动

用户明确要求配置真实 .env 并使用其提供的 DeepSeek Key，取代此前仅本地生成的约定；保留数据库、JWT、私有文件和向量索引。按用户持续授权直接实现，技术选择由 AI 作出。

采用 DeepSeek 官方 HTTPS Chat Completions（deepseek-flash，非思考、非流式、JSON Output）生成有引用的知识库回答；本地 bge-m3:567m / 1024 维继续向量化。相比替换整个检索栈，这不需要重建索引；相比转发代理，本机后端直接调用官方域名，避免额外代理进程。默认示例仍支持 Ollama，真实 .env 选择 DeepSeek。

边界：官方固定地址 https://api.deepseek.com/chat/completions；不跟重定向、不读代理环境；两并发、35秒生成、64KiB响应、12000字节消息预算。复用严格引用/JSON检查；Key仅API进程持有，不传前端或Worker，不记录上游错误原文。不读取真实上传文件做验收；真实调用仅用虚构资料。选择云生成后，实际提问和召回的相关片段会发送到DeepSeek。

提供 Python3.8+ scripts/dev.py，安全解析根目录.env（不执行shell），启动 api/worker/web、status、warmup。保留现有配置，只补全需要项；文件0600，Git忽略。不开公网、不中断无关进程、不删数据、不自动提交。验证用新适配器HTTP边界测试、配置/服务测试、全Go回归及独立虚构数据的实际模型链路；给出浏览器操作和日后启动文档。

依据：2026-11-11查阅 https://api-docs.deepseek.com/ 、https://api-docs.deepseek.com/api/create-chat-completion/ 、https://api-docs.deepseek.com/guides/json_mode/。
