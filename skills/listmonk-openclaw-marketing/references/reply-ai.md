# AI 回信分类与回信邮箱退信：能力、边界与用法

本部署在标准邮件营销之上增加了 **AI 回信分类** 和 **回信邮箱 DSN 退信处理**。
两者都是平台级能力，营销侧（个人 API Key）只能读取结论，不能配置。

## 它解决什么问题

营销邮件发出去之后，客户会回信，邮件系统也会退回 DSN。人工逐封判断
“这是退订、投诉还是普通回复”很慢，而且漏掉一封明确的退订就会继续打扰客户。
AI 分类把回信变成可审计的 intent/confidence/reason_code；
DSN 处理把硬退信变成可计数、可拉黑的退信记录。

## 处理链路

```text
已启用的回信邮箱（scan_enabled=true）
        │  非破坏 IMAP 扫描（UID 游标 + BODY.PEEK），只读新邮件
        ▼
┌───────────────────────┬────────────────────────────┐
│ ai_enabled            │ bounce_enabled             │
│ reply_ai_events       │ reply_bounce_events        │
│ 去重/排队/租约/可重试 │ 严格外层 DSN 解析          │
└──────────┬────────────┴──────────────┬─────────────┘
           │ OpenAI 兼容 /chat/completions│
           ▼                            ▼
intent + confidence + reason_code    hard/soft/unknown
           │                            │
├─ unsubscribe → 拉黑 + 退订所有列表   ├─ hard 且策略 blocklist → 拉黑/剔除
├─ complaint   → 拉黑 + 退订 + 退信    └─ soft/unknown → 只记录
└─ product_complaint / other → 仅审计
```

扫描使用 `BODY.PEEK`，因此**不会把邮件标记为已读、不会删除、不会移动**；
正文只在待重试期间保留，任何 API 都不返回正文。

## 四类意图

| intent | 含义 | 自动动作 |
| --- | --- | --- |
| `unsubscribe` | 明确要求停止发送 | 拉黑并退订所有列表 |
| `complaint` | 明确的垃圾/滥用投诉或威胁举报 | 拉黑、退订所有列表，并写 `source=reply_ai` 退信 |
| `product_complaint` | 对产品或服务的抱怨 | 仅审计 |
| `other` | 其他内容 | 仅审计 |

低于 `min_confidence`（默认 0.98）的判定按 `ignored` 处理，不执行任何动作。
自动回复、无唯一投递来源、工作区不可写等也进入 `ignored`。

归属规则：发件人按**实际接收邮箱和工作区内的 sent 投递记录**解析，私域与公海
一起匹配；有回复引用时优先按引用定位，旧回信无引用时必须来源唯一。
共享的组织回信邮箱可以处理其他成员已投递的私域客户；未匹配、未发送、
歧义或跨工作区的回信只审计，不改客户。

## 事件状态

| status | 含义 |
| --- | --- |
| `pending` | 已入队，等待处理 |
| `processing` | 正被某个 worker 持有（带租约，超时会被回收） |
| `processed` | 已分类，动作已执行或明确不执行 |
| `ignored` | 终态；置信度不足、自动回复、无唯一来源或内容不明确 |
| `failed` | 调用失败，等待重试；**不代表已退订** |

| action | 含义 |
| --- | --- |
| `pending` | 尚未决定 |
| `ignored` | 明确不处理 |
| `blocklisted` | 已拉黑并退订所有列表（unsubscribe/complaint） |

常见 `ignored` reason_code：`global_ai_disabled`、`mailbox_not_active`、
`unmatched_or_ambiguous_delivery`、`empty_reply`、`low_confidence`、
`workspace_not_writable`、`malformed_message`、`message_too_large`。

## 三部分配置

**平台 AI 设置**（`settings.reply_ai`，需管理员与 `settings:manage`）：
`enabled=false`、网关地址、API Key、模型、`timeout=15s`、
`min_confidence=0.98`、`scan_interval=60s`（10s–24h）。
个人 Key 无法读写，探测接口 `/api/settings/reply-ai/models` 与
`/api/settings/reply-ai/test` 同样会被统一入口拒绝。

