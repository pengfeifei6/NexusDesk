# Week 04：原子领取、Vue前端与G1本地业务闭环

2026-11-10；依据第四周手册和用户“完全开发完”的明确授权完成工程工作。AI负责设计补齐、代码、测试、环境处理、审查协调与文档；开发者个人实现比例、闭卷理解和独立演示未代填。原始手册没有改写。

## 1. 当前成果

新增领取API，连同W02七个、W03九个业务API共17个，另有GET /healthz。Vue可真实登录、选组织、分页筛选、建单、查看详情、领取、开始/解决、管理员关闭/重开、评论和事件查看。没有Mock业务数据源；专用测试账号和数据通过真实API建立。

本周六条migration与根Go依赖完全保留；没有新增support、成员停用、上传、Redis或AI。最新数据库版本仍6。领取只设置当前操作者为受理人，保持OPEN、version+1；成功事件只有一条。失败重复请求409，不声称请求级幂等或抢单公平性。

## 2. 七天任务对照

| 日程 | 实际交付 | 证据/入口 |
| --- | --- | --- |
| Day 1 | 冻结领取、错误语义、前端范围 | [Claim API](../api/ticket-claim.md)、[ADR](../decisions/0004-atomic-ticket-claim.md)、[前端](../frontend/web-mvp.md) |
| Day 2 | Handler/Service/Repository原子领取及单元测试 | [Service](../../internal/ticket/service.go)、[SQL](../../internal/ticket/repository.go)、[测试](../../internal/ticket/claim_test.go) |
| Day 3 | PostgreSQL并发、跨组织、事件及COMMIT失败、取消、反证实验 | [集成测试](../../tests/integration/ticket_claim_test.go)、[矩阵](../testing/claim-concurrency.md) |
| Day 4 | 官方Vue脚手架、TS/Router/Pinia、统一客户端、回环代理与无损int64 | [web](../../web/README.md)、[client](../../web/src/lib/client.ts) |
| Day 5 | 登录/me、组织选择、列表分页筛选、建单表单 | [路由](../../web/src/router/index.ts)、[表单](../../web/src/components/TicketForm.vue) |
| Day 6 | 详情/领取/状态/评论/事件、六项真实联调、截图和录屏 | [浏览器脚本](../../scripts/week04-browser.mjs)、[G1](../demo/g1-demo-script.md) |
| Day 7 | 后端/全量独立审查、修复、回归、CI配置、复盘与参考答案 | [审查](../reviews/week04-final-review.md)、[20题](week04-answers.md)、[源码清单](evidence/week04-source-baseline.json) |

全部20项C01～C20有自动验证，对照见[矩阵](../testing/claim-concurrency.md)。单元测试中的受控响应不替代真实数据库或浏览器证据。

## 3. 真实执行结果

| 检查 | 实际结果 |
| --- | --- |
| Go普通race全套 | 顶层69通过，0失败，9个DB入口跳过；[JSON](evidence/week04-go-test.jsonl) |
| W04隔离全套 | 顶层78通过，0失败/跳过；包含全部前三周DB测试；[最终日志](evidence/week04-final-smoke.log) |
| 并发领取 | 2请求一轮，20请求×20轮；每轮恰好1成功，version=1，1条领取事件；20个独立PG连接，实际观测多个UPDATE等锁 |
| 故障回滚 | 事件INSERT失败、延迟COMMIT失败、锁等待取消均无部分领取；异常安全500，取消传递Context |
| W1历史回归 | 固定migration1通过，含持久卷重启/信号/连接失败；[日志](evidence/week04-week01-regression.log) |
| W2历史回归 | 固定migration3通过，含七接口及Alice/Bob/过期凭据演示；[日志](evidence/week04-week02-regression.log) |
| W3回归 | W04全套启用原W03 schema/T01～T26和六段TCP演示，最新migration6；最终日志可定位 |
| 前端单元 | 3文件、28项通过；[日志](evidence/week04-web-green-final.log) |
| 前端质量 | TypeScript+Vite build、ESLint无fix、Prettier check通过；[构建](evidence/week04-web-build.log)、[lint](evidence/week04-web-lint.log)、[格式](evidence/week04-web-format-check.log) |
| 新目录安装 | 临时目录复制前端源/lock，npm ci后lint/format/unit/build全通过；[日志](evidence/week04-web-clean-install.log)、[清单](evidence/week04-web-reproduction-manifest.json) |
| 静态与保护检查 | Go vet/build/mod verify、文件格式/链接/示例/CI语法/敏感数据/清理；[检查](evidence/week04-delivery-check.log) |

