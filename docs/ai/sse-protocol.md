# SSE 与取消

完整API字段见 [AI接口](../api/ai-conversation.md)。采用POST fetch是为了同时携带JSON消息和Authorization。原生EventSource主要用于GET，当前前端使用内存Bearer，所以不采用URL参数Token。

```text
event: content_delta
data: {"message_id":"...","delta":"你好\n下一行"}

: ping

```

空行分隔事件；data由json.Marshal产生，文本中的换行转义，不能注入第二条event。Content-Type为text/event-stream；no-cache/no-store/no-transform和X-Accel-Buffering:no帮助代理避免缓存/压缩缓冲。服务端逐事件Flush，每10秒心跳。代理仍须支持流式转发，这些头不能强迫所有第三方代理遵守。

前端TextDecoder以stream模式保留半个UTF8字符，SSE解析器保留未完成行/事件；一次reader.read可能包含半条或多条事件。无message_end的EOF不能当成功。error事件使用固定code解释，不输出上游原文。

Handler在提交SSE响应前验证身份、JSON、会话owner、去重和Flush能力，失败使用正常HTTP状态。开始后不能改变HTTP状态，只能发送error后关闭。message_end只在保存COMPLETED成功后发送。数据库提交失败不能伪装为成功完成。

HTTP线程是唯一writer。Runner通过受Context控制的通道交出事件；没有两个goroutine同时写ResponseWriter。ResponseController针对真正的底层writer获取FlushError，避免Gin无返回值Flush隐藏网络错误；写有5秒deadline。HTTP75秒总写超时不增加，新轮限60秒。服务关停通过Server.BaseContext取消已有请求。

点击停止/切换页面 → AbortController → 请求Context取消 → 模型HTTP请求取消/工具SQL取消 → Runner返回 → 独立最多3秒清理保存INTERRUPTED。失败保存FAILED。HTTP线程可以立即结束，名额在后台有界清理完成后释放。清理本身数据库不可用时记录固定错误码；后续读历史或开始新轮会将超过120秒的遗留STREAMING转为INTERRUPTED，不自动重新执行工具。

模型请求中断、代理Flush和真实浏览器停止的验证证据见 [测试计划](../testing/week08-test-plan.md)。暂不提供Last-Event-ID、自动续传或跨实例全局额度；当前是本机模块化单体。
