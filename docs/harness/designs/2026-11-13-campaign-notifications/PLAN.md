# 活动生命周期通知与发送错误率告警计划（v6.74.0）

日期：2026-11-13
状态：已实施（2026-11-13，v6.74.0）
来源：用户确认的方案——“新增发送到操作手上，然后补充手动暂停/取消、额度延期提醒邮件；新增独立的错误率/时间窗口告警监控，异常向操作者和系统管理员报错”。

## 0. 已确认的产品决策（2026-11-13）

1. “操作者”收件人 = 活动 `owner_user_id` 对应用户的邮箱 + 现有 `app.notify_emails` 系统管理员邮箱；不扩展到组织管理员。
2. 新的错误率窗口告警只发邮件，不改变活动状态；现有 `app.max_send_errors` 累计阈值仍按原逻辑自动暂停。
3. 窗口告警默认开启：窗口 5 分钟、阈值 50 条、冷却 30 分钟；在“设置 → 投递与性能”提供开关和参数。

## 1. 背景与现状

现有邮件通知（`internal/notifs`）只发给全局 `app.notify_emails`：

- 活动自然完成：`internal/manager/pipe.go::cleanup` 调 `sendNotif(c, finished, "")`。
- 发送错误达到 `app.max_send_errors` 自动暂停：`pipe.cleanup` 的 `withErrors` 分支。
- 个人 SMTP 不可用自动暂停：`pipe.cleanup` 的 `stopReasonPersonalSMTP` 分支。
- 手动暂停/取消、每日额度延期：只有审计日志，没有邮件。

缺口：

- 活动 owner 收不到任何生命周期通知。
- 手动暂停/取消、额度延期没有提醒。
- 没有独立的时间窗口错误率监控；错误告警只能等累计阈值触发自动暂停。

## 2. 目标

1. 活动完成、自动暂停（错误阈值 / 个人 SMTP 不可用）通知 owner + 系统管理员。
2. 新增手动暂停/取消通知（含操作人信息）和每日额度延期通知（含恢复时间）。
3. 新增独立的错误率/时间窗口告警：运行中的活动在窗口内失败事件数达到阈值时，向 owner + 系统管理员发告警邮件；按活动冷却，不自动暂停。
4. 告警收件人、阈值、窗口、冷却均可在设置页调整；默认开启。
5. 全链路可测试、可运维、无重复轰炸。

## 3. 范围与非目标

### 范围

- `internal/notifs` 收件人合并与隐私隔离（系统管理员与 owner 不互相看到对方地址）。
- 活动完成、自动暂停通知补 owner。
- 手动暂停/取消、每日额度延期通知。
- 错误率窗口监控、告警去重表、设置项、迁移、设置页、模板、i18n。
- Go 测试、迁移测试、设置页 Cypress、文档与 harness 台账。

### 非目标

- 不改变 `app.max_send_errors` 自动暂停语义，也不替换现有失败事件写入。
- 不增加组织管理员、角色或用户级别的通知偏好。
- 不实现告警历史页面、告警确认/静默 UI、Webhook/IM 通道。
- 不对活动完成/暂停做“摘要日报”或聚合发送。
- 不修改导入完成、opt-in、找回密码等既有通知的收件人。
- **不覆盖**以下非活动状态接口触发的批量暂停：`cmd/personal_smtp.go::pauseCampaignsForUserSMTP`（停用个人 SMTP 时批量暂停）、`internal/core/organizations.go`（移除成员/归档组织时置 paused）、`internal/core/users.go`（删除用户时置 paused）。这些属于账号/组织管理清理，owner 可能正在被转移或删除；本次只在显式活动暂停/取消接口发送通知，其余路径继续写审计。若后续需要，可复用同一通知 helper 扩展。

## 4. 关键决策

D1. **收件人组合**：`app.notify_emails` + 活动 owner 邮箱；owner 不存在、账号禁用或邮箱为空时只发系统管理员。owner 已经在系统管理员名单中时只发一次。

D2. **隐私隔离**：系统管理员和 owner 分别单独发送（不共用 `To` 列表），避免把管理员内部邮箱暴露给普通用户，也避免 owner 地址暴露给其他管理员。`notifs` 新增 `NotifySystemAnd(extra []string, ...)`：模板只渲染一次，然后系统名单一封、extra 中不在系统名单的地址逐个一封；去重按 trim + 小写。部分失败语义：单个地址失败只记录日志并继续；返回错误仅当模板渲染失败或**没有任何**收件人发送成功。生命周期通知忽略返回值；窗口告警在“零成功”时释放冷却重试，部分成功则保留冷却避免重复轰炸。