普通Go全部节点413通过/9跳过；真实库508通过节点，包含子测试，不与顶层数混算。[统计摘要](evidence/week04-test-summary.json)保留口径。新增web依赖中带有Go示例，初次全套发现go ./...递归扫描node_modules；增加[web/go.mod](../../web/go.mod)作为遍历边界，无Go代码/依赖，不改变根Module，使项目Go检查仍只遍历8个业务包。

独立前端复现复用了宿主Node/npm缓存，浏览器使用现有Chromium143.0.7499.4，不能称为全新机器验收。远程CI尚未实际触发；本次仅补齐web job并本地验证对应命令。

## 4. 浏览器六项联调与演示

| 场景 | 实际观察 |
| --- | --- |
| Bob和Charlie同时领取 | 两个独立浏览器会话使用同一version；真实HTTP一200一409，失败页面显示最新受理人和冲突提示 |
| 刷新丢失登录态 | 回/login，未发送携带旧Token的受保护请求；localStorage/sessionStorage/Cookie无持久会话 |
| 篡改组织路径 | David访问A路径、B-org/A-ticket组合和伪造claim均404，A标题未渲染 |
| 服务器/网络不可达 | 浏览器离线显示网络错误，恢复后手动重试成功；不自动重发写操作 |
| 409刷新 | GET最新版本而非用旧页面覆盖；DB显示OPEN/version1和唯一ASSIGNED |
| 切组织 | A请求延迟时切B，旧请求取消/结束后仍只显示B；单元层另验证忽略取消的迟到成功/失败 |

另验证初始组织请求网络失败后重试能恢复、已登录浏览器Back不能重进登录表单、真实21条列表分页及状态/优先级筛选。API代理/建单/状态/评论/事件都是真实网络请求；为制造故障使用请求中止、离线及向真实后端发送无效Token，没有伪造成功响应。

[浏览器结果JSON](evidence/week04-browser/results.json) · [真实录屏](evidence/week04-browser/g1-demo.webm) · [创建截图](evidence/week04-browser/01-created.png) · [冲突截图](evidence/week04-browser/02-claim-conflict.png) · [开始处理](evidence/week04-browser/03-in-progress.png) · [离线](evidence/week04-browser/04-network-error.png) · [刷新登录](evidence/week04-browser/05-refresh-login.png) · [跨组织](evidence/week04-browser/06-cross-organization.png) · [组织切换](evidence/week04-browser/07-organization-switch.png)。

录屏由AI驱动浏览器自动生成，无口述音轨，覆盖核心业务及异常展示，不能冒充开发者亲自演示。原始录制45.72秒，另保留[原速文件](evidence/week04-browser/g1-demo-raw.webm)；展示版仅以2/3速播放至约69秒，方便阅读，不改变操作内容或伪造结果。实际时长由交付检查记录。截图只含虚构测试资料，未展示密码、JWT或数据库连接串。

## 5. 真正遇到的问题与修复

