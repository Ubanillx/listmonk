# 对外营销 API：接口、参数与返回值

核对日期：2026-11-13，仓库迁移 v6.73.0。本文描述本部署的实际路由和参数。
JSON 成功响应通常为 `{"data":...}`，错误为 `{"message":"..."}`。
Python 客户端自动解包 data，脚本输出再包装为本地结果对象。

## 目录与通用约定

模块：认证边界 → 客户列表 → 私域客户 → 导入 → 模板 → 活动/SMTP →
报表与退订 → 发送失败与补发 → 素材/退信/事务邮件 → 公海受众 → AI 回信分类 →
平台级 delivery-provider。

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
| 发送失败/失败客户查询及导出 | `campaigns:recipients` |
| 确认退订明细 | `campaigns:recipients` |
| 定向补发失败客户 | `campaigns:send` + `campaigns:recipients` |
| 发送概览 delivery-overview | `campaigns:analytics` |
| AI 分类结果（客户活动） | `customers:read` |
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
`/api/campaigns/:id/unsubscribes` 属于 campaigns 路径，可用。
以下即使持有全部 scopes 也不可调用：`/api/profile/*`（含回信邮箱、最新邮件检查、
退信处理历史）、`/api/workspace`、`/api/organizations/*`（含组织回件邮箱、
回复转发、待转移资源）、`/api/pools/*`、`/api/org-pool-allocations/*`、
`/api/dashboard/*`、`/api/settings`（含 `/api/settings/reply-ai/*` AI 探测）、
`/api/users`、`/api/logs`、`/api/health`、`/api/custom-fields`、
`/api/delivery-provider/*`、`/api/audit-events`、`/api/maintenance/*`。
不要用其他认证类型绕过本次任务的能力边界。

Key 固定绑定个人空间（0）或一个组织空间。个人 Key 传入不符的
`X-Listmonk-Organization-ID` 会返回 403，不能靠请求头切换空间。

用户先在浏览器创建 Key。创建表单/API 的字段是 `name`、
`workspace_organization_id`、`scopes`、`expires_at`（YYYY-MM，下个月起至 24 个月）。
Key 到选定月份末到期；创建/轮换只返回一次 token；个人 Key 不能自管 Key。
个人空间还要求用户具备 `workspaces:personal`，组织空间要求有效成员资格且组织活动。

## 2. 客户列表

| Method | Endpoint | 用途、参数和返回 |
| --- | --- | --- |
| GET | `/api/customer-lists` | 列表查询；query、重复 tag、type、type_group、optin、status、scope、order_by、order、page、per_page；返回分页列表 |
| GET | `/api/customer-lists/{id}` | 单列表元数据、客户数量和可用范围；返回列表对象 |
| POST | `/api/customer-lists` | 创建；name 必填；type、optin、status、tags、description、visibility、mask_emails |
| PUT | `/api/customer-lists/{id}` | 维护自有列表；提交完整的名称与需保留的配置，visibility 省略时保持原值 |
| DELETE | `/api/customer-lists/{id}` | 删除单列表，额外需要列表删除角色权限；返回 true |
| DELETE | `/api/customer-lists` | 按重复 id 或 query 删除；明确目标后执行，返回 true |

查询 `type_group=private` 包含普通 private/public；`pool` 包含公海及其分配。
`type` 为精确类型。标签查询使用单数 `tag`，请求体字段为复数 `tags`。
`scope=all_owners` 只用于平台管理员跨所有者查看普通列表，要求
`type_group=private`；个人 Key 不应使用该交互式例外。
`minimal=true` 是无客户统计的快速读取；此模式主要使用 type_group/status，
不按普通 query/tag/分页规则搜索。精确名称查找使用非 minimal 查询再在客户端匹配。

公海列表元数据、计数、联系人查询与导出按当前组织的实际分配收敛：
detached 分配排除，源列表/分配列表处于 pending transfer 时隐藏，
disabled 但仍绑定可读；投放授权本身不授权浏览。个人空间平台管理员的
跨组织浏览是单独例外。

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
| GET | `/api/customers` | search（邮箱/姓名/客户编号等支持的文本搜索）、重复 customer_list_id、subscription_status、contact_status、order_by、order、page、per_page |
| GET | `/api/customers/{id}` | 详情；可选 customer_list_id 提供列表脱敏上下文 |
| GET | `/api/customers/{id}/activity` | 客户的活动互动记录：campaign_views、link_clicks 与 **reply_ai_events**（AI 回信分类结果，见第 11 节） |
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

`contact_status` 可选 `all`（默认）、`enabled`、`unsubscribed`、
`blocklisted`、`disabled`。`blocklisted`/`disabled` 匹配客户全局状态；
`unsubscribed` 匹配请求列表范围内（未给列表则当前可读列表）的退订成员关系；
`enabled` 要求客户 enabled 且该范围内没有退订。它与 `subscription_status`
独立叠加，不扩大权限。分页结果可能带派生 `unsubscribed:true`。
退订成员关系不等于活动确认退订事件：客户可能在某个列表退订、但活动
`unsubscribes` 不增加。

