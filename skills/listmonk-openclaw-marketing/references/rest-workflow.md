# REST 与 Python 营销操作流程

先读 [API reference](api-reference.md) 的任务相关模块。
以下 CLI 从技能目录运行；HTTP 示例为 Bash，认证来自环境变量，不把密钥硬编码在命令中。
Python 3.10+；普通 HTTP 仅使用标准库，Excel 本地解析需要 openpyxl。

## 1. 配置和参数选择

在技能目录根据 .env.example 配置本地 .env：

```dotenv
LISTMONK_BASE_URL=https://listmonk.example.com
LISTMONK_BEARER_TOKEN=lmpk_replace_in_local_secret_store
```

个人 Key 省略 organization_id；legacy token 可传 `--organization-id`。
本技能不调用个人 Key 的自管、SMTP 配置或组织管理接口。

| 参数 | 类型 / 默认 | 用途 |
| --- | --- | --- |
| --base-url / --bearer-token | string / 环境或 .env | 站点根地址与 Key |
| --organization-id | int / 无 | 仅传统令牌选择空间 |
| --customer_list-id / --customer_list-name | int / string，二选一 | 流程/ensure_list 的目标；ID 复用，名称查找或创建；拼写包含下划线 |
| --customer_list-type | private / public，默认 private | 只在新建普通列表时生效 |
| --customer_list-optin / --customer_list-status | single / active | single/double 与 active/archived |
| --customer_list-tags / --customer_list-description | string / 空 | 逗号分隔标签与描述，仅创建使用 |
| --customers-file / --excel-file | 文件路径，二选一 | JSON 数组或 .xlsx |
| --preconfirm-subscriptions | flag / false | 直接创建预确认；批量路径设置 confirmed；省略为 unconfirmed |
| --source-template-id / --new-template-name | int / string | 完整流程的模板克隆模式，必须配对 |
| --template-subject | string / 空 | 克隆 tx 模板主题；不设置活动主题 |
| --source-campaign-id / --source-campaign-name | int / string，二选一 | 从活动内容蓝本创建新草稿；名称在搜索结果中精确匹配，优先用 ID |
| --campaign-name | string / 必填 | 新活动名称 |
| --subject | string / 蓝本继承 | 无蓝本时必填，即使模板克隆成功也需要 |
| --template-id | int | 仅 create_campaign.py；完整流程自动取克隆或蓝本模板 ID |
| --campaign-body / --content-type | string / 空正文、richtext | 或从蓝本继承；支持 html/markdown/plain/visual |
| --messenger / --from-email | string / email、服务器配置 | from_email 兼容字段，最终发件人由 SMTP 决定 |
| --reply-mailbox-id | int / 不指定 | 本次活动可用的已配置回信邮箱 |
| --smtp-source | personal / organization / 不指定 | 不指定则 API 默认 personal，不继承源活动发件来源 |
| --smtp-pool-id | 正整数 / 不指定 | 必须搭配 --smtp-source organization；属于 Key 绑定组织 |
| --smtp-rate-limit | int / 不指定 | 每分钟活动合计封数，1–1000000；不指定用服务端来源默认值 |
| --daily-send-limit | 正整数 / 蓝本继承 | regular email CLI 必填；无蓝本时显式传入 |
| --daily-resume-time | HH:MM / 09:00 或蓝本值 | 服务器本地时间，不按运行脚本电脑时区转换 |
| --visibility | private/organization/global / 不指定 | 本次活动可见性；不指定 API 默认 private |
| --auto-track-links / --no-auto-track-links | flag / 蓝本值或服务端 false | 显式开/关自动链接追踪 |
| --send-at | RFC 3339 / 空 | 未来时间；仅保存排期，不自动启动 |
| --tag | 可重复 string / 蓝本值或 [] | 活动标签，与列表 --customer_list-tags 不同 |
| --attribs-file | JSON object 路径 | 活动属性，非客户导入资料 |
| --auto-start | flag / false | 仅完整流程；明确要求立即发信/排期时启用 |
| --report-from / --report-to | string / 流程可省略 | 成对提供后取单活动报表；独立报表脚本必填 |
| --recipient-per-page | int / 100 | 收件人明细页大小 |
| --verbose | flag / false | 进度写 stderr |

Excel 参数：

| 参数 | 类型 / 默认 | 用途 |
| --- | --- | --- |
| --excel-sheet | 名称或从 1 起的序号 / 首表 | 工作表 |
| --email-column / --customer-code-column | 名称或字母 / 必填 | 例“邮箱”/A、“客户编号”/C |
| --name-column | 名称或字母 / 无 | 姓名 |
| --header-row / --start-row | int / 1、表头下一行 | 表头与数据起始行 |
| --skip-empty-rows | flag / false | 空行作为跳过项处理 |
| --dedupe-by-email / --no-dedupe-by-email | flag / true | 仅 Excel 在请求前按邮箱去重，JSON 不做此本地去重 |

