# G1 本地演示与复现

成果范围：注册/登录和组织权限（W02）、工单状态/评论/事件（W03）、原子领取和Vue业务页面（W04）。角色为admin/member，不增加support、角色编辑UI、知识库或AI。

## 自动复现

前提：Go、Docker Compose、本地postgres:17、Goose3.29.0、Node24.12.0或满足web/package.json engines的版本、已安装锁定前端依赖和Chromium。首次准备：

```bash
cd web
npm ci --no-audit --no-fund
npx playwright install chromium
cd ..
python3 scripts/week04-smoke.py
```

脚本自动创建独立测试库与临时凭据，运行全部Go真实数据库测试、历史TCP演示，启动临时API与回环Vite，然后用两个独立浏览器会话完成G1。自动截图、录屏，停止自己启动的服务、容器，删除自己的标签卷。**不要向脚本传入日常数据库，不需读取.env。** Node24本机备选路径为`.local/week04-tools/node_modules/node/bin/node`，该目录被忽略、不进入仓库；runner优先使用它，其他机器使用PATH中的Node。

本机Ubuntu20.04不受新Playwright Chromium下载器支持；实际使用已有Chromium143.0.7499.4并记录版本。本机FFmpeg1013通过`PLAYWRIGHT_HOST_PLATFORM_OVERRIDE=ubuntu22.04-x64 npx playwright install ffmpeg`下载静态兼容包并实测录屏；该操作不代表Ubuntu20获得官方支持。脚本优先环境`NEXUSDESK_CHROMIUM_PATH`、已安装匹配版本，其次已有Chromium缓存。若均无，明确报错，不跳过浏览器测试。新机器建议在Playwright支持的操作系统安装配套浏览器，不能把本机缓存复用称为全新机器验收。

只跑后端：`python3 scripts/week04-smoke.py --backend-only`；仅迭代浏览器/进程：`python3 scripts/week04-smoke.py --web-only`，它不会重跑Go测试，不能用其日志冒充全量验收。

## 1～2分钟展示顺序

1. Alice登录，选择A，从真实列表创建一张高优先级OPEN工单。
2. Bob、Charlie各自登录独立会话，打开同一工单，同时点击领取。
3. 展示恰好一个200、另一个409；失败页面刷新后能看到受理人。数据库版本从0到1，只有一条领取事件。
4. 赢家点击开始处理，状态IN_PROGRESS，再发一条评论；展示TICKET_CREATED、TICKET_ASSIGNED和TICKET_STATUS_CHANGED事件。
5. 展示网络离线提示与恢复；刷新页面回登录，浏览器存储无持久Token。
6. David登录B，使用A的路径或B-org/A-ticket组合，后端404且不显示A内容；再演示切换组织清空旧数据。

自动化录制会在关键页面停留以便阅读，这些展示停顿不参与并发成功判定。它是真实浏览器/API操作录屏，不是开发者个人独立演示，不含音轨讲解。密码为临时随机值且输入框掩码；Token/数据库连接串不会进入页面、视频、截图、HAR或日志。

产物在`docs/weekly/evidence/week04-browser/`：`g1-demo.webm`、七张截图和`results.json`；最终运行记录与具体结果见[第四周周报](../weekly/week04.md)。

## 自己操作日常开发环境

1. 按[README](../../README.md)由你自己配置环境并启动PostgreSQL、Goose up到6和Go API。启动API不自动迁移。
2. Node满足engines后在web执行`npm ci`、`npm run dev`，浏览器打开终端给出的127.0.0.1地址；默认代理到127.0.0.1:8080。不同API端口用`NEXUSDESK_API_TARGET=http://127.0.0.1:端口 npm run dev`。
3. 用W02 API注册演示用户、创建组织、添加已注册成员；前端没有账号注册/组织管理页面。密码由你维护，不保存到HTTP示例或Git。
4. 使用两个独立浏览器会话登录不同成员，按上面顺序操作。Network面板可看URL/状态码和expected_version，截图或分享前隐藏Authorization和登录请求体。

这是本地G1，`npm run build`产物不是已经配置好代理/HTTPS的生产部署。公开服务前还需认证防滥用、HTTPS、会话撤销、备份与发布设计。

本次交付视频约68.56秒：将45.72秒真实原始录制按2/3速播放，方便阅读页面和错误提示，不改动作、内容或结果。原始文件保留为[原速录屏](../weekly/evidence/week04-browser/g1-demo-raw.webm)，[阅读版](../weekly/evidence/week04-browser/g1-demo.webm)只调整时间戳。重新运行脚本会生成当次原速录屏；时长随操作与机器变化。