活动接口只返回 views、clicks 与 AI 事件，**不返回退信和退订事件**。
退信读 `GET /api/customers/{id}/bounces`（最多 1000 条，scope `customers:read`；
用户另需 `bounces:get`，组织经理和平台管理员可豁免；无敏感权限时邮箱为空、
UUID 为空、`meta` 为空）。删除需要 customers:write 与 bounces:delete 角色权限。
每条退信含 type（soft/hard/complaint）、source（provider 名称、
`reply_ai`、`reply_mailbox`）、时间与活动身份。

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

`GET /api/import-templates/{kind}`（kind=`users` 或 `members`）下载 Excel 模板，
其余 kind 返回 404。它只需要认证、不含资源数据，但对个人 Key 同样不可用——
不在业务前缀内。

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
| GET | `/api/campaigns/{id}/delivery-overview` | 私域/公海统一发送概览：planned、sent、unsent、waiting、failed、progress、send_errors 与逐组织进度；analytics scope |
| GET | `/api/campaigns/{id}/unsubscribes` | 确认/点击退订明细；query search、status（confirmed 默认）、page/per_page，上限 100；`campaigns:recipients` |
| GET | `/api/campaigns/{id}/send-errors` | 失败尝试的分类汇总与明细；query search、category、organization_id、page/per_page，每页上限 100；`campaigns:recipients` |
| GET | `/api/campaigns/{id}/send-errors/export` | 同筛选条件的 Excel 汇总，含错误原因与历史缺明细计数 |
| GET | `/api/campaigns/{id}/failed-recipients` | 当前仍未发送、且至少失败过一次的收件人；query 同上；返回 can_export、can_retry |
| GET | `/api/campaigns/{id}/failed-recipients/export` | 同筛选条件的 Excel 导出 |
| POST | `/api/campaigns/{id}/failed-recipients/retry` | 仅对勾选的失败客户定向补发；活动必须为 paused；send scope |
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
| send_at | RFC 3339/null | 必须为未来时刻；此字段不自动切换 scheduled 状态；目标 running 且 send_at 非空会落库为 scheduled |
| messenger | string | email 默认；email-* 会规范化 email，不能借名称锁定 SMTP |
| from_email | string | 兼容字段；实际 From 取选中的 SMTP 配置 |
| daily_send_limit | int | regular email 每日总上限；REST 缺失/小于 1 回退 300，更新省略时沿用保存值（保存值 <1 才回退）；管理端新建 personal=启用发件数×50、organization=2000；只有成功投递计入，非 regular/非邮件强制 0 |
| daily_resume_time | string | HH:MM，服务器本地时间；默认 09:00；额度耗尽后总是等到次日该时刻，不按 UTC 时间戳 |
| smtp_source | string | personal 默认；organization 要求活动属于组织空间（organization_id>=1），没有全组织公海例外；改受众不会自动改来源 |
| smtp_pool_id | int/null | 所选组织自己的营销池；个人来源不选池；组织来源缺失时服务端取该组织 MIN(id) 的池，无池则 400 |
| smtp_rate_limit | int | 活动所有 SMTP 合计封/分钟，1–1000000；新建缺失/0 默认 personal=5、organization=200；更新省略保留保存值，显式 0 按来源默认 |
| reply_mailbox_id | int/null | 当前工作区已验证启用的回信邮箱；个人空间限邮箱所有者，组织空间成员可选共享邮箱；公海按路由规则 |
| pool_scope | string | organization 默认，或 all_organizations；创建后不可改 |
| pool_reply_priority | string | contact_first 默认 / organization_first，公海逐收件人回信地址优先级 |
| visibility | string | REST 缺省 private、organization、global；组织可见性要求组织空间；管理端组织工作区新建默认 organization |
| auto_track_links | bool | 自动转换链接追踪；直接 REST 新建省略为 false，管理端表单默认 true |
| tags / attribs / headers | array / object / array | 标签、自定义活动属性、SMTP 头（例 `[{"X-Custom":"value"}]`） |
| name_fallback | object | visual 活动的称呼兜底快照，结构同模板 |
| archive / archive_template_id / archive_meta / archive_slug | bool / int / object / string | 公开归档配置；仅按任务要求启用 |

SMTP 概览只返回 `id/name/from_email/daily_limit/sent_today` 及适用的组织身份，不返回凭据。
新建个人 SMTP 账户的 `daily_limit` 默认 50；组织 SMTP 账户默认 0（不限）。
无可用发件邮箱时结果可能为空；检查来源及预配置。没有营销 SMTP 时不回退系统通知 SMTP。
新建全组织公海概览额外传 `pool_scope=all_organizations` 和逗号分隔的
`customer_list_ids=7,8`；与普通数组 query 的重复编码规则不同。
邮箱/SMTP 的配置端点在 profile/organizations 下，个人 Key 不可访问。

### 活动统计字段（列表/详情/running stats）

活动对象除状态外还返回只读统计：

| 字段 | 含义 |
| --- | --- |
| delivery_total / delivery_sent / delivery_pending | 当前有效收件人快照的计划、已发送、待发送（pending/queued/deferred） |
| delivery_unavailable | 当前无法发送的收件人：failed/cancelled（含重试耗尽、退订、失效取消） |
| send_errors | 累计失败尝试次数，不是人数 |
| unsubscribes / unsubscribe_clicks / unsubscribe_activity | 确认退订、点击退订、两者并集的人数 |

