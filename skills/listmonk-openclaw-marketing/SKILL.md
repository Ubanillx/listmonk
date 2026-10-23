---
name: listmonk-openclaw-marketing
description: Operate this Listmonk deployment from OpenClaw through workspace-bound personal API keys. Use for customer lists and imports, marketing templates, campaign drafts and delivery, SMTP selection, send-failure diagnosis and selective retry, single or cross-campaign analytics (including open locations), and reading AI-classified inbound replies. 适用于 Listmonk 营销自动化、客户导入、活动排期、发送失败排查与补发、效果报表和 AI 回信分类结果；公海管理与 AI 调试须先核对个人 Key 的接口边界。
---

# Listmonk OpenClaw Marketing

用当前部署的 REST API 完成营销任务。接口基线核对于 **2026-10-23（迁移 v6.63.0）**，
依据仓库的 `cmd/handlers.go`、`cmd/api_keys.go` 和业务处理器，不套用上游旧版
`/api/lists`、`/api/subscribers` 或旧参数名。

本部署包含上游没有的能力：组织工作区、公海客户与分配、组织发件池、
发送失败明细与定向补发、以及 **AI 回信分类**。后两者的接口和边界分别在
API reference 第 8 节和第 11 节，不要在未读之前臆造参数。

## 按任务读取

- 接口、参数、scope、返回值和权限边界：读 [API reference](references/api-reference.md) 中对应模块。
- 具体执行顺序、完整请求、CLI 参数和异常恢复：读 [REST workflow](references/rest-workflow.md)。
- 发送失败排查、失败客户定向补发、以及 AI 回信分类结果的读取与判读：
  API reference 第 8、11 节，工作流第 7、8 节。
- AI 能力的完整边界、事件状态与使用红线：读 [AI 回信分类](references/reply-ai.md)。
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
个人 Key 传入不同的组织 ID 会被拒绝。不要用 `/api/profile` 或 `/api/health`
检测个人 Key，选择任务所需的业务读接口，例如 `GET /api/customer-lists`。

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
3. 导入客户。每行必须有 `email` 和 `customer_code`。
   只有原生字段时使用批量导入；含非空 `attribs` 时用逐行客户 API。
   等待导入终态，核对 `imported_count`、`failed_rows`、`skipped_rows`。
   `--preconfirm-subscriptions` 默认为关闭；确认方式按用户任务选择。
4. 从模板库克隆模板，或读取已有活动作为内容蓝本。
   普通模板的 subject 不作为活动主题，创建活动仍需 `subject`。
   蓝本继承内容和追踪配置；SMTP 来源、池、频率、回信邮箱和可见性按本次任务显式选择，
   不复制源活动的发送身份、旧排期、状态或受众。
5. 用 `POST /api/campaigns` 创建草稿，受众字段必须为 `customer_list_ids`。
   设置 `daily_send_limit` 和服务器本地时间 `daily_resume_time`；
   组织 SMTP 还设置 `smtp_source=organization` 和适用的 `smtp_pool_id`。
6. 检查草稿、预览、SMTP 概览和回信邮箱。用户要求测试发送时调用 test 接口。
   创建草稿不会自动发送；仅在本次任务明确要求立即发送或排期时启动。
7. 立即发送：`PUT /api/campaigns/{id}/status`，`{"status":"running"}`。
   排期：先保存未来的 RFC 3339 `send_at`，再设置 `{"status":"scheduled"}`。
   状态变更需 `campaigns:write`；running/scheduled 另需 `campaigns:send`。
8. 读取活动状态和报表，报告实际发送与互动结果，保留资源 ID 供后续复用。

## 报表与“今日”统计

- 单活动：`GET /api/campaigns/{id}/report/{summary|timeseries|links|geo|recipients}`。
- 多活动/工作区：`GET /api/campaigns/report/{summary|timeseries|links|geo|recipients}`。
  用重复 `id` 指定活动；省略 ID（可加 `all=true`）使用当前工作区内有报表权限的活动。
- 每次提供 `from` 和 `to`。推荐带时区的 RFC 3339，参数由 URL 编码器处理。
  日期字符串不会自动补齐一天；`from=to=2026-10-05` 只表示午夜边界。
- “今天发生的发送/打开/点击”直接按今日时间范围请求跨活动报表。
  不按活动创建日期过滤，否则会漏掉旧活动今天产生的事件。
  “今天新建的活动”才读取活动集合并在客户端筛选 `created_at`。
- 个人 Key 不能调用 `/api/dashboard/*`，但可以调用上述跨活动报表。
- 汇总 scope 为 `campaigns:analytics`；收件人明细独立需要 `campaigns:recipients`、
  用户明细/客户权限及开启 individual tracking。明细被拒绝时保留聚合报告并标明原因。
- 地理报表表示打开像素的近似位置；GeoIP 未配置、历史数据或邮件代理可导致未知位置。
  唯一人数、比率为 `null` 时表示不可计算/不可用，不当作零。

## 公海流程边界

