---
name: listmonk-openclaw-marketing
description: Operate this Listmonk deployment from OpenClaw through workspace-bound personal API keys. Use for customer lists and imports, media uploads with inline images or attachments, marketing templates, campaign drafts and delivery, SMTP source and send-limit defaults, send-failure diagnosis and selective retry, confirmed unsubscribe and cross-campaign analytics (including open locations), reply-mailbox bounce data, and reading AI-classified inbound replies. 适用于 Listmonk 营销自动化、客户导入、图片/附件与邮件草稿、活动排期、发送失败排查与补发、确认退订与效果报表、回信退信核对和 AI 回信分类结果；公海管理、delivery-provider 与 AI 调试须先核对个人 Key 的接口边界。
---

# Listmonk OpenClaw Marketing

用当前部署的 REST API 完成营销任务。接口基线核对于 **2026-11-13（迁移 v6.73.0）**，
依据仓库的 `cmd/handlers.go`、`cmd/api_keys.go` 和业务处理器，不套用上游旧版
`/api/lists`、`/api/subscribers` 或旧参数名。

本部署包含上游没有的能力：组织工作区、公海客户与分配、组织发件池、
持久化收件人历史与多轮重试、确认退订明细、回信邮箱 DSN 退信处理、
发送失败明细与定向补发、平台级 delivery-provider 失败治理、以及 **AI 回信分类**。
最近一次接口变化集中在：个人发件速率默认值（5/分钟）、组织默认值（200/分钟）、
新建活动的每日上限策略、`max_recipient_retries` 重试轮次、`/api/campaigns/{id}/unsubscribes`、
`/api/customers?contact_status=`、回信邮箱退信和 AI 投诉动作。

## 按任务读取

- 接口、参数、scope、返回值和权限边界：读 [API reference](references/api-reference.md) 中对应模块。
- 具体执行顺序、完整请求、CLI 参数和异常恢复：读 [REST workflow](references/rest-workflow.md)。
- 发送失败排查、失败客户定向补发、确认退订名单、回信邮箱退信：
  API reference 第 7、8、9 节，工作流第 6.1、7、8 节。
- AI 能力的完整边界、事件状态与使用红线：读 [AI 回信分类](references/reply-ai.md)。
- 所有默认值（发送速率、每日上限、重试次数、AI 扫描等）：先读本文末尾「默认值速查」。
- 需要调用未封装的接口时，按 API reference 直接构造 HTTP 请求；不要虚构 CLI 参数。
  配套 Python HTTP 客户端只依赖标准库，读取 Excel 另需 `openpyxl`。

## 连接与认证

需要 `base_url`（站点根地址，不带 `/api`）和 `bearer_token`。常规集成使用用户在
**Profile → API Keys** 创建的 `lmpk_` 个人 Key：

```http
Authorization: Bearer <personal_api_key>
```

Key 固定绑定个人空间（组织 ID 为 0）或一个组织空间；通常省略工作区头。
传统内部服务令牌可以使用 `X-Listmonk-Organization-ID` 选择工作区；
个人 Key 传入与绑定不符的组织 ID 会被拒绝（403），不是静默切换。
不要用 `/api/profile` 或 `/api/health` 检测个人 Key，选择任务所需的业务读接口，
例如 `GET /api/customer-lists`。

脚本从技能目录的 `.env` 读取 `LISTMONK_BASE_URL`、`LISTMONK_BEARER_TOKEN`；
从 `.env.example` 配置，或用 `LISTMONK_ENV_FILE` 指定文件。
优先级：命令行 > 进程环境 > 技能目录 `.env` > 当前目录 `.env`。
`LISTMONK_ORGANIZATION_ID` 只用于传统服务令牌。密钥不写入文档或日志。

Scope 只收紧调用范围，不能替代用户角色、组织成员资格、资源所有权或业务动作权限。
发送和邮箱选用还要 `mailboxes:use`；修改邮箱配置需要 `mailboxes:manage`，但个人
Key 本身不能调用配置接口。看到 403 应根据错误检查 scope、角色和工作区，不能只归因为 scope。