三类 delivery 之和等于有效快照目标；无快照时回退到持久目标/成功数。
客户/公海联系人删除、转移或工作区失效的历史快照不推断补齐，
因此当前范围目标可能与旧 `to_send` 不同。不要把 `send_errors` 或
“目标减成功”当作失败人数。

创建返回 draft。常用状态转换：
draft → running；draft → scheduled；scheduled → draft；
paused → running/scheduled；deferred → running/scheduled；
running/deferred → paused；running/paused/deferred → cancelled。
目标 running 但 send_at 非空时会落库为 scheduled；要立即发送先清空 send_at。
deferred/finished 由服务器控制；不能直接写 finished。
取消会把 pending/deferred/queued 置 cancelled；暂停会把 queued 退回 pending。
用户发送/排期/控制是各自独立动作权限。
CLI 蓝本模式重新 POST 创建新受众草稿；与服务端 clone 的快照复制语义不同。

## 7. 报表

下列五种报表各有单活动和跨活动端点：

| Method | 单活动 | 跨活动 | 内容 / scope |
| --- | --- | --- | --- |
| GET | `/api/campaigns/{id}/report/summary` | `/api/campaigns/report/summary` | sent、bounced、views_total、clicks_total、unique_viewers、unique_clickers、open_rate、click_rate、ctor；analytics |
| GET | `/api/campaigns/{id}/report/timeseries` | `/api/campaigns/report/timeseries` | views/clicks/bounces 时间序列；analytics |
| GET | `/api/campaigns/{id}/report/links` | `/api/campaigns/report/links` | link_id、url、total_clicks、unique_clickers、unique_click_rate；跨活动另有活动身份；analytics |
| GET | `/api/campaigns/{id}/report/geo` | `/api/campaigns/report/geo` | enabled、total_opens、located_opens、unknown_opens、locations；analytics |
| GET | `/api/campaigns/{id}/report/recipients` | `/api/campaigns/report/recipients` | 客户投递状态、发送时间、打开/点击/退信与最后互动；分页；recipients；`recipient_status` 可能为 `failed` |
| GET | `/api/campaigns/{id}/unsubscribes` | — | 确认/点击退订明细，见下方退订小节；recipients |

| Query 参数 | 适用 / 要求 | 说明 |
| --- | --- | --- |
| from / to | 所有报表 / 必填 | YYYY-MM-DD 或带时区 RFC 3339；长度 10–30；from ≤ to |
| id | 跨活动 / 可选 | 重复正整数，如 id=31&id=32；逐活动验证报表权限 |
| all | 跨活动 / 可选 | 仅为兼容接受；服务端不处理，省略 id 本身已取已授权集合；不能扩权 |
| page / per_page | recipients | 分页；完整导出必须逐页取，不把第一页当全量 |
| search | recipients | 姓名/邮箱文本搜索 |
| opened / clicked / bounced | recipients | yes / no / all，默认 all，不能传 true/false 代替 yes/no |
| link_id | recipients | 非负整数，按指定链接点击筛选 |
| sort_by | 单活动 recipients | email、view_count、click_count、sent_at、last_engaged_at |
| sort_by | 跨活动 recipients | 上述字段另有 campaign_subject、bounce_count |
| order | recipients | ASC / DESC，默认 DESC；默认按 last_engaged_at 排序 |

注意：summary 报表本身不返回 `unsubscribes`/`unsubscribe_clicks`；
退订人数在活动列表/详情/running stats 对象中，或读独立退订端点。
recipients 报表也不返回退订/确认时间，只有投递、打开、点击、退信和最后互动。

日期不会自动扩展到全天：数据库直接比较事件/发送时间，边界是 `>= from` 且 `<= to`。
今日截至当前时间应使用带时区的今日 00:00 到当前时刻。
完整自然日可使用到次日 00:00 的边界，但那一刻也会计入；连续日报须统一边界处理。
部署时区由 `TZ`/`app.timezone` 决定（未配置回退 Asia/Shanghai），
报表、额度窗口和 `Date` 模板函数共用该时区。
不传时间的集合 `GET /api/campaigns` 返回累计计数，不能代表今日事件。

sent 按成功投递时间，互动按事件时间；私域和公海发送都计入。
`summary.sent` 以收件人快照为准，并回退到 `campaign_daily_usage` 的持久用量；
回退值是**按自然日**的计数，`from/to` 取部分白天时可能按整天计入，
细小差异应以快照/收件人明细为准。收件人/公海联系人被删除后，历史发送数仍可能保留。
跨活动唯一人数是各活动内唯一人数相加，同一人在不同活动可能重复计数。
individual tracking 关闭时唯一人数/比率可能为 null；分母为 0 时比率也可能 null，
不是 0。有打开/点击不代表客户人工操作。

### 确认退订与点击退订

活动列表、详情和 `GET /api/campaigns/running/stats` 返回：

| 字段 | 含义 |
| --- | --- |
| unsubscribes | 通过该活动公开退订链接**确认**退订的不同收件人；含 One-Click POST；重复确认只计一次 |
| unsubscribe_clicks | 点击过该活动退订链接的不同收件人；点击不等于退订 |
| unsubscribe_activity | 两者的不同收件人并集 |

