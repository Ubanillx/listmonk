# 对外营销 API：接口、参数与返回值

核对日期：2026-10-05，仓库 v6.56.0。本文描述本部署的实际路由和参数。
JSON 成功响应通常为 `{"data":...}`，错误为 `{"message":"..."}`。
Python 客户端自动解包 data，脚本输出再包装为本地结果对象。

## 目录与通用约定

模块：认证边界 → 客户列表 → 私域客户 → 导入 → 模板 → 活动/SMTP →
报表 → 素材/退信/事务邮件 → 公海受众。

`{id}` 为正整数路径 ID。下表未说明位置的写入参数为 JSON body；
GET 参数为 query。JSON 使用 snake_case，尤其是 `customer_list_ids`、
`customer_lists`（响应对象数组）、`target_customer_list_ids`，不能替换为 camelCase。
数组查询按重复参数编码，如 `id=31&id=32`、`tag=launch&tag=vip`。
`page` 从 1 开始；通常 `per_page` 为整数，集合 API 支持 `all`。
分页响应为 `data.results/total/page/per_page`；空集合也须兼容空 results 或空数组。
布尔字段使用 JSON boolean；URL 中使用 `true|false`。不要默认所有 POST 返回 201：
当前列表、客户、模板克隆和活动创建均返回 200；活动服务端克隆返回 201。

## 1. 认证、scope 与工作区

请求：`Authorization: Bearer <token>`；JSON 请求另带 `Content-Type: application/json`。
个人 Key 通常省略 `X-Listmonk-Organization-ID`，因为空间已由 Key 绑定。
传统服务令牌可以带此头：0 为个人空间，正数为组织空间。

| 能力 | 个人 Key scope |
| --- | --- |
| 列表读 / 创建更新删除 | `customer_lists:read` / `customer_lists:write` |
| 客户读 / 创建更新删除及订阅维护 | `customers:read` / `customers:write` |
| 启动、状态、日志、停止批量导入 | `customers:import` |
| 模板读 / 创建克隆更新删除 | `templates:read` / `templates:write` |
| 素材与文件夹读 / 上传维护 | `media:read` / `media:write` |
| 活动读与 SMTP 查询 / 草稿与状态维护 | `campaigns:read` / `campaigns:write` |
| 开始、排期（还需 write）/ 测试发送 | `campaigns:send` |
| summary、timeseries、links、geo、旧 analytics | `campaigns:analytics` |
| 收件人明细 | `campaigns:recipients` |
| 退信读 / 维护 | `bounces:read` / `bounces:write` |
| 事务发送 | `transactional:send` |

路由 scope 与角色权限是两层：客户查看、敏感信息、导出、导入、维护、删除，
列表删除，活动创建、发送、排期、控制、测试、统计、收件人明细，
模板/素材使用、维护、共享及邮箱使用都仍按用户权限校验。
组织经理的审查能力不自动授予其他成员资源的发送、修改或明细能力。
缺少 `customers:sensitive_read` 角色权限时，私域响应中的邮箱/UUID/attribs 会脱敏；
公海非平台管理员响应也脱敏，不能通过导出或报表绕过。

个人 Key 只允许 customer-lists、customers（排除 bulk 与集合 export）、
import/customers、campaigns、templates、media、bounces、tx 业务路径。
以下即使持有全部 scopes 也不可调用：`/api/profile/*`、`/api/workspace`、
`/api/organizations/*`、`/api/pools/*`、`/api/org-pool-allocations/*`、
`/api/dashboard/*`、`/api/settings`、`/api/users`、`/api/logs`、`/api/health`。
不要用其他认证类型绕过本次任务的能力边界。

用户先在浏览器创建 Key。创建表单/API 的字段是 `name`、
`workspace_organization_id`、`scopes`、`expires_at`（YYYY-MM，下个月起至 24 个月）。
Key 到选定月份末到期；创建/轮换只返回一次 token；个人 Key 不能自管 Key。
个人空间还要求用户具备 `workspaces:personal`，组织空间要求有效成员资格且组织活动。

