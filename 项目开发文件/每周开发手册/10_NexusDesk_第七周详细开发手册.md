# NexusDesk 第七周详细开发手册

**版本**：W07 · v1.0（2026-11-11，结合W06实际实现与W08前置条件补齐）
**主题**：Redis＋Asynq、独立Worker、可靠异步文档处理、重试与崩溃恢复
**建议投入**：24～30小时；并发与故障演练可分两周学习
**基础**：W06已实现私有文件、TXT/MD提取、固定1024维Embedding、pgvector精确Search和非流式Ask。

本手册由AI按开发者明确委托编写，并在同一任务内实施。这里的任务安排不是测试通过证明；最终事实、失败记录和证据统一写入 `docs/weekly/week07.md`。开发者的独立掌握、自测和Git提交不由AI代填。

---

## 0. 第七周为什么需要存在

第六周的请求路径是“浏览器发请求→提取/向量化→等待返回”。文件稍大、模型冷启动、网络断开都会影响体验；进程崩溃可能遗留PROCESSING或INDEXING。第八周明确要求W07异步索引可提交、执行和查询，Worker能独立启动，然后才能继续SSE和受控工具调用。

本周把耗时链路迁移到独立后台执行，同时保持第六周Search/Ask和旧同步接口可用。任务队列负责投递，PostgreSQL负责业务事实，Worker必须适应重复消息，不能把队列返回成功当作索引已经完成。

### 0.1 P0交付

1. 增加Redis7.4.9本地服务和固定Asynq0.26.0依赖，保持Go和既有依赖版本。
2. 实现独立 `cmd/worker`，和API共享现有业务Service与私有文件目录。
3. migration00011新增knowledge_jobs及域处理所有权标记，不改00001～00010。
4. 提交文档任务快速返回202和任务ID；查询能看到状态、阶段、实际尝试次数和安全失败码。
5. Worker完成“读取已保存文档→必要时提取→切块/Embedding→原子发布索引”。
6. 用稳定业务键去重；重复投递、重复提交和确认丢失不能产生重复Chunk。
7. 最多3次实际尝试，失败按永久/暂时分类，5秒/30秒退避；提供受权限保护的重试和取消。
8. 实现租约、心跳、所有权校验与有界恢复，失效Worker不能覆盖新任务结果。
9. 处理数据库已提交但消息未入Redis、Redis重启丢失投递的情况。
10. 前端支持“上传并异步处理”、状态轮询、重试/取消；关闭页面不取消后台任务。
11. 用真实PG/Redis/独立Worker进程验证崩溃、重复、删除、撤权、故障与恢复。
12. 更新API、设计、ADR、测试、运行、阶段介绍与答疑，保留W01～W06回归证据。

### 0.2 本周不做

SSE、对话持久化、Function Calling、写操作人工确认属于W08；不在W07提前搭空架子。也不加入PDF解析、ANN索引、任意Shell/SQL工具、复杂通用工作流、Redis Cluster或公网部署。

历史W05/W06的无所有权遗留PROCESSING/INDEXING不会被自动强行重置。本周只自动恢复由新任务协议拥有的执行；其他情况需维护人员核查，避免抢占仍在运行的旧操作。

---

## 1. Day0：基线与环境

Linux/Bash进入现有仓库；先查看状态，不覆盖尚未提交的前几周代码：

```bash
cd /media/yjz/pzp/Go/projects/NexusDesk
git status --short
go test ./...
go vet ./...
go build ./...
```

普通Go测试会跳过真实数据库门禁，不能当作数据库已验证。完整W06复现见 `python3 scripts/week06-smoke.py`。不打印/读取真实.env、连接串、Token或私有上传文件。

确认W06业务API28个、migration10，模型固定bge-m3:567m/1024与qwen3.5:35b；现有公开评测数据可复用。原日常PG17容器重建需要带compose.pgvector.yaml。新增Redis使用独立卷、回环绑定、AOF；测试Redis使用本次临时服务，禁止对共享Redis执行FLUSHDB。

