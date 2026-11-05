# 回信邮箱自动退信处理实现计划（v6.71.0）

日期：2026-11-04
状态：已实施（2026-11-04，v6.71.0）
来源：用户确认的方案——“回信邮箱用确定性 DSN 解析处理退信，仅硬退信按现有阈值自动拉黑”。

## 0. 已确认的产品决策（2026-11-04）

1. 私域拉黑只设 `customers.status='blocklisted'`，不退订列表成员，与现有退信策略一致。
2. 必须同时开启全局 `bounce.enabled` 和邮箱 `bounce_enabled`，缺一不生效。
3. 全局动作是 `unsubscribe` / `delete` 时只记录退信，写 `action_not_supported`，不执行退订或删除。
4. 本次不替换现有全局 POP 退信邮箱协议；新增回信邮箱路径使用 IMAP。全局 IMAP 迁移见第 13 节，作为独立后续计划。

## 1. 背景与目标

当前 AI 回信分类在入库前明确跳过 DSN、`mailer-daemon` 和自动回复（`cmd/reply_ai.go::isAutomatedInboundReply`），所以设置页的「回信 AI 分类」和「客户回信邮箱」不会分析退信，也不会拉黑客户。

目标：允许每个 IMAP 回信邮箱单独开启「自动处理退信」。后台扫描到明确投递状态通知（DSN）时：

- 解析 `Final-Recipient` / `Original-Recipient` / `X-Failed-Recipients` / `Status` / `Diagnostic-Code`，不把正文推断结果当作动作依据。
- 只对硬退信（`5.x.x`）按全局 `bounce.actions.hard` 的次数阈值执行拉黑；软退信（`4.x.x`）只记录，不动作。
- 私域客户设为 `blocklisted`；公海客户只写实际投递组织维度的剔除记录，不影响其他组织。
- 匹配范围严格限定为该回信邮箱实际发出的 `sent` 投递；未匹配、歧义、跨工作区一律只记录不动作。
- 全链路可审计、可重试、幂等；默认关闭。

> **路由前提**：DSN 回到信封发件人（`Return-Path` / SMTP `MAIL FROM`），不是 `Reply-To`。如果 SMTP 发件账号和回信邮箱不是同一地址，退信不会进入回信邮箱，解析器再正确也看不到。上线前必须用 6.8 的只读检测确认目标邮箱里确实有退信；如果没有，先修路由（发件账号/Return-Path 指向回信邮箱，或在邮箱侧把 DSN 转发过去），不要误判为解析问题。

## 2. 范围与非目标

### 范围

- IMAP 回信邮箱的 DSN 识别、解析与处理。
- `reply_mailboxes.bounce_enabled` 逐邮箱开关及连接/验证语义。
- 回信邮箱只读退信检测：读取最新一封并解析，不写事件、不拉黑、不删除、不标记已读。
- 私域拉黑、公海按组织剔除（仅硬退信）。
- `bounces` 记录、`reply_bounce_events` 处理台账、审计事件。
- 扫描游标、重试幂等；现有 POP 退信邮箱保持原样，不读取、不修改。
- API、前端、i18n、用户文档、harness 台账和测试。

### 非目标

- 不调用 LLM 判断退信；DSN 分类是确定性解析。
- 不处理正文推断地址、ARF 滥用报告、非标准纯文本退信的动作；这些只记录忽略原因。
- 不自动解除拉黑、不恢复订阅、不恢复公海分配。
- 不新增权限：沿用 `mailboxes:manage` 控制邮箱配置。
- 不改变现有 POP 退信邮箱、SES/SendGrid/Postmark/ForwardEmail webhook 的行为。
- 不把现有全局 POP 退信邮箱迁移到 IMAP；该迁移另立计划，见第 13 节。
- 本路径只执行 `blocklist` 动作，不执行全局配置里的 `unsubscribe` / `delete`。

## 3. 关键决策

D1. **确定性解析，非 LLM**：复用 `internal/bounce/mailbox/preview.go` 的 MIME 遍历与 report 字段解析，新增严格模式，禁止正文推断参与动作。

D2. **仅硬退信触发动作**：`Status` 以 `5.` 开头或 `Action: failed` 且状态为 5.x.x → hard；`4.`/`delayed` → soft；无状态但有 report 收件人 → 按 soft 记录；无 report 收件人 → ignored。soft/unknown 永不自动拉黑。只有**外层邮件**本身是 DSN（顶层 `multipart/report; report-type=delivery-status` 或顶层 `message/delivery-status` part）才处理；嵌套在转发邮件、`message/rfc822` 附件或人工回复里的 DSN 不识别为退信。

D3. **沿用全局策略**：阈值取 `bounce.actions.hard.count`；仅当 `action = blocklist` 时拉黑。全局动作是 `none` 时只记录 bounce（台账 `recorded`）；是 `unsubscribe`/`delete` 时只记录并写 `action_not_supported`，不执行退订或删除。

D4. **双重开关**：全局 `bounce.enabled` 与邮箱 `bounce_enabled` 同时为真、邮箱 `status=active` 且 `verified_at` 非空，才执行退信处理。仅开启邮箱开关而全局未启用时，保存成功但不生效，前端显示提示。

D5. **只匹配本邮箱的实际投递**：按以下顺序定位，任一歧义即放弃：

1. DSN 内嵌原始邮件的 `Message-ID`（格式 `<lm.<campaign-uuid>.c|p<id>@reply.listmonk>`）交给 `FindReplyAIDelivery` 的 references 过滤。
2. DSN 内嵌原始邮件的 `X-Listmonk-Customer` / `X-Listmonk-Campaign`，与解析出的投递交叉校验。
3. 失败收件人地址 + `FindReplyAIDelivery`（要求唯一）。

D6. **幂等**：`reply_bounce_events` 以 `(reply_mailbox_id, message_key, recipient)` 唯一；`message_key` 优先取 DSN 的 `Message-ID`（trim、截断 512 字符），为空时退回 `sha256(raw)`，避免同一消息被 MTA 重新投递、额外加了 `Received` 头就重复计数。处理、bounce 写入、客户变更、台账更新在同一事务；事务失败则不推进游标，下一轮重试并靠唯一键跳过已完成收件人。