## 2. 客户列表

| Method | Endpoint | 用途、参数和返回 |
| --- | --- | --- |
| GET | `/api/customer-lists` | 列表查询；query、重复 tag、type、type_group、optin、status、order_by、order、page、per_page；返回分页列表 |
| GET | `/api/customer-lists/{id}` | 单列表元数据、客户数量和可用范围；返回列表对象 |
| POST | `/api/customer-lists` | 创建；name 必填；type、optin、status、tags、description、visibility、mask_emails |
| PUT | `/api/customer-lists/{id}` | 维护自有列表；提交完整的名称与需保留的配置，visibility 省略时保持原值 |
| DELETE | `/api/customer-lists/{id}` | 删除单列表，额外需要列表删除角色权限；返回 true |
| DELETE | `/api/customer-lists` | 按重复 id 或 query 删除；明确目标后执行，返回 true |

查询 `type_group=private` 包含普通 private/public；`pool` 包含公海及其分配。
`type` 为精确类型。标签查询使用单数 `tag`，请求体字段为复数 `tags`。
`minimal=true` 是无客户统计的快速读取；此模式主要使用 type_group/status，
不按普通 query/tag/分页规则搜索。精确名称查找使用非 minimal 查询再在客户端匹配。

普通创建示例：

```json
{"name":"OpenClaw Launch","type":"private","optin":"single","status":"active","tags":["openclaw"],"visibility":"private"}
```

`type=public` 是可公开订阅的私域列表，与 `visibility` 或公海无关。
`optin=single|double`，`status=active|archived`。
普通列表可见性：个人空间只能 private；组织空间可 private/organization，
共享的是列表元数据，客户仍是所有者私有；不能设 global。

## 3. 私域客户

| Method | Endpoint | 用途和参数 |
| --- | --- | --- |
| GET | `/api/customers` | search（邮箱/姓名/客户编号等支持的文本搜索）、重复 customer_list_id、subscription_status、order_by、order、page、per_page |
| GET | `/api/customers/{id}` | 详情；可选 customer_list_id 提供列表脱敏上下文 |
| GET | `/api/customers/{id}/activity` | 客户的活动互动记录 |
| GET | `/api/customers/{id}/export` | 单客户 JSON 数据导出；仍需角色导出与敏感信息权限 |
| POST | `/api/customers` | 新建；字段见下表，返回客户对象 |
| PUT | `/api/customers/{id}` | 更新；省略邮箱/attribs 保留存储值；明确提交目标列表关系 |
| PUT | `/api/customers/customer-lists` | 用 body.ids 批量维护订阅关系 |
| PUT | `/api/customers/customer-lists/{id}` | 用路径 ID 维护单客户订阅关系 |
| PUT | `/api/customers/{id}/blocklist` | 单客户封禁 |
| PUT | `/api/customers/blocklist` | body `{"ids":[21,22]}` 批量封禁 |
| POST | `/api/customers/{id}/optin` | 发送确认订阅邮件，真实发信动作 |
| DELETE | `/api/customers/{id}` | 删除单客户 |
| DELETE | `/api/customers` | query 中重复 id，删除指定客户 |
| GET / DELETE | `/api/customers/{id}/bounces` | 读取 / 清除该客户退信；分别需要 customers:read / customers:write |

| 创建字段 | 类型 / 必填 | 含义 |
| --- | --- | --- |
| email | string / 是 | 有效单邮箱；客户身份按工作区与所有者隔离 |
| customer_code | string / 是 | 非空业务编号 |
| name | string / 否 | 姓名 |
| attribs | object / 否 | 自定义资料；原生列映射导入不能保留，使用直接客户 API |
| status | string / 否 | enabled（默认）、disabled、blocklisted；封禁动作仍需相应权限 |
| customer_list_ids | int[] / 否 | 要关联的普通自有列表 ID |
| preconfirm_subscriptions | bool / 否 | 是否预确认，默认 false |

