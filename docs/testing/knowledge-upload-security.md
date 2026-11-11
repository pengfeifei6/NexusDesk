# W05 文件上传与解析验收

日期：2026-11-11。下表均由最终隔离环境实际运行，通过状态不由文件生成推断。命令：`python3 scripts/week05-smoke.py`；[完整日志](../weekly/evidence/week05-final-smoke.log)。底层使用真实 PostgreSQL17、pgx、成员校验、JWT 中间件、HTTP Handler、私有临时目录和原文件；故障用测试库触发器注入。

## 手册 24 项

| ID | 验证内容与实际结果 | 状态 |
| --- | --- | --- |
| B01 | active 成员 TXT 上传201，UPLOADED、上传者和组织正确 | PASS |
| B02 | MD 上传201，磁盘原字节、长度、SHA-256一致 | PASS |
| B03 | 分页、状态过滤、相同时间以id降序排序、空页及非法参数 | PASS |
| B04 | 本组织详情200；精确12字段，不含路径/key/正文 | PASS |
| B05 | 猜其他组织ID，详情/下载/text/process/delete均404；不先读文件 | PASS |
| B06 | 实际停用成员后旧Token仍存在，但组织内全部操作被拒绝 | PASS |
| B07 | 非上传者member删除403；上传者/admin分别204 | PASS |
| B08 | 软删后普通接口404，列表隐藏，数据库和磁盘保留 | PASS |
| S01 | 准确5,242,880字节合法TXT上传成功且hash一致 | PASS |
| S02 | 上限+1字节413，无新增元数据或对象 | PASS |
| S03 | exe/html固定415且无残留 | PASS |
| S04 | ZIP/NUL等二进制伪装MD被拒绝并清理 | PASS |
| S05 | 伪造part MIME不能绕过校验；四种CTE均400且无残留 | PASS |
| S06 | ../../x.md及反斜杠名称400，不逃逸 | PASS |
| S07 | 空文件/缺file/多部分400，错误请求媒体415 | PASS |
| S08 | 元数据INSERT与延迟COMMIT触发器失败500，行和文件均未遗留 | PASS |
| P01 | BOM/CRLF/CR规范化，READY、正文hash/码点数/版本/时间正确 | PASS |
| P02 | 开头及后段非法UTF-8均415，无对象发布 | PASS |
| P03 | 空白正文上传后解析422 EMPTY_TEXT，FAILED、无正文 | PASS |
| P04 | 超过1MiB输出422 TEXT_TOO_LARGE，FAILED、无超大正文 | PASS |
| P05 | 真实SQL取得处理权后用通道暂停，第二请求409，第一请求最终200 | PASS |
| P06 | READY重复解析409，无重复正文或时间/hash修改 | PASS |
| P07 | 正文INSERT和延迟COMMIT失败，不出现READY/正文；恢复后可重试 | PASS |
| P08 | 提取取消/超时、真实claim行锁和正文INSERT超时，错误/状态符合契约 | PASS |

用例定位：[knowledge_documents_test.go](../../tests/integration/knowledge_documents_test.go)的两个入口 `TestWeek05Schema` / `TestWeek05Knowledge`，子用例名称携带编号。更多断言及证据边界见[集成报告](../reviews/week05-integration-report.md)。

## 补充验证

- 迁移 6→8、重复Up、逆序退至6、再升8，保留原组织哨兵；约束、复合外键、唯一性和两个partial索引在真实库验证。
- process/delete 通道屏障竞争：删除先完成时禁止正文发布；源文件缺失/同长度篡改安全500；下载长度异常500，正常下载的附件头和hash正确。
- HTTP 整体大小含尾部、重复/未知参数、请求体、身份和路径严格检查。PDF只上传下载，process422。
- Storage race 测试覆盖随机key碰撞不覆盖、权限、符号链接、祖先链接绕过公开根目录、读失败/取消清理、损坏journal修复、journal发布失败重试、临时删除失败记录。
- 单元故障夹具覆盖上传COMMIT结果不明时保留对象和记录；未在真实网络中模拟COMMIT响应丢失，因此不把该项写成网络故障实测。
- Vue 70 项测试覆盖 FormData、Blob、无损ID、错误白名单、旧响应、下载URL释放、401及首次组织引导。真实浏览器 W05 十项检查见[results.json](../weekly/evidence/week05-browser/results.json)，W04十一项回归单独保存在[W05下的旧流程记录](../weekly/evidence/week05-week04-browser/results.json)。

## 复现与边界

```bash
python3 scripts/week05-smoke.py --backend-only
python3 scripts/week05-smoke.py
```

第二条包含后端与真实浏览器。`--web-only`跳过Go套件，只用于迭代浏览器，不能当作全量验收。脚本创建专有随机Compose project、回环端口、专用库名前缀和临时存储，结束只清理自己的资源，不读取真实.env。

普通 `go test ./...`：85个顶层通过、11个数据库入口Skip；显式启用真实数据库后的最终race：96个顶层通过、0失败、0Skip，10个包。统计以顶层Test计，子用例不重复累加。浏览器使用本机已有Chromium143和FFmpeg缓存，没有声称在全新机器安装验证。

没有测试断电后的文件系统恢复、分布式对象存储、PDF解析器、恶意软件扫描或公开流量下限流。这些不在本周P0范围。