额外非空 Excel 列转为客户 attribs；含属性时整批使用逐行客户 API。
客户 JSON 每行支持 email、customer_code、可选 name/attribs/status/customer_list_ids；
额外列表或非默认客户状态只能走符合该配置的直接 REST 维护流程，原生批量脚本不接受。

## 2. 执行前核对

确定目标列表及内容模式，再确认发送安排和 Key scopes。
完整流程至少需要 customer_lists:read；新建列表另需 write。
原生导入需要 customers:import；直接客户路径需要 customers:write，
已有客户查询复用还需 customers:read。模板克隆需 templates:write；
读取源活动需 campaigns:read；创建草稿需 campaigns:write。
发信/排期另需 campaigns:send；报表需 campaigns:analytics，
可选收件人明细需 campaigns:recipients。

源活动与源模板模式选其一；不要同时设置。源活动可能有同名项，
使用已核对的 ID 避免复用错误蓝本。
先校验客户文件必填列、subject、每日上限、SMTP 来源/池、未来 send_at 和报表起止值，
然后执行写入。写接口成功不代表整个流程已成功。

## 3. 分步流程：先创建草稿

1. 读 `GET /api/customer-lists?query=...&per_page=all`，
   精确匹配 name 或用 `GET /api/customer-lists/{id}`。
2. 确定没有合适列表后创建普通列表，记录 customer_list_id。
3. 导入客户，等待 finished 并查看日志；明确处理跳过或失败行。
4. `POST /api/templates/{id}/clone` 克隆模板，记录 template_id，
   或 `GET /api/campaigns/{id}` 读取内容蓝本。
5. `POST /api/campaigns` 创建草稿，记录 campaign_id。
6. 核对详情、预览、SMTP 概览；要求测试发送时再调用 test。

对应模块 CLI：

```shell
python scripts/ensure_list.py --customer_list-name "OpenClaw Launch"
python scripts/import_subscribers.py --customer_list-id 12 --customers-file ./customers.json
python scripts/clone_template.py --source-template-id 4 --new-template-name "Launch Template"
python scripts/create_campaign.py \
  --customer_list-id 12 --template-id 14 \
  --campaign-name "Launch" --subject "Launch day" \
  --campaign-body "<p>Hello {{ .Customer.Name }}</p>" --content-type html \
  --daily-send-limit 300 --daily-resume-time "09:00" \
  --smtp-source organization --smtp-pool-id 5 --smtp-rate-limit 100 \
  --visibility organization --auto-track-links
```

从各步 JSON 取真实 ID 替换示例数字。组织来源示例要求 Key 已绑定目标组织，
池 5 为该组织可用的营销池；个人来源用 `--smtp-source personal` 并省略池 ID。

JSON 客户示例：

```json
[
  {"email":"jane@example.com","customer_code":"C001","name":"Jane"},
  {"email":"alex@example.com","customer_code":"C002","name":"Alex"}
]
```

这些行走原生批量导入。加入非空 attribs 会改用直接客户 API；
直接路径遇到已有邮箱时查回客户并维护订阅，不覆盖旧 attribs。
导入成功返回 imported_count/skipped_rows/failed_rows，批量路径还有 import_status/import_logs。
imported_count 包含复用/维护的数量，不能解释为新增客户数量。

直接 multipart 原生请求：

```shell
curl -X POST -H "Authorization: Bearer $LISTMONK_BEARER_TOKEN" \
  "$LISTMONK_BASE_URL/api/import/customers" \
  -F 'params={"mode":"subscribe","subscription_status":"unconfirmed","customer_list_ids":[12],"overwrite_userinfo":false,"overwrite_subscription_status":true,"field_map":{"email":"A","name":"B","customer_code":"C"}}' \
  -F "file=@./customers.csv"
```

CSV 为 email/name/customer_code 三列，无表头。
随后 GET 相同路径取 status；GET `/api/import/customers/logs` 取纯文本诊断。
订阅状态按用户任务选择，不能因为示例改为 confirmed。

## 4. 完整流程与内容蓝本

模板克隆模式，默认停在草稿：

```shell
python scripts/run_marketing_flow.py \
  --customer_list-name "OpenClaw Launch" --customers-file ./customers.json \
  --source-template-id 4 --new-template-name "Launch Template" \
  --campaign-name "Launch" --subject "Launch day" \
  --campaign-body "<p>Hello {{ .Customer.Name }}</p>" --content-type html \
  --daily-send-limit 300 --daily-resume-time "09:00" \
  --smtp-source personal --smtp-rate-limit 20 --auto-track-links
```

活动蓝本模式：

```shell
python scripts/run_marketing_flow.py \
  --customer_list-id 12 --customers-file ./customers.json \
  --source-campaign-id 31 --campaign-name "Launch follow-up" \
  --smtp-source organization --smtp-pool-id 5 --smtp-rate-limit 100
```

