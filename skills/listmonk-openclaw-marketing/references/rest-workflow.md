# REST 与 Python 营销操作流程

接口基线：2026-11-13，迁移 v6.73.0。先读 [API reference](api-reference.md) 的任务相关模块。
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
| --template-id | int | 仅 create_campaign.py 和完整流程；直接使用已有普通 campaign 模板，不克隆；visual 内容需 campaign_visual 模板 |
| --campaign-body / --content-type | string / 空正文、richtext | 或从蓝本继承；支持 html/markdown/plain/visual |
| --body-file | 文件路径 | 从 UTF-8 文件读取正文；与 --campaign-body 二选一 |
| --media-file | 可重复文件路径 | 先上传图片/附件再关联；可重复 |
| --media-id | 可重复 int | 关联已有媒体 ID；可重复 |
| --media-folder-id / --media-visibility | int / private、organization | 上传媒体的目标文件夹与可见性 |
| --file | 可重复文件路径 | upload_media.py：要上传的图片/附件 |
| --folder-id / --visibility | int / private、organization | upload_media.py：目标文件夹与可见性 |
| --messenger / --from-email | string / email、服务器配置 | from_email 兼容字段，最终发件人由 SMTP 决定 |
| --reply-mailbox-id | int / 不指定 | 本次活动可用的已配置回信邮箱 |
| --smtp-source | personal / organization / 不指定 | 不指定则 API 默认 personal；组织来源要求活动属于组织空间，改受众不会自动改来源 |
| --smtp-pool-id | 正整数 / 不指定 | 必须搭配 --smtp-source organization；属于 Key 绑定组织；省略时服务端取该组织最小 ID 的池 |
| --smtp-rate-limit | int / 不指定 | 每分钟活动合计封数，1–1000000；不指定用服务端来源默认值（personal=5，organization=200）；CLI 不接受 0，REST 的 0 表示取默认 |
| --daily-send-limit | 正整数 / 蓝本继承 | regular email CLI 必填；无蓝本时显式传入。REST 缺失时服务端回退 300 |
| --daily-resume-time | HH:MM / 09:00 或蓝本值 | 服务器本地时间，不按运行脚本电脑时区转换；额度耗尽后等到次日该时刻 |
| --visibility | private/organization/global / 不指定 | 本次活动可见性；不指定 API 默认 private |
| --auto-track-links / --no-auto-track-links | flag / 蓝本值或服务端 false | 显式开/关自动链接追踪 |
| --send-at | RFC 3339 / 空 | 未来时间；仅保存排期，不自动启动 |
| --tag | 可重复 string / 蓝本值或 [] | 活动标签，与列表 --customer_list-tags 不同 |
| --attribs-file | JSON object 路径 | 活动属性，非客户导入资料 |
| --auto-start | flag / false | 仅完整流程；明确要求立即发信/排期时启用 |
| --report-from / --report-to | string / 流程可省略 | 成对提供后取单活动报表；独立报表脚本必填 |
| --recipient-per-page | int / 100 | 收件人明细页大小 |
| --error-category | string / 空 | inspect_campaign_delivery.py：按失败分类筛选 |
| --filter-organization-id | int / 空 | inspect_campaign_delivery.py：公海活动按目标组织筛选 |
| --skip-errors / --skip-recipients | flag / false | inspect_campaign_delivery.py：跳过失败历史或失败名单 |
| --page / --per-page | int / 1、50 | inspect_campaign_delivery.py：失败历史与失败名单分页 |
| --customer-id / --search | int / string，二选一 | inspect_customer_replies.py：直接给 ID，或按邮箱/姓名/编号精确匹配 |
| --replies-only | flag / false | inspect_customer_replies.py：只输出 AI 回信分类结论 |
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
可选收件人明细需 campaigns:recipients。确认退订名单同样用 campaigns:recipients，
并按私域/公海追加 customers:read 或 customer_lists:read。
失败导出另需 customers:export（私域）/pools:export（公海）。
退信列表读取需 bounces:read；客户退信读取用 customers:read，用户另需
bounces:get（组织经理/平台管理员豁免）。AI 结果读取用 customers:read。

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
  --smtp-source organization --smtp-pool-id 5 --smtp-rate-limit 200 \
  --visibility organization --auto-track-links