D7. **POP/IMAP 同账号**：本次不读取、不修改全局 POP 退信邮箱，也不实现同账号互斥（用户明确要求只处理回信邮箱）。管理员如同时配置两条链路，需自行避免 POP 扫描删除邮件；不同账号/别名转发造成的跨路径重复，v1 以文档说明，不引入共享去重键。

D8. **默认关闭 + 二次确认**：邮箱开关默认 false；开启时弹确认框，明确当前硬退信阈值与动作（默认硬退信 1 次即拉黑）。

D9. **失败语义**：

- 单个收件人事务失败 → 整封 DSN 返回错误，IMAP 游标不前进，下一轮重试。
- 已提交的收件人由台账唯一键跳过，不重复写 bounce、不重复计数。
- 邮箱在扫描期间被停用/删除、成员离组、组织归档 → 事务内重验后放弃并记录原因，不产生客户变更。
- 解析失败/无 report 收件人 → 写 ignored 台账并推进游标，不阻塞后续邮件。

## 4. 数据流

```text
runReplyAIProcessor (常驻)
  -> scanReplyAIMailboxes
       查询 effective AI / bounce 开关，选择 active + verified 的邮箱
  -> scanOneReplyAIMailbox (共用 "ai" 游标)
       -> ingestReplyAIMessage(raw)
            -> ParseDSNStrict
                 ├─ 是 DSN 且 mailbox.bounce_active
                 │    ├─ 每个 report 收件人：
                 │    │    resolve delivery（references / headers / recipient）
                 │    │    ├─ 未匹配/歧义 -> 台账 ignored + 审计
                 │    │    └─ 匹配 -> core.RecordReplyBounce(tx)
                 │    │          revalidate mailbox + delivery + workspace
                 │    │          insert ledger (unique) -> 若已存在则跳过
                 │    │          insert bounces + count
                 │    │          hard + blocklist + threshold -> 拉黑/剔除
                 │    │          update ledger outcome
                 │    └─ return nil（游标推进）
                 ├─ 自动回复/非 DSN -> 跳过
                 ├─ 普通回信且 mailbox.ai_active -> 插入 reply_ai_event
                 └─ 其余 -> 跳过
```

## 5. 数据模型与迁移（v6.71.0）

新增 `internal/migrations/v6.71.0.go`，并在 `cmd/upgrade.go` 注册 `{"v6.71.0", migrations.V6_71_0}`；同步 `schema.sql`。

```sql
ALTER TABLE reply_mailboxes
    ADD COLUMN IF NOT EXISTS bounce_enabled BOOLEAN NOT NULL DEFAULT FALSE;

CREATE TABLE IF NOT EXISTS reply_bounce_events (
    id                    BIGSERIAL PRIMARY KEY,
    reply_mailbox_id      INTEGER NOT NULL REFERENCES reply_mailboxes(id) ON DELETE CASCADE ON UPDATE CASCADE,
    message_key           TEXT NOT NULL,
    recipient             TEXT NOT NULL DEFAULT '',
    sender                TEXT NOT NULL DEFAULT '',
    subject               TEXT NOT NULL DEFAULT '',
    report_status         TEXT NOT NULL DEFAULT '',
    diagnostic            TEXT NOT NULL DEFAULT '',
    bounce_type           TEXT NOT NULL DEFAULT 'unknown'
                          CHECK (bounce_type IN ('hard','soft','unknown')),
    outcome               TEXT NOT NULL DEFAULT 'ignored'
                          CHECK (outcome IN ('recorded','blocklisted','ignored')),
    reason_code           TEXT NOT NULL DEFAULT '',
    campaign_id           INTEGER NULL REFERENCES campaigns(id) ON DELETE SET NULL ON UPDATE CASCADE,
    customer_id           INTEGER NULL REFERENCES customers(id) ON DELETE SET NULL ON UPDATE CASCADE,
    pool_contact_id       BIGINT NULL,
    pool_id               INTEGER NULL,
    source_allocation_id  BIGINT NULL,
    source_organization_id BIGINT NULL,
    action_count          INTEGER NOT NULL DEFAULT 0,
    action_threshold      INTEGER NOT NULL DEFAULT 0,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    processed_at          TIMESTAMPTZ NULL,
    UNIQUE (reply_mailbox_id, message_key, recipient)
);
CREATE INDEX IF NOT EXISTS idx_reply_bounce_events_mailbox
    ON reply_bounce_events(reply_mailbox_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_reply_bounce_events_customer
    ON reply_bounce_events(customer_id, created_at DESC)
    WHERE customer_id IS NOT NULL;
```

说明：

- 不存储 DSN 正文；`diagnostic` 截断到 2 KB，`subject`/`sender` 按邮件头原值截断。
- `recipient` 统一 trim + lower；无 report 收件人的忽略行用空字符串。
- `outcome=blocklisted` 表示本次触发拉黑/剔除；`recorded` 表示只记录了 bounce；`ignored` 表示未匹配、软退信不动作、动作不受支持等。
- 台账同时服务两处：幂等依据 + 邮箱卡片内的「最近退信处理」；用户可见的最终结果仍是「退信」列表、客户状态和审计日志。
- `schema.sql` 的清理顺序：`DROP TABLE IF EXISTS reply_bounce_events` 必须排在 `reply_mailboxes` / `bounces` / `campaigns` / `customers` 之前，避免外键依赖导致 fresh install 失败。
- 保留策略：邮箱删除时级联清理；活跃邮箱的台账按 90 天清理（复用维护任务或在扫描轮次中低频执行）。若本次不实现清理，必须在 `TECH_DEBT.md` 登记，不能默认无限增长。

## 6. 后端实现

### 6.1 DSN 解析器

文件：`internal/bounce/mailbox/preview.go` 或新增 `internal/bounce/mailbox/dsn.go`，并补 `dsn_test.go`。