`GET /api/campaigns/{id}/unsubscribes` 的 query：

| Query | 说明 |
| --- | --- |
| page / per_page | 默认 20，服务端上限 100 |
| search | 最长 500；匹配当前客户编号/姓名；邮箱搜索需敏感权限 |
| status | `confirmed` 默认；`visited` 含所有点击者（含已确认）、`clicked` 仅未确认点击、`all` |

行字段：`recipient_type`、`recipient_id`、`organization_id`、`customer_code`、
`name`、`email`、`created_at`（首次点击，缺失时用确认时间）、`clicked_at`、
`confirmed_at`、`click_count`（点击次数，不是人数）、`status`（clicked/confirmed）。
不返回 UUID 或退订 URL；已删除的确认身份字段为空。
权限：活动所有者/管理边界、`campaigns:analytics` 与 `campaigns:recipients`、
`customers:read`（私域）或 `customer_lists:read`（公海）；
私域邮箱无敏感权限时脱敏，公海对非平台管理员脱敏。

口径：管理员退订、退信处理、AI 回信拉黑**不写入** `campaign_unsubscriptions`；
AI 的 unsubscribe/complaint 只改客户状态和列表订阅，不增加活动退订数。
偏好页移除本活动列表的订阅会计入；只编辑客户资料或其他列表不计。
重复或并发确认按活动+收件人去重，只计一次。
公海确认退订只影响实际投递来源组织。确认历史在收件人删除、暂停、续发后保留；
删除活动会删除事件。

recipients 另需用户 `campaigns:recipients`、客户查看权限、敏感资源访问及
individual tracking。公海行仅有 `pools:get` 时纳入；非平台管理员只看本组织且邮箱脱敏。
公海行包含 pool_contact_id、customer_id=0、空 uuid；跨活动行另带活动身份。
跨活动 recipients 选“可管理”的活动，其他报表选“可分析”的活动；两个集合可能不同。
聚合能读不代表明细能读。GeoIP 未配置 enabled=false；历史未知数据不补定位，
locations 是国家/地区/城市、近似经纬度与 count，不含原始 IP。

## 8. 发送失败、补发与发送概览

这一组接口回答三个不同的问题，不能互相替代：

1. **这次活动现在到哪一步了** → `delivery-overview`。
2. **历史上哪些尝试失败了、为什么** → `send-errors`。
3. **还有谁没收到、能不能单独补发** → `failed-recipients`。

三个口径不能混用：`send_errors` 是累计失败尝试次数（重试失败会重复计数），
不是当前失败人数，也不是退信数。退信看 `/api/bounces`。重试成功、暂停续发和
服务重启都不会把 `send_errors` 清零。

### 发送概览

`GET /api/campaigns/{id}/delivery-overview`，scope `campaigns:analytics`。

| 字段 | 含义 |
| --- | --- |
| campaign_id / status | 活动身份与当前状态 |
| source | private（普通受众）、pool（公海受众）或 mixed（混合） |
| planned / sent / unsent | 计划量、已发送、仍可发送的待处理量 |
| failed | 当前仍未发送且曾失败的收件人数（含重试耗尽者） |
| waiting | 排队等待中、仍可发送的数量；重试耗尽者不计入 |
| progress | 0–100 的完成百分比 |
| send_errors | 累计失败尝试次数 |
| organizations | 公海活动才有内容：逐组织 planned/sent/unsent/failed |

`failed` 与 `unsent` 不再互相包含：重试耗尽后状态为 `failed` 的收件人
退出 `unsent`/`waiting`，但保留在 `failed`。私域活动 `organizations` 为空数组。
组织维度的筛选统一用 `organization_id`，分页与导出使用同一条件。

### 失败历史（send-errors）

`GET /api/campaigns/{id}/send-errors`，scope `campaigns:recipients`，同时需要
用户权限 `campaigns:analytics` 与 `campaigns:recipients`、活动所有者/管理边界，
以及对应来源的客户查看权限：
私域来源要 `customers:get`/`customers:get_all`，公海来源要 `pools:get`。
两者都没有时返回 403 `customer read permission required`。

| Query | 说明 |
| --- | --- |
| search | 始终匹配客户编号/姓名；邮箱和错误原文另需敏感权限；最长 500 字符 |
| category | 错误分类，最长 50 字符，见下表 |
| organization_id | 公海活动按目标组织筛选；必须是正整数 |
| page / per_page | 服务端分页；`per_page` 会被收敛到 1–100 |

| 错误分类 | 含义 |
| --- | --- |
| smtp_auth | SMTP 认证失败 |
| smtp_rejected | 上游拒信（如 550/554） |
| smtp_temporary | 临时拒绝（如 421/4xx） |
| timeout | 连接或读写超时 |
| network | 网络不可达 |
| smtp_unavailable | 没有可用发件通道 |
| reply_unavailable | 回信路由不可用 |
| media | 媒体依赖缺失或被拦截（会立即暂停活动） |
| render | 模板渲染失败 |
| other | 其他 |

响应包含 results、reasons（分类计数）、recorded_errors、historical_errors、
total、page、per_page、can_export。
行字段：recipient_type、recipient_id、organization_id、customer_code、name、
email、stage（render/send）、category、smtp_code（无状态码为 0）、error、
count、first_at、last_at。快照反映失败当时的数据，不随后续编辑变化。
`historical_errors` 表示旧版本只累加计数、没有保存明细的失败次数，
不能用日志反推原因。

