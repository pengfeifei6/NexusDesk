# 第五周开发与验收记录

执行日期：2026-11-10～11（北京时间）。依据[第五周手册](../../项目开发文件/每周开发手册/08_NexusDesk_第五周详细开发手册.md)，在W04现有工作区增量实现。开发者明确委托整周完成，因此代码、文档、测试和审查由AI执行；手册中的“本人先写”属于教学默认，本次不据此中断任务，也不虚构个人已独立完成或掌握。

## 交付结果

W05 P0工程任务完成：组织内TXT/MD上传→元数据→显式解析→READY正文→授权下载→软删除，具有真实数据库/文件系统和浏览器证据。PDF仅签名检查后的附件上传/下载，自动解析明确422；可选PDF提取挑战未实施，不影响P0。知识库页面不代表RAG或G2上线。

结合此前“登录后没有创建功能”的反馈，补齐注册与创建组织页面。新用户不必先用curl建组织：注册成功回登录，工作台可创建首个组织并自动进入工单区，顶部切换知识库。没有顺带扩展成员管理、工单编辑/删除页面。

当前24业务API + healthz、8业务表、8迁移、7主要页面。W04未提交增量完整保留，当前分支`feat/week05-knowledge-ingest`，HEAD仍`7f1d7d2`；没有自动stage/commit/push。真实.env和私有文件未读取/改写，根目录无关`--data`/`-H`未处理。

## 按手册逐日交付

| 日程 | 实际内容 | 关键文件/验证 |
| --- | --- | --- |
| Day 1 | 七API、权限、状态、大小/超时及PDF边界 | [API](../api/knowledge-documents.md)、[入库设计](../knowledge/ingestion-design.md) |
| Day 2 | 元数据/正文分表、组织复合外键、partial索引 | 00007/00008；隔离库6→8→6→8且旧组织保留 |
| Day 3 | 私有Root、随机key、流式hash/计数、内容校验、上传补偿 | internal/platform/storage、knowledge/upload；边界与故障测试 |
| Day 4 | 分页/过滤、精简DTO、鉴权下载、上传者/admin软删 | scoped Repository/Service/Handler；B01～B08 |
| Day 5 | TXT/MD规范化、条件处理权、正文与READY同事务 | Extractor/Process；并发、触发器、超时/取消 |
| Day 6 | Vue列表/详情/预览/下载/删除、首次使用补齐 | web/src；真实API浏览器截图、旧响应与权限用例 |
| Day 7 | 24项验收、独立审查、回归、维护与问答、W06样例 | [矩阵](../testing/knowledge-upload-security.md)、[20道回答](week05-answers.md)、[十份样例](../knowledge/samples/README.md) |

每日个人代码贡献/独立解释等级均为“待开发者复习自评”，未记作已掌握。AI实际发现并验证的问题列在下面；没有将代理审查冒充开发者人工复核。

## 实际验证

| 检查 | 结果 | 证据 |
| --- | --- | --- |
| 完整真实库race与浏览器 | 退出0；10包、96个顶层Test PASS，0失败/Skip | [完整日志](evidence/week05-final-smoke.log) |
| 普通Go测试 | 85顶层PASS、11个DB入口Skip | [JSON日志](evidence/week05-go-test.jsonl) |
| Go vet/build/mod verify/gofmt | 全部退出0 | [质量日志](evidence/week05-go-quality.log) |
| web单元测试 | 4文件、70项PASS（含最终Unicode回归） | [单元日志](evidence/week05-web-unit.log) |
| web build/lint/format | 全部退出0 | [build](evidence/week05-web-build.log)、[lint](evidence/week05-web-lint.log)、[format](evidence/week05-web-format.log) |
| W01历史独立烟测 | 退出0；版本1、保卷重启、Go连接、信号退出和错误配置 | [历史回归](evidence/week05-week01-regression.log) |
| 日常库增量Up | 已核对本项目容器，只从6升8，未Down | [迁移证据](evidence/week05-development-migration.log) |

完整脚本的后端测试包含W02认证/组织、W03工单、W04领取竞争和W05入库。测试自行创建随机专用库、回环端口和临时存储，结束只删除本次自有资源。版本1/3/6历史脚本仍保持各自迁移边界，不能把历史脚本通过当成最新结构验证。

首次完整浏览器验证W04十一项、W05九项通过。最终仅增加前端Unicode校验后，又运行`--web-only`定向复验；它跳过Go，不替代前述后端全量结果。最新浏览器结果和截图见[W05 results](evidence/week05-browser/results.json)、[W04回归 results](evidence/week05-week04-browser/results.json)、[最终前端复验日志](evidence/week05-web-review-smoke.log)。本机使用已安装Chromium143/FFmpeg缓存，未声称全新机器安装成功。远程CI尚未执行。

