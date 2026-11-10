# W03 状态机与对象权限

角色精确admin/member。请求者必须是目标组织active成员，SQL必须属于同组织未删除工单。

| 当前 | 目标 | 权限和受理人条件 |
| --- | --- | --- |
| OPEN | IN_PROGRESS | 已指派同组织active受理人，受理人本人或admin |
| IN_PROGRESS | RESOLVED | 受理人仍active，受理人本人或admin |
| RESOLVED | CLOSED | 仅admin；保留原受理人 |
| RESOLVED | IN_PROGRESS | 仅admin，已有同组织active受理人 |

其他边（含原状态、自跳、CLOSED重开）409；合法边操作者不足403，非法目标字符串400。PATCH不能包含status；只有OPEN可编辑/指派。member只能编辑本人OPEN基础字段，admin可编辑所有OPEN及指派。DELETE仅admin，任意未删除状态可软删除；重复删除404。

transition/PATCH要求expected_version非负int64，旧版本409。工单加版本与事件同tx，评论不加版本；CLOSED不新增评论409。行锁+旧版本/旧状态条件更新保证两个相同旧版本修改至多一成功，不实现/claim。