- 导出 `ParseDSN(raw []byte) (DSN, error)`，返回：
  - `IsBounce bool`
  - `MessageID`, `From`, `Subject`, `Date`
  - `Recipients []DSNRecipient{Address, Source, Status, Diagnostic, Action}`
  - `OriginalMessageID`, `OriginalHeaders map[string]string`
- 识别：只看外层邮件。顶层 `Content-Type` 为 `multipart/report; report-type=delivery-status`，或顶层存在 `message/delivery-status` / `message/global-delivery-status` part；仅 `Auto-Submitted` 或 `mailer-daemon` 不作为唯一依据。嵌套在 `message/rfc822`（转发/附件）或人工回复正文里的 DSN 不识别为退信，只按普通回信处理，避免把转发的旧退信重新拉黑。
- 收件人：按 `message/delivery-status` 的每个 block 解析；优先级 `Final-Recipient` > `Original-Recipient` > `X-Failed-Recipients`；字段折行按 RFC 处理。没有 report 字段时返回空收件人，不做正文推断。
- 原始邮件：遍历 `message/rfc822` 与 `text/rfc822-headers` part（现有 preview 跳过它们），只提取 `Message-ID`、`X-Listmonk-Customer`、`X-Listmonk-Campaign`、`To`，不读正文；限制 MIME 深度 20、单封 5 MiB（沿用现有上限）。
- 分类：`Status` 5.x.x → hard；4.x.x → soft；`Action: failed` + 无状态 → unknown；`Action: delayed` → soft；无状态但有收件人 → unknown（按 soft 记录）。
- 复用 `parseRecipientFields` 的地址正则与折行处理；为严格模式抽出参数，避免影响现有「读取最新一封」诊断的正文推断能力。

### 6.2 扫描选择与分流

文件：`queries/replies.sql`、`models/reply_mailboxes.go`、`cmd/reply_ai.go`、`cmd/main.go`、`cmd/reply_mailboxes.go`。

- `get-reply-ai-mailboxes` 改为返回有效开关：
  - `ai_active = m.ai_enabled AND global reply_ai.enabled`；后者沿用 `value->>'enabled'='true'`。
  - `bounce_active = m.bounce_enabled AND global bounce.enabled`；注意 `bounce.enabled` 是**普通 JSON 布尔值**（不是 `{enabled:...}`），应使用 `value::BOOLEAN`。
  - `WHERE m.status='active' AND m.verified_at IS NOT NULL AND (ai_active OR bounce_active)`，并保留原组织/成员/启用用户检查；PostgreSQL 不允许在 `WHERE` 引用 SELECT 别名，需用子查询/CTE 或重复表达式。
- `replyAIMailboxSource` 增加 `AIActive`、`BounceActive`（数据库列名 `ai_active`、`bounce_active`）。
- `runReplyAIProcessor` 不再以 `replyAI.Enabled()` 作为启动条件；`cmd/main.go` 常驻启动（空查询成本可忽略），`processReplyAIEvents` 仍在全局 AI 关闭时按现有逻辑把事件标记 ignored。保留 `a.replyAI == nil` 的保护：nil 时只跳过 AI 分类，不跳过错退信扫描。若担心无意义轮询，可在启动时查询 `EXISTS(reply_mailboxes.bounce_enabled)`，但不要依赖启动时快照，因为逐邮箱开关可运行期修改。
  - 兼容性说明：此前若启动时全局 AI 关闭，处理器完全不运行，已排队的 AI 事件会保持 pending；改为常驻后，这些事件会被标记 `ignored/global_ai_disabled`。这与现有 `processReplyAIEvent` 的终态设计一致，但属于可观察行为变化，需在发布说明中注明。
- `ingestReplyAIMessage` 顺序：
  1. 解析邮件实体。
  2. `ParseDSN` 且 `source.BounceActive` → `a.ingestReplyBounceMessage(source, raw, entity)`，返回。
  3. 现有 `isAutomatedInboundReply` 跳过。
  4. 普通回信且 `source.AIActive` → 现有 AI 事件插入；否则跳过。
- DSN 不写入 `reply_ai_events`，不进入模型调用。
- 无收件人/解析失败的忽略行由 App 直接写入台账（recipient 为空，`message_key` 优先 Message-ID，否则 `sha256(raw)`），然后返回 nil 推进游标；台账写入失败必须返回错误，不能静默丢诊断。

### 6.3 投递匹配

文件：`internal/core/reply_ai_delivery.go`、`cmd/reply_ai.go`。

- `ReplyAIDelivery` 增加 `CustomerUUID`，SQL 私域返回 `s.uuid::TEXT`，公海返回 `pc.uuid::TEXT`；用于和 DSN 原始头交叉校验。
- 匹配入口：
  - references 使用原始邮件 `Message-ID`（必要时拼接 DSN 的 `In-Reply-To`/`References`），交给 `FindReplyAIDelivery`。
  - 失败收件人用 report 地址；不要使用 DSN 外层 `From`/`To`。
  - 若原始头存在 `X-Listmonk-Customer`/`X-Listmonk-Campaign`，必须与解析出的 `CustomerUUID`/`CampaignUUID` 一致，否则 ignored（`header_delivery_mismatch`）。
- `FindReplyAIDelivery` 已保证：只匹配 `campaign_recipients.status='sent'` 或 `campaign_pool_recipients.status='sent'`、回信路由属于该邮箱、私域客户与活动组织一致、转移中客户排除、公海联系人 active。无需放宽。

### 6.4 策略执行与事务

新增 `internal/core/reply_bounce.go`，方法建议：

```go
type ReplyBounce struct {
    ReplyMailboxID int
    MessageKey, Recipient, References string
    Type, Status, Diagnostic, Source string
    Meta json.RawMessage
    Delivery ReplyAIDelivery // 事务内会重新解析并核对，不能直接信任
}

func (c *Core) RecordReplyBounce(access models.WorkspaceAccess, b ReplyBounce) (result, error)
```

事务步骤：