订阅维护 body：`{"ids":[21],"action":"add","target_customer_list_ids":[12],"status":"confirmed"}`。
`action=add|remove|unsubscribe`；status 使用 `confirmed|unconfirmed|unsubscribed`。
单客户路径可省略 ids。修改只发生在本次工作区和已授权列表内。

`GET /api/customers?query=...` 已删除高级查询能力，会返回 400；
使用 `search` 和列表筛选。个人 Key 无法调用 `/api/customers/bulk/*` 或
`/api/customers/export`；不要生成 SQL 查询。

## 4. 批量导入

所有导入接口需要 `customers:import`；服务端每进程一次只运行一个导入，
状态、日志及停止按导入所有者和工作区限制。

| Method | Endpoint | 用途 |
| --- | --- | --- |
| POST | `/api/import/customers` | multipart 上传，file 为 .csv/.xlsx/.zip，params 为 JSON 字符串；上传文件最大 64 MiB |
| GET | `/api/import/customers` | 状态对象：name、total、imported、status |
| GET | `/api/import/customers/logs` | text/plain 导入日志，不是 JSON data |
| DELETE | `/api/import/customers` | 请求停止自己的导入；继续读状态直至终止 |

| params 字段 | 类型 / 要求 | 含义 |
| --- | --- | --- |
| mode | string / 必填 | subscribe 或 blocklist |
| customer_list_ids | int[] | 普通列表为已授权目标；公海规则见第 9 节 |
| subscription_status | string / 可选 | subscribe 默认 unconfirmed；blocklist 默认 unsubscribed |
| field_map | object / 可选 | 原生字段到列标识，例 `{"email":"A","name":"B","customer_code":"C"}` |
| overwrite_userinfo | bool | 是否覆盖已有客户资料；脚本为 false |
| overwrite_subscription_status | bool | 是否覆盖已有订阅状态；脚本为 true |
| overwrite | bool | 旧兼容选项；新调用明确使用上面两个分项字段 |

普通映射只允许 email、name、customer_code。
脚本生成无表头 UTF-8 CSV，按字母映射。REST 本身也可上传 .xlsx；
脚本 Excel 模式先本地解析，额外列转 attribs，含属性时整批走直接客户 API。
ZIP 只处理其中一个 CSV。不要把本地脚本的表头/工作表参数当成 REST params。

状态：`none`（尚无会话）、`importing`、`stopping`、`finished`、`failed`、`stopped`。
POST 成功仅表示启动；只有 finished 才表示成功终态，仍可能有跳过行。
检查日志与 imported 数量；另一个导入尚在运行时不能重新提交。

## 5. 模板

模板读需要 templates:read，写需要 templates:write；用户仍需素材查看/维护权限，
共享范围改变另需角色 `assets:share`。克隆在绑定工作区创建自有副本。

| Method | Endpoint | 用途和参数 |
| --- | --- | --- |
| GET | `/api/templates` | 可读模板集合（数组），可选 no_body=true |
| GET | `/api/templates/{id}` | 单模板，no_body=true 可省正文 |
| GET | `/api/templates/{id}/preview` | 已保存模板预览，响应 HTML |
| POST | `/api/templates/preview` | 未保存预览；form 字段 body、template_type、name_fallback、preview_name_mode=custom、preview_name |
| POST | `/api/templates` | 创建；name、type、body，tx 模板还需 subject；可选 body_source、media、visibility、name_fallback |
| POST | `/api/templates/{id}/clone` | 克隆；name、可选 subject（仅 tx）、target_organization_id |
| PUT | `/api/templates/{id}` | 更新模板；name/type/body 等完整配置；name_fallback 省略保留 |
| PUT | `/api/templates/{id}/default` | 设置工作区默认模板；返回模板集合 |
| DELETE | `/api/templates/{id}` | 删除自有模板；返回 true |