导出：`GET /api/campaigns/{id}/send-errors/export`，同筛选条件，可选 `lang`，
忽略分页，返回 .xlsx（明细 + 原因汇总）。明细列含客户编号、姓名、邮箱、原因、阶段、
SMTP 状态码、错误原文、次数、首次/最后时间、收件人类型/ID、组织 ID、活动 ID、
分类代码。缺少 `customers:export`（私域）或 `pools:export`（公海）时导出 403。

脱敏规则与客户接口一致：缺少 `customers:sensitive_read` 时私域邮箱脱敏；
缺少 `campaigns:private_send_error_details` 时私域错误原文置空；
公海明细默认脱敏且原文置空，只有平台管理员可见。
分类、SMTP 状态码和次数始终可见。聚合能读不等于明细能读。

### 重试轮次与上限

`settings.smtp_delivery.max_recipient_retries` 默认 **3**（0–1000），表示首次投递
之后最多再重试的次数；它只作用于 regular SMTP 活动。

- 失败历史持久化，暂停、续发、重启、服务重启都不丢；按最小失败轮次整轮推进，
  在途收件人会阻挡更快失败者抢跑。
- 配额 defer、主动暂停/取消不计入重试次数；渲染/投递失败才计入。
- 达到上限后收件人状态为 `failed`、`retryable=false`：不再可续发或定向补发，
  但仍出现在失败名单/导出和概览 `failed` 中。
- 调高设置不会自动复活已耗尽的人。
- `max_msg_retries` 只作用于事务/系统邮件，不约束活动收件人。
- 成功投递消耗活动每日额度；失败尝试不消耗。

### 当前失败客户与定向补发

`GET /api/campaigns/{id}/failed-recipients`，query 与脱敏规则同上。

服务端取每个收件人**最近一次**失败事件，再与当前收件人快照关联，
只返回仍然未发送的记录。重试成功后该收件人自动退出名单，历史报表仍可查。
响应额外给出 `can_export` 和 `can_retry`；行内 `retryable` 才是单个收件人
当前是否还能补发（要求快照为 pending/deferred 且受众仍有效）。
重试耗尽、已删除收件人等历史行仍会出现，但 `retryable=false`。
导出：`GET /api/campaigns/{id}/failed-recipients/export`，列含客户编号、
姓名、邮箱、原因、阶段、SMTP 状态码、错误原文、次数、最后时间、收件人类型/ID、
组织 ID、状态、是否可重试；同样支持可选 `lang`。

`POST /api/campaigns/{id}/failed-recipients/retry`，需要活动可发送：

```json
{"recipients":[{"type":"private","id":1234},{"type":"pool","id":98765}]}
```

- scope：`campaigns:send` **且** `campaigns:recipients`；
  用户另需 `campaigns:send` 角色权限和活动所有权。
- 活动状态必须是 `paused`；草稿、运行中、已完成的补发都会失败。
- 一次 1–500 个**唯一**收件人；重复、过期、已发送、不可投递或无权 id
  会让整个请求失败，不扩大范围。
- 成功后返回 `{"campaign_id":…, "selected":N, "status":"running"}`，
  表示已重新排队并启动，不代表已经投递完成。
- 只发送选中的人；选中者耗尽后，若仍有未发送者回到 `paused`，否则 `finished`。
- 普通手动续发会清空选中集合、恢复常规受众；对运行中的活动重复调用会冲突。

> 补发是真实发信动作。仅在用户明确要求时调用，并在之后用
> `delivery-overview` 与 `send-errors` 核对结果。

## 9. 素材、退信、事务邮件（直接 REST）

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
| GET | `/api/customers/{id}/bounces` | 该客户最近最多 1000 条退信；scope `customers:read`，用户另需 `bounces:get`（组织经理/平台管理员可豁免） |
| DELETE | `/api/bounces/{id}` | 删除退信 |
| DELETE | `/api/bounces` | 重复 id 删除退信；all=true 删除当前已授权工作区范围内全部退信，仅在明确要求清空时使用 |
| PUT | `/api/bounces/blocklist` | 无请求筛选参数，封禁当前已授权工作区范围内的已退信客户；不是按指定 id 操作 |
| POST | `/api/tx` | 事务邮件，scope transactional:send，用户 tx:send 和 mailboxes:use |

素材读/写分别 media:read/media:write，改变组织共享另需 assets:share。
媒体不能设 global。`GET /api/media/categories` 是平台管理员媒体目录，
虽然路径属于 `/api/media`，个人 Key 仍会被 handler 拒绝。

上传：`POST /api/media` 使用 multipart，字段 `file` 必填，可选
`folder_id`、`visibility`（private/organization）。请求体上限 32 MB（含
multipart 开销；文件本身应低于约 31.9 MB）；处理器单文件上限 32 MiB，
图片还有边长 20000、总像素 5000 万的限制。响应 `data` 含 `id`、`filename`、
`url`（`/api/media/file/{id}/{filename}`）、`content_type`、`thumb_url`、`meta`。
上传需要 scope `media:write` + 角色 media:manage；共享到组织另需 `assets:share`。
正文可引用该 URL；保存活动/模板时会校验并自动补齐 `campaign_media` 关联，
即使请求的 `media` 数组没列出该图片。缺失、歧义、同名克隆或无权媒体会拒绝保存。
注意：共享文件夹的受众会覆盖媒体自身的 `private` 可见性；上传到共享文件夹时
应显式使用 organization 可见性。

