# 全系统导出审查

日期：2026-10-09

审查范围是系统提供的文件导出、订阅者资料邮件附件和导入模板下载。普通查询 API 的 JSON 响应、素材原文件和用户上传的 CSV/XLSX 属于各自接口契约。导出契约见 [用户说明](../docs/content/exports.md)。

| 入口 | 原问题 | 字段与可读性调整 |
| --- | --- | --- |
| 私域客户与黑名单 | CSV 英文字段；属性挤在 JSON 列；内部时间字符串难读 | XLSX；客户编码、姓名、邮箱、客户状态和时间靠前；配置自定义字段独立成列，其余属性保留；UUID 放在末尾；无敏感信息权限时省略敏感列或保留授权的打码邮箱。 |
| 公海单列表 | CSV；客户状态与当前组织剔除状态容易混淆；缺少剔除原因 | 分开显示客户状态、当前范围分配状态及剔除原因；保留分配部门与回信地址；收件地址沿用脱敏。 |
| 公海汇总 | 内部公海 ID 排在业务字段之前 | 所属公海名称靠前、ID 放在末尾；同一客户属于两个公海时仍保留两条来源成员记录；勾选和筛选只收窄授权结果。 |
| 单客户资料 | JSON 对普通用户不便阅读；资料缺客户编码 | 补客户编码；拆为客户资料、列表订阅、邮件打开和链接点击工作表；打开/点击次数是数值；禁用的数据类别不导出；私有列表名称仍隐藏。 |
| 订阅者自助资料 | 邮件附件为 JSON、邮件文案指导用文本编辑器打开 | 邮件附相同结构的 XLSX，更新默认邮件说明；只发送到对应客户邮箱。 |
| 审计日志 | 大量编号和技术代码占据主要列，缺操作人/对象名称 | 时间、已翻译操作、操作人、对象、结果和原因靠前；优先使用事件发生时的姓名/用户名快照；保留原始代码、ID、请求 ID、元数据等调查字段。 |
| 活动发送失败明细 | 新增出口原为 CSV，未纳入统一格式 | XLSX；原因、阶段、SMTP 状态码、原始诊断、累计次数、首次/最后失败时间、来源与追溯 ID；原因汇总单独成表；保留历史缺明细次数；诊断继续脱敏。 |
| 用户导入模板 | XLSX 但表头固定英文，必填与值规则不直观 | 所选语言表头；空数据页及单独填写说明；说明必填项、长度、角色名称/ID和状态值；明文密码只存在于用户自行填写的文件。 |
| 组织成员导入模板 | 固定英文表头，无填写说明 | 翻译账号/角色，单独解释已注册账号和 member/manager 值；角色留空默认成员；兼容旧英文及简繁中文表头。 |

共同规则：表头深色底白字、冻结首行、合理列宽、自动换行、隔行底色、有数据时建立可筛选 Excel 表。时间统一为真实 Excel 日期、表头明确 UTC；编号按文本保存前导零与长数字。输入文本不会转成公式。工作表超过 1,048,576 行时续表；单元格超出 32,767 字符时明确报错，避免静默截断。权限、所有权、组织分配和 API Key 约束由原处理器/Core 保留。

语言优先级：请求 `lang` → `X-Listmonk-Language` → 应用语言，缺失翻译沿用英语回退。英语、简体中文和繁体中文新增字段译文完整；其它语言复用已有翻译并对新增项回退英语。客户/组织名称、原始错误等业务内容保持原值；自定义字段标签遵循管理员配置，不能自动推断它们的译名。

实现出处：`cmd/excel_{exports,customer_exports,import_templates}.go`、`cmd/{customers,pools,audit,public,campaign_send_errors}.go`、`queries/customers.sql::export-customer-data`、`internal/core/workspace_queries.go::GetWorkspaceCustomerProfileForExport`、`frontend/src/utils/importTemplateHeaders.js`、导出按钮所在页面。

验证出处：`cmd/excel_exports{,_integration}_test.go`、`cmd/audit_integration_test.go::TestAuditExportSelectedAndFilteredFullExcelPostgresIntegration`、`cmd/campaign_send_error_report_test.go`、`frontend/cypress/e2e/excel-exports.cy.js`。实际验证结果在 [STATUS.md](STATUS.md) 记录。
