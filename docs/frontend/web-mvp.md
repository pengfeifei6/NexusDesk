# W04 Vue 业务 MVP

> W04设计与证据归档；W05增加注册、首个组织创建和知识库页面，当前入口见[W05演示](../demo/g2-ingestion-demo.md)。

AI 按整周委托实现。Vue 3 + TypeScript + Router + Pinia + Vitest + ESLint/Prettier，使用官方 create-vue。当前宿主 Node 20 不满足 create-vue 3.24.0 要求，独立 `.local/week04-tools` 使用 Node 24.12.0，不修改系统 Node。开发仅回环监听。

按登录→GET /me→组织选择→列表/建单→详情/领取→开始处理/评论/事件顺序实现。组织和成员由现有 API 预置，前端不加管理 UI。只调用真实 `/api/v1`，开发 Vite 代理 `/api` 和 `/healthz` 到回环 Go API，默认 127.0.0.1:8080，可由非 VITE 的 NEXUSDESK_API_TARGET 配置。生产构建不提供代理，不宣称公开部署完成。

Token 仅 Pinia/内存，刷新必须重新登录；不写 localStorage/sessionStorage/Cookie，不把凭据放 VITE 变量、日志或页面。401 清理当前会话并回登录；旧会话迟到的 401 不能退出新会话。组织切换立即清空旧组织列表、详情和表单作用域，取消旧请求并用作用域校验屏蔽迟到响应。

分页真实使用 page/page_size（最大100）；状态/优先级筛选重置第一页。优先级 1低/2普通/3高。创建仅 title/body/priority。详情仅 OPEN 且未指派显示领取；请求 expected_version；409 提示后刷新详情，保留冲突提示。受理人或 admin 可开始/解决，admin 可关闭/重开，遵守 W03；CLOSED 不允许评论。事件/评论使用分页，不冒充完整历史。

统一 HTTP 模块区分401、403、404、409、5xx、网络断开、超时、取消和非 JSON；不显示服务端原始异常/HTML。写操作不自动重试。JS 与 Go int64 精度必须有显式处理及测试，禁止静默取整。服务端授权仍为最终依据。

验证：客户端与会话/组织过期响应单元测试；真实浏览器调用 Go + PostgreSQL 完成 C17～C20 和六项联调；截图与视频不含密码、Token、连接串，录制的是自动化操作，不冒充开发者亲自演示。