```

从各步 JSON 取真实 ID 替换示例数字。组织来源示例要求 Key 已绑定目标组织，
池 5 为该组织可用的营销池；个人来源用 `--smtp-source personal` 并省略池 ID。
不指定 `--smtp-source` 时服务端用 personal，即使 Key 绑定组织空间也不会自动切换。
管理端新建的默认值不同：personal 每日上限 = 启用发件数×50，organization = 2000；
REST 省略每日上限则回退 300。CLI 对 regular email 强制显式传入，避免歧义。

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

### 3.1 带正文、图片/附件的草稿

正文可以直接写在 `--body-file` 指向的 UTF-8 HTML 中，图片用
`{{media:文件名}}` 占位；脚本会先上传 `--media-file`，再把占位符替换成
上传返回的 `/api/media/file/{id}/{filename}` URL。占位符使用本地文件名；
服务端会规范化空格等字符，但正文占位符仍按本地文件名匹配，`local_filename`
保留原文件名。

```html
<p>Hello {{ .Customer.Name }}</p>
<img src="{{media:hero.png}}" alt="hero">
<p><a href="{{media:catalog.pdf}}">产品目录</a></p>
```

```shell
python scripts/create_campaign.py \
  --customer_list-id 12 --template-id 4 \
  --campaign-name "Launch" --subject "Launch day" \
  --body-file ./email.html --content-type html \
  --media-file ./hero.png --media-file ./catalog.pdf \
  --daily-send-limit 300 --daily-resume-time "09:00" \
  --smtp-source personal
```

- 上传需要 scope `media:write` + 角色 media:manage；共享到组织另需 `assets:share`。
  带媒体创建活动还需要角色 media:get。请求体上限 32 MB（含 multipart 开销），
  单文件应低于约 31.9 MB；图片另有尺寸/像素上限。
- `--media-id` 可关联已存在的媒体；`--media-file` 与 `--media-id` 可混用、自动去重。
  占位符只匹配本次 `--media-file` 上传的文件；`--media-id` 不能用于 token 替换。
  空 token、未知 token、未闭合 token 会在上传前直接报错。
- 正文中已引用的媒体会自动补齐关联；`--media-file` 上传的未引用文件
  仍会进入 `media` 数组。
- 活动媒体在发送时转成收件人 `/email-media/` 链接，仅对 email/email-* 活动成立：
  图片保留正文位置远程加载；未引用的图片会追加为正文图片；
  未引用的非图片文件进入邮件末尾附件资料区；plain 模式追加“文件名 + URL”文本行。
  **不是**传统 MIME 附件；需要真附件时使用事务邮件 `/api/tx`。
- 正文图片只接受上传媒体或绝对 HTTP(S) URL；`blob:`、`data:`、
  缺失/歧义媒体的正文会在保存或启动时被拒绝。markdown 的 `![](...)` 图片
  不在保存/启动扫描范围内，只有发送前才会拦截；需要保存时校验请用 html/richtext。
- 测试发送不会自动扫描请求正文里的新图片；正文引用新上传媒体时，
  必须同时把该媒体 ID 放进 `media` 数组。预览不保证加载受保护图片。
- 创建后默认草稿；预览用 `/preview`，测试发送用 `/test`。
- 上传发生在创建前；若后续创建失败，已上传媒体会保留。失败输出的 `progress`
  会带上已上传的 `media` / `media_ids`，可直接复用或删除。
- 模板应包含 `{{ template "content" . }}`；`--template-id` 只换包装器，
  不导入模板正文；`content_type=visual` 需要 campaign_visual 模板，
  `--body-file` 不支持 visual。
- 共享文件夹中的他人 private 媒体可能保存通过但发送不关联；
  上传到共享文件夹时显式使用 `--media-visibility organization`。
  模板自带的私有图片不要放进 `--media-id`。

也可以先单独上传再手工组装：

```shell
python scripts/upload_media.py --file ./hero.png
# 输出 media[].id 和 /api/media/file/... URL
```

## 4. 完整流程与内容蓝本

模板克隆模式，默认停在草稿：

```shell
python scripts/run_marketing_flow.py \
  --customer_list-name "OpenClaw Launch" --customers-file ./customers.json \
  --source-template-id 4 --new-template-name "Launch Template" \
  --campaign-name "Launch" --subject "Launch day" \
  --campaign-body "<p>Hello {{ .Customer.Name }}</p>" --content-type html \
  --daily-send-limit 300 --daily-resume-time "09:00" \
  --smtp-source personal --smtp-rate-limit 5 --auto-track-links