24项B/S/P逐一对应真实用例，参见[矩阵](../testing/knowledge-upload-security.md)。文件权限、hash、5MiB及+1边界、额外part/尾部、路径/符号链接、跨组织、disabled旧Token、软删、并发处理/删除、真实INSERT/COMMIT失败、真实数据库等待超时均有证据。未使用测试通过来推断断电或所有网络文件系统行为。

## 审查发现与修正

1. **数据库阶段超时被转换成500。** Claim/finalize返回通用存储错误后丢失deadline；Service现按操作Context恢复504，并尽力保存PROCESS_TIMEOUT。真实行锁和正文INSERT超时验证无READY/正文误发布，未知claim结果不盲改另一请求的处理状态。
2. **清理journal可能留下截断内容且重试误报成功。** 改为先temp写入/Sync/Close，再原子Rename并同步目录。已存在空/截断/错误key/symlink记录、发布失败重试均有验证；临时对象删除失败也记录。
3. **multipart自动传输解码改变原字节。** 改NextRawPart，拒绝所有Content-Transfer-Encoding（含空header），四种真实HTTP用例无元数据/对象残留。
4. **Storage根目录可经祖先symlink指向web。** 增加canonical目录检查，并保留Root范围与0600/0700限制；先RED再GREEN。
5. **前端错误码原型属性和登录清空竞态。** 白名单改own-property检查；登录先保存当前输入再清会话。前端RED/GREEN证据保留；旧请求与Blob下载作用域保护覆盖切组织/退出/导航。
6. **Unicode输入计数不一致。** 去除与API码点计数冲突的原生min/max长度，按Array.from计数做本地提示；三个新增单元测试先失败后通过，另补真实注册/登录/100码点组织用例。该浏览器场景也暴露直接打开/register时初始路由未就绪导致跳登录的问题；main.ts改为等待router.isReady再挂载，失败日志见[直接注册RED](evidence/week05-web-direct-register-red.log)。

报告：[后端初审](../reviews/week05-backend-review.md)、[后端复审](../reviews/week05-backend-review-followup.md)、[最终独立审查](../reviews/week05-final-review.md)。审查为只读源码核对；测试执行由实现/集成任务负责，不混淆“review看过日志”和“review亲自重跑”。

集成开发前两轮backend烟测成功；第三轮恰在根任务新增storage RED用例时启动，W05真实库用例全部通过但总体退出1，不能记作全绿。随后存储修复并重新完整验收退出0；详情见[集成报告](../reviews/week05-integration-report.md)。所有早期RED均保留，不覆盖最终结果。

## 当前运行与维护

现有日常库已仅Up到8；容器日志时间为UTC，周报使用北京时间。未检查用户正文或凭据。原来运行的Go API不会热更新，需要在原终端Ctrl+C后重新`go run ./cmd/api`；新开终端先自行加载已有配置。前端已运行则刷新后登录，未运行见[演示步骤](../demo/g2-ingestion-demo.md)。

五个新配置可省略，默认私有`./var/storage`、5MiB文件、8MiB请求、1MiB正文、10秒处理。已有十二个必需配置保持；`.env.example`更新不代表真实.env被修改。HTTP读请求15秒/写30秒，为同步处理及失败收尾留时间。

数据库和私有对象需一起备份。软删除保留对象/正文；journal和孤儿文件需核对引用，不能直接删除。进程被杀、claim提交不明、数据库持续故障可能留下PROCESSING或孤儿；本周没有自动恢复/清理器。局限和人工核对顺序见[ADR-0005](../decisions/0005-private-object-storage.md)。

## W06交接与未完成项

- 十份原创虚构TXT/MD已全部在隔离库解析READY，包含标题、组织A/B分配、实际ID/hash：[清单](evidence/week05-sample-manifest.json)。隔离库已清理，日常库没有自动导入这些样例。
- W06：从READY正文设计切块、Embedding、组织过滤检索与效果评估。文档仍是不可信数据，不能执行其中指令。
- W07：持久任务、租约/心跳、重试预算、幂等完成与PROCESSING恢复；当前同步处理不冒充生产异步架构。
- PDF正文提取挑战、全局去重、物理清理/恢复、磁盘配额、生产备份恢复、HTTPS/可靠限流与公开部署尚未实现。
- 当前源码/差异/统计见[交付清单](evidence/week05-source-baseline.json)、[测试统计](evidence/week05-test-summary.json)。原00001～00006、Go依赖和web锁定依赖保持W04基线；没有自动提交。

建议复习顺序：先按演示操作一遍，再看20道回答，最后从Handler→Service→Repository/Storage跟一遍上传与解析链路。工程已交付，独立重写和理解程度留给本人后续确认。