## 普通私域营销流程

1. 确定列表、客户来源、内容来源、发件来源及是否发送。先核对所需参数和 scopes。
2. 按 ID 读取列表，或按名称搜索后精确匹配；找不到再创建。
   普通流程只处理 `type=private|public`，二者都是私域客户列表；
   `public` 是公开订阅类型，与公海 `pool` 不同。
   公海列表的元数据、计数和联系人浏览现在按「当前组织的实际分配」收敛
   （detached 排除、pending transfer 隐藏、disabled 但仍绑定可读）；
   投放授权本身只用于创建活动受众，不等于可浏览联系人。
3. 导入客户。每行必须有 `email` 和 `customer_code`。
   只有原生字段时使用批量导入；含非空 `attribs` 时用逐行客户 API。
   等待导入终态，核对 `imported_count`、`failed_rows`、`skipped_rows`。
   `--preconfirm-subscriptions` 默认为关闭；确认方式按用户任务选择。
4. 从模板库克隆模板，或读取已有活动作为内容蓝本。
   普通模板的 subject 不作为活动主题，创建活动仍需 `subject`。
   蓝本继承内容和追踪配置；SMTP 来源、池、频率、回信邮箱和可见性按本次任务显式选择，
   不复制源活动的 SMTP 发送身份、旧排期、状态或受众。`from_email` 会作为兼容字段继承，
   但实际 From 仍由选中的 SMTP 配置决定。
5. 用 `POST /api/campaigns` 创建草稿，受众字段必须为 `customer_list_ids`。
   新建活动**默认 personal SMTP**，组织空间也不例外；用组织发件池必须显式传
   `smtp_source=organization`，且活动必须属于组织空间（个人空间的平台管理员
   创建全组织公海活动也不能选组织来源）。
   设置 `daily_send_limit` 和服务器本地时间 `daily_resume_time`；
   组织 SMTP 还设置适用的 `smtp_pool_id`（省略时服务端取该组织最小 ID 的池）。
6. 检查草稿、预览、SMTP 概览和回信邮箱。组织 SMTP 用于私域/混合受众时，
   未选回信邮箱会在管理端显示红色风险警告（API 不阻止保存/发送，但客户回信可能丢失）；
   纯公海使用独立的逐客户/组织回信路由。用户要求测试发送时调用 test 接口。
   创建草稿不会自动发送；仅在本次任务明确要求立即发送或排期时启动。
7. 立即发送：`PUT /api/campaigns/{id}/status`，`{"status":"running"}`。
   注意：活动仍保存着未来 `send_at` 时，目标 `running` 会落库为 `scheduled`；
   要立即发送先清空 `send_at`。
   排期：先保存未来的 RFC 3339 `send_at`，再设置 `{"status":"scheduled"}`。
   状态变更需 `campaigns:write`；running/scheduled 另需 `campaigns:send`。
8. 读取活动状态和报表，报告实际发送、互动、退订和失败结果，保留资源 ID 供后续复用。

## 报表与“今日”统计

- 单活动：`GET /api/campaigns/{id}/report/{summary|timeseries|links|geo|recipients}`。
- 多活动/工作区：`GET /api/campaigns/report/{summary|timeseries|links|geo|recipients}`。
  用重复 `id` 指定活动；省略 ID 使用当前工作区内的已授权活动。
  `all=true` 只是兼容参数，服务端不处理；真正生效的是「省略 id」。
- 每次提供 `from` 和 `to`。推荐带时区的 RFC 3339，参数由 URL 编码器处理。
  日期字符串不会自动补齐一天；`from=to=2026-10-05` 只表示午夜边界。
- “今天发生的发送/打开/点击”直接按今日时间范围请求跨活动报表。
  不按活动创建日期过滤，否则会漏掉旧活动今天产生的事件。
  “今天新建的活动”才读取活动集合并在客户端筛选 `created_at`。