活动发送时媒体库文件会转成收件人 `/email-media/{token}/{filename}` 链接，
仅对 email/email-* 活动成立：
图片保留在正文位置远程加载；未在正文引用的图片会追加为正文图片；
未引用的非图片文件进入邮件末尾的附件资料区；plain 模式追加“文件名 + URL”文本行；
不会作为 CID 或二进制 MIME 附件发送。需要真附件时使用事务邮件 `/api/tx`。
markdown 的 `![](...)` 图片语法在保存/启动阶段不参与媒体引用扫描，只会在发送前拦截；
需要保存时校验请用 html/richtext。测试发送只采用请求的 `media` 数组，
不会自动扫描请求正文里的新图片；预览也不保证加载受保护图片。
模板自带的私有媒体不要显式放入 `media` 数组，应由模板/正文关联自动带出。

退信读/写分别 bounces:read/bounces:write；封禁和删除仍需业务动作权限。

退信 `source` 区分来源：服务商 webhook 名称（ses/postmark 等）、
`reply_ai`（AI 判定 complaint）、`reply_mailbox`（回信邮箱 IMAP 解析的 DSN）。
`type` 为 soft/hard/complaint。回信邮箱 DSN 只有 hard 才可能触发拉黑，
soft 仍记录；它写入 `source=reply_mailbox` 并计入活动退信统计。
AI complaint 写入 `source=reply_ai`，用于和真实服务商反馈区分。
公海退信还带 `source_pool_id`、`source_allocation_id`、`source_organization_id`，
用于定位实际投递来源；`customer_id=0` 而 `pool_contact_id>0` 表示公海联系人。
无敏感权限时退信响应中的邮箱、客户 UUID、`meta` 会被清空，但类型、来源、
时间和活动身份仍可见。

事务邮件参数：template_id（tx 类型）、customer_emails / customer_ids、
customer_mode（default/fallback/external）、data（模板变量 object），
可选 subject、from_email、headers、content_type、messenger、altbody。
可用 JSON 或 multipart；multipart 中 data 字段是整份请求 JSON，
file 部分是附件。使用发起用户的营销 SMTP，不借用别人的凭据。
default 模式的 customer_emails 与 customer_ids 二选一；fallback/external 只接受 customer_emails。
最多 5000 个合并收件人、20 附件、单附件 10 MiB、合计 20 MiB。
真实发送不自动重试，超时先核对是否已经入队。

## 10. 已授权公海营销与兼容接口

公海主数据、组织投放授权、组织分配是不同能力。
`/api/pools/*` 和 `/api/org-pool-allocations/*` 对个人 Key 整体拒绝，
不能因为路由有 customer_lists scope 就认为可用。传统令牌按其角色另行授权。

组织工作区只能浏览已实际分配给本组织的公海及分配：
`state='detached'` 排除；源列表或分配列表处于 pending transfer 时隐藏；
disabled 但仍绑定的分配可读。该边界适用于成员、管理员和持有主数据/投放/
平台分配/全组织活动权限的账号；投放授权本身不等于可浏览联系人。
列表元数据、计数、联系人、筛选项和导出使用同一谓词；访问其他组织返回 403。
个人空间的平台管理员跨组织浏览是单独例外。

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
投放授权或组织分配都可授权活动受众；但联系人浏览仍要求实际分配。
pool_reply_priority=contact_first 优先每行导入的 reply_to，
organization_first 优先已验证启用的组织回信邮箱；缺少地址才用另一来源。
发送路由保留快照；排期前调用 pool-send-status，查看 ready、organizations、issues。
公海回复/明确退订/投诉只剔除实际投递来源组织的分配，不影响其他组织；
AI 处理公海回信也只更新该组织分配，不全局拉黑一级公海联系人。

源码定位（仓库内路径，便于下次维护）：`cmd/{handlers,api_keys,customer_lists,customers,import,templates,campaigns,campaign_send_errors,campaign_failed_recipients,campaign_unsubscriptions,organization_smtp,organization_smtp_pools,media,media_folders,bounce,tx,pools,reply_ai,reply_ai_settings,reply_mailboxes,delivery_provider,delivery_provider_failures,delivery_provider_blocklist,delivery_provider_automation}.go`、
`models/{campaigns,report,campaign_send_errors,campaign_delivery_overview,campaign_unsubscriptions,customer_lists,templates,messages,reply_ai,settings}.go`、
`internal/{core,replyai,bounce,providerautomation,manager,media}/**`。

## 11. AI 回信分类（接入与边界）

这是本部署**新增的 AI 能力**：把入站客户回信交给一个 OpenAI 兼容的模型做意图分类，
再由后端决定是否退订/拉黑。模型只输出意图，**不直接改数据**。

能力边界、事件状态机与使用红线另见 [AI 回信分类](reply-ai.md)。

### 能力边界