`type=campaign|campaign_visual|tx`；body_source 为可视化源 JSON **字符串**；
media 为附件 ID 数组。name_fallback 结构为
`{"enabled":true,"value":"Customer","invalid_values":["N/A"]}`。
普通模板主要提供邮件外壳（如 `{{ template "content" . }}`）；活动内容在 campaign.body。
只有模板本身包含完整内容时，空的活动 body 才合理。

target_organization_id 省略使用当前空间；0 为个人，正数为组织。
个人 Key 只能克隆到自身绑定空间，不能借此跨空间。

## 6. 活动、发送控制和 SMTP

| Method | Endpoint | 用途和参数 |
| --- | --- | --- |
| GET | `/api/campaigns` | query、重复 status、重复 tag、order_by、order、page、per_page、no_body；分页集合；不支持 from/to 时间筛选 |
| GET | `/api/campaigns/{id}` | 活动详情；可选 no_body |
| POST | `/api/campaigns` | 创建草稿；参数见下表 |
| PUT | `/api/campaigns/{id}` | 更新可编辑活动；先取详情再合并配置，不作为任意字段 PATCH |
| POST | `/api/campaigns/{id}/clone` | 服务端快照复制；可选 name、target_organization_id；返回新草稿；个人 Key 不能跨绑定空间 |
| GET / POST | `/api/campaigns/{id}/preview` | 邮件 HTML 预览；POST 使用 form 字段 body、content_type、template_id、auto_track_links、name_fallback、preview_name_mode/preview_name |
| POST | `/api/campaigns/{id}/text` | 文本预览；同 preview 的 form 字段 |
| POST | `/api/campaigns/{id}/preview/archive` | form 字段 template_id、archive_meta（JSON 字符串）；返回 HTML |
| POST | `/api/campaigns/{id}/content` | body/from/to；目前用于 markdown → html/richtext 内容转换 |
| POST | `/api/campaigns/{id}/test` | 完整活动配置加 customers（邮箱 string[]）；scope campaigns:send，用户 campaigns:test 与 mailboxes:use，真实发送 |
| PUT | `/api/campaigns/{id}/status` | body.status；write；running/scheduled 还需 send |
| PUT | `/api/campaigns/{id}/archive` | archive、archive_template_id、archive_meta、archive_slug；公开归档配置 |
| DELETE | `/api/campaigns/{id}` | 删除单活动 |
| DELETE | `/api/campaigns` | 重复 id 或 query 删除活动 |
| GET | `/api/campaigns/running/stats` | 可统计的运行活动进度快照；需 read scope 和用户统计权限，不按传入 campaign_id 筛选 |
| GET | `/api/campaigns/analytics/{type}` | 旧统计接口；type=views/clicks/bounces/links，重复 id、from/to；analytics scope；新调用优先用 report |
| GET | `/api/campaigns/smtp-pools` | 新建活动所在组织的可用营销池，返回 id/name 等；需 read 和 mailboxes:use |
| GET | `/api/campaigns/{id}/smtp-pools` | 已有活动所属组织的营销池 |
| GET | `/api/campaigns/smtp-overview` | 新活动发件邮箱概览；source、smtp_pool_id；需 read、活动创建权限和 mailboxes:use |
| GET | `/api/campaigns/{id}/smtp-overview` | 指定活动所有者/组织的发件邮箱概览；source 不传默认 personal，组织活动应显式传 organization |
| GET | `/api/campaigns/{id}/pool-send-status` | 公海全组织活动逐组织发送准备情况；read scope |
| POST | `/api/campaigns/{id}/pools` | 为活动附加公海；pool_id、可选 org_pool_allocation_id，organization_id 仅平台管理员适用；write scope，受众和管理权限仍校验 |

