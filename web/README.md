# NexusDesk Web

Vue 3 / TypeScript 业务工作台，调用真实 Go API。要求 Node ^22.18.0 或 >=24.12.0；本周使用项目独立 Node 24.12.0。

```sh
npm ci
npm run dev
npm run build
npm run test:unit -- --run
npm run lint
npm run format:check
```

开发服务仅监听 127.0.0.1。`/api` 与 `/healthz` 默认代理到 `http://127.0.0.1:8080`；可在启动进程环境中设置 `NEXUSDESK_API_TARGET` 为回环 HTTP(S) 地址。Vite 禁用 dotenv 加载；不读取真实 `.env`。生产产物需要同源 API 路由，未提供公开部署方案。

登录使用已有账号；组织/成员由后端 API 预置。Token 只在内存，刷新页面需重新登录。详情可访问 `/organizations/:orgID/tickets/:ticketID`，后端执行授权。所有 ID/version 保留十进制字符串，通过 lossless-json 解析/编码，提交 expected_version 仍为 JSON 整数。失败写操作不自动重试。

前端实现由 AI 按 W04 整周委托完成；浏览器真实联调由根目录隔离 runner 负责。