| 问题 | 原因与处理 | 证据 |
| --- | --- | --- |
| Node20不能运行新版脚手架 | 项目.local隔离Node24.12，不替换系统Node；npm10安装遇edgesOut，隔离npm11.8完成锁定安装 | [前端说明](../frontend/web-mvp.md) |
| priority无损解析后undefined | 误用lossless-json数值接口；先失败再改安全数字转换，ID/version保留字符串 | [首次真实RED](evidence/week04-web-first-tests.log) |
| 旧401退出同Token文本的新会话 | 文本比较不足，增加会话代次；同用户同秒Token可能相同 | 同上及[GREEN](evidence/week04-web-green.log) |
| 新用户短暂见旧组织 | 退出同步清orgs、重置组织分页，阻止旧响应写回 | [最终单元](evidence/week04-web-green-final.log) |
| 组织失败被抹掉/重试无效 | watcher继续load清错误，Retry未针对组织；保存失败操作并按实际结果继续 | [审查RED](evidence/week04-web-review-red.log)、[GREEN](evidence/week04-web-review-green.log) |
| Back重进登录导致会话混用 | 已登录/login替换/desk，登录函数与会话清理入口也作防护 | 同上，真实浏览器也验证 |
| body下载中断被报JSON错 | 将response.text移出解析catch，恢复网络/取消/超时分类 | 同上 |
| 浏览器录屏缺FFmpeg | Ubuntu20不受新版下载器支持；保留Chromium143，单独下载静态FFmpeg1013兼容包并实测录制 | [首次浏览器失败](evidence/week04-browser-attempt1.log) |
| E2E精确select标签匹配失败 | 嵌套select的label文本包含选项，夹具改非精确标签并限定创建表单；没有取消断言或改API | [第二次失败](evidence/week04-browser-attempt2.log)、[第一次全流程通过](evidence/week04-browser-attempt3.log) |
| 领取冲突提示位于长详情下方 | 检查真实截图后将全局错误/提示移到业务内容上方，方便立即看到 | 最终浏览器截图及前端回归 |

领取核心没有捏造已发生的业务Bug。真实“先查再无条件写”反证实验在专用夹具中让两个事务都成功，证明该方案危险；正式Claim没有采用它。测试代码失败路径清理也按backend review补齐，避免失败时连接未释放导致原错误被清理超时遮蔽。

## 6. 审查与选择记录

[后端独立Review](../reviews/week04-backend-review.md)未发现阻断，提出夹具清理建议，已补齐。[整体验证Review](../reviews/week04-final-review.md)报告2个Important及1个Minor，均用新增6项真实失败测试修复；报告保留审查时原结论，后面附最终处理记录，不改写成“从未发现问题”。审查者没有重跑测试或复审最终修复；修复证据是RED→GREEN及作者最终回归。

实施选择：继续当前目录并新建W04功能分支；admin既有代办权限保留；成员在事务内读取但不增加撤权线性化承诺；Node独立安装；浏览器复用本机缓存并明确兼容边界。设计/执行记录见[计划](../superpowers/plans/2026-11-10-week04.md)。

[npm audit](evidence/week04-web-audit.json)实际报告12项moderate、0 high/critical，均来自开发工具链（ESLint/TS-eslint/minimatch、Test Utils/js-beautify/glob）；未用强制降主版本或覆盖传递依赖隐藏报告。需要后续关注兼容补丁并复测，不声称供应链无风险；没有对Go执行额外漏洞扫描或渗透测试。

## 7. 维护与W05交接

日常修改先定位业务契约和责任层，再增加能捕获真实错误的测试。Go变更跑race/vet/build，Vue跑unit/build/lint/format；数据库/跨端变化跑`python3 scripts/week04-smoke.py`。错误时先看HTTP状态/安全提示和本地失败日志，不把所有错误归为网络问题。修改接口同步客户端DTO/示例，结构演进只能追加migration。

用户W03提交为7f1d7d2，本次分支feat/week04-claim-web-mvp；AI未stage/commit/push/merge/部署。真实.env未读取、修改或复制，日常数据库未使用；仅清理自身隔离资源，旧周报与原始手册保持历史事实。

本周可交接W05文档上传与知识库管理。公开服务前仍需可靠认证限流、HTTPS、会话撤销、备份和发布设计。个人需要自行走一次演示、读[20道回答](week04-answers.md)并闭卷解释；不能仅凭AI完成工程将自己标为掌握。