1. `lockReplyBounceAuthority`：锁定 `reply_mailboxes` 行，重验 `status='active'`、`verified_at IS NOT NULL`、`bounce_enabled=TRUE`、全局 `bounce.enabled`、组织 active、邮箱 owner 仍启用且是组织成员（私域为本人）。复用 `lockReplyAIAuthority` 的检查模式。
2. **事务内重新解析投递**：用 `b.Recipient` + `b.References` 重新调用 `findReplyAIDelivery`，并与 `b.Delivery` 的 campaign/customer/pool/allocation 逐项核对；不一致或不再唯一 → `workspace_not_writable`/`delivery_changed`，放弃。只锁 ID 不够，必须重跑匹配以消除解析与事务之间的竞态。
3. 锁定目标：私域 `campaign_recipients` + `customers`（`FOR SHARE/UPDATE`），公海 `campaign_pool_recipients` + `pool_contacts`；再次校验 `sent`、组织/所有者、`transfer_pending_at IS NULL`、pool contact `active`。
4. **事务内读取策略**：从 `settings` 表读取 `bounce.enabled` 与 `bounce.actions`，不要依赖 `c.consts.BounceActions`。后者只在进程启动时加载，活动运行期间修改设置不会热更新，会造成阈值/动作与实际配置不一致。
5. `INSERT INTO reply_bounce_events(...) ON CONFLICT (reply_mailbox_id,message_key,recipient) DO NOTHING`；0 行 → 已处理，直接返回 `already_processed`，不做任何客户变更。
6. 已拉黑/已剔除检查：
   - 私域 `customers.status='blocklisted'` → 台账写 `ignored/already_blocklisted`，不插入新 bounce。
   - 公海已存在 `org_pool_allocation_exclusions` 有效记录 → 台账写 `ignored/already_excluded`，不插入新 bounce。
   - 说明：私域与现有 `record-bounce` 的 `NOT EXISTS (status='blocklisted')` 一致；公海有意比 `recordPoolBounce` 更严格，避免重复 DSN 持续推高计数。
7. `INSERT INTO bounces(...)`：
   - 私域：`customer_id, campaign_id, type, source, meta, created_at`。
   - 公海：额外 `pool_contact_id, source_pool_id, source_allocation_id, source_organization_id`。
   - `source` 使用稳定标签 `reply_mailbox`；`meta` 写入 `mailbox_id`、`mailbox_email`、`message_id`、`report_status`、`diagnostic`、`classify_reason`。
8. 计数：私域按 `(customer_id, type)`；公海按 `(pool_contact_id, type, source_organization_id)`。与现有 `record-bounce` / `recordPoolBounce` 口径一致。
9. 仅当 `type='hard'`、策略 `action='blocklist'`、`count >= threshold` 时：
   - 私域：`UPDATE customers SET status='blocklisted', updated_at=NOW() WHERE id=$1 AND status <> 'blocklisted'`（沿用 bounce 策略语义，不改列表成员状态）。
   - 公海：`INSERT INTO org_pool_allocation_exclusions(...) ... ON CONFLICT ... DO UPDATE`（与 `recordPoolBounce` 相同）。
10. 更新台账 `outcome`（`recorded`/`blocklisted`/`ignored`）、`reason_code`、`action_count`、`action_threshold`、`processed_at`。
11. 提交，返回结果供审计。

注意：

- 软退信/unknown 只执行第 5–8、10 步，不进入拉黑分支。
- 策略动作为 `unsubscribe`/`delete` 时，不执行删除/退订，台账写 `recorded` + `reason_code=action_not_supported`。
- `ignored` 是终态：同一 DSN 不会因为之后工作区恢复可写而重新处理；需要人工重新投递或删除台账行。

### 6.5 审计与原因码

- 复用现有 `bounce.policy_applied` 记录已记录/已拉黑的处理，不另造一套高频动作；metadata 增加 `channel="reply_mailbox"`、`mailbox_id`、`bounce_type`、`action`、`count`、`threshold`、`campaign_id`（可选）。
- 忽略/放弃使用 `reply_bounce.ignored`，`result="ignored"`，`reason_code` 固定为下表值；实现统一走 `a.recordBackgroundAuditResult`。
- 每次 DSN 收件人处理写一条：`actor=system`、`object=reply_bounce_event`、`object_id=台账 ID`、`organization_id=邮箱组织`。不写正文、完整头、附件、密码或邮箱明文。

| reason_code | 含义 | 是否终态 |
| --- | --- | --- |
| `blocklisted` | 硬退信达到阈值并拉黑/剔除 | 是 |
| `recorded` | 已记录 bounce，未触发动作 | 是 |
| `soft_recorded` | 软退信只记录 | 是 |
| `already_blocklisted` | 客户已拉黑，跳过 bounce | 是 |
| `already_excluded` | 公海联系人已被该组织剔除，跳过 bounce | 是 |
| `unmatched_delivery` | 找不到唯一 sent 投递 | 是 |
| `ambiguous_delivery` | 多条投递无法消歧 | 是 |
| `header_delivery_mismatch` | DSN 原始头与解析投递不一致 | 是 |
| `delivery_changed` | 事务内重跑匹配结果变化 | 是 |
| `workspace_not_writable` | 邮箱/组织/成员/客户状态已不可写 | 是 |
| `action_not_supported` | 全局动作是 delete/unsubscribe | 是 |
| `no_report_recipient` | DSN 没有 report 收件人 | 是 |
| `malformed_dsn` | DSN 解析失败 | 是 |
| `dsn_too_large` | 超过 5 MiB / 嵌套上限 | 是 |

- 前端「最近退信处理」的原因文案必须覆盖上表；未翻译时回退英文，不能显示原始 reason_code。

### 6.6 邮箱 CRUD、测试连接与互斥

文件：`cmd/reply_mailboxes.go`、`queries/replies.sql`、`models/reply_mailboxes.go`。