- 个人 Key 不能调用 `/api/dashboard/*`，但可以调用上述跨活动报表。
- 汇总 scope 为 `campaigns:analytics`；收件人明细独立需要 `campaigns:recipients`、
  用户明细/客户权限及开启 individual tracking。明细被拒绝时保留聚合报告并标明原因。
- 跨活动 recipients 使用“可管理”的活动集合；其他报表使用“可分析”的活动集合。
  能读聚合不等于能读明细。
- `summary.sent` 以收件人快照为准，并回退到持久化的每日用量；
  回退值按自然日计，`from/to` 取部分白天时可能按整天计入；
  客户/公海联系人删除后，历史发送数仍可能保留。
- 活动列表/详情/running stats 还带 `delivery_total`、`delivery_sent`、
  `delivery_pending`、`delivery_unavailable`、`send_errors` 与三个退订人数；
  `delivery_unavailable` 含重试耗尽、退订和失效取消，
  不要用目标减成功来推算失败人数。summary/recipients 报表本身不带退订人数，
  退订看活动对象或 `/unsubscribes` 端点。
- 地理报表表示打开像素的近似位置；GeoIP 未配置、历史数据或邮件代理可导致未知位置。
  唯一人数、比率为 `null` 时表示不可计算/不可用，不当作零。

## 确认退订、点击退订与 AI 退订

活动列表、详情和 `GET /api/campaigns/running/stats` 现在返回三个只读人数：

- `unsubscribes`：通过该活动公开退订链接**确认**退订的不同收件人数（含 One-Click POST）。
- `unsubscribe_clicks`：点击过该活动退订链接的不同收件人数；只点击不构成退订。
- `unsubscribe_activity`：两者并集，同一人只计一次。

明细：`GET /api/campaigns/{id}/unsubscribes`（scope `campaigns:recipients`；
用户另需 `campaigns:analytics`、活动所有者/管理边界和对应客户读取权限）。
`status` 默认 `confirmed`，可选 `clicked`、`visited`、`all`；
`search` 匹配客户编号/姓名，邮箱搜索受敏感权限限制。
行含 `recipient_type`、`recipient_id`、`organization_id`、`customer_code`、`name`、
`email`、`created_at`、`clicked_at`、`confirmed_at`、`click_count`、`status`。
`click_count` 是点击次数，不是人数；不返回收件人 UUID 或退订 URL。

口径红线：

- 后台管理员退订、退信处理、AI 回信拉黑都**不会**写入 `campaign_unsubscriptions`；
  不要把 `unsubscribes` 当作“所有不再接收的人”。
- AI 的 unsubscribe/complaint 会让客户 `blocklisted` 并退订所有列表，
  但活动退订数不变；需要单独读取客户活动或退订名单来解释。
- 已确认的退订历史在收件人删除、暂停、续发后仍保留；删除活动才删除事件。
- 偏好页移除本活动列表的订阅会计入；只编辑资料或其他列表不计；
  重复/并发确认只计一次。
- 公海退订只影响实际投递来源组织，不改其他组织的分配。

## 公海流程边界

个人 Key 的允许路径**不包含** `/api/pools/*` 和 `/api/org-pool-allocations/*`，
即使这些路由声明了 scope，也会先被统一入口拒绝。
允许的 `/api/customer-lists/{id}/pool-contacts` 等兼容路径仍受公海业务权限、
当前组织实际分配和脱敏限制。

已有投放授权（或组织分配）的一级公海可通过 `customer_list_ids` 创建活动；
不要提交二级 `org_pool_allocation` 列表 ID。普通 CLI 列表/导入流程会拒绝公海列表；
使用直接 REST 请求处理已授权公海受众，见 API reference。
一级公海统一导入使用 `/api/import/customers`，须有 `customers:import` 和
`pools:master_manage`，只能选择一个一级公海，且禁止 overwrite 选项。
`pool_scope=all_organizations` 另需角色权限 `campaigns:public_pool_send`，
不能混入私域列表；发送前检查 `pool-send-status`。
`pool_reply_priority` 默认 `contact_first`：优先导入的客户 `reply_to`，
缺失时用已验证启用的组织回信邮箱；`organization_first` 顺序相反。