| 项目 | 说明 |
| --- | --- |
| 入口 | 非破坏性 IMAP 扫描（UID 游标 + `BODY.PEEK`）已启用的回信邮箱，只读取新邮件，不改动已读标记 |
| 分类结果 | unsubscribe、complaint、product_complaint、other 四类 |
| 模型接入 | 任意 OpenAI 兼容 `/chat/completions` 网关，含 LiteLLM、one-api、new-api |
| 输出 | intent + confidence + reason_code，不返回邮件正文 |
| 落库 | `reply_ai_events`，正文只在待重试期间保留，任何接口都不返回 |
| 自动动作 | `unsubscribe` 与明确的垃圾/滥用 `complaint` **都会**把客户拉黑并退订所有列表；complaint 另写 `source=reply_ai` 退信 |
| 不动作 | `product_complaint`、`other`、低于 `min_confidence`、自动回复、无法唯一匹配来源的回信 |

公海回信的自动动作只更新实际投递来源组织的分配剔除，不全局拉黑一级公海联系人。
分类依据是回信正文，不是打开/点击数据。AI 动作**不写** `campaign_unsubscriptions`，
因此不会增加活动 `unsubscribes`。

### 配置界面与接口

AI 分类是**平台级设置**，保存在 `settings` 的 `reply_ai` 行，随所有已启用的
回信邮箱共享。三个接口都在设置域下：

| Method | Endpoint | 说明 |
| --- | --- | --- |
| POST | `/api/settings/reply-ai/models` | 用已保存或本次提交的网关配置拉取模型清单 |
| POST | `/api/settings/reply-ai/test` | 用真实样本跑一次分类，返回分步结果 |
| PUT | `/api/settings/:key` | 保存 `reply_ai` 设置（`settings:manage`） |

> **个人 Key 无法调用这些接口。** 它们不在
> `/api/customers`、`/api/campaigns`、`/api/templates` 等业务前缀内，
> 会被统一入口直接拒绝；即使传统令牌持有 `settings:manage` 也只按角色放行。
> 不要尝试用营销 Key 探测它们。

探测与测试请求体：

```json
{
  "base_url": "https://gateway.example.com/v1",
  "api_key": "sk-...",
  "model": "gpt-4o-mini",
  "timeout": "15s",
  "sample_text": "请不要再给我发邮件了",
  "expected_intent": "unsubscribe"
}
```

`api_key` 省略时使用已保存的密钥；`expected_intent` 可为空，
否则必须是四类之一。测试只提交样本文本，**不涉及真实客户数据**。

`reply_ai` 设置字段与默认值：`enabled=false`、`base_url`、`api_key`、`model`、
`timeout=15s`、`min_confidence=0.98`（0 < x ≤ 1）、
`scan_interval=60s`（10s–24h）。读取设置时 `api_key` 始终被掩码，
回写空值表示保留原密钥。

### 回信邮箱开关

回信邮箱现在有三个独立开关：

| 字段 | 含义 |
| --- | --- |
| scan_enabled | 检测总开关；关闭时 AI 和 DSN 退信都不扫描。升级时按 `ai_enabled OR bounce_enabled` 回填 |
| ai_enabled | 选择 AI 分类；还需全局 `reply_ai.enabled=true` 和已验证连接 |
| bounce_enabled | 选择 DSN 退信处理；还需全局 `bounce.enabled=true` 和已验证连接 |

| Method | Endpoint | 说明 |
| --- | --- | --- |
| GET | `/api/profile/reply-mailboxes` | 当前用户/组织的回信邮箱 |
| POST | `/api/profile/reply-mailboxes` | 新建：email、name、username、password、imap_host、imap_port、imap_tls、folder、is_default、scan_enabled、ai_enabled、bounce_enabled |
| PUT | `/api/profile/reply-mailboxes/{id}` | 更新，含三个开关 |
| POST | `/api/profile/reply-mailboxes/test` | 连接测试 |
| POST | `/api/profile/reply-mailboxes/{id}/inspect-latest` | 只读检查最新一封是否 DSN |
| GET | `/api/profile/reply-mailboxes/{id}/bounce-events` | 最近的 DSN 处理历史（只读） |
| PUT | `/api/organizations/{id}/reply-mailbox` | 组织统一回件邮箱 |

这些同样**不在个人 Key 的业务前缀内**。

### 回信邮箱 DSN 退信

同一回信邮箱的另一条链路，与 AI 分类共享 IMAP 连接和扫描间隔：

- 只解析**外层**投递状态报告（`multipart/report; report-type=delivery-status`
  或 `message/delivery-status`），字段 `Final-Recipient`、`Original-Recipient`、
  `X-Failed-Recipients`、`Status`、`Diagnostic-Code`。
  转发/附件中的 DSN、ARF 滥用报告、纯文本失败信和正文推断都不算退信。
- 匹配同一邮箱内 `status='sent'` 的投递：原始 `Message-ID`、
  `X-Listmonk-Customer`/`X-Listmonk-Campaign`、或唯一失败收件人。
  未匹配、歧义、跨工作区的报告只记录、不动作。
- 只有 hard（5.x.x）才能改变客户；soft 只记录。全局 `bounce.actions.hard`
  动作为 blocklist 且计数达到阈值时：私域客户设为 `blocklisted`（不改列表成员），
  公海联系人只从投递来源组织剔除。动作为 delete/unsubscribe 时只记录。