- `replyMailboxRequest` 增加 `BounceEnabled bool`；`models.ReplyMailbox` 增加 `BounceEnabled`。
- 创建/更新查询增加 `bounce_enabled`：
  - create：`$13`，默认 false。
  - update：`$14`；密码保留条件改为 `NOT ($12 OR $14)`；`status` 的 pending/active 分支和 `verified_at` 失效条件都改为 `($12 OR $14)`。
- 密码要求：`AIEnabled || BounceEnabled` 时必须有密码或已保存密码；两者都关时维持现有“可只保存地址”的行为。
- `TestReplyMailbox`：允许 `ai_enabled || bounce_enabled`；验证更新的 `WHERE` 与配置快照包含两个开关，避免测试期间切换开关被旧结果标记为已验证；i18n 用新 key `replyMailbox.testRequiresReceiving`。
- 保存时互斥检查：本次不读取、不修改全局 POP 退信邮箱，也不实现同账号互斥（用户明确要求只处理回信邮箱）。管理员如同时配置两条链路，需自行避免同一账号被 POP 删除邮件。
- 列表/详情响应增加两个只读展示字段（`db:"-"`，不返回凭据）：
  - `bounce_processing_enabled`：全局 `bounce.enabled`，用于显示“已开启但全局未启用”的提示。
  - `bounce_policy`：`{enabled, threshold, action}`，**每次请求从 settings 读取** `bounce.actions.hard`，不使用可能过期的 `c.consts`，用于开关确认框和卡片内的策略提示。

### 6.7 处理记录接口

- 新增 `GET /api/profile/reply-mailboxes/:id/bounce-events?page=1`，与现有 `/api/profile/reply-mailboxes*` 路由同组注册在 `cmd/handlers.go`（当前约 326–332 行）。
- 不新增 personal API Key scope：现有回信邮箱管理接口不在 API Key 路径 allowlist 内，新端点保持一致，使用浏览器会话 + `mailboxes:manage`；未授权返回 403。
- 服务端分页，每页 20 条，按 `created_at DESC`；响应结构沿用 `models.PageResults`：`{results, total, page, per_page}`。
- 权限：`mailboxes:manage` + 邮箱所有者；组织邮箱允许该组织经理；平台管理员按现有组织管理规则。普通组织成员只能选用邮箱，不能读取处理记录。
- 返回字段：`created_at`、`recipient`（按 `customers:sensitive_read` 与现有工作区脱敏规则处理）、`bounce_type`、`outcome`、`reason_code`、`campaign_id`、`customer_id`、`pool_contact_id`；不返回 DSN 正文、原始邮件头或诊断全文。
- 查询使用 `(reply_mailbox_id, created_at DESC)` 索引；邮箱不存在、已删除或不在调用者管理范围时统一返回 404，不泄漏邮箱是否存在。
- 这是只读 GET，不写入 `auditRoutes`；如后续增加“手动重试/删除台账”动作，再按变更接口补审计路由和权限。

### 6.8 只读退信检测接口

解决“邮箱到底有没有收到退信/为什么没识别”的排障问题，对齐现有退信邮箱的“读取最新一封”能力。

- 新增 `POST /api/profile/reply-mailboxes/:id/inspect-latest`，与 6.7 同权限（`mailboxes:manage` + 所有者/组织经理/平台管理员）。
- IMAP 只读获取当前文件夹 **UID 最大的一封邮件**（`Peek: true`），用 `ParseDSN` 严格解析：
  - 返回 `empty`、`is_bounce`、`from`、`subject`、`date`、`recipients[{address, source, status}]`、`bounce_type`、`reason`、`message_id`。
  - 不返回正文、附件和完整头；`diagnostic` 截断。
- 不推进游标、不写 `reply_bounce_events`、不写 `bounces`、不改变客户、不删除、不标记已读、不触发 AI。
- 普通邮件返回 `is_bounce=false` 并提示“最新一封不是退信”；空邮箱单独提示；连接失败返回可读错误，不把异常当“没有退信”。
- 该接口用于确认三件事：邮箱能否连上、最近是否有信、最近的退信是否被正确解析。它与 6.7 的处理记录互补：记录回答“处理了什么”，检测回答“为什么没处理”。
- UI 放在「测试连接」旁边，按钮文案“检测最新一封”，结果在卡片内展开，不弹新页面。

## 7. 前端实现

文件：`frontend/src/components/ReplyMailboxSettings.vue`、`frontend/src/views/settings/bounces.vue`、`frontend/src/views/settings/inbound-replies.vue`、`frontend/src/api/index.js`（如字段透传需要）。

### 7.1 信息架构：单一入口放在邮箱卡片内

退信处理的本质是“这个邮箱是否被扫描、如何处理扫描到的退信”，因此配置和状态必须跟邮箱本身放在一起，不另开全局页面，也不放到「回信 AI 分类」页。

每张回信邮箱卡片内部按以下顺序组织：

```text
┌ 客户回信邮箱 ───────────────────────────────────────┐
│ 邮箱 / 名称 / 状态 / 默认标记 / 停用 / 删除            │
│                                                      │
│ 回信地址                                              │
│ [邮箱地址]                [显示名称]                   │
│                                                      │
│ 自动处理                                              │
│ [●] AI 回信分类                                       │
│     识别退订和投诉；只有明确意图才会动作。              │
│ [ ] 退信处理（硬退信）                                 │
│     明确 5.x.x 达到阈值时自动拉黑；软退信只记录。        │
│     当前策略：硬退信 1 次 → 拉黑    [前往退信设置]       │
│                                                      │
│ 收信连接（AI 与退信处理共用）                           │
│ [用户名] [密码] [IMAP 主机] [端口] [文件夹]             │
│ [测试连接]  [检测最新一封]                              │
│   └ 检测结果：退信 / 未识别 / 空邮箱 / 连接失败          │
│                                                      │
│ ▸ 最近退信处理（默认收起，仅所有者/组织经理可见）        │
│                                                      │
│ [设为默认]                            [保存配置]       │
└──────────────────────────────────────────────────────┘
```

原则：

