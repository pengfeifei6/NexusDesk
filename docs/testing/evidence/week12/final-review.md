# W12 最终独立审查与一次修复记录

审查者：独立新上下文 gpt-6-astra/high，依据 executing-plans / requesting-code-review，审查 W12 相对994文件基线的变化。只读，不修改文件、不运行测试、不访问私有配置、不派生其他审查者。不是根据包含 W08～W11 的 HEAD 总差异判断 W12。

初次结论 With fixes：0 Critical、2 Important、0 Minor。实现者核实两项均成立，按一次修复流程分别先红后绿，没有二次审查或声称审查者复验了修改。

## I1 权限拒绝后迟到读取恢复旧内容

管理 PATCH 与 Desk 详情 GET 使用独立 Scope。PATCH 返回403/404清空引用后，较早发出的 GET 仍可能被 Desk 接收并显示旧详情。

新增 management.spec 两个 mounted 场景（403、404）：提交 PATCH → 启动挂起的刷新GET → 写请求拒绝 → 旧GET返回。修复前两项失败，旧标题重新显示；[RED](review-access-red.txt)。新增统一 Desk.invalidateAccess，取消Desk请求、推进列表/评论/事件/组织序列，并向管理/成员作用域广播失效；清空详情和草稿。管理拒绝统一调用该入口。修复后14项management测试通过，旧标题不恢复且不再启动评论/事件读取；[GREEN](review-access-green.txt)。

## I2 源码包漏掉前端格式配置

隐藏文件过滤排除了web/.prettierrc.json与.editorconfig，初始预览只验证构建，没有验证原样格式检查。实际预览解包 format:check 报43文件风格不符，保留[失败输出](package-preview-format-failure.txt)。

新增包内容与配置符号链接回归，修复前失败；[RED](review-package-red.txt)。仅白名单这两个具体安全路径，仍排除其他隐藏文件并执行同样符号链接拒绝。修复后5项打包测试通过；[GREEN](review-package-green.txt)。在完整解包检查中进一步发现固定评测语料的.jsonl被扩展名白名单漏掉，保留[失败](package-candidate-initial-checks.txt)；新增实际解包后加载60题/8文档的测试先失败后通过，并补齐requests的.jsonl白名单，未放开原始证据目录。最终候选包另外执行全套七项检查，结果见[独立解包检查](package-candidate.json)。

## 修复后的回归

主目录全套前端163项、Python34项、Go vet/build、前端类型/build/lint/format通过，[checks.json](checks.json)。九组真实浏览器重新通过，[web.json](web.json)。Go源码、迁移及依赖无修改，已完成本周真实PG/Redis1181 PASS节点/286顶层/0Skip，[backend.json](backend.json)。最终演示单独重录，不把此前视频当最终版本。

审查者排除判断的事项由实现者明确裁定：

1. GitHub/公网部署/许可证选择/个人掌握仍是已声明的范围边界；后续需本人决定或完成，不由当前本地验收代替。
2. 未改 W08～W11 后端内部实现；本周仍做真实库全回归，但性能和真实模型质量只引用原证据，不保证未来模型别名或负载不变。
3. 最终归档、保留性与链接核验在文档冻结后执行，以实际报告为准，未执行前不标记通过。
4. 当前命令与证据限定 Linux/Bash；未验证 Windows/macOS，不能宣称跨平台已验收。

两项重要发现已由实现者以行为测试修复；没有搁置的 Minor。归档/保留性/链接最终核验由实现者执行，见summary.json与preservation.json。源码与文档未自动提交，忽略的审查工作区继续保留。