D3. **owner 解析**：复用 `core.GetUser(id, "", "")`，仅当 `status='enabled'` 且 `email` 非空时返回邮箱。manager 侧通过 `Config.OwnerEmail func(int) (string, error)` 注入，便于单测；CMD 侧复用同一 helper。

D4. **手动暂停/取消在 HTTP 层发通知**：仅覆盖 `PUT /api/campaigns/:id/status`（`cmd/campaigns.go::UpdateCampaignStatus`，活动列表/详情的暂停、取消都走它）。成功提交后，若 `out.Status` 为 `paused`/`cancelled`，异步（goroutine）发送通知，避免 SMTP 阻塞 API 请求。原因文案包含操作人：`Name` 优先，回退 `Username`，再回退 `ID`。活动没有运行管道（如 scheduled → paused）时同样发送。
   发送器可注入：`App` 增加 `notifyCampaign func(extra []string, subject, tplName string, data any) error`，生产在 `main.go` 注入 `notifs.NotifySystemAnd`，测试不注入（nil）时**在解析 owner、启动 goroutine 之前直接返回**——现有 `cmd/campaign_admin_operations_test.go`、`cmd/email_media_resume_test.go` 在 notifs 未初始化/模板为 nil 且测试库之后关闭的情况下调用状态接口，必须不受影响。owner 解析在请求 goroutine 内同步完成，异步段只做渲染与发送，避免 SIGHUP 关库竞争。同时 `notifs.Notify`/`NotifySystem`/`NotifySystemAnd` 增加 `no == nil || Tpls == nil || em == nil` 守卫，作为第二道防线。

D5. **自动事件在 manager 层发通知**：
   - `stopReasonDeferred` 分支（SMTP 日额度导致立即停止）。
   - `p.deferred` 分支（活动日上限，队列自然排空后）。
   两处都调用同一个 `pipe.notifyDeferred()`，内部用 `store.GetCampaign` 读取最新 `next_resume_at`；`store == nil` 或读取失败时仍发送，只是不带恢复时间。每次 deferral 只发一封（`p.deferred` 去重 + `cleanup` 只执行一次）。现有 `internal/manager/manager_test.go::deferCaptureStore` 内嵌 nil `Store`，需补 `GetCampaign` 并注入 notifier，否则 `p.deferred` 分支测试会 panic。
   - 完成、错误阈值暂停、个人 SMTP 暂停继续走 `sendNotif`，只扩展收件人与原因文案。
   - **优先级**：`cleanup` 的 `withErrors` 分支先于 stopped/deferred 且直接 return，因此同一轮里“先延期、后错误数达标”只会发“Paused（错误阈值）”一封，不补发延期邮件——最终状态是 paused，延期邮件会误导。该优先级写入业务不变量。

D6. **窗口告警是独立监控**：只在非 `passive` 实例启动（与 `ScanCampaigns` 一致），每 `clamp(window/5, 15s, 1m)` 采样一次。查询只取 `status='running'` 且窗口内失败事件数 ≥ 阈值的活动。告警只发邮件，不暂停、不写活动状态。

D7. **告警去重原子化且统一用数据库时钟**：新表 `campaign_send_error_alerts(campaign_id PK, last_alert_at)`；窗口起点与冷却截止都用 SQL 计算（`NOW() - make_interval(secs => $n)`），不传应用侧 `time.Time`，避免应用/数据库时钟偏差。`INSERT ... ON CONFLICT DO UPDATE ... WHERE last_alert_at <= NOW() - make_interval(...) RETURNING last_alert_at` 原子抢占，跨实例、重启都不会重复轰炸。发送零成功时按 `campaign_id + last_alert_at` 精确删除本次 claim，下一轮可重试；部分成功保留 claim。窗口计数按 `campaign_send_errors.created_at`，重试失败各算一次（与现有 `send_errors` 语义一致）。

D8. **无收件人时不抢占冷却**：先解析 owner + 系统名单；两者都为空则跳过该活动，不写 `campaign_send_error_alerts`，避免之后配置收件人仍被冷却吞掉。