## 发送失败排查与定向补发

活动因错误阈值暂停后，按「概览 → 原因 → 名单 → 补发 → 复核」的顺序处理，不要直接重发。

1. `GET /api/campaigns/{id}/delivery-overview`（`campaigns:analytics`）：
   看 planned / sent / unsent / waiting / failed / send_errors / progress；
   `source` 为 `private`、`pool` 或 `mixed`；公海活动另有逐组织进度。
   `unsent` 是仍可发送的待处理量，`failed` 是当前仍未发送且曾失败的收件人；
   重试耗尽后状态为 `failed` 的收件人不再计入 `unsent`/`waiting`，但仍计入 `failed`。
2. `GET /api/campaigns/{id}/send-errors`（scope `campaigns:recipients`；
   用户另需 `campaigns:analytics`、活动所有者/管理边界和对应客户读取权限）：
   按 `category`（smtp_auth、smtp_rejected、smtp_temporary、timeout、network、
   smtp_unavailable、reply_unavailable、media、render、other）与 `search` 筛选。
   `search` 始终匹配客户编号/姓名；邮箱和错误原文搜索另需敏感权限。
   `per_page` 服务端收敛到 1–100。
   `send_errors` 是累计失败尝试次数，不是失败人数，也不是退信数。
   `historical_errors` 表示旧版本未保存明细的计数，不能反推原因。
3. `GET /api/campaigns/{id}/failed-recipients`：只返回仍未发送且至少失败一次的人，
   并给出 `status`、`retryable`、`can_export`、`can_retry`。
   已耗尽 `max_recipient_retries` 的人 `retryable=false`，仍会出现在名单和导出中。
4. `POST /api/campaigns/{id}/failed-recipients/retry`：**仅活动为 paused、
   且用户明确要求时**调用。请求体 `{"recipients":[{"type":"private|pool","id":…}]}`，
   一次 1–500 个唯一收件人；需要 `campaigns:send` 与 `campaigns:recipients` 两个 scope。
   只重新排队选中的人；选中者耗尽后，若仍有未发送者回到 paused，否则 finished。
   对运行中的活动重复调用会冲突；普通手动续发会清空选中集合、恢复常规受众。
5. 复核：重读概览与失败名单。已成功者会退出名单，`send_errors` 不会减少。

重试语义（v6.65–v6.67）：

- 每个收件人最多「首次投递 + `max_recipient_retries` 次」，默认 3 次（0–1000）。
  `max_msg_retries` 只作用于事务/系统邮件，不约束活动收件人。
- 常规发送按失败轮次整轮推进：先让所有可投递收件人完成第 1 次，再做第 2 轮；
  在途收件人会阻挡更快的失败者抢跑。暂停、续发、重启后轮次仍保持。
- 配额延迟（defer）和主动停止不消耗重试次数；渲染/投递失败才消耗。
- 成功投递计入活动每日额度；失败尝试不计入。已耗尽的人不会被续发或调高上限自动复活。
- 单次运行的错误阈值默认 1000（`app.max_send_errors`），超过会暂停活动；
  这与累计 `send_errors` 是两个概念。媒体/图片依赖错误会立即暂停。

缺少 `customers:sensitive_read` 或 `campaigns:private_send_error_details` 时，
邮箱脱敏、私域错误原文置空，但分类与状态码仍可见——这是权限边界，不是接口故障。
对硬退信重复补发没有意义，应先清理名单或走黑名单流程。

## 回信、退信与 AI 分类结果

本部署的入站回信处理分为三条独立链路，数据不能混用：