本任务保留原未提交工作，记录安全源码快照，并在feat/week07-async-ingestion分支增量工作；不擅自commit/push。

---

## 2. Day1：任务边界、状态机与契约（3小时）

### 2.1 三种状态各表示什么

| 状态对象 | 状态 | 回答的问题 |
| --- | --- | --- |
| 文档 | UPLOADED/PROCESSING/READY/FAILED | 正文提取是否完成？ |
| 索引 | ABSENT/INDEXING/READY/FAILED | 当前正文是否有有效向量？ |
| 任务 | PENDING/RUNNING/RETRYING/SUCCEEDED/FAILED/CANCELED | 后台流程由谁执行、是否还会继续？ |

阶段WAITING/EXTRACTING/INDEXING/DONE用于解释当前工作，不能把它当作精确完成百分比。SUCCEEDED必须建立在现有业务结果完成后，202仅表示数据库已接受任务。

状态主链：PENDING→RUNNING→SUCCEEDED；暂时失败RUNNING→RETRYING→RUNNING；永久失败或次数耗尽→FAILED；有效取消→CANCELED。FAILED/CANCELED可在重新鉴权和条件检查后开启新run_version；SUCCEEDED不自动重跑。

### 2.2 任务数据与消息

业务去重键包含org_id、document_id、原文件sha256、extractor_version、embedding_model、chunker_version。多个组织不能因为hash相同就共享权限。任务包含可信requested_by，重试执行者记录为actor_user_id；消息不携带用户身份。

Redis载荷仅包含job_id、org_id、run_version，最长1KiB，使用严格JSON整数ID。不能携带文件路径、正文、Token、角色或模型地址。Worker必须从数据库重新取真实任务与当前成员关系，不能信任消息本身的授权。

**交付**：`docs/jobs-design.md`、`docs/api/knowledge-jobs.md`、ADR-0007、任务结构与状态规则。

---

## 3. Day2：任务表、提交与幂等（3～4小时）

### 3.1 数据模型

knowledge_jobs使用与项目一致的BIGINT主键；包含组织/文档复合外键、请求者/执行者成员关联，以及状态、阶段、run_version、attempts、total_attempts、failure_code、retryable、时间字段。内部还有lease_token/lease_until与派发控制字段，不能全部透传给浏览器。

约束必须能拒绝非法状态、次数、模型版本、hash、租约与终态时间组合。派发需要的索引围绕待执行时间和租约到期时间设计，不为未来筛选无节制增加索引。

同时给文档和索引增加可空task_token：记录是哪次执行取得处理权。它是防止旧执行覆盖新执行的条件，不是用户Token。

### 3.2 提交任务

提交在短事务内锁可见文档，检查active成员和状态，插入或复用去重记录，返回202。不在此请求里调用模型，也不依赖Redis在线。Redis不可用时任务仍可PENDING，必须诚实显示“等待执行”。

保留旧上传201接口；前端新按钮依次执行上传与任务提交。两次HTTP不是一个原子事务。若上传成功而任务提交失败，文档已经保存，应保留详情和重新提交入口，不能让用户盲目重复上传。

### 3.3 API

| 方法 | 路径（组织前缀 `/api/v1/organizations/:orgID`） | 返回 |
| --- | --- | --- |
| POST | `/knowledge/documents/:documentID/jobs` | 202，任务或已有去重任务 |
| GET | `/knowledge/documents/:documentID/jobs` | 200，最近20条 |
| GET | `/jobs/:jobID` | 200，任务详情 |
| POST | `/jobs/:jobID/retry` | 200，新一轮任务状态 |
| POST | `/jobs/:jobID/cancel` | 200，取消后的状态 |

这些接口不接受body/query，用户与角色由服务端取得。active成员可查看本组织可见文档任务；只有任务请求者或管理员可取消/重试。非成员、disabled、跨组织、软删除返回404；同组织无操作权限403；无身份401；状态冲突409。

**交付**：migration00011、JobService/Repository/Handler与重复提交/权限/约束测试。

---