| 创建/更新字段 | 类型 / 要求 | 描述 |
| --- | --- | --- |
| name | string / 必填 | 非空活动名，最大 2000 字符 |
| subject | string / 必填 | 主题，最大 5000 字符，可使用模板表达式 |
| customer_list_ids | int[] / 必填 | 目标普通列表或已授权一级公海；禁止用 customer_lists/customerLists |
| type | string | regular（默认）或 optin |
| content_type | string | richtext（默认）、html、markdown、plain、visual |
| body | string | 内容；空正文是否有效取决于模板是否已有完整内容 |
| body_source | string/null | visual 源 JSON 字符串；其他格式会清空此字段 |
| altbody | string/null | 备用纯文本 |
| template_id | int/null | 可用的营销模板；省略使用工作区默认模板 |
| media | int[] | 可用附件素材 ID |
| send_at | RFC 3339/null | 必须为未来时刻；此字段不自动切换 scheduled 状态 |
| messenger | string | email 默认；email-* 会规范化 email，不能借名称锁定 SMTP |
| from_email | string | 兼容字段；实际 From 取选中的 SMTP 配置 |
| daily_send_limit | int | regular email 每日总上限，显式传正整数；旧客户端缺失/小于 1 时服务器兼容回退 300 |
| daily_resume_time | string | HH:MM，服务器本地时间；默认 09:00，非 UTC 时间戳 |
| smtp_source | string | personal 默认；organization 需组织空间（全组织公海例外） |
| smtp_pool_id | int/null | 所选组织自己的营销池；个人来源不选池；普通组织来源缺失时由服务端解析默认池 |
| smtp_rate_limit | int | 活动所有 SMTP 合计封/分钟，1–1000000；新建缺失/0 默认 personal=20、organization=100 |
| reply_mailbox_id | int/null | 当前工作区已验证启用的回信邮箱；个人空间限邮箱所有者，组织空间成员可选共享邮箱；公海按路由规则 |
| pool_scope | string | organization 默认，或 all_organizations；创建后不可改 |
| pool_reply_priority | string | contact_first 默认 / organization_first，公海逐收件人回信地址优先级 |
| visibility | string | private 默认、organization、global；组织可见性要求组织空间 |
| auto_track_links | bool | 自动转换链接追踪；直接 REST 新建省略为 false，管理端表单默认 true |
| tags / attribs / headers | array / object / array | 标签、自定义活动属性、SMTP 头（例 `[{"X-Custom":"value"}]`） |
| name_fallback | object | visual 活动的称呼兜底快照，结构同模板 |
| archive / archive_template_id / archive_meta / archive_slug | bool / int / object / string | 公开归档配置；仅按任务要求启用 |

SMTP 概览只返回 `id/name/from_email/daily_limit/sent_today` 及适用的组织身份，不返回凭据。
无可用发件邮箱时结果可能为空；检查来源及预配置。没有营销 SMTP 时不回退系统通知 SMTP。
新建全组织公海概览额外传 `pool_scope=all_organizations` 和逗号分隔的
`customer_list_ids=7,8`；与普通数组 query 的重复编码规则不同。
邮箱/SMTP 的配置端点在 profile/organizations 下，个人 Key 不可访问。

创建返回 draft。常用状态转换：
draft → running；draft → scheduled；scheduled → draft；
running → paused/cancelled；paused → running。deferred/finished 由服务器控制；
不能直接写 finished。用户发送/排期/控制是各自独立动作权限。
CLI 蓝本模式重新 POST 创建新受众草稿；与服务端 clone 的快照复制语义不同。

## 7. 报表

下列五种报表各有单活动和跨活动端点：

| Method | 单活动 | 跨活动 | 内容 / scope |
| --- | --- | --- | --- |
| GET | `/api/campaigns/{id}/report/summary` | `/api/campaigns/report/summary` | sent、bounced、views_total、clicks_total、unique_viewers、unique_clickers、open_rate、click_rate、ctor；analytics |
| GET | `/api/campaigns/{id}/report/timeseries` | `/api/campaigns/report/timeseries` | views/clicks/bounces 时间序列；analytics |
| GET | `/api/campaigns/{id}/report/links` | `/api/campaigns/report/links` | link_id、url、total_clicks、unique_clickers、unique_click_rate；跨活动另有活动身份；analytics |
| GET | `/api/campaigns/{id}/report/geo` | `/api/campaigns/report/geo` | enabled、total_opens、located_opens、unknown_opens、locations；analytics |
| GET | `/api/campaigns/{id}/report/recipients` | `/api/campaigns/report/recipients` | 客户投递状态、发送时间、打开/点击/退信与最后互动；分页；recipients |