- 写入 `bounces`，`source='reply_mailbox'`，计入活动退信统计。
- DSN 发往信封发件人（Return-Path / SMTP MAIL FROM），不是 Reply-To；
  发件账户与回信邮箱不同时，需要先把 DSN 转发/路由到回信邮箱。
- 个人 Key 可读聚合退信（`/api/bounces?source=reply_mailbox`）和客户退信
  （`/api/customers/{id}/bounces`），但读不到 `reply_bounce_events` 明细、
  邮件原文或处理历史。

### 读取分类结果

分类结果通过客户活动接口返回，这是个人 Key 唯一能读到 AI 结果的地方：

`GET /api/customers/{id}/activity` → `data.reply_ai_events`

每行包含 intent、confidence、reason_code、model、action、status、reply_mailbox_id、
from_email、subject、received_at、classified_at、actioned_at、attempts、body_hash。
查询会额外校验回信邮箱归属与客户的工作区/组织一致，避免跨所有者读到旧事件。

| 字段 | 取值 |
| --- | --- |
| status | pending、processing、processed、ignored、failed |
| action | pending、ignored、blocklisted |

`ignored` 是终态，常见 reason_code：`global_ai_disabled`、`mailbox_not_active`、
`unmatched_or_ambiguous_delivery`、`empty_reply`、`low_confidence`、
`workspace_not_writable`、`malformed_message`、`message_too_large`。
`failed` 会退避重试；不代表已退订。**正文不返回**，只有分类结论。

公海联系人没有客户活动接口；AI/DSN 对公海回信的处理结果只体现在
实际投递来源组织分配的排除/退订状态（见第 10 节 pool-contacts 的
`unsubscribed`/`removed`），事件明细和个人 Key 不可读。

### 在营销流程里怎么用

- 报告“客户为什么退订/投诉”：读 `reply_ai_events`，按 intent 与 action 汇总。
- 判断 AI 是否生效：`action=blocklisted` 只应出现在 `intent=unsubscribe|complaint` 的行。
- 解释活动退订与客户状态不一致：AI/退信/管理员动作不进入活动退订统计。
- 怀疑误判：先看 confidence 与 reason_code，再看 model；
  不要因为没有正文就推断客户原话。
- 调整阈值或换模型属于平台设置，超出个人 Key 能力，交回管理员。

> 分类是辅助判断。**不要把 AI 结果当作客户同意或授权的证据**，
> 也不要在退订/拉黑后继续发送。

## 12. 平台级 delivery-provider（个人 Key 不可访问）

`/api/delivery-provider/*` 是平台管理员的失败治理模块；个人 Key 即使持有全部营销
scope 也会被统一入口拒绝，且没有对应的个人 Key scope。

| Method | Endpoint | 说明 |
| --- | --- | --- |
| GET | `/api/delivery-provider/config` | 服务商配置状态；需 `delivery_provider:get`；不返回密码 |
| PUT | `/api/delivery-provider/config` | 保存配置；仅平台管理员；旧版本用 `expected_updated_at` 触发 409 |
| POST | `/api/delivery-provider/test` | 连接测试；仅平台管理员 |
| GET | `/api/delivery-provider/account` | 账户/服务信息；传 `mode` 会 400 |
| GET | `/api/delivery-provider/statistics` | 按日期或任务统计；`ident` 与 `id` 互斥 |
| GET | `/api/delivery-provider/failures` | 失败列表；`date/date_end` 或 `task_id/task_ident` 至少一项；`type` 必填（-1 或 1..10）；`group_by=email`；`search` 最长 320 |
| GET | `/api/delivery-provider/failures/export` | Excel；明细列含 Email、Customer code、Error count、Error type、Error detail、Deletable、Source |
| PUT | `/api/delivery-provider/failures/blocklist` | 按邮箱批量拉黑/解封，最多 500；`action=block|unblock`，`scope=all|private|pool` |
| GET/POST/PUT/DELETE | `/api/delivery-provider/automation/*` | 定时自动拉黑规则与运行历史；仅平台管理员 |

失败搜索要点：

- 未分组时 `search` 只匹配邮箱子串；`group_by=email` 时匹配邮箱或任一
  `matches[].customer_code` 子串，**不匹配姓名**。
- `search` 只在服务端本地过滤，不上送服务商。
- `Deletable` 只出现在导出：类型 1（邮箱不存在）、8（无效地址）、9（格式错误）
  为“是”，其余为“否”。它只是人工清理提示，不删除客户、不自动重试、不改状态。
- 自动拉黑规则默认关闭；默认类型 `[1,3,6,8,9]`、每日 02:00、间隔 24h、
  回溯 7 天。启用或执行前要求 15 分钟内同 scope/version/config 的成功预览；
  手动运行只应用勾选条件，不保存配置。真实动作会把匹配客户设为 `blocklisted`，
  私域同时把名单成员置 `unsubscribed`；不自动解封。
- 营销侧对应能力是 `/api/campaigns/{id}/send-errors`、`/failed-recipients`、
  `/delivery-overview` 和补发接口；不要引用 delivery-provider 的 JSON 结构。