参考：[Vue 官方快速开始](https://vuejs.org/guide/quick-start)、[Vite 开发代理](https://vite.dev/config/server-options)、[Playwright 视频](https://playwright.dev/docs/videos)。

## 实际前端结构与运行（2026-11-10）

官方 `create-vue@3.24.0 web --ts --router --pinia --vitest --eslint --prettier --bare` 生成起点，业务由 AI 按委托实现。实际锁定 Vue 3.5.45、Router 5.4.0、Pinia 4.1.0、TypeScript 6.0.3、Vite 8.3.7、Vitest 4.1.12、ESLint 10.14.0、Prettier 3.9.6、lossless-json 4.3.3，完整树见 `web/package-lock.json`。测试环境固定 jsdom 26.1.0 / Vue Test Utils 2.4.6，避免新版传递依赖要求 Node 24.15，保持独立 Node 24.12.0。移除未使用 devtools、Oxlint 和 npm-run-all2。浏览器 runner 使用开发依赖 Playwright 1.65.0，实际浏览器环境与结果由根目录验证记录负责。

`App.vue` 只装配页头、组织选择、提示和 RouterView；`views/` 分登录、组织入口、工单列表和详情，`components/TicketForm.vue` 单独建单表单；`composables/desk.ts` 管理业务请求作用域，`lib/client.ts` 统一传输，`lib/types.ts` 为 DTO，Pinia session 保存当前用户及会话代次。路由为 `/login`、`/desk`、`/organizations/:orgID/tickets`、`/organizations/:orgID/tickets/:ticketID`。详情按路径请求后端，对象可见性由 API 判断。

Vite 配置 `envDir: false`，禁用自动 dotenv 读取；只从启动进程环境取 `NEXUSDESK_API_TARGET`，验证为显式回环 HTTP(S) 且不能携带用户密码。开发及 preview 都监听 `127.0.0.1`，API 默认代理至 `http://127.0.0.1:8080`，未添加真实 `.env`。可在 `web` 目录运行 `npm ci`、`npm run dev`；本周工具路径可将 `../.local/week04-tools/node_modules/node/bin` 加入 PATH。首次安装曾遇宿主 npm 10.8.2 的 `edgesOut` 内部错误，改用 `/tmp` 隔离 npm 11.8.0 完成安装，未修改系统工具。

所有 `id`、`*_id`、`version`、`ticket_version` 由 lossless-json 解析为十进制字符串，避免先经过 JS Number；提交 `expected_version` 时用 LosslessNumber 输出 JSON 整数。其他数字要求 `Number.isSafeInteger`，超出范围拒绝响应。GET 使用内存 Token、`credentials: omit`、`cache: no-store`；客户端 15 秒超时。独立 Token 代次处理同一用户同秒重新登录可能收到相同 Token 文本的情况，旧请求 401 不能退出新会话。会话失效后保留安全提示。组织/会话切换取消请求并校验作用域；同组织筛选及分页另加递增序号，旧成功/失败均不能覆盖新结果。退出同步清组织列表并恢复组织第一页，防止新用户短暂看到旧组织选项。

列表显示状态、优先级、受理人和更新时间，状态/优先级筛选重置第一页；所有分页明确显示当前页，因 API 没有 total，下一页按本页是否满 20 项判断，末尾可能出现空页。组织列表不包含目标组织时按实际分页查找当前角色，服务端仍最终授权。详情包含版本、优先级、受理人、更新时间。领取仅 OPEN 且未指派可见；409 刷新后保留提示，刷新失败明确提示手动重试。重新打开严格遵守 W03 的 RESOLVED→IN_PROGRESS，仅 admin；CLOSED 没有重开入口。

前端质量命令为 `npm run build`（包含 vue-tsc）、`npm run lint`（不自动修复）、`npm run test:unit -- --run`、`npm run format:check`。本次前端执行全部通过，22 项测试覆盖 int64 上限及版本编码、安全错误/非 JSON/异常响应、超时、旧 401 与同 Token 新代次、组件挂载后的组织切换/筛选乱序/退出/新登录组织清空。初始测试因依赖未安装未能执行，不能视为行为 RED；后续真实观察并修复 priority 解码、同 Token 401、失效提示、异常响应和旧筛选错误、跨会话组织残留的失败，日志保存在 `docs/weekly/evidence/week04-web-*.log` 后由整周验收保存。这里的单元测试使用受控响应，不代替真实浏览器或 PostgreSQL 联调。

最终审查补充：新增 6 项真实失败测试后修复组织初始加载错误被清除、组织分页错误重试目标不正确、已登录浏览器 Back 返回登录表单，以及响应头成功后 body 流断开/取消/超时被误分为 JSON 异常。错误提示保存对应重试操作；初始组织失败时不继续清除错误的后续加载；已登录 `/login` 替换为 `/desk`，登录函数同时检查身份并为匿名登录建立清空后的会话边界；body 读取错误由统一传输层分类，只有解析/信封异常映射响应格式错误。最终质量检查再次全部通过，单元测试由上述 22 项增至 28 项，真实 RED/GREEN 为 `docs/weekly/evidence/week04-web-review-{red,green}.log`，最终结果为 `docs/weekly/evidence/week04-web-green-final.log`。依赖审计报告 12 项 moderate、0 high/critical，集中在开发工具 ESLint/TS-eslint/minimatch 与 Test Utils/js-beautify/glob 链，无运行时依赖节点；未通过强制跨主版本覆盖来隐藏审计结果。

最终截图检查后，将全局错误/冲突提示置于业务内容上方，避免长详情页面的反馈落到首屏之外。web/go.mod仅作Go命令遍历边界，阻止根go ./...扫描npm包附带的Go示例，不新增Go服务或依赖。
