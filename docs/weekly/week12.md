# 第十二周开发记录：本地 MVP 完整交付

依据十二周总计划补写[七天手册](../../项目开发文件/每周开发手册/15_NexusDesk_第十二周详细开发手册.md)，在原 NexusDesk/main 完成。AI 负责此次代码、测试、视频与文档；开发者后续审阅、独立演示与 Git 发布，不将生成结果写成个人已经掌握。

## 七天交付

| 日程 | 实际结果 |
|---|---|
| Day1 | 核对 W11 和原计划；保存原目录 994 个既有文件哈希与文本基线；确定本地 MVP 交付边界 |
| Day2 | 新增组织成员页、分页、admin 添加已注册成员、工作台账号编号；权限失效和旧响应隔离 |
| Day3 | 工单 OPEN 编辑、admin 指派/解除、删除确认；version409 刷新不重放，晚删除不影响新路由 |
| Day4 | 安全初始化与本地源码打包工具；临时配置 0600、实际随机库迁移14、无覆盖与符号链接防护 |
| Day5 | Go 真实数据库 race、前端/Python/static 与九组真实浏览器验收；可移植 Node 选择 |
| Day6 | 5分19.00秒字幕演示、17截图、架构/API/功能矩阵、简历与十二个深入问题 |
| Day7 | README 当前入口、历史归档、维护/回答/最终介绍；源码解包构建与独立审查收尾 |

39 个业务 API、14 个迁移、14 业务表+1缓存版本表保持不变，主要页面从9个增至10个。没有新增后端路由、第三方依赖或修改真实 .env；日常 DB/Redis 和私有数据不参与实验。

## 实际测试

| 检查 | 结果与证据 |
|---|---|
| Go real PG/Redis race | 1181 PASS 节点、286 顶层测试、0 Skip；[backend](../testing/evidence/week12/backend.json)、integration.log |
| 前端 | 163 项/8 文件；TypeScript/build、lint、format 通过；[checks](../testing/evidence/week12/checks.json) |
| Python | 34 项通过；初始化/打包/Node 版本选择与既有工具回归 |
| Go static | vet/build 通过，W12 不改 Go 源码 |
| 真实迁移 | 临时 init0600 → Up14 → status → 再Up；[setup](../testing/evidence/week12/setup.json) |
| 浏览器 | 九组流程、17截图、0 pageerror；[web](../testing/evidence/week12/web.json) |
| 演示 | 独立重跑九组断言；实际视频容器319.00秒、七章，无音轨；[video](../testing/evidence/week12/video.json) |
| 源码包 | 初版构建通过后发现格式/语料缺漏并修正；最终候选504文件解包后独立npm ci和全部七项检查通过；复用工具链/下载缓存，不复用原node_modules；[最终候选证据](../testing/evidence/week12/package-candidate.json) |
| 最终审查 | 一次独立审查发现2项Important，作者均以先红后绿测试修复，无遗留Minor；[记录](../testing/evidence/week12/final-review.md) |

浏览器验收覆盖普通成员读、admin 添加/重复拒绝、编辑/指派/解除、实际409不覆盖、成员PATCH/DELETE403、取消删除不写入/确认删除后404，以及原有注册/组织/工单领取/评论、异步文档、引用问答、SSE人工确认、390px布局与组织切换。数据库和页面都真实运行，模型是本机确定性 HTTP 夹具，云调用0；不能声称真实 AI 质量重新评估。

## 失败与修正记录

最初12 个新增 mounted 测试先在缺失 UI/撤权旧详情残留下失败，再实现；另外验证409后刷新失败不能谎称刷新成功。Python 缺失模块测试先失败；手册父目录符号链接可能越过打包边界的测试先失败，随后补齐祖先检查。Node 选择测试先失败后加入可移植解析。

初次浏览器脚本使用的重复成员提示文字与产品实际文本不一致，等待超时；保留[失败日志](../testing/evidence/week12/browser-initial-selector-failure.txt)，修正精确选择器后九组全过。没有删业务断言或修改产品错误消息来掩盖失败。

最终独立审查补出两个边界并修复：共享Desk请求在权限拒绝后仍可恢复旧详情，新增403/404延迟读写回归；源码包漏掉Prettier/EditorConfig，新增精确白名单与符号链接回归，并实际确认初版解包格式检查失败。修复后前端163、Python34和九组浏览器全过，候选包补跑完整七项检查，并据实际失败补齐测试所需的固定JSONL语料；新增解包加载语料回归先红后绿，Python最终34项。

查看了新增成员页、工单管理区和手机成员页实际截图：文字可读、管理入口与原工作台风格一致、390px 页面无水平溢出。完整桌面1440×1000，手机390×844。演示过程只含合成数据，不展示密钥或 Token。

## 交接入口

[运行](../release/quickstart.md) · [维护](../release/runbook.md) · [架构](../release/architecture.md) · [39API](../release/api-index.md) · [边界](../release/acceptance.md) · [演示](../demo/week12-demo.md) · [本周回答](week12-answers.md) · [深入问题](../release/interview.md) · [最终项目介绍](../项目最终交付介绍_第十二周.md)。

## 实施选择与影响

- 按已有授权直接在原 main 开发，不重复确认、不创建 worktree/提交。若错误理解范围，额外需求须另行实现；已有未提交工作多，因此本周审阅用994文件基线区分，不以HEAD全部差异冒充W12。
- 采用 inline 实现和一次全局独立审查；任务之间共享组件和交付文件，避免拆目录导致管理困难。代价是作者阶段自查的盲点留到全局审查处理。
- “完成”限定十二周本地求职 MVP；不新增角色修改、PDF提取或公网部署。若期待这些远期能力，需要另外开发，不能把当前边界写成已完成。
- W12 复用既有 W11 归属保护夹具和其临时目录，独立写 week12 证据；历史夹具未复制修改。源码包不带原始证据，独立源码用户需重新生成W12结果，历史实验仍需主仓库证据。
- 保留忽略的审查工作区，因为尚未提交；它可追溯本次选择，不删除旧周工作记录。


## 最终收尾核验

994个既有文件无删除，496份旧证据逐字节保留，迁移/依赖锁未改，旧README原文一致，HEAD未变；[保留性](../testing/evidence/week12/preservation.json)。当前267份源码/配置哈希见[source-manifest](../testing/evidence/week12/source-manifest.json)。主API、前端、API诊断和Worker诊断均HTTP200，见[runtime](../testing/evidence/week12/runtime.json)。

最终本地源码包为主目录.local/releases/nexusdesk-v1.0.0-source.tar.gz；归档校验、与已通过七项检查的候选包非文档内容一致性、链接与秘密模式扫描见[交付汇总](../testing/evidence/week12/summary.json)。原始证据/视频排除在精简源码包外，保留于当前主仓库；没有自动公开发布。