D9. **原因文案本地化**：新增 i18n key（en 为基准，zh-CN 覆盖），manager 用 nil-safe helper（单测中 `i18n == nil` 时回退英文原文）。模板标签继续走 `L.Ts`，其他语言缺 key 时由 en 兜底。

D10. **配置读取**：设置保存在 `settings` 表，启动时由 `initSettings` 载入 koanf；保存设置触发 SIGHUP 重启，因此监控在启动时读取一次窗口/阈值/冷却即可。迁移保证旧库也有默认值。`UpdateSettings` 只更新已存在的 key（`update-settings` 语句），因此迁移必须建好 4 个 key；`UpdateSettingsByKey` 对 4 个 key 补同等校验，防止单 key 写入绕过。整包保存用原始 JSON 判断 4 个 key 是否存在：缺席回填 `cur`，显式 `false` 照常生效。

D11. **测试隔离与 nil 安全**：除 `App.notifyCampaign` 注入外，`notifs.Notify`/`NotifySystemAnd` 在未初始化（`no == nil`）、模板未加载（`Tpls == nil`）或 emailer 为 nil 时记录日志并返回错误，绝不 panic。manager 的原因本地化 helper 在 `i18n == nil`（现有单测）时回退英文原文。

## 5. 数据与接口

### 5.1 新设置项（`models.Settings` + `settings` 表 + 设置页）

| key | 类型 | 默认值 | 校验 |
| --- | --- | --- | --- |
| `app.send_error_alert_enabled` | bool | `true` | — |
| `app.send_error_alert_window` | duration string | `5m` | 解析成功且 1m ≤ d ≤ 24h |
| `app.send_error_alert_threshold` | int | `50` | 1 ≤ v ≤ 1,000,000 |
| `app.send_error_alert_cooldown` | duration string | `30m` | 解析成功且 1m ≤ d ≤ 24h |

校验放在 `cmd/settings.go::UpdateSettings`（与 SMTP 投递设置同一段落），非法值返回 400。整包保存时先保留原始 JSON 的 key 存在性：4 个新 key 缺席则回填 `cur`（避免旧客户端/旧前端把告警静默关闭或清零），显式 `false` 仍生效。`UpdateSettingsByKey` 对 4 个 key 做同等校验；单 key 写入必须提供非空 duration，空值返回 400（无法回填当前值）。

### 5.2 新表与迁移（v6.74.0）

```sql
CREATE TABLE IF NOT EXISTS campaign_send_error_alerts (
    campaign_id   INTEGER PRIMARY KEY REFERENCES campaigns(id) ON DELETE CASCADE,
    last_alert_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

迁移同时 `INSERT ... ON CONFLICT (key) DO NOTHING` 写入 4 个设置默认值。`schema.sql` 同步新表与默认值；`cmd/upgrade.go` 注册 `v6.74.0`。

### 5.3 查询（`queries/campaigns.sql` + `models.Queries`）

```sql
-- name: get-running-campaign-send-error-window
SELECT c.id AS campaign_id, c.name, c.uuid, c.owner_user_id, c.organization_id,
       c.send_errors, w.window_errors, w.affected_recipients, w.last_error_at