- **一个邮箱、一套扫描配置**：两个开关共用同一组 IMAP 凭据、同一个扫描间隔、同一个验证状态；不出现“AI 用这个账号、退信用另一个账号”的分裂状态。
- **两个开关、两次授权**：AI 分类与退信拉黑的风险不同，不能合并成一个开关；打开退信开关必须二次确认。
- **两者都关 = 仅保存地址**：收信连接区隐藏，邮箱不参与任何扫描；任一开启才显示连接区和「测试连接」。
- **策略仍在设置 → 退信**：卡片只展示当前生效的硬退信阈值/动作和跳转链接，不在这里重复维护全局策略。

### 7.2 交互状态

- 数据模型：`bounceEnabled`，`normalize`/`blankMailbox`/`wire` 同步 `bounce_enabled`。
- 开关：
  - `data-cy="reply-mailbox-bounce"`，label `replyMailbox.bounceToggle`。
  - 帮助文案：仅硬退信、按设置 → 退信阈值、需全局启用、软退信只记录；并注明扫描间隔由「设置 → 回信 AI 分类」的检查间隔统一控制（默认 60s），避免只开退信的用户找不到频率配置。
  - 从关到开时用 `$dialog.confirm` 二次确认，正文显示服务端返回的当前策略（见 6.6），例如“硬退信 1 次后拉黑，软退信只记录”。
  - 全局退信处理未启用：开关下方显示 warning（`replyMailbox.bounceRequiresGlobal`），提供跳转设置 → 退信的链接；开关仍可保存，但不生效。
  - 当前硬退信动作不是 `blocklist`：显示 warning（`replyMailbox.bouncePolicyNotBlocklist`），“开启后只记录退信，不会拉黑客户”。
  - 只读邮箱（其他成员的）：两个开关都禁用，提示只有所有者能开启。
- 收信连接区（用户名/密码/端口/文件夹）和「测试连接」按钮的显示条件改为 `aiEnabled || bounceEnabled`；保存校验同样改为任一开启。
- 「检测最新一封」只在已保存密码时可用；调用 6.8 的只读接口，结果显示在卡片内。空邮箱、普通回信、退信、连接失败四种结果必须区分，不能都显示为“无退信”。
- 状态标签：active 且两者都关 → “地址已保存”；任一开启 → “已验证/已启用”。`statusAddressReady` 逻辑同步。
- 待验证/停用状态：
  - `pending`：开关可保存，但标签显示“待验证”，扫描不会运行；提示先点「测试连接」。
  - `disabled`：邮箱不参与扫描；两个开关保留原值但置灰，恢复启用后按原值继续。
  - `retained`：沿用现有保留语义，退信处理不改变转发状态。
- 设置 → 退信页：在硬退信行或邮箱区补一句 “回信邮箱开启自动退信处理后也按此阈值执行；软退信只记录”。不在此页新增 IMAP 邮箱配置。
- 设置 → 回信 AI 页：`scan_intervalHelp` 补充该间隔同时驱动回信邮箱的退信扫描；不在此页新增退信开关。
- i18n：`i18n/en.json`、`zh-CN.json`、`zh-TW.json` 增加上述 key；另补 `audit.actions.reply_bounce.ignored` 和 6.5 原因码文案，避免审计页/卡片回退显示原始 reason_code。其他语言按现有回退机制处理。

### 7.3 最近退信处理（同卡片内，随第一版交付）

- 卡片底部增加可折叠区域，默认收起；展开时按需请求 `GET /api/profile/reply-mailboxes/:id/bounce-events?page=1`。
- 每行显示：时间、失败收件人、硬/软、结果（拉黑/仅记录/忽略）、原因码对应文案；不显示 DSN 正文。
- 失败收件人按 `customers:sensitive_read` 与现有工作区规则脱敏；没有敏感权限的邮箱所有者只能看到 `liu***@example.com`。这是与退信列表一致的隐私边界，不为了排障放宽；需要完整地址时走客户详情或授权流程。
- 仅邮箱所有者、组织经理或平台管理员可见；接口复用 `mailboxes:manage` 与现有工作区校验，普通组织成员看不到该区域。
- 空态：“暂无退信处理记录”。请求失败显示错误，不静默显示空；分页使用“加载更多”，不一次性拉全量。
- 这样用户不必去日志或数据库查“为什么这个客户被拉黑/为什么这封退信没处理”。

### 7.4 明确不做

- 不在「回信 AI 分类」页放退信开关或邮箱列表；该页只保留全局模型配置。
- 不在「设置 → 退信」里维护第二套 IMAP 凭据；该页只保留全局策略、POP 退信邮箱和 webhook。
- 第一版不做按邮箱的统计图表、批量启停和退信趋势。

## 8. 文档与台账

- `docs/harness/BUSINESS_LOGIC.md`：新增「回信邮箱退信处理不变量（v6.71.0）」：双重开关、report-only、hard-only、blocklist-only、工作区边界、公海组织剔除、幂等、失败语义、无自动恢复。
- `docs/ARCHITECTURE.md` / `docs/harness/TECH_ARCHITECTURE.md`：入站邮件链路增加 DSN 分支与 `reply_bounce_events` 归属。
- `docs/docs/content/bounces.md`：用户可见说明——如何开启、依赖、阈值、软/硬退信差异、限制。
- `docs/docs/content/configuration.md`：`bounce.enabled` / `bounce.actions` 对回信邮箱的适用范围。
- `docs/docs/content/apis/settings.md` 或 `docs/swagger/collections.yaml`：补 `GET /api/profile/reply-mailboxes/:id/bounce-events` 的权限、分页和返回字段。
- `docs/harness/TECH_DEBT.md`：登记两条已知债务——退信策略 SQL 与 `record-bounce`/`recordPoolBounce` 存在有意差异，后续可抽公共策略函数；活跃邮箱台账的 90 天清理若本次未实现，登记清理责任与时间点。
- 实施完成后更新 `docs/harness/STATUS.md`，关闭 PLAN 条目。

## 9. 测试计划

### 单元测试

