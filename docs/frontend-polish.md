# 完整 API 文档与前端视觉优化

本次在原 NexusDesk 主目录完成，后端保持现有实现。目标是把已有业务工作台整理成排版、层级和交互一致的现代 B2B 界面，并交付统一 API 契约。

## 完整 API 文档

[中文完整手册](api/NexusDesk_API完整文档.md)包含39个业务操作的权限、参数、输入约束、DTO、请求/响应、错误和完整示例；另列healthz与诊断边界。[OpenAPI3.1 JSON](api/openapi.json)可导入支持该版本的接口工具，含40个operationId（含healthz）、82个schema、6类SSE事件及合成帧。HTTP GET/JSON/multipart/二进制下载/SSE的媒体与信封分别说明。

静态检查将文档与当前Handler、总索引、14个DTO字段集合核对，验证707引用和400示例和376条逐操作语义断言。这是标准库静态子集校验，不是第三方完整OpenAPI认证；接口工具对OpenAPI3.1及int64无损支持仍需按其实际能力选择。JS不能先JSON.parse再把已经舍入的Number转为字符串。

## 新界面

采用石墨导航、浅灰画布、低饱和蓝强调、细边框、圆角与适度阴影，强调正文可读性和操作层级。设计参考[Linear2026界面刷新](https://linear.app/now/behind-the-latest-design-refresh)的密度和层次，以及[VercelGeist](https://vercel.com/geist/introduction)的一致性原则，读取日期2026-11-11；没有复制品牌图像或引入网络字体。

- 登录/注册：重排品牌叙事、功能预览与表单，区分背景与操作区域。
- 导航/顶栏：团队选择区、当前位置、选中态、身份与移动抽屉统一。
- 组织工作台：新的欢迎区域、代码原生图形与真实组织快捷入口；保持账号编号和创建组织。
- 工单：当前页概览、新建锚点、列表标题、过滤/分页、表单和详情分层。概览仅统计当前加载页，随筛选更新，不是组织总量。
- 知识库/成员：文档说明、上传入口、资源与成员表格、管理表单统一。
- RAG/AI：问题输入、引用来源、对话列表、消息、确认提议和输入框层次一致，保持明确的人为确认。

视觉样式在`web/src/assets/workspace-theme.css`，建立在原`main.css`结构契约上，后者继续负责基础布局和既有页面结构。更换主题先修改当前主题层，避免在各Vue文件散落内联颜色。没有加入新的组件库或字体依赖。

## 实际前后对比

| 页面 | 优化前 | 优化后 |
|---|---|---|
| 登录 | [before](testing/evidence/frontend-polish/before/01-login.png) | [after](testing/evidence/frontend-polish/after/01-login.png) |
| 组织工作台 | [before](testing/evidence/frontend-polish/before/03-workspace-empty.png) | [after](testing/evidence/frontend-polish/after/03-workspace-empty.png) |
| 工单 | [before](testing/evidence/frontend-polish/before/04-tickets.png) | [after](testing/evidence/frontend-polish/after/04-tickets.png) |
| 成员 | [before](testing/evidence/frontend-polish/before/14-members.png) | [after](testing/evidence/frontend-polish/after/14-members.png) |
| 引用问答 | [before](testing/evidence/frontend-polish/before/08-rag.png) | [after](testing/evidence/frontend-polish/after/08-rag.png) |
| AI提议 | [before](testing/evidence/frontend-polish/before/09-ai-proposal.png) | [after](testing/evidence/frontend-polish/after/09-ai-proposal.png) |
| 手机工单 | [before](testing/evidence/frontend-polish/before/12-mobile-tickets.png) | [after](testing/evidence/frontend-polish/after/12-mobile-tickets.png) |

截图来自本次前后两轮实际浏览器，不是设计稿。仅使用随机独立PG/Redis、合成数据和确定性本机模型；真实.env、日常业务数据与DeepSeek API未参与。

## 验证与维护

新增三个行为测试先失败后通过：当前页统计随过滤变化且不新增API调用；组织快捷入口保持大整数ID和真实导航；页内新建入口保留草稿、筛选及路由。全量前端166项/9文件、类型/build、lint和format通过。API文档校验通过。

真实浏览器12组流程通过：原有注册、组织成员、工单编辑/指派/冲突/删除、异步知识库、引用问答、SSE确认、退出/组织隔离，以及新增多尺寸检查。17张主要页面截图加12张360/768补充截图；另覆盖390/1440视口。353处核心文字样本对比度均≥4.5:1；初测发现AI选中项4.47:1；独立审查后扩展到全部标签/标题又发现AI侧栏3.79/2.46，均调深后通过。该抽样忽略背景图案并按计算样式合成纯色背景，不能等同全面无障碍认证或所有设备验收。键盘Escape关闭抽屉与焦点返回、reduced-motion模式均由真实流程检查。页内新建/上传入口只做焦点与滚动定位，实际验证第二页草稿、筛选、分页及已选文件不丢失，也不增加GET请求。

证据：[检查](testing/evidence/frontend-polish/checks.json)、[浏览器](testing/evidence/frontend-polish/after/smoke.json)、[原始页面记录](testing/evidence/frontend-polish/after/browser.json)。独立审查发现的三项Important均经失败回归修正，详见[审查与修复](testing/evidence/frontend-polish/final-review.md)，[最终汇总](testing/evidence/frontend-polish/summary.json)和[保留性](testing/evidence/frontend-polish/preservation.json)。

```bash
python3 scripts/check_api_reference.py
python3 scripts/frontend-polish-smoke.py --stage after
```

前端检查继续使用`npm --prefix web run test:unit -- --run`、`build`、`lint`、`format:check`；Node须满足package.json。浏览器沿用已安装的Chromium与独立测试夹具，前提见[运行指南](release/quickstart.md)。这次没有新增后端能力或改变接口/授权语义。

本机服务运行时打开http://127.0.0.1:5173；若仍显示旧样式，做一次强制刷新。W12源码包与旧演示保留为历史交付快照，本次最新界面以当前源码及新截图为准。