```

活动蓝本模式：

```shell
python scripts/run_marketing_flow.py \
  --customer_list-id 12 --customers-file ./customers.json \
  --source-campaign-id 31 --campaign-name "Launch follow-up" \
  --smtp-source organization --smtp-pool-id 5 --smtp-rate-limit 200
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
暂停用 paused；取消用 cancelled；撤回排期用 draft；暂停后也可直接排期（paused → scheduled）。
目标 running 时若活动已有未来 send_at，服务端会落库为 scheduled；
要立即发送先清空 send_at。成功响应 status 仍需后续读取确认实际发送进度，
不能把 running 当作投递完成。

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
  --data-urlencode "from=2026-10-05T00:00:00+08:00" \
  --data-urlencode "to=2026-10-05T15:00:00+08:00"
```

省略 `id` 即表示当前工作区的全部已授权活动；不要再依赖 `all=true`。

个人 Key 无 dashboard API，但有此报表。
当用户问“今天新建的活动”，读 campaigns 集合后筛选 created_at；
当问“今天发生的营销效果”，用报表事件时间，不过滤活动创建日期。
`all=true` 只是兼容参数，服务端不处理；真正生效的是省略 `id`。
跨活动 recipients 用“可管理”的活动集合，其他报表用“可分析”的活动集合。

summary/timeseries/links 始终请求，geo 可选。
recipients 返回 403 时脚本仍成功输出聚合报告，
并附 `recipients_unavailable` 错误原因；其他错误使脚本失败。
CLI 输出对象为 campaign_id/campaign_ids/all_campaigns 加 reports；
null 比率/唯一人数保持 null。

### 6.1 确认退订与点击退订

活动列表、详情和 `running/stats` 的 `unsubscribes`、`unsubscribe_clicks`、
`unsubscribe_activity` 是三种不同口径：确认退订、点击过退订链接、两者并集。
明细没有 CLI 封装，直接请求：

```shell
curl -G -H "Authorization: Bearer $LISTMONK_BEARER_TOKEN" \
  "$LISTMONK_BASE_URL/api/campaigns/41/unsubscribes" \
  --data-urlencode "status=all" \
  --data-urlencode "search=C001" \
  --data-urlencode "page=1" --data-urlencode "per_page=100"
```

- `status=confirmed` 默认；`visited` 含点击者（含已确认），`clicked` 仅未确认点击，
  `all` 全部。
- `search` 匹配客户编号/姓名；邮箱搜索需敏感权限。
- `click_count` 是点击次数，不是人数；同一人在 confirmed 和 clicked 中各出现一次。
- 管理员退订、退信处理、AI 拉黑不会写入活动退订统计；AI 退订需要读
  `/api/customers/{id}/activity` 或客户/列表状态来解释。
- 收件人删除后确认历史仍保留；删除活动才删除事件。

## 7. 发送失败排查与定向补发

活动暂停或出现发送错误后，按下面的顺序排查，不要一上来就重发。

### 7.1 先看概览

`GET /api/campaigns/{id}/delivery-overview`，scope `campaigns:analytics`：

```shell
curl -H "Authorization: Bearer $LISTMONK_BEARER_TOKEN" \
  "$LISTMONK_BASE_URL/api/campaigns/41/delivery-overview"
```

重点字段：`sent`、`unsent`、`waiting`、`failed`、`send_errors`、`progress`。
`source` 可能是 `private`、`pool` 或 `mixed`。公海活动还会给出 `organizations`，
可据此定位是哪个组织卡住。
`unsent` 是仍可发送的待处理量；重试耗尽的 `failed` 不再计入 `unsent`/`waiting`，
但仍是 `failed` 和失败名单的一部分。

### 7.2 再看失败原因

`GET /api/campaigns/{id}/send-errors`，scope `campaigns:recipients`；
用户侧还要 `campaigns:analytics`、`campaigns:recipients`、活动所有者/管理边界
和对应客户查看权限：

```shell
curl -G -H "Authorization: Bearer $LISTMONK_BEARER_TOKEN" \
  "$LISTMONK_BASE_URL/api/campaigns/41/send-errors" \
  --data-urlencode "category=smtp_rejected" \
  --data-urlencode "search=张" \
  --data-urlencode "page=1" --data-urlencode "per_page=50"