1. **AI 回信分类**：非破坏 IMAP 扫描，OpenAI 兼容模型判为
   unsubscribe / complaint / product_complaint / other。
   `unsubscribe` 和明确的垃圾/滥用 `complaint` 都会把客户拉黑并退订所有列表；
   `complaint` 额外写入一条 `source=reply_ai` 的退信记录；
   `product_complaint` 与 `other` 只审计、不改客户。
2. **回信邮箱退信（IMAP DSN）**：同一回信邮箱可解析严格 DSN。
   只有 hard（5.x.x）且全局 `bounce.actions.hard` 动作为 blocklist 时才可能拉黑；
   soft 只记录。该路径不会执行 delete/unsubscribe 动作。退信写入 `bounces`，
   `source=reply_mailbox`，并计入活动退信统计。
3. **传统 POP/Webhook 退信**：`GET /api/bounces` 可见，按 `source` 区分来源。

个人 Key 能读到的位置：

- `GET /api/customers/{id}/activity` → `reply_ai_events`：
  intent、confidence、reason_code、model、action、status、reply_mailbox_id、
  from_email、subject、received_at、classified_at、actioned_at、attempts、body_hash，
  **不含正文**。
- `GET /api/bounces?source=reply_mailbox|reply_ai`、`GET /api/customers/{id}/bounces`：
  客户活动接口不含退信，必须走退信接口。
- 回信邮箱的 `scan_enabled`、`ai_enabled`、`bounce_enabled` 配置、DSN 处理历史和
  AI 网关设置都在 `/api/profile/*`、`/api/organizations/*`、`/api/settings/*`，
  个人 Key 一律不可访问。不要用营销 Key 探测这些接口。
- 公海联系人没有 `/api/customers/{id}/activity`；AI/DSN 对公海的处理结果只体现在
  该投递来源组织分配的排除/退订状态，事件明细不可由个人 Key 读取。

AI 判读红线：

- `action=blocklisted` 只应出现在 `intent=unsubscribe|complaint` 的行；
  出现其他组合说明数据异常，先核对再行动。
- `status=failed` 是调用失败待重试，不代表已退订；`ignored` 是终态，不执行动作。
- AI 拉黑不会增加活动的 `unsubscribes`，也不会写入 `campaign_unsubscriptions`。
- 不要把 AI 结论当作客户同意或授权的证据，也不要在退订/拉黑后继续发送。
- 平台级配置（模型、阈值、扫描间隔、退信动作）超出个人 Key 边界，交回管理员。

## 平台级 delivery-provider（个人 Key 不可访问）

`/api/delivery-provider/*` 是平台管理员的失败治理模块，个人 Key 即使持有全部
营销 scope 也会被统一入口拒绝。它提供：

- `/config`、`/account`、`/statistics`、`/logs`：上游服务商配置、账户和任务查询；
- `/failures`、`/failures/export`：按日期或任务查失败；`group_by=email` 时
  `search` 可匹配客户编号（不匹配姓名），Excel 导出含客户编号和 `Deletable` 列；
  `Deletable=是` 仅表示该邮箱属于“地址不存在/无效/格式错误”，是人工清理提示，
  不会删除客户或自动重试；
- `/failures/blocklist`：按邮箱批量拉黑/解封（最多 500 个）；
- `/automation/*`：按失败类型定时自动拉黑，默认关闭。

营销侧不要引用该模块的 JSON 结构，也不要尝试绕过。对应到个人 Key 的能力是
`/api/campaigns/{id}/send-errors`、`/failed-recipients`、`/delivery-overview` 和补发接口。

## 脚本选择与输出