| Query 参数 | 适用 / 要求 | 说明 |
| --- | --- | --- |
| from / to | 所有报表 / 必填 | YYYY-MM-DD 或带时区 RFC 3339；长度 10–30；from ≤ to |
| id | 跨活动 / 可选 | 重复正整数，如 id=31&id=32；逐活动验证报表权限 |
| all | 跨活动 / 可选 | all=true 表明使用全部已授权活动；省略 id 时本身已取已授权集合；不能扩权 |
| page / per_page | recipients | 分页；完整导出必须逐页取，不把第一页当全量 |
| search | recipients | 姓名/邮箱文本搜索 |
| opened / clicked / bounced | recipients | yes / no / all，默认 all，不能传 true/false 代替 yes/no |
| link_id | recipients | 非负整数，按指定链接点击筛选 |
| sort_by | 单活动 recipients | email、view_count、click_count、sent_at、last_engaged_at |
| sort_by | 跨活动 recipients | 上述字段另有 campaign_subject、bounce_count |
| order | recipients | ASC / DESC，默认 DESC；默认按 last_engaged_at 排序 |

日期不会自动扩展到全天：数据库直接比较事件/发送时间，边界是 `>= from` 且 `<= to`。
今日截至当前时间应使用带时区的今日 00:00 到当前时刻。
完整自然日可使用到次日 00:00 的边界，但那一刻也会计入；连续日报须统一边界处理。
不传时间的集合 `GET /api/campaigns` 返回累计计数，不能代表今日事件。

sent 按成功投递时间，互动按事件时间；私域和公海发送都计入。
跨活动唯一人数是各活动内唯一人数相加，同一人在不同活动可能重复计数。
individual tracking 关闭时唯一人数/比率可能为 null；分母为 0 时比率也可能 null，
不是 0。有打开/点击不代表客户人工操作。

recipients 另需用户 `campaigns:recipients`、客户查看权限、敏感资源访问及
individual tracking。公海行仅有 `pools:get` 时纳入；非平台管理员只看本组织且邮箱脱敏。
公海行包含 pool_contact_id、customer_id=0、空 uuid；跨活动行另带活动身份。
聚合能读不代表明细能读。GeoIP 未配置 enabled=false；历史未知数据不补定位，
locations 是国家/地区/城市、近似经纬度与 count，不含原始 IP。

## 8. 素材、退信、事务邮件（直接 REST）

这些能力没有专用 CLI；需要时直接调用，不能用个人 Key 配置邮件服务器。

| Method | Endpoint | 描述 / 参数 |
| --- | --- | --- |
| GET | `/api/media` | 素材分页集合；query、folder_id、page/per_page |
| GET | `/api/media/{id}` | 素材详情与可访问 URL |
| POST | `/api/media` | multipart file，可选 folder_id、visibility；上传到当前空间 |
| DELETE | `/api/media/{id}` | 删除自有素材 |
| GET | `/api/media/folders` | 已授权文件夹树 |
| POST | `/api/media/folders` | name、parent_id（根为 0）、visibility；创建文件夹 |
| PUT | `/api/media/folders/{id}` | name、可选 visibility；重命名/改变共享范围 |
| PUT | `/api/media/folders/{id}/move` | parent_id；移动文件夹 |
| DELETE | `/api/media/folders/{id}` | 删除文件夹，仍受所有权/引用规则限制 |
| PUT | `/api/media/{id}/folder` | folder_id（根为 0）；移动素材 |
| GET | `/api/bounces` | 退信分页；campaign_id、source、order_by、order、page/per_page |
| GET | `/api/bounces/{id}` | 单条退信 |
| DELETE | `/api/bounces/{id}` | 删除退信 |
| DELETE | `/api/bounces` | 重复 id 删除退信；all=true 删除当前已授权工作区范围内全部退信，仅在明确要求清空时使用 |
| PUT | `/api/bounces/blocklist` | 无请求筛选参数，封禁当前已授权工作区范围内的已退信客户；不是按指定 id 操作 |
| POST | `/api/tx` | 事务邮件，scope transactional:send，用户 tx:send 和 mailboxes:use |