```

- `reasons` 是分类计数，用来回答“主要是哪类失败”。
- `search` 始终匹配客户编号/姓名；邮箱和错误原文搜索另需敏感权限。
  `per_page` 服务端收敛到 1–100。
- `historical_errors` 是旧版本只累计计数、没有明细的部分，
  不要从日志反推原因，也不要把它当作客户数。
- 缺少敏感权限时邮箱会被脱敏、错误原文可能为空，但分类与状态码仍可见。
  这属于正常的权限边界，不是接口异常。

### 7.3 看还能不能补发

`GET /api/campaigns/{id}/failed-recipients`：只返回仍未被发送、且至少失败一次的人。
响应中的 `can_retry` 反映“当前活动是否可补发”，它与 `can_export` 是两件事；
行内 `retryable` 才是单个收件人是否还能补发。
重试耗尽（`max_recipient_retries`，默认 3 次追加重试）的人仍会出现，
但 `retryable=false`，不能再补发。导出：
`GET /api/campaigns/{id}/failed-recipients/export`（需 customers:export/pools:export）。

### 7.4 定向补发

**只有活动处于 `paused`、且用户明确要求时**才执行：

```shell
curl -X POST -H "Authorization: Bearer $LISTMONK_BEARER_TOKEN" \
  -H "Content-Type: application/json" \
  "$LISTMONK_BASE_URL/api/campaigns/41/failed-recipients/retry" \
  -d '{"recipients":[{"type":"private","id":1234},{"type":"pool","id":98765}]}'
```

- 需要 `campaigns:send` **和** `campaigns:recipients` 两个 scope。
- `type` 必须是 `private` 或 `pool`，`id` 来自 `failed-recipients` 的 `recipient_id`。
- 一次 1–500 个唯一收件人；重复、过期、已发送、不可投递或越权 id 会使整个请求失败。
- 返回 `selected` 是本次重新排队的数量，`status` 变为 running 只代表已启动。
- 只发送选中的人；选中者耗尽后，若仍有未发送者回到 paused，否则 finished。
- 补发会让活动从暂停变为发送中；不要对同一活动并发调用。
  普通手动续发会清空选中集合、恢复常规受众。

### 7.5 补发后复核

再次读取 `delivery-overview` 与 `failed-recipients`。
已成功的收件人会从失败名单消失，`send_errors` 不会因此减少
（它是累计失败尝试次数）。如果活动再次因错误阈值暂停，重复 7.1–7.2 的排查。

重试轮次要点：

- `max_recipient_retries` 默认 3，表示首次投递后最多再试 3 次（0–1000）。
- 常规发送按最小失败轮次整轮推进；在途收件人阻挡更快的失败者抢跑。
- 配额 defer 和主动暂停/取消不消耗重试次数；渲染/投递失败才消耗。
- 成功投递消耗每日额度，失败尝试不消耗；重试耗尽者不会被调高设置自动复活。
- `app.max_send_errors` 默认 1000 是单次运行阈值，超过暂停；与累计 `send_errors` 不同。

> 重发前先确认失败原因是否已经解决（邮箱是否存在、SMTP 是否可用、
> 媒体依赖是否补齐）。对硬退信（邮箱不存在）重复补发通常没有意义，
> 应先清理名单或走黑名单流程。

### 7.6 失败恢复与验证

- 400：核对字段名、列表类型、必填编号、未来排期、状态转换、时间范围及是否已有导入。
- 401/403：核对凭据有效性、scope、角色动作权限、绑定工作区、组织资格、所有权及脱敏规则。
  补发另需活动处于 paused。
- 404：核对资源是否存在且在可读范围；不推断资源可跨工作区访问。
- 409：常见为资源/SMTP/组织状态冲突；先核对发送来源与可用营销池。
- 413：缩小导入或附件体积。
- 超时/5xx：先查询是否已创建、已入队或已变更状态，再决定重试。
  不自动重发、删除草稿或停止别人的导入。

验证命令与连通检查见文末。

### 7.7 用脚本一次看完

只读脚本把概览、失败历史与失败名单一次取回，适合先判断再决定是否补发：

```shell
python scripts/inspect_campaign_delivery.py --campaign-id 41 --per-page 50
python scripts/inspect_campaign_delivery.py --campaign-id 41 --error-category media --search 张
```

输出对象包含 `overview`、`send_errors`、`failed_recipients`。
缺 `campaigns:recipients` scope 时脚本仍成功，这两个字段改为
`send_errors_unavailable` / `failed_recipients_unavailable` 并附原因，概览照常可用。
`--search` 可传客户编号或姓名；`--page/--per-page` 服务端上限 100。
脚本没有任何写操作，**不会**触发补发。
需要完整失败明细/失败客户 Excel 时，直接请求
`/send-errors/export` 或 `/failed-recipients/export`；CLI 未封装。
## 8. 回信、退信与 AI 分类

AI 分类和 DSN 退信都由平台管理员配置（`settings/reply-ai`、回信邮箱开关，
个人 Key 不可调用），营销侧能做的是**读取结果并据此调整名单**。

### 8.1 读取 AI 分类结果

```shell
curl -H "Authorization: Bearer $LISTMONK_BEARER_TOKEN" \
  "$LISTMONK_BASE_URL/api/customers/1234/activity"