FROM campaigns c
JOIN LATERAL (
    SELECT COUNT(*) AS window_errors,
           COUNT(DISTINCT e.recipient_type || ':' || e.recipient_id) AS affected_recipients,
           MAX(e.created_at) AS last_error_at
    FROM campaign_send_errors e
    WHERE e.campaign_id = c.id
      AND e.created_at >= NOW() - make_interval(secs => $1::DOUBLE PRECISION)
) w ON TRUE
WHERE c.status = 'running' AND w.window_errors >= $2
ORDER BY w.window_errors DESC;
```

```sql
-- name: claim-campaign-send-error-alert
INSERT INTO campaign_send_error_alerts(campaign_id, last_alert_at)
VALUES($1, NOW())
ON CONFLICT(campaign_id) DO UPDATE SET last_alert_at = NOW()
WHERE campaign_send_error_alerts.last_alert_at <= NOW() - make_interval(secs => $2::DOUBLE PRECISION)
RETURNING last_alert_at;
```

```sql
-- name: release-campaign-send-error-alert
DELETE FROM campaign_send_error_alerts WHERE campaign_id = $1 AND last_alert_at = $2;
```

`$1` 为窗口秒数，`$2` 为阈值或冷却秒数，全部由 SQL 时钟计算；claim 查询中 `$1 = campaign_id`、`$2 = cooldown 秒数`，`sql.ErrNoRows` 表示冷却未到（未抢占），不是错误。横向子查询按 `campaign_id` 走 `campaign_send_errors_campaign_idx`，运行中的活动数量很小。`campaign.owner_user_id/organization_id` 可为 NULL，模型字段必须用 `null.Int`。claim 返回 `last_alert_at` 供精确释放。

### 5.4 模型

`models/campaign_send_errors.go` 增加 `CampaignSendErrorWindow`：`CampaignID/Name/UUID/SendErrors/WindowErrors/AffectedRecipients/LastErrorAt`，`OwnerUserID/OrganizationID` 用 `null.Int`。

## 6. 代码改动清单

### 后端

- `internal/notifs/notifs.go`
  - 新增 `TplSendErrorAlert = "send-error-alert"`。
  - 新增 `SystemEmails() []string`（返回副本）。
  - 新增 `NotifySystemAnd(extra []string, subject, tplName string, data any, hdr textproto.MIMEHeader) error`：模板渲染一次；系统名单一封 + extra 中未在系统名单的地址逐个一封；去重（trim + 小写比较）；单地址失败记录日志并继续，返回错误仅当渲染失败或零成功。
  - `Notify`/`NotifySystemAnd` 增加未初始化/模板 nil/emailer nil 守卫，返回错误而非 panic。
- `internal/manager/manager.go`
  - `Config` 增加 `OwnerEmail func(int) (string, error)`；`fnNotify` 改为 `func(extra []string, subject string, data any) error`，默认实现解析 owner（`OwnerEmail != nil && OwnerUserID.Valid`）后调 `notifs.NotifySystemAnd`。同步更新 `internal/manager/manager_test.go:287/314`、`internal/manager/pause_test.go:33` 的注入签名。
  - `sendNotif` 改为 `sendNotif(c, status, reason string, extra map[string]any)`：通过 `m.fnNotify(ownerEmails, subject, data)` 发送，data 合并 extra（`ResumeAt`、`Actor` 等）。现有 3 个调用点（pipe.go:419/466/542）同步更新。
  - 新增 nil-safe `reasonText(key, fallback string, params ...string)`。
- `internal/manager/pipe.go`
  - `withErrors` 分支：原因改为 `reasonText("email.status.reasonTooManyErrors", "Too many errors")`。
  - `stopReasonPersonalSMTP` 分支：原因改为本地化。
  - `stopReasonDeferred` 分支与 `p.deferred` 分支：调用 `notifyDeferred()`；读取最新活动拿 `NextResumeAt`（`store == nil` 或失败时省略）；状态用 `deferred`，原因 `reasonDailyLimit`，`ResumeAt` 用 `NextResumeAt.Time.In(apptz.Location())` 按部署时区 `2006-01-02 15:04` 格式化为字符串。
  - 完成分支：`sendNotif(c, c.Status, "", nil)`。
- `cmd/init.go`
  - 注入 `OwnerEmail`：`core.GetUser` → 仅 enabled 且邮箱非空返回。
- `cmd/campaigns.go`
  - `UpdateCampaignStatus` 成功后，`paused`/`cancelled` 异步发送通知；新增 `App.notifyCampaignStatusChange(out models.Campaign, actor auth.User)`。
  - 原因：`email.status.reasonManualPause` / `reasonManualCancel`，参数 `{actor}`；data 增加 `Actor`。
- `cmd/main.go`
  - `App` 增加可注入字段 `notifyCampaign func(owner []string, subject, tplName string, data any) error`（生产默认 `notifs.NotifySystemAnd`，测试为零值即跳过）。
  - App 构造后赋值；现有测试 App 不赋值时手动通知自动跳过，不会触碰未初始化的 notifs。
- `cmd/send_error_alerts.go`（新）
  - `campaignSendErrorAlertSource` 接口（窗口计数 / 抢占 / 释放）与生产实现（包装 `models.Queries`）。
  - `runSendErrorAlerts(ctx, src, ownerEmail, notify, i18n, log, cfg)`：ticker 循环；逐活动先解析 owner + `notifs.SystemEmails()`，两者都空则跳过（不占冷却）；否则抢占冷却 → 发送 → 零成功时按 `campaign_id+last_alert_at` 释放。
  - `alertConfigFromKo`：读取并校验；非法时记录日志并禁用监控（不阻塞启动）。
  - ticker 间隔 `clamp(window/5, 15s, 1m)`；`ctx.Done()` 立即退出。
- `cmd/main.go`（监控启动）
  - `automationCtx` 在 `cmd/main.go:388` 才创建，监控必须在其之后启动（不能紧跟 `go mgr.Run()`）：非 passive 且 enabled 时 `go runSendErrorAlerts(automationCtx, ...)`，并用 `alertDone` channel 记录退出。
  - 关闭顺序：`stopAutomation()` 后等待 `alertDone`（最多 2s）再 `db.Close()`，避免监控线程与关库竞争，也不让同步 SMTP 无限拖延 SIGHUP。
- `cmd/settings.go`：`UpdateSettings` 新增 4 个设置项校验；`UpdateSettingsByKey` 对 4 个 key 做同样校验；空 duration 回填当前值。
- `models/settings.go`：新增 4 个字段。
- `models/queries.go`：新增 3 个查询字段。
- `queries/campaigns.sql`：新增 3 条查询。
- `internal/migrations/v6.74.0.go` + `v6.74.0_test.go`、`cmd/upgrade.go`、`schema.sql`。
- `static/email-templates/campaign-status.html`：增加可选 `ResumeAt`、`Actor` 行（`{{ if index . "ResumeAt" }}` 守卫）。
- `static/email-templates/send-error-alert.html`（新）：活动链接、窗口、窗口错误数、累计错误数、受影响收件人数、最后错误时间。
- `i18n/en.json`、`i18n/zh-CN.json`：新增 `email.status.reason*`、`email.status.campaignResumeAt`、`email.status.campaignActor`、`email.alerts.*`、`settings.performance.sendErrorAlert*`。

### 前端

- `frontend/src/views/settings/performance.vue`：在 `max_send_errors` 后新增告警分组：开关（`app.send_error_alert_enabled`）、阈值（number）、窗口（文本，`regDuration`）、冷却（文本，`regDuration`），关闭时禁用三个输入。
- `frontend/cypress/e2e/settings.cy.js`：新增保存/重载断言。

### 文档

- `docs/ARCHITECTURE.md`：通知路由与告警监控边界。
- `docs/harness/BUSINESS_LOGIC.md`：新增“活动通知与错误率告警”不变量。
- `docs/harness/STATUS.md`、`docs/harness/PLAN.md`：完成记录与来源。
- `docs/docs/content/configuration.md`：4 个设置项。
- `docs/swagger/collections.yaml`：settings schema 增加 4 个属性。

## 7. 事件流与邮件内容

| 事件 | 触发点 | 状态/主题 | 原因 | 附加字段 |
| --- | --- | --- | --- | --- |
| 自然完成 | `pipe.cleanup` | `finished` / `Finished: <name>` | 空 | — |
| 错误阈值自动暂停 | `pipe.cleanup`（withErrors） | `paused` / `Paused: <name>` | `Too many errors` | — |
| 个人 SMTP 不可用 | `pipe.cleanup` | `paused` | `Personal SMTP unavailable` | — |
| 手动暂停 | `cmd.UpdateCampaignStatus` | `paused` | `Paused manually by {actor}` | `Actor` |
| 手动取消 | `cmd.UpdateCampaignStatus` | `cancelled` | `Cancelled manually by {actor}` | `Actor` |
| 额度延期 | `pipe.cleanup`（deferred 两分支） | `deferred` | `Daily limit reached` | `ResumeAt` |
| 窗口错误告警 | `runSendErrorAlerts` | `send-error-alert` 模板 | — | 窗口、窗口错误数、累计错误、受影响人数、最后错误时间 |

所有主题沿用现有英文状态前缀（`Finished:`/`Paused:` 等）；告警主题用 i18n 模板 `email.alerts.subject`，zh-CN 输出中文。模板标签按部署语言翻译。

## 8. 边界与失败语义

1. owner 已删除/禁用/无邮箱：跳过 owner，只发系统管理员；两者都空则不发、不占冷却。
2. 通知发送失败：只记录日志，不影响活动状态、不重试（窗口告警例外：零成功释放冷却，下一轮重试；部分成功保留冷却）。
3. 多实例：只有非 passive 实例运行窗口监控；`passive` 实例不重复发送。生命周期通知由实际处理管道/请求的实例发送。
4. 活动完成与手动暂停竞争：状态以数据库最终值为准；手动暂停由 HTTP 层发送一次，cleanup 的 `stopReasonPause` 分支不重复发送。状态接口本身的合法转换由 Core 校验（`scheduled` 不能直接到 `paused`，实际存在的是 `deferred → paused`），通知只跟随成功后的 `out.Status`。
5. 额度延期重复/丢失：同一次运行内 `p.deferred` 保证最多一次；`withErrors` 优先于延期（见 D5），恢复后再次延期按新事件再发。
6. 窗口告警与累计阈值暂停可能同时发生：告警查询只取 `running`，暂停后不再告警；已发出的告警不回收。
7. 模板缺失/渲染失败：`notifs.Notify` 记录错误并返回；生命周期调用方忽略错误，活动流程不受影响。未初始化的 notifs 不得 panic（现有 cmd 测试依赖）。
8. 设置非法值：保存时拒绝；启动时若 koanf 值非法（例如手工改库），监控记录日志并禁用，不阻塞发送。`UpdateSettingsByKey` 同样校验。
9. 收件人隐私：管理员与 owner 分开发送，互不可见；同一收件人列表内保持现状。
10. 告警邮件不含收件人邮箱、错误原文或客户敏感信息，只含数量、时间和活动链接，避免二次泄露。
11. owner 用户被禁用/删除：解析为空，只发系统管理员；通知失败不影响活动操作。
12. 关闭顺序：监控随 `automationCtx` 取消，最多等待 2s（best-effort，受现有 3s 强退上限约束），随后才关闭 DB；发送前检查 `ctx`，已进入的同步 `Push` 由 SMTP 自身超时兜底，不宣称绝对不阻塞 SIGHUP。

## 9. 测试计划

### 单测

- `internal/notifs/notifs_test.go`：收件人合并/去重/小写 trim；系统名单为空；extra 与系统名单重叠；未初始化/模板 nil 时不 panic。
- `internal/manager/notification_test.go`：
  - 完成通知：owner + 系统（通过注入 `fnNotify`/notifier 捕获）；无 owner 时只发系统。
  - 错误阈值暂停、个人 SMTP 暂停原因与状态。
  - `stopReasonDeferred` 与 `p.deferred` 各发一次、`ResumeAt` 来自最新活动、读取失败仍发送。
  - `withErrors` 优先时只发 paused、不补发延期。
  - `sendNotif` extra 合并；`i18n == nil` 回退英文。
  - `deferCaptureStore` 补 `GetCampaign`；manager 测试的 `fnNotify` 按新签名 `func([]string, string, any) error` 注入。
- `cmd/send_error_alerts_test.go`（fake source）：
  - 达到阈值才抢占/发送；未达阈值不发。
  - 冷却未到（`sql.ErrNoRows`）不重复发；零成功释放并可重试；部分成功保留 claim。
  - 无收件人不抢占；owner 与系统名单去重。
  - `ctx` 取消立即退出。
- `cmd/campaign_notifications_test.go`：手动暂停/取消原因、actor 回退、owner 解析；未注入 notifier 时状态接口不 panic（沿用现有测试）。
- `internal/migrations/v6.74.0_test.go`：表/默认值存在，重复执行幂等；schema.sql 与迁移默认值一致。
- SQL 集成测试（现有 DB 测试基建可用时）：窗口计数、并发抢占只成功一次、release 只删除匹配 `last_alert_at` 的 claim。

### 前端与端到端

- `yarn lint`、`yarn build`。
- `settings.cy.js`：修改并保存 4 个值，重开设置页断言回显；关闭开关后输入禁用。
- 浏览器手工验证：配置 Mailhog 收件人，触发完成/手动暂停/延期/窗口告警，检查收件人、主题、模板字段与本地化。

### 回归

- `go test ./...`。
- 现有活动暂停/续发、每日上限、错误阈值相关测试。

## 10. 验收标准

1. 配置 `app.notify_emails` 后，活动完成/自动暂停邮件同时到达 owner 与系统管理员；owner 与系统管理员互相看不到对方地址。
2. 手动暂停、取消各收到一封邮件，原因含操作人；额度延期收到一封邮件并含恢复时间；非活动接口触发的批量暂停不发送（按非目标）。
3. 运行中活动在 5 分钟内失败 50 条（测试阈值）时，owner 与系统管理员各收到一封告警；30 分钟内不重复；暂停后不再告警；发送零成功时下一轮可重试。
4. 设置页可开关并调整窗口/阈值/冷却；非法值被拒绝（整包与单 key 两条路径）；保存后重启生效。
5. `go test ./...`、迁移测试、前端 lint/build、settings Cypress 通过；现有 cmd 活动状态测试在未初始化 notifs 时通过。
6. 本地 Docker 后端重启后 `http://localhost:9173` 返回 200，日志无迁移/启动错误。