素材读/写分别 media:read/media:write，改变组织共享另需 assets:share。
媒体不能设 global。退信读/写分别 bounces:read/bounces:write；
封禁和删除仍需业务动作权限。

事务邮件参数：template_id（tx 类型）、customer_emails / customer_ids、
customer_mode（default/fallback/external）、data（模板变量 object），
可选 subject、from_email、headers、content_type、messenger、altbody。
可用 JSON 或 multipart；multipart 中 data 字段是整份请求 JSON，
file 部分是附件。使用发起用户的营销 SMTP，不借用别人的凭据。
default 模式的 customer_emails 与 customer_ids 二选一；fallback/external 只接受 customer_emails。
最多 5000 个合并收件人、20 附件、单附件 10 MiB、合计 20 MiB。
真实发送不自动重试，超时先核对是否已经入队。

## 9. 已授权公海营销与兼容接口

公海主数据、组织投放授权、组织分配是不同能力。
`/api/pools/*` 和 `/api/org-pool-allocations/*` 对个人 Key 整体拒绝，
不能因为路由有 customer_lists scope 就认为可用。传统令牌按其角色另行授权。

个人 Key 可访问的兼容路径如下，仍检查公海权限和组织范围：

| Method | Endpoint | 参数 / 返回 |
| --- | --- | --- |
| GET | `/api/customer-lists/{id}/pool-contacts` | search（兼容 customer_code）、status、order_by/order、page/per_page；返回脱敏 DTO |
| GET | `/api/customer-lists/{id}/pool-contacts/export` | CSV 导出；沿用筛选及可选重复 contact，只能缩小已授权范围 |
| GET | `/api/customer-lists/{id}/org-pool-allocations` | 该一级公海的可见组织分配 |
| POST | `/api/customer-lists/{id}/pool-contacts` | email、customer_code、可选 name/reply_to/allocation_department/attribs/status；需要 pools:master_manage，并受主数据范围限制 |
| DELETE | `/api/customer-lists/{id}/pool-contacts/{contact_id}/email` | 清除邮箱，主数据维护权限 |
| DELETE | `/api/customer-lists/{id}/pool-contacts/{contact_id}` | 删除联系人，服务端额外授权；不是普通客户 DELETE |

统一公海导入走 `POST /api/import/customers`：Key 需 customers:import，用户需
pools:master_manage；只选一个一级 pool，无二级分配或私域混合，不用任何 overwrite。
field_map 额外支持 allocation_department、reply_to；reply_to 是单邮箱，
只提供 Reply-To 来源，不创建收件邮箱或凭据。
普通脚本不封装此路径，按公海规则直接构造 multipart 请求。

发送用 `POST /api/campaigns`，customer_list_ids 选一级公海：
pool_scope=organization 按活动工作区组织分配解析；all_organizations
按所有活跃组织的分配解析，须 campaigns:public_pool_send 且不能包含普通列表。
pool_reply_priority=contact_first 优先每行导入的 reply_to，
organization_first 优先已验证启用的组织回信邮箱；缺少地址才用另一来源。
发送路由保留快照；排期前调用 pool-send-status，查看 ready、organizations、issues。

源码定位（仓库内路径，便于下次维护）：`cmd/{handlers,api_keys,customer_lists,customers,import,templates,campaigns,organization_smtp,organization_smtp_pools,media,media_folders,bounce,tx,pools}.go`、
`models/{campaigns,report,customer_lists,templates,messages}.go`、
`internal/core/workspace_analytics.go`。