蓝本继承主题、正文、visual body_source、纯文本、格式、模板、messenger、
每日限额、标签、attribs、headers、链接追踪、称呼兜底和归档配置。
显式 CLI 参数优先；不继承资源 ID、受众、send_at、status、
smtp_source/smtp_pool_id/smtp_rate_limit、reply_mailbox_id、visibility 或 pool_scope。
蓝本如开启 archive，会继续继承；不需要继承归档/附件等完整复制语义时，
先用分步 REST 创建所需草稿。附件关系的完整复制使用服务端 clone，并核对目标受众。

完整流程 JSON 包含 customer_list_id、template_id、campaign_id、status、
import_source、imported_count、skipped_rows、failed_rows 及资源详情，
蓝本模式另含 source_campaign_id。
报告时间都省略时不取报告；提供一半会报错。
脚本并非事务：后续出错时前面已创建的资源仍保留。分步执行便于记录 ID 和恢复。

## 5. 立即发送、排期、暂停

仅在任务已明确要求发送时使用；“建一个活动”默认草稿。

```shell
python scripts/update_campaign_status.py --campaign-id 41 --status running
```

排期先把未来时间写入活动。完整流程可传未来的 `--send-at` 和 `--auto-start`；
send_at 非空时 auto-start 选择 scheduled，否则选择 running。

```shell
curl -X PUT -H "Authorization: Bearer $LISTMONK_BEARER_TOKEN" \
  -H "Content-Type: application/json" \
  "$LISTMONK_BASE_URL/api/campaigns/41/status" -d '{"status":"scheduled"}'
```

单独 update_campaign_status.py 的 scheduled 不设置 send_at；
先用活动更新接口保存时间，保持已有内容和受众配置。
暂停用 paused；取消用 cancelled；撤回排期用 draft。
成功响应 status 仍需后续读取确认实际发送进度，不能把 running 当作投递完成。

## 6. 单活动、跨活动与今日报表

```shell
python scripts/fetch_campaign_reports.py \
  --campaign-id 41 \
  --report-from "2026-10-05T00:00:00+08:00" \
  --report-to "2026-10-05T15:00:00+08:00" --include-geo
```

指定多个活动用 `--campaign-ids 31 41`；与 `--campaign-id`、
`--all-campaigns` 三选一。读取当前工作区所有有权限活动：

```shell
python scripts/fetch_campaign_reports.py \
  --all-campaigns \
  --report-from "2026-10-05T00:00:00+08:00" \
  --report-to "2026-10-05T15:00:00+08:00" \
  --include-geo --recipient-page 1 --recipient-per-page 100
```

示例时间需替换为目标日期和当前/目标截止时刻。
--include-geo 默认 false；--recipient-page 默认 1，每次只返回该页，
有更多数据时继续 2、3……。独立脚本支持分页，完整流程仅取单活动第 1 页。
需要 opened/clicked/bounced 等筛选时直接使用 REST，CLI 未封装这些筛选。

直接跨活动今日摘要（用 --data-urlencode 保留时区中的 +）：

```shell
curl -G -H "Authorization: Bearer $LISTMONK_BEARER_TOKEN" \
  "$LISTMONK_BASE_URL/api/campaigns/report/summary" \
  --data-urlencode "all=true" \
  --data-urlencode "from=2026-10-05T00:00:00+08:00" \
  --data-urlencode "to=2026-10-05T15:00:00+08:00"
```

个人 Key 无 dashboard API，但有此报表。
当用户问“今天新建的活动”，读 campaigns 集合后筛选 created_at；
当问“今天发生的营销效果”，用报表事件时间，不过滤活动创建日期。

summary/timeseries/links 始终请求，geo 可选。
recipients 返回 403 时脚本仍成功输出聚合报告，
并附 `recipients_unavailable` 错误原因；其他错误使脚本失败。
CLI 输出对象为 campaign_id/campaign_ids/all_campaigns 加 reports；
null 比率/唯一人数保持 null。

## 7. 失败恢复与验证

- 400：核对字段名、列表类型、必填编号、未来排期、状态转换、时间范围及是否已有导入。
- 401/403：核对凭据有效性、scope、角色动作权限、绑定工作区、组织资格、所有权及脱敏规则。
- 404：核对资源是否存在且在可读范围；不推断资源可跨工作区访问。
- 409：常见为资源/SMTP/组织状态冲突；先核对发送来源与可用营销池。
- 413：缩小导入或附件体积。
- 超时/5xx：先查询是否已创建、已入队或已变更状态，再决定重试。
  不自动重发、删除草稿或停止别人的导入。

技能代码验证（在仓库根目录）：

```shell
python -m pytest skills/listmonk-openclaw-marketing/tests -q
python scripts/check_docs.py
```

接口连通检查使用所需 scope 的只读业务接口；需要现场验证写入时仅在明确的测试
工作区创建草稿并记录 ID。验证步骤默认不发邮件，不从后端日志提取真实令牌。