| 脚本 | 用途 |
| --- | --- |
| `ensure_list.py` | 按 ID 复用或按名称查找/创建普通客户列表 |
| `import_subscribers.py` | 从 JSON 或 .xlsx 导入私域客户；文件名兼容，HTTP 路径使用 customers |
| `clone_template.py` | 克隆模板到绑定工作区 |
| `upload_media.py` | 上传图片/附件，输出媒体 ID 与 `/api/media/file/{id}/{filename}` URL |
| `create_campaign.py` | 直接创建草稿；支持正文文件、图片/附件、模板直选和内容蓝本 |
| `update_campaign_status.py` | 启动、排期、暂停、取消或撤回排期 |
| `fetch_campaign_reports.py` | 单活动、多个活动或整个工作区报表，可选 geo 和指定收件人分页 |
| `inspect_campaign_delivery.py` | 只读：发送概览、失败原因与当前失败客户名单 |
| `inspect_customer_replies.py` | 只读：客户活动与 AI 回信分类结论 |
| `run_marketing_flow.py` | 普通列表 → 客户 → 模板/蓝本 → 正文/媒体 → 草稿 → 可选启动 → 可选单活动报表 |

带图片/附件的草稿流程：

```shell
python scripts/upload_media.py --file ./hero.png --file ./catalog.pdf
# 返回 media[].id 与 media[].url；正文用 {{media:hero.png}} 占位
python scripts/create_campaign.py \
  --customer_list-id 12 --template-id 4 \
  --campaign-name "Launch" --subject "Launch day" \
  --body-file ./email.html --content-type html \
  --media-file ./hero.png --media-file ./catalog.pdf \
  --media-id 88 \
  --daily-send-limit 300 --daily-resume-time "09:00"
```

- `--body-file` 读取 UTF-8 正文；`{{media:文件名}}` 会被替换为对应上传媒体的 URL
  （占位符使用本地文件名；服务端会规范化空格等字符，但占位符仍按本地文件名匹配，
  `local_filename` 保留原文件名）。
- 占位符只匹配本次 `--media-file` 上传的文件；空 token、未知 token、未闭合 token
  会在上传前直接报错。`--media-id` 关联已有媒体，但不能用于占位符替换。
- `--media-file` 先上传再关联；`--media-id` 关联已存在的媒体，两者可混用、自动去重。
- 上传需要 scope `media:write` + 角色 media:manage；共享到组织另需 `assets:share`。
  带媒体创建活动还需要角色 media:get。单次请求上限 32 MB（含 multipart 开销），
  文件本身应低于约 31.9 MB；图片另有尺寸/像素限制。
- 活动媒体只对 email 活动（email/email-*）转成收件人 `/email-media/` 链接：
  图片保留正文位置远程加载；未引用的图片会追加为正文图片；
  未引用的其他文件进入邮件末尾的附件资料区；plain 模式追加“文件名 + URL”文本行。
  **不会作为 MIME 附件发送**。需要真附件时使用事务邮件 `/api/tx`。
- markdown 正文的 `![](...)` 图片不会在保存/启动时被媒体校验识别，
  只有发送前才会拦截；需要可校验的内嵌图片请用 html/richtext。
- 测试发送不会自动扫描请求正文里的新图片；正文引用了新上传媒体时，
  必须同时把该媒体 ID 放进 `media` 数组。预览也不保证能加载受保护图片。
- 模板应包含 `{{ template "content" . }}`；`--template-id` 用于普通 campaign 模板，
  `content_type=visual` 需要 campaign_visual 模板；`--source-template-id` 则克隆模板。
  草稿创建后默认不发送；`--body-file` 不支持 visual。
- 共享文件夹中的他人 private 媒体可能被保存校验接受但发送不关联；
  上传到共享文件夹时显式使用 `--media-visibility organization`。
  模板自带的私有图片不要放进 `--media-id`，应由模板/正文关联自动带出。

CLI 未封装的新能力：确认退订名单、失败/失败客户 Excel 导出、定向补发、
`contact_status` 筛选、回信邮箱退信历史。需要时按 API reference 直接发 HTTP 请求。
`inspect_campaign_delivery.py` 的 `--search` 可传客户编号或姓名；
`--page/--per-page` 服务端上限 100。