```

响应中的 `reply_ai_events` 每行给出 intent、confidence、reason_code、
model、action、status、from_email、subject、attempts、body_hash 与时间戳，
**不含正文**。

判读要点：

| 现象 | 结论 |
| --- | --- |
| `intent=unsubscribe` 且 `action=blocklisted` | 已退订所有列表并拉黑 |
| `intent=complaint` 且 `action=blocklisted` | 已拉黑并退订；另有一条 `source=reply_ai` 退信 |
| `intent=product_complaint` / `other` | 仅审计，未修改客户 |
| `status=failed` | 分类调用失败，稍后由队列重试；不代表已退订 |
| `status=ignored` | 终态：置信度不足、自动回复、无唯一投递来源或内容不明确 |

AI 的 unsubscribe/complaint **不会**增加活动 `unsubscribes`，也不写
`campaign_unsubscriptions`。回答“为什么这个客户退订了”时，引用 intent 与
reason_code，不要编造客户原文。数组为空可能因为邮箱未启用 AI、没有回信、
事件属于其他所有者，或该邮箱只开启了退信处理。

> `action=blocklisted` 是硬约束；不要在任何后续流程里重新订阅该客户。

### 8.2 读取回信邮箱退信

回信邮箱的 IMAP DSN 退信与 AI 分类共享连接，但数据独立：

- 只有 hard 且全局 `bounce.actions.hard` 为 blocklist 时才可能拉黑；
  soft 只记录。`source='reply_mailbox'`。
- AI complaint 写入 `source='reply_ai'`；传统 POP/Webhook 退信用服务商名称。
- 客户活动接口不含退信，必须读：

```shell
curl -G -H "Authorization: Bearer $LISTMONK_BEARER_TOKEN" \
  "$LISTMONK_BASE_URL/api/bounces" \
  --data-urlencode "source=reply_mailbox" \
  --data-urlencode "per_page=50"

curl -H "Authorization: Bearer $LISTMONK_BEARER_TOKEN" \
  "$LISTMONK_BASE_URL/api/customers/1234/bounces"
```

无敏感权限时邮箱、客户 UUID 和 `meta` 会被清空，但 type/source/时间仍可见。
DSN 明细（`reply_bounce_events`）和“最近退信处理历史”属于 `/api/profile/*`，
个人 Key 不可读；不要尝试。

### 8.3 平台级配置边界

模型网关、阈值、扫描间隔、退信动作、回信邮箱开关都在平台/资料/组织接口，
个人 Key 一律不可调用。不要用营销 Key 探测 `/api/settings/reply-ai/*`、
`/api/profile/reply-mailboxes*`、`/api/organizations/*`，也不要索取或记录密钥。

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