- `internal/bounce/mailbox/dsn_test.go`：
  - `multipart/report` + `message/delivery-status` 单/多收件人。
  - `5.1.1` hard、`4.2.2` soft、无 `Status`、`Action: delayed`。
  - `Final-Recipient` 优先于 `Original-Recipient` 和 `X-Failed-Recipients`。
  - `message/rfc822` 与 `text/rfc822-headers` 中的原始 `Message-ID` / `X-Listmonk-*`。
  - 无 report 字段的纯文本退信 → 空收件人，不 body-infer。
  - 转发的 DSN（外层普通邮件 + 内嵌 `message/rfc822` report）→ 不识别为退信。
  - 折行、超长、嵌套超限、畸形 MIME。
  - ARF `feedback-report` 不被识别为 DSN。

### 后端集成测试（真实 PostgreSQL，沿用现有 cmd 测试夹具）

- 硬退信阈值 1 → 私域 `blocklisted` + 1 条 hard bounce + 审计。
- 阈值 2 → 第一封只记录，第二封拉黑；重复同一 DSN 不重复计数。
- 软退信 → 记录 soft，客户状态不变。
- 全局动作 `none` / `unsubscribe` / `delete` → 不拉黑、不删除、不批量退订，台账原因正确。
- 公海 → 仅来源组织剔除；其他组织分配不受影响；一级公海不全局拉黑；已剔除联系人只写 `already_excluded`，不新增 bounce。
- 工作区隔离：A 组织邮箱不能拉黑 B 组织客户；成员离组、组织归档、客户转移中均放弃。
- 匹配：原始 `Message-ID`、`X-Listmonk-*`、失败收件人三条路径；头与投递不一致、歧义、未匹配 → ignored。
- 事务竞态：预解析后、事务前投递被取消/转移，事务内重跑匹配必须拒绝并写 `delivery_changed`。
- 策略时效：活动运行中修改 `bounce.actions.hard`，下一次处理按新值执行（不读启动时的 `c.consts`）。
- 幂等/重试：相同 `message_key + recipient` 第二次调用不写 bounce、不重复计数；模拟事务失败后游标不前进。
- 扫描：仅 `bounce_enabled` 且全局 AI 关闭时仍扫描；全局 bounce 关闭时不生效；AI 开关关闭不影响退信扫描。
- 邮箱 API：创建/更新 `bounce_enabled`、密码/验证状态、测试连接允许任一开关；确认不读取或修改现有 POP 退信邮箱配置。
- 处理记录接口：所有者/组织经理可分页读取、普通成员 404、跨组织 404、脱敏字段正确、空态、分页边界。
- 只读检测接口：空邮箱、普通回信、硬退信、软退信、连接失败、超大邮件；调用前后 `reply_ai_events`、`reply_bounce_events`、`bounces`、`reply_mailbox_scan_cursors` 和客户状态均无变化。
- 迁移：v6.71.0 幂等、旧邮箱默认 false、fresh schema 一致、`DROP TABLE` 顺序可重复执行。
- 回归：DSN 不写入 `reply_ai_events`、不调用模型；现有 AI 回信/投诉/退订链路不变；全局 AI 关闭时已排队事件按现有终态标记 ignored。

### 前端

- 扩展 `frontend/cypress/e2e/reply-mailbox-config.cy.js` 或新增 spec：
  - 两个开关同卡片可见、保存 payload 含 `bounce_enabled`、连接区随任一开关显示。
  - 两者都关时连接区隐藏，只保存地址。
  - 只读邮箱无开关操作。
  - 全局未启用 warning、非 blocklist 策略 warning。
  - 开启确认框显示当前阈值/动作，确认与取消行为正确。
  - 仅退信开启时测试连接按钮可用。
  - 「检测最新一封」空邮箱/普通回信/退信/连接失败四种结果展示正确，调用不产生处理记录。
  - 最近退信处理默认收起，展开加载分页，普通成员不可见，空态与错误态。
  - 现有 AI 开关与保存流程不回归。

### 验证命令

- `go test ./...`（含新增专项；真实 DB 测试按现有 `*_TEST_DSN` 机制）。
- `cd frontend && yarn lint && yarn cypress run`（相关 spec）。
- `make build-frontend`、`make build`。
- 按 `AGENTS.md` 重启 `dev-backend-1`，确认日志无错误、`http://localhost:9173` 返回 200；迁移日志显示 v6.71.0。

## 10. 实施顺序与工作量

| 阶段 | 内容 | 预估 |
| --- | --- | --- |
| 1 | DSN 严格解析器 + 单元测试 | 0.5–1 天 |
| 2 | v6.71.0 迁移、schema、模型、查询 | 0.5 天 |
| 3 | Core 事务、匹配、策略、台账 + 集成测试 | 1.5–2 天 |
| 4 | 扫描分流、邮箱 CRUD/验证/互斥 + 测试 | 0.5–1 天 |
| 5 | 处理记录查询接口 + 权限/脱敏测试 | 0.5 天 |
| 6 | 只读检测接口（最新一封） + 测试 | 0.5 天 |
| 7 | 前端开关/确认/提示/处理记录/检测 + i18n + Cypress | 1–1.5 天 |
| 8 | 文档、harness、Docker 同步与全量验证 | 0.5 天 |

总计约 6–7.5 个工程日。

## 11. 风险与回滚

