# W03 工单API契约

统一前缀/api/v1/organizations/:orgID/tickets，九个接口均使用W02 RequireUser。orgID/ticketID为正十进制int64；认证→组织active关系→对象归属/未删除→动作权限→状态/版本。非成员/disabled/跨组织/软删404，身份401，角色不足403，状态或旧版本409，输入400，JSON超64KiB为413，媒体415，故障安全500。错误格式沿用W02，Cache-Control:no-store，401带Bearer challenge。

## 接口

| 方法 | 相对路径 | 输入/结果 |
| --- | --- | --- |
| POST | / | title/body必需，priority可选默认2；201，OPEN/version0 |
| GET | / | page/page_size默认1/20最大100，status/priority可选；200 |
| GET | /:ticketID | 无query/body；200 |
| PATCH | /:ticketID | expected_version必需，title/body/priority/assignee_id至少一项实际变化；200 |
| DELETE | /:ticketID | 无query/body，仅admin；204，软删并加version |
| POST | /:ticketID/transition | to_status、expected_version必需；200 |
| POST | /:ticketID/comments | content必需；201，不加version，CLOSED409 |
| GET | /:ticketID/comments | 仅page/page_size；200 |
| GET | /:ticketID/events | 仅page/page_size；200 |

表中集合路径没有尾斜杠。POST/PATCH禁止query，GET/DELETE无body。JSON仅允许契约字段，单对象、严格大小写/重复字段/Unicode；数值拒绝字符串/小数/指数/null/溢出。expected_version允许0；assignee_id仅admin可用，null解除OPEN指派，否则正int64且同组织active成员，非法目标400 INVALID_ASSIGNEE。

title去空白1..200码点，body1..10000，content1..5000，均有效UTF-8无NUL；priority为1低/2普通/3高。status仅OPEN/IN_PROGRESS/RESOLVED/CLOSED。列表无total，返回items/page/page_size，空items=[]；工单created_at/id DESC，子列表ASC，重复/未知query及偏移溢出400。

创建例：

```json
{"title":"示例问题","body":"仅为虚构输入","priority":2}
```

PATCH例：

```json
{"expected_version":0,"priority":3,"assignee_id":102}
```

工单DTO为id/org_id/creator_id/assignee_id/title/body/priority/status/version/created_at/updated_at，未分配assignee_id为null；不返回deleted_at。评论DTO为id/ticket_id/author_id/content/created_at；事件为id/ticket_id/actor_id/event_type/ticket_version/before_data/after_data/created_at，仅结构化安全字段，评论事件只含comment_id。时间UTC RFC3339。

成功统一data包裹，列表data含items/page/page_size。错误固定code/message/request_id空串；NOT_FOUND/UNAUTHENTICATED/FORBIDDEN/INVALID_REQUEST/INTERNAL_ERROR沿用W02，新增409 CONFLICT（state or version conflict），400 INVALID_ASSIGNEE（assignee must be active in organization）。无实际变化PATCH400。重复DELETE404。无请求级幂等去重；重复POST正常可创建多条，失败提交结果不确定时不自动重试。
