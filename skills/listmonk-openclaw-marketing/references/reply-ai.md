# AI 回信分类：能力、边界与用法

本部署在标准邮件营销之上增加了 **AI 回信分类**。它是平台级能力，
营销侧（个人 API Key）只能读取结论，不能配置模型。

## 它解决什么问题

营销邮件发出去之后，客户会回信。人工逐封读信判断“这是退订还是投诉”很慢，
而且漏掉一封明确的退订就会继续打扰客户。AI 分类把这项工作变成可审计的记录：
每封被扫描到的回信都会得到 intent、confidence 与 reason_code，
并由后端按固定规则决定是否退订/拉黑。

## 处理链路

```text
已启用的回信邮箱（ai_enabled=true）
        │  非破坏 IMAP 扫描（UID 游标 + BODY.PEEK），只读新邮件
        ▼
reply_ai_events（去重、排队、租约、可重试）
        │  调用 OpenAI 兼容 /chat/completions
        ▼
intent + confidence + reason_code
        │
        ├─ unsubscribe 且置信度达标 → 退订 + 加入黑名单（action=blocklisted）
        ├─ complaint                → 同步记录为退信（bounces）
        └─ product_complaint / other → 仅审计，不修改客户
```

扫描使用 `BODY.PEEK`，因此**不会把邮件标记为已读、不会删除、不会移动**；
正文只在待重试期间保留，任何 API 都不返回正文。

## 四类意图

| intent | 含义 | 自动动作 |
| --- | --- | --- |
| `unsubscribe` | 明确要求停止发送 | 退订并加入黑名单 |
| `complaint` | 垃圾/滥用投诉 | 记录为退信，不自动退订 |
| `product_complaint` | 对产品或服务的抱怨 | 仅审计 |
| `other` | 其他内容 | 仅审计 |

低于 `min_confidence` 的判定按 unknown 处理，不执行任何动作。

## 事件状态

| status | 含义 |
| --- | --- |
| `pending` | 已入队，等待处理 |
| `processing` | 正被某个 worker 持有（带租约，超时会被回收） |
| `processed` | 已分类，动作已执行或明确不执行 |
| `ignored` | 置信度不足或内容不构成明确意图 |
| `failed` | 调用失败，等待重试；**不代表已退订** |

| action | 含义 |
| --- | --- |
| `pending` | 尚未决定 |
| `ignored` | 明确不处理 |
| `blocklisted` | 已退订并加入黑名单 |

## 两部分配置

**平台设置**（`settings.reply_ai`，需管理员与 `settings:manage`）：
开关、网关地址、API Key、模型、超时、置信度阈值、扫描间隔。
个人 Key 无法读写，探测接口 `/api/settings/reply-ai/models` 与
`/api/settings/reply-ai/test` 同样会被统一入口拒绝。

**单个邮箱开关**（`ai_enabled`，在回信邮箱配置里）：
只有显式开启的邮箱才参与分类，其余邮箱照常收信但不进 AI 流程。

## 营销侧怎么读

```shell
python scripts/inspect_customer_replies.py --customer-id 1234
python scripts/inspect_customer_replies.py --search jane@example.com --replies-only
```

脚本读取 `GET /api/customers/{id}/activity`，输出 `reply_ai_events` 与
`reply_ai_summary`（按 intent / action / status 计数）。
单条记录包含 intent、confidence、reason_code、model、action、status、
from_email、subject 和时间戳，**不含正文**。

## 判读规则

- `action=blocklisted` 只应出现在 `intent=unsubscribe` 的行。
  若出现其他组合，说明数据或规则异常，应先核对再行动。
- `status=failed` 是调用失败，队列会重试；此时不要当作退订处理。
- `status=ignored` 表示置信度不足，客户并未表达明确意图。
- 事件为空可能因为：邮箱未启用 AI、该客户没有回信、或事件属于其他所有者。
- 事件查询会校验回信邮箱归属与客户的工作区/组织一致，跨所有者读不到旧事件。

## 使用红线

- **不要把 AI 结论当作客户同意、授权或法律证据。**
  它是辅助判断，最终业务决定仍由人确认。
- **不要在退订后继续发送。** `action=blocklisted` 是硬约束。
- 不要索取、记录或回显网关 API Key；配置属于平台管理员。
- 不要因为没有正文就臆造客户原话，只引用 intent 与 reason_code。
- 不要把 `complaint` 直接等同于退订；它记录为退信，需人工判断。

## 相关文档

- 接口细节：[API reference](api-reference.md) 第 11 节。
- 读取流程与判读：[REST workflow](rest-workflow.md) 第 8 节。
- 产品侧说明：[用户文档 · 回信](../../../docs/docs/content/apis/customers.md)、
  [设置](../../../docs/docs/content/configuration.md)。