## 11. 风险与回滚

- **邮件量增加**：完成/暂停/延期每次一封（owner 与管理员各一封）；窗口告警按活动冷却。必要时管理员可关闭窗口告警，或清空 `notify_emails`。
- **性能**：窗口查询按活动 LATERAL 计数，运行中活动数量小；默认每分钟最多一次全量采样。
- **迁移风险**：仅新增表和 settings 默认行，不改现有列；回滚可删除表并删除 4 个 key，旧代码忽略未知 settings 行。
- **误报**：阈值/窗口可调；告警不改变活动状态，误报成本低。

## 12. 实施结果（2026-11-13）

- 收件人：`app.notify_emails` + 活动 owner 分开发送；`internal/notifs/notifs.go::NotifySystemAnd` 渲染一次、去重、部分失败不阻断，未初始化/nil 模板返回错误不 panic。
- 生命周期：完成/错误阈值/个人 SMTP 不可用补 owner；手动暂停/取消在 `cmd/campaigns.go::UpdateCampaignStatus` 异步发送并带操作人；额度延期在 `pipe.notifyDeferred` 发送并带部署时区恢复时间；`withErrors` 优先于延期。
- 窗口告警：`cmd/send_error_alerts.go` 非 passive 实例运行，SQL 时钟窗口 + `campaign_send_error_alerts` 原子冷却，零成功精确释放，只告警不暂停；设置 `app.send_error_alert_{enabled,window,threshold,cooldown}`，默认 5m/50/30m，整包与单 key 均校验。
- 迁移：v6.74.0 新增告警表和 4 个设置默认值，重复执行幂等；dev 库已应用，`no upgrades to run`。
- 验证：`go test ./...`、`go vet ./...` 通过；模板渲染单测覆盖 alert 与 campaign-status 可选字段；事务回滚 SQL 验证窗口计数 55、首次 claim 1、冷却内 claim 0；隔离 Cypress `settings.cy.js` 3/3 通过（含新增用例）；前端 lint/生产构建通过；本地 dev backend 重启后 9173 返回 200，告警监控启动。