## 4. Day3：Asynq与独立Worker（3～4小时）

### 4.1 程序职责

API负责提交/查询/取消/重试。Worker负责派发扫描、Asynq消费、执行现有提取与索引、心跳与恢复。Worker不需要JWT签发密钥，不将认证配置复制到后台任务。

Redis队列固定 `nexusdesk:knowledge:v1`，任务类型 `knowledge:ingest:v1`。工作并发默认2、范围1～4；读取模型服务自身两并发限制，不通过多开无限goroutine绕过限制。

### 4.2 正常处理

Worker取得可信任务，检查版本、状态、next_run_at、尝试次数，领取一枚新的随机lease_token。文档已READY就跳过提取；索引与当前版本匹配则复用。沿用现有Service，不复制第二套解析或Chunk SQL。

每次实际开始业务执行才增加attempts，收到重复/旧版本消息不消耗业务尝试次数。任何重复消息都不能抢走仍有效租约。

### 4.3 超时与关停

任务总预算180秒，Worker索引预算150秒，单批Embedding20秒；API原同步预算保持。数据库读取3秒、事务5秒、独立清理3秒。模型调用不持数据库事务。

收到SIGTERM/SIGINT后停止接新任务和扫描，取消在途任务并有界等待。执行不能使用无限的Background Context脱离生命周期；取消后的失败记录只用保留执行身份的短清理Context。

**交付**：`cmd/worker/main.go`、队列适配器、配置和独立进程测试；首次真实异步解析＋索引。

---

## 5. Day4：重试、取消、租约与崩溃恢复（4小时）

### 5.1 重试规则

每个手动run最多3次实际执行：第一次失败后等5秒，第二次失败后等30秒，第三次仍失败则终态FAILED。Asynq最多2次消息重试；即使派发扫描又发一条消息，数据库的attempts/next_run_at仍限制总执行次数和退避，不能叠乘成无限重试。

可重试：模型暂不可用、限流、忙、超时、临时存储故障。不可自动重试：无效正文/PDF不支持、向量数量/维度/有限性错误、非法模型结果、权限撤销、源变化等确定性问题。仅保存固定失败码，不能保存数据库异常或模型原始正文。

人工重试是明确动作：重新查权限和文档，递增run_version，重置本轮attempts，保留total_attempts。旧消息版本不匹配立即忽略。

### 5.2 为什么只靠Asynq唯一任务ID不够

进程可能在业务提交后、消息确认前崩溃；Redis任务保留期也不是永久业务去重。业务正确性必须由数据库唯一键、当前run/lease条件以及域结果原子发布保证。

### 5.3 租约与失效执行

租约30秒，每5秒心跳。Worker暂停、退出或失联后，另一进程可在租约到期后恢复。完成、失败回写、取消和恢复都核对本次token；旧Worker恢复后即使模型已经返回，也不能发布新结果。

锁顺序统一为“父文档→任务”，心跳只锁任务。重置PROCESSING/INDEXING必须匹配task_token；不能为了恢复而全表更新所有处理中记录。提取与索引结果提交前仍检查成员、软删除和来源版本。

取消只阻止后续执行，不能撤销此前已经提交的正文或索引；取消后不会伪装成回滚全部历史效果。取消与提交的先后顺序由数据库锁明确决定。

**交付**：重复消费、同时领取、SIGKILL后重启、旧Worker迟到、撤权/删除/取消与重试上限测试。

---

## 6. Day5：数据库与队列之间的可靠投递（3小时）

不能写成“数据库任务提交成功后调用Redis，出错就假装没发生”。PostgreSQL与Redis不是同一事务；本周让knowledge_jobs兼做持久投递意图，不引入通用Outbox框架。

Worker每2秒扫描最多50条到期记录。短事务领取派发机会并递增dispatch_seq，再在事务外入队。任务未开始时15秒后可以重新派发；入队失败只保存QUEUE_UNAVAILABLE。无论进程死在派发前后，还是Redis重启遗失队列，数据库仍保有任务，后续扫描可重新投递。