| 风险 | 缓解 |
| --- | --- |
| 误把普通自动回复当退信 | 只认外层 delivery-status report；转发/嵌套 DSN 不识别；Auto-Submitted 不作为唯一依据 |
| 正文推断地址误拉黑 | 禁止 body-inferred 参与匹配和动作 |
| 软退信被拉黑 | 只对 hard 进入拉黑分支，测试覆盖 4.x.x |
| 全局策略为 delete 造成误删 | 本路径只允许 blocklist；delete/unsubscribe 记录原因不执行 |
| 跨组织拉黑 | 事务内重验邮箱组织、客户组织、delivery 的组织/所有者 |
| DSN 重复计数 | 台账唯一键 + 单事务；已拉黑/已剔除只写台账 |
| POP/IMAP 同账号双处理 | 本次不读取/不修改 POP 退信邮箱；两条链路由管理员自行避免同账号配置 |
| 策略修改后仍用旧阈值 | 事务内从 settings 读取 `bounce.enabled`/`bounce.actions`，不用启动时 `c.consts` |
| 大批量退信拖慢轮询 | 单封 DSN 只做本地事务；沿用 200 封/2 分钟扫描预算；必要时后续增加限流 |
| 邮箱被停用/删除时仍处理 | 事务内 `FOR UPDATE` 重验 `bounce_enabled`/`status`/`verified_at`/成员资格 |
| 台账无限增长 | 邮箱删除级联清理；活跃邮箱 90 天清理或登记 `TECH_DEBT` 并设定清理时间点 |
| 脱敏后难以排障 | 卡片只做定位；完整地址走客户详情/授权流程，不在退信日志放宽隐私边界 |

回滚：关闭逐邮箱开关或全局 `bounce.enabled` 即停止动作；迁移只新增列和表，旧版本忽略即可，无需破坏性回滚。

## 12. 验收标准

1. 开启邮箱开关 + 全局退信处理 + hard 动作为 blocklist/阈值 1 后，一封 5.1.1 DSN 使匹配的私域客户变为 `blocklisted`，写 1 条 hard bounce 和 1 条审计；同一 DSN 重放无第二次变更。
2. 4.x.x DSN 只写 soft bounce，客户状态、列表成员、公海分配均不变。
3. 公海 DSN 只产生实际投递组织的剔除记录，其他组织仍可投放；一级公海联系人不被全局拉黑；已剔除联系人不新增 bounce。
4. 未匹配、歧义、跨工作区、邮箱头与投递不一致、事务内投递已变化的 DSN 不产生任何客户变更，台账原因可查。
5. 转发的旧 DSN、ARF 报告、普通自动回复不触发退信处理；DSN 永不进入模型。
6. 活动运行期间修改硬退信阈值/动作，下一次处理立即按新值执行。
7. 全局退信处理未启用时，邮箱开关保存成功但不执行，页面有明确提示。
8. 邮箱卡片的「最近退信处理」能按权限分页看到拉黑/仅记录/忽略及原因；普通组织成员看不到。
9. 「检测最新一封」能区分空邮箱、普通回信、退信、连接失败，且不写台账、不写 bounce、不推进游标、不改变客户。
10. 迁移、Go 测试、前端 lint/build、相关 Cypress、Docker 重启与 9173 健康检查全部通过。

## 13. 协议范围：为什么本次不全局替换成 IMAP

### 13.1 现状

- 新增的回信邮箱退信处理**已经使用 IMAP**（`cmd/reply_imap.go`），不依赖 POP。
- 现有「设置 → 退信 → 退信邮箱」仍是 POP-only：
  - `internal/bounce/bounce.go::New` 只接受 `type=pop`，其它值直接报 `unknown bounce mailbox type`。
  - `internal/bounce/mailbox/pop.go::Scan` 处理成功后调用 `DELE` 删除源邮件，因此不需要游标。
  - 设置页协议下拉只有 POP；`TestBounceMailbox` 和只读检测也走 POP 实现。
- `reply_mailbox_scan_cursors.mailbox_id` 有外键指向 `reply_mailboxes`，不能直接复用于全局退信邮箱。

### 13.2 可以换，但不是改一行配置

如果要把全局退信邮箱的检测协议替换成 IMAP，至少需要：

1. **IMAP 实现**：新增 `type=imap`，支持 folder、TLS/STARTTLS；复用 `go-imap` 和现有 IMAP 连接/超时/预算逻辑，不要另写一套。
2. **非破坏性扫描**：POP 靠 `DELE` 避免重复；IMAP 不建议删除用户邮件，因此必须新增 UID 游标（如 `bounce_mailbox_scan_cursors`，按邮箱 UUID + UIDVALIDITY + connection hash 记录）和/或入库去重键。否则每轮扫描会重复处理同一封退信。
3. **设置与迁移**：设置模型/UI 增加 IMAP 字段；已有 POP 配置继续可用；需要明确“切协议后旧游标/旧邮件”的处理方式。
4. **测试与诊断**：`TestBounceMailbox`、`mailbox.Test`、只读预览都要按协议分支，否则页面测试和后台扫描行为不一致。
5. **同账号语义**：如果全局退信邮箱和某个回信邮箱指向同一个 IMAP 账号，两个扫描器会看到同一批邮件。要么保留本计划的互斥校验，要么把两条链路合并成一个扫描器并共享游标/去重；后者是更大的重构。

### 13.3 两条可选路线

**路线 A：给全局退信邮箱增加 IMAP 选项（保留 POP）**

- 适用：仍然需要一个平台级统一退信地址（例如系统通知、非回信邮箱发送的活动）。
- 代价：新增协议实现、游标表、设置字段和迁移；POP 继续保留一个兼容周期。
- 建议版本：v6.72+，独立迁移，不和 v6.71 的退信处理混在一起。

**路线 B：退役全局退信邮箱，统一由回信邮箱处理**

- 适用：所有营销/事务邮件的 DSN 都能路由到各自的回信邮箱。
- 收益：只有一条 IMAP 入站管线、一套游标、一套台账；不再维护 POP。
- 代价：平台级统一退信地址消失，依赖它的部署需要迁移；所有活动的回信路由必须完整配置。

### 13.4 建议

- v6.71 先交付“回信邮箱 IMAP 退信处理”，把用户实际需要的 IMAP 检测跑通；现有 POP 退信邮箱保持不动，避免协议迁移和功能开发同时进行。
- v6.71 上线后根据实际使用决定：
  - 仍需要统一退信地址 → 走路线 A；
  - 所有 DSN 都能进回信邮箱 → 走路线 B，并另立计划处理退役与迁移。
- 无论选哪条，都不在 v6.71 内删除 POP 或改变 `bounce.mailboxes` 的现有语义。