运行 `python scripts/<script>.py --help` 查实际参数。
成功输出 JSON 到 stdout；业务错误输出 `{"error":{"message":...,"status":...}}` 到 stderr，
退出码 1；CLI 参数错误退出码 2，`--verbose` 进度也写 stderr。
报表脚本每次只取一页收件人，完整明细须继续分页。

失败后先查询已创建的列表、模板或活动再重试。请求超时不能推断服务端未写入；
不盲目重新创建资源或重发邮件。逐行导入可能部分成功，已有客户复用只维护列表订阅，
不会自动覆盖其姓名、客户编号或 attribs。按用户要求另调用更新接口处理。

## 默认值速查

数值以当前代码为准；管理端表单、REST 和 CLI 的默认值并不总是一致。

| 项目 | 默认值 / 语义 |
| --- | --- |
| `smtp_source` | `personal`。组织来源要求活动属于组织空间（`organization_id>=1`），没有全组织公海例外 |
| `smtp_rate_limit` | 新建缺失/0：personal **5 封/分钟**，organization **200 封/分钟**；显式范围 1–1000000；已有活动省略时保留保存值 |
| 活动每日上限（REST） | `daily_send_limit` 缺失或 <1 时回退 **300**，更新省略沿用保存值；仅 `regular` + 邮件有效，其他类型强制 0 |
| 活动每日上限（管理端新建） | personal = 已启用发件邮箱数 × **50**；organization = **2000**；手动修改后不再自动覆盖，切换来源会重应用来源默认 |
| `daily_resume_time` | `09:00`，部署时区的墙上时间；额度耗尽后总是等到**次日**该时刻，即使今日该时刻尚未到 |
| 部署时区 | 由 `TZ` / `app.timezone` 决定；数据库、报表日期、`Date` 模板函数和额度窗口共用该时区，未配置时回退 **Asia/Shanghai** |
| 个人 SMTP 账户每日额度 | 新建账户默认 **50**；0 表示不限，额度在共用该账户的活动间共享 |
| 组织 SMTP 账户每日额度 | 默认 **0（不限）**；同样在活动间共享 |
| 重试次数 | `max_recipient_retries` 默认 **3** 次追加重试（0–1000）；配额延迟不计 |
| 事务/系统邮件重试 | `max_msg_retries` 默认 **2**；只作用于 opt-in、事务和系统邮件，不约束活动收件人 |
| 全局退信处理 | `bounce.enabled` 默认 **false**；未开启时 POP3、Webhook 和回信邮箱 DSN 都不处理 |
| 错误阈值 | `app.max_send_errors` 默认 **1000** 次/单次运行，超过自动暂停 |
| `auto_track_links` | REST 新建缺省 **false**；管理端表单默认 true |
| `type` / `content_type` / `messenger` | `regular` / `richtext` / `email` |
| `visibility` | REST 缺省 **private**；组织空间管理端表单默认 organization；global 仅活动/模板适用 |
| `pool_scope` | 默认 `organization`，创建后不可改；`all_organizations` 需专用权限 |
| `pool_reply_priority` | `contact_first` |
| `send_at` | 必须为未来时间；保存本身不切换状态；目标 running 且 `send_at` 非空会落库为 scheduled |
| AI 分类 | `enabled=false`；`timeout=15s`；`scan_interval=60s`（10s–24h）；`min_confidence=0.98` |
| 回信邮箱扫描 | `scan_enabled=false`；开启后由 `ai_enabled` / `bounce_enabled` 选择处理方式；升级时按 `ai_enabled OR bounce_enabled` 回填 |
| 退信动作 | 新装 `bounce.actions`：soft count=2/none，hard count=1/blocklist，complaint count=1/blocklist；旧库升级保留原值 |
| delivery-provider 自动拉黑 | 默认关闭；类型 `[1,3,6,8,9]`；每日 02:00；间隔 24h；回溯 7 天（平台管理员专用） |