**单个邮箱开关**：
`scan_enabled` 是检测总开关；关闭时 AI 和 DSN 都不扫描。
`ai_enabled` 选择 AI 分类，`bounce_enabled` 选择 DSN 退信处理。
升级时 `scan_enabled` 按 `ai_enabled OR bounce_enabled` 回填。

**全局退信策略**（`bounce.enabled`、`bounce.actions.hard`）：
DSN 路径只处理 hard，且只有 blocklist 动作会改变客户；delete/unsubscribe
动作在该路径只记录。新装默认 hard count=1/blocklist；旧库升级保留原值。

## 回信邮箱 DSN 退信

- 只解析外层 `multipart/report; report-type=delivery-status` 或
  `message/delivery-status`；转发/附件 DSN、ARF、纯文本失败信不算。
- 通过原始 `Message-ID`、`X-Listmonk-Customer`/`X-Listmonk-Campaign`
  或唯一失败收件人匹配同一邮箱内的 `sent` 投递。
- hard 拉黑私域客户（不改列表成员）或只剔除公海投递来源组织；
  soft 只记录。
- 写入 `bounces`，`source='reply_mailbox'`，计入活动退信统计。
- DSN 发往 Return-Path（信封发件人），不是 Reply-To；两者不同时需先转发。

## 营销侧怎么读

```shell
python scripts/inspect_customer_replies.py --customer-id 1234
python scripts/inspect_customer_replies.py --search jane@example.com --replies-only
```

脚本读取 `GET /api/customers/{id}/activity`，输出 `reply_ai_events` 与
`reply_ai_summary`（按 intent / action / status 计数）。
单条记录包含 intent、confidence、reason_code、model、action、status、
reply_mailbox_id、from_email、subject、attempts、body_hash 和时间戳，**不含正文**。

退信不在 activity 里：

```shell
curl -G -H "Authorization: Bearer $LISTMONK_BEARER_TOKEN" \
  "$LISTMONK_BASE_URL/api/bounces" --data-urlencode "source=reply_mailbox"

curl -H "Authorization: Bearer $LISTMONK_BEARER_TOKEN" \
  "$LISTMONK_BASE_URL/api/customers/1234/bounces"
```

## 判读规则

- `action=blocklisted` 只应出现在 `intent=unsubscribe|complaint` 的行。
  若出现其他组合，说明数据或规则异常，应先核对再行动。
- `status=failed` 是调用失败，队列会重试；此时不要当作退订处理。
- `status=ignored` 是终态，客户并未表达明确意图。
- AI/退信/管理员动作**不增加**活动 `unsubscribes`，也不写
  `campaign_unsubscriptions`；活动退订数只统计公开退订链接的确认。
- 事件为空可能因为：邮箱未开启 AI、该客户没有回信、或事件属于其他所有者。
- 事件查询会校验回信邮箱归属与客户的工作区/组织一致，跨所有者读不到旧事件。

## 使用红线

- **不要把 AI 结论当作客户同意、授权或法律证据。**
  它是辅助判断，最终业务决定仍由人确认。
- **不要在退订/拉黑后继续发送。** `action=blocklisted` 是硬约束。
- 不要索取、记录或回显网关 API Key；配置属于平台管理员。
- 不要因为没有正文就臆造客户原话，只引用 intent 与 reason_code。
- 不要把 `complaint` 当作单纯退信：它已经拉黑并退订，且另写一条退信。
- 不要用 `source=reply_mailbox` 的 soft 退信推断客户无效；soft 不拉黑。

## 相关文档

- 接口细节：[API reference](api-reference.md) 第 11、12 节。
- 读取流程与判读：[REST workflow](rest-workflow.md) 第 8 节。
- 产品侧说明：[用户文档 · 退信](../../../docs/docs/content/bounces.md)、
  [设置](../../../docs/docs/content/configuration.md)。