派发成功不将任务标成SUCCEEDED；Worker消费取得业务租约才RUNNING。重复派发由业务领取与幂等检查收敛。任务消息只包含ID，不在Redis保存文档全文。

故障演练用本次独立Redis实例：不可达时提交仍202/PENDING，恢复后执行；不执行共享实例FLUSHDB，不修改真实Redis配置。还需验证API在Redis和模型不可用时原工单业务仍正常。

**交付**：可靠派发扫描、Redis中断/恢复与消息丢失模拟、重启恢复记录。

---

## 7. Day6：前端与端到端联调（3～4小时）

知识库列表增加“上传并异步处理”，保留旧手工上传。详情增加后台任务面板，显示当前状态、阶段、尝试次数、失败码、重试/取消/刷新入口，不把202显示成“索引完成”。

轮询每2秒且不重叠，最多自动轮询5分钟；标签页隐藏时暂停、重新可见时恢复，超出窗口可手动刷新。切组织、路由、会话或卸载组件应清定时器、取消请求并丢弃旧响应。403/404立即清除受保护的文档预览，401只清当前会话。

成功后刷新正文和索引状态，用户可继续第六周Search/Ask并核对来源。页面关闭只停止轮询；真正取消后台任务需要显式点击“取消任务”。模型和文档输出继续按纯文本渲染，不进入localStorage/sessionStorage。

**交付**：Vue任务面板、生命周期测试、真实上传→后台处理→索引→Search/Ask浏览器证据。

---

## 8. Day7：验收、审查、文档与第八周交接（3小时）

两轮审查分别聚焦“状态/事务/租约/恢复”和“队列/取消/权限/前端生命周期”。针对实际发现先复现再修复，不删失败断言获得绿色。

```bash
go test ./...
go test -race ./...
go vet ./...
go build ./...
go mod verify
git diff --check
# 真实PG/Redis/Worker与浏览器由W07隔离烟测脚本执行；记录实际命令与结果。
```

日常库仅在私有不透明备份后Up到11；Down只在本次专用库。旧W06API需要重启，不能让不认识任务所有权的旧二进制与新Worker并行处理文档。保留旧配置，新增示例变量，不读取/改写真实.env。

### 8.1 验收清单

| 编号 | 场景 | 预期 |
| --- | --- | --- |
| T01 | 未登录提交/查询任务 | 401 |
| T02 | 跨组织和disabled成员 | 404，无模型调用 |
| T03 | 成员查看他人任务 | 同组织可见文档允许读 |
| T04 | 非请求者普通成员取消/重试 | 403 |
| T05 | 伪造body/query/身份字段 | 400，不信任消息身份 |
| T06 | 提交新文档 | 快速202/PENDING，模型不在HTTP执行 |
| T07 | 同一文档并发重复提交 | 同一业务任务，不重复行 |
| T08 | 错误任务状态/次数/FK/hash | 数据库约束拒绝 |
| T09 | Worker独立启动 | 无API进程也可消费 |
| T10 | 正常TXT/MD任务 | 提取READY、索引READY、任务SUCCEEDED |
| T11 | 已READY正文/索引 | 复用，不重复提取/向量化 |
| T12 | 重复消息 | 不重复Chunk，不增加实际次数 |
| T13 | 旧run_version消息 | 无副作用 |
| T14 | 两个Worker争同任务 | 一个有效租约 |
| T15 | 模型临时失败 | RETRYING和5/30秒退避 |
| T16 | 第3次失败 | FAILED，不继续自动执行 |
| T17 | 永久错误 | 立即FAILED，固定失败码 |
| T18 | 显式人工重试 | 新run版本，total_attempts保留 |
| T19 | 取消等待中任务 | CANCELED，旧投递不启动 |
| T20 | 取消运行中任务 | 停止后续调用，禁止迟到发布 |
| T21 | SIGTERM关停 | 有界退出与上下文取消 |
| T22 | SIGKILL后重启 | 到期恢复自己拥有的执行 |
| T23 | 发布索引后、确认消息前崩溃 | 重放收敛，不重复Chunk |
| T24 | 旧Worker恢复后写入 | token/run/lease校验拒绝 |
| T25 | 执行时删除文档/撤权 | 不发布或返回受保护内容 |
| T26 | DB任务成功、Redis发送失败 | durablePENDING，稍后可投递 |
| T27 | Redis重启/丢失投递 | 扫描恢复，不把任务丢掉 |
| T28 | 重复消息提前到达 | next_run_at阻止绕过退避 |
| T29 | 域写入中途失败 | 无半成品READY/Chunk |
| T30 | 前端202与轮询 | 正确等待/执行/成功/失败展示 |
| T31 | 页面组织/路由/会话变化 | 无旧响应污染，定时器释放 |
| T32 | 前端403/404、重试与取消 | 清保护内容，操作按后端授权 |
| T33 | Redis/模型不可用 | 工单/认证/原文业务继续可用 |
| T34 | 消息/日志检查 | 无私有正文、路径、Token、密码 |
| T35 | W01～W06回归与真实RAG | 原功能通过、来源仍可核验 |
| T36 | 迁移Up/Down/Up | 仅专用库回滚，日常库只Up11 |