### 12.1 独立 QA 修复（2026-11-13）

- 修复 P1：每日额度延期已先落库为 `deferred` 后错误阈值才达标时，cleanup 的 running-only 暂停转换影响 0 行并直接 return，暂停和延期通知都不发；现按持久化终态补发延期通知（含恢复时间），`notifyDeferred` 在读取到非 deferred 终态时跳过，避免与手动暂停/取消邮件矛盾。D5 中“最终状态一定是 paused”的前提不成立，实际终态由持久化状态决定。同类修复：延期后 SMTP 不可用停止时也按 deferred 终态补发延期通知；延期审计统一在 `notifyDeferred` 内写入。
- 组织发件池 SMTP 不可用使用独立 stop reason、审计值 `pool_smtp_unavailable` 与 `email.status.reasonPoolSMTPUnavailable`，不再误报“个人 SMTP 不可用”；投递阶段的组织池/组织来源 SMTP 失效也透传 pool 错误并设置 pool stop reason。
- `NotifySystemAnd` 对每个额外收件人单独发送，失败日志同时脱敏收件人和 SMTP 错误中回显的地址；手动暂停/取消操作人第三级回退改为用户 ID，i18n 缺失时也替换 `{actor}`。
- `campaign-status` 模板 Reason 改用 `with` 守卫；告警窗口显示 `5m` 而非 `5m0s`；release claim 保持按 `last_alert_at` 精确匹配并补充注释，避免删除同微秒的新 claim。
- 修复既有 `TplForgotPassword` 与模板名不一致导致找回密码邮件无法渲染。
- 测试补齐：notifs 分发/去重/部分失败/零成功/模板名解析、manager 延期+错误竞态与池 SMTP 文案、cmd 手动暂停取消通知、告警参数与窗口格式、迁移默认值与 `schema.sql` 一致。
- 验证：`go test ./...`、`go vet ./...`、skill `pytest` 68 passed + 18 subtests、`yarn lint`（仅既有 CampaignGeoHeatmap 警告）、生产前端最新、`dev-backend-1` 重启后 9173 返回 200。