个人 Key 的允许路径**不包含** `/api/pools/*` 和 `/api/org-pool-allocations/*`，
即使这些路由声明了 scope，也会先被统一入口拒绝。
允许的 `/api/customer-lists/{id}/pool-contacts` 等兼容路径仍受公海业务权限与脱敏限制。

已有投放授权的一级公海可通过 `customer_list_ids` 创建活动；不要提交二级
`org_pool_allocation` 列表 ID。普通 CLI 列表/导入流程会拒绝公海列表；
使用直接 REST 请求处理已授权公海受众，见 API reference。
一级公海统一导入使用 `/api/import/customers`，须有 `customers:import` 和
`pools:master_manage`，只能选择一个一级公海，且禁止 overwrite 选项。
`pool_scope=all_organizations` 另需角色权限 `campaigns:public_pool_send`，
不能混入私域列表；发送前检查 `pool-send-status`。

## 发送失败排查与定向补发

活动因错误阈值暂停后，按「概览 → 原因 → 名单 → 补发 → 复核」的顺序处理，不要直接重发。

1. `GET /api/campaigns/{id}/delivery-overview`（`campaigns:analytics`）：
   看 planned / sent / unsent / failed / send_errors / progress；公海活动另有逐组织进度。
   `unsent` 包含 `failed`。
2. `GET /api/campaigns/{id}/send-errors`（`campaigns:recipients`）：
   按 `category`（smtp_auth、smtp_rejected、smtp_temporary、timeout、network、
   smtp_unavailable、reply_unavailable、media、render、other）与 `search` 筛选。
   `send_errors` 是累计失败尝试次数，不是失败人数，也不是退信数。
   `historical_errors` 表示旧版本未保存明细的计数，不能反推原因。
3. `GET /api/campaigns/{id}/failed-recipients`：只返回仍未发送且至少失败一次的人，
   并给出 `can_export` / `can_retry`。
4. `POST /api/campaigns/{id}/failed-recipients/retry`：**仅活动为 paused、
   且用户明确要求时**调用。请求体 `{"recipients":[{"type":"private|pool","id":…}]}`，
   需要 `campaigns:send` 与 `campaigns:recipients` 两个 scope。
5. 复核：重读概览与失败名单。已成功者会退出名单，`send_errors` 不会减少。

缺少 `customers:sensitive_read` 或 `campaigns:private_send_error_details` 时，
邮箱脱敏、私域错误原文置空，但分类与状态码仍可见——这是权限边界，不是接口故障。
对硬退信重复补发没有意义，应先清理名单或走黑名单流程。

## AI 回信分类结果

本部署新增 **AI 回信分类**：入站回信经非破坏 IMAP 扫描，由 OpenAI 兼容模型判为
unsubscribe / complaint / product_complaint / other，仅明确的退订会退订并加入黑名单，
投诉同步记录为退信，其余只留审计，**不直接改客户**。

个人 Key 只能通过 `GET /api/customers/{id}/activity` 读取分类结论（`reply_ai_events`）：
intent、confidence、reason_code、model、action、status 与时间戳，**不含正文**。

- `action=blocklisted` 只应出现在 `intent=unsubscribe` 的行。
- `status=failed` 是调用失败待重试，不代表已退订；`ignored` 表示置信度不足。
- 配置网关、模型、阈值属于平台设置（`/api/settings/reply-ai/*`），
  个人 Key 调用会被统一入口拒绝；不要用营销 Key 探测，也不要索取或记录密钥。
- 不要把 AI 结论当作客户同意或授权的证据，也不要在退订后继续发送。

## 脚本选择与输出

| 脚本 | 用途 |
| --- | --- |
| `ensure_list.py` | 按 ID 复用或按名称查找/创建普通客户列表 |
| `import_subscribers.py` | 从 JSON 或 .xlsx 导入私域客户；文件名兼容，HTTP 路径使用 customers |
| `clone_template.py` | 克隆模板到绑定工作区 |
| `create_campaign.py` | 直接创建草稿或复用已有活动的内容蓝本；支持个人/组织 SMTP 选择 |
| `update_campaign_status.py` | 启动、排期、暂停、取消或撤回排期 |
| `fetch_campaign_reports.py` | 单活动、多个活动或整个工作区报表，可选 geo 和指定收件人分页 |
| `inspect_campaign_delivery.py` | 只读：发送概览、失败原因与当前失败客户名单 |
| `inspect_customer_replies.py` | 只读：客户活动与 AI 回信分类结论 |
| `run_marketing_flow.py` | 普通列表 → 客户 → 模板/蓝本 → 草稿 → 可选启动 → 可选单活动报表 |

运行 `python scripts/<script>.py --help` 查实际参数。
成功输出 JSON 到 stdout；业务错误输出 `{"error":{"message":...,"status":...}}` 到 stderr，
退出码 1；CLI 参数错误退出码 2，`--verbose` 进度也写 stderr。
报表脚本每次只取一页收件人，完整明细须继续分页。

失败后先查询已创建的列表、模板或活动再重试。请求超时不能推断服务端未写入；
不盲目重新创建资源或重发邮件。逐行导入可能部分成功，已有客户复用只维护列表订阅，
不会自动覆盖其姓名、客户编号或 attribs。按用户要求另调用更新接口处理。