每项对应具体测试/演练文件及真实结果；不运行的明确写未验收，不用文件存在冒充完成。

### 8.2 第八周可以依赖什么

- W06组织内Search/Ask和来源校验保持稳定。
- W07五个任务API、Worker独立入口、任务状态和故障恢复证据可用。
- 新迁移从00012开始；保持既有组织权限与工单Service边界。
- W08自行新增SSE/工具/待确认操作，不将Worker重试语义直接当作工单确认的事务幂等保障。
- W07没有对话、模型工具调用或审批模块；这些仍由第八周完整实现。

---

## 9. 复习与个人能力检查

1. 为什么HTTP请求内启动goroutine不等于可靠异步任务？
2. Asynq的至少一次投递为什么要求业务幂等？
3. 为什么任务ID唯一不等于业务结果唯一？
4. 为什么任务状态放PostgreSQL，而消息放Redis？
5. 为什么202不能表示成功？
6. 入队成功后、开始执行前Redis故障会怎样？
7. 如何解释任务表兼作持久投递意图？
8. 哪些错误可自动重试，哪些应立即终止？
9. 为什么多条消息不能让总执行次数超过3？
10. lease_token和run_version各防什么问题？
11. 为什么租约过期后仍需在业务提交前校验？
12. 如何防止旧Worker的失败清理覆盖新Worker？
13. 为什么恢复时不能重置所有PROCESSING记录？
14. 为什么统一父文档→任务的锁顺序？
15. 取消任务为什么不等于回滚已完成的所有步骤？
16. Worker如何在API请求结束后仍有可信身份？
17. 文件路径为什么不放到队列载荷？
18. Worker为什么不需要JWT签发密钥？
19. 浏览器停止轮询与取消后台执行有何区别？
20. W08的写操作确认与W07消息幂等为什么不能混为一谈？

建议关闭AI后手写一次任务领取条件SQL，并画出“发布索引成功→进程崩溃→消息重放”的时序。参考回答另存 `docs/weekly/week07-answers.md`；掌握情况由开发者真实记录。

## 10. 官方参考

- [Asynq v0.26.0发布](https://github.com/hibiken/asynq/releases/tag/v0.26.0)：本周固定版本，避免随意升级。
- [Asynq入门](https://github.com/hibiken/asynq/wiki/Getting-Started)：独立Client/Worker、任务与Redis。
- [Asynq重试](https://github.com/hibiken/asynq/wiki/Task-Retry)：MaxRetry、RetryDelayFunc、SkipRetry；本项目另用PG限制实际业务次数。
- [Redis持久化](https://redis.io/docs/latest/operate/oss_and_stack/management/persistence/)：AOF/RDB与持久化边界；持久化不替代应用幂等。

本周安全/可靠性实现由当前仓库代码和真实测试证明，官方示例不自动构成本项目可直接照搬的业务策略。
