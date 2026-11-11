# W05 知识库入库演示（G2前置）

本周可演示附件到受权限控制正文的流程；尚无RAG问答，G2未完成。完整证据见[周报](../weekly/week05.md)。

## 启动当前本机

2026-11-11 已将现有项目数据库从6追加升级到8，未回滚或改用户数据，见[迁移记录](../weekly/evidence/week05-development-migration.log)。Go API启动不会自动迁移；换机器仍按[README](../../README.md)配置和迁移。

在原来运行Go的终端 Ctrl+C 后，从项目根目录重新执行：

```bash
go run ./cmd/api
```

原终端已加载的环境变量仍保留。若新开终端，先由自己按README加载本地配置，否则仍可能出现JWT_ISSUER等缺失；AI未读取或改写真实.env。新存储变量有默认值，已有配置无需为了W05额外增加必填项。

前端已运行则刷新页面并重新登录；未运行时在仓库根目录：

```bash
export PATH="$PWD/.local/week04-tools/node_modules/node/bin:$PATH"
npm --prefix web run dev
```

该PATH是当前机器已有的Node24工具路径；其他机器准备满足package.json要求的Node后执行npm ci。浏览器打开Vite显示的地址，默认 `http://127.0.0.1:5173`，代理API到8080。

## 页面步骤

1. 登录页点注册，填写自己的测试账号；成功回登录后登录。
2. 无组织时直接在工作台“创建组织”；创建成功自动进入工单列表，创建者角色由服务端返回为admin。已有组织可直接选。
3. 顶部切“知识库”，上传 [01-onboarding.md](../knowledge/samples/01-onboarding.md)，观察UPLOADED、大小、上传者与时间。
4. 点击解析，观察READY和纯文本预览；下载并对照原文件。预览不会执行文档里的HTML或脚本。
5. 返回列表切状态筛选、翻页。空白文本解析显示FAILED；PDF显示明确不支持提取，但仍能下载。
6. 上传者或管理员删除，页面回列表；原详情/预览/下载都变成404。
7. 用另一组织成员访问同一详情路径验证404。添加已注册成员目前使用[W02成员API](../api/auth-organization.md)，没有成员管理页面。

刷新页面回登录是内存Token策略，属于当前设计。403表示有效成员无该动作权限，404可能表示无组织权限或对象已删除。旧Go进程若未重启，新知识库接口会404；不要因此重复改数据库或注册。

## 自动演示与截图

`python3 scripts/week05-smoke.py` 创建独立测试环境，真实注册→建组织→建工单→知识库全流程；十份原创样例分别进入A/B组织并解析READY，结束清理隔离库。因此日常库不会自动出现这些样例，需要通过页面自行上传。

- [首次组织页面](../weekly/evidence/week05-browser/01-create-first-organization.png)
- [上传成功](../weekly/evidence/week05-browser/02-uploaded.png)
- [READY纯文本预览](../weekly/evidence/week05-browser/03-ready-preview.png)
- [分页过滤](../weekly/evidence/week05-browser/04-list-filter.png)
- [PDF边界](../weekly/evidence/week05-browser/05-pdf-boundary.png)
- [跨组织拒绝](../weekly/evidence/week05-browser/06-cross-organization.png)
- [删除后列表](../weekly/evidence/week05-browser/07-after-delete.png)

本机沿用已安装的Chromium143，未重新安装最新浏览器；兼容背景见[G1演示](g1-demo-script.md)。本周截图与最终results.json同步生成，不包含Token/密码。
