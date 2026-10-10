# 业务权限（v6.56.0）

权限分配页面按业务操作分组，不预设岗位或角色。一项显示权限可对应多个功能权限，保存时仍使用原权限 ID。旧角色只拥有其中一部分时显示“部分已授权”；打开、保存不会补齐授权，主动勾选整组才授予全部。

| 业务模块 | 显示权限名称 | 功能权限 ID |
| --- | --- | --- |
| 私域客户与列表 | 查看私域客户与列表 | `customers:get`、`customers:get_all`、`customer_lists:get_all` |
| | 维护私域客户与列表 | `customers:manage`、`customers:import`、`customers:blocklist`、`customers:membership_manage`、`customer_lists:manage_all` |
| | 删除私域客户与列表 | `customers:delete`、`customer_lists:delete` |
| | 导出私域客户数据 | `customers:export` |
| | 查看客户敏感信息 | `customers:sensitive_read` |
| 公海客户与列表 | 查看公海客户与列表 | `pools:get` |
| | 管理组织公海分配 | `pools:manage` |
| | 维护公海主数据 | `pools:master_manage` |
| | 管理公海投放授权 | `pools:delivery_manage` |
| | 导出公海客户数据 | `pools:export` |
| 营销活动 | 查看营销活动与统计 | `campaigns:get`、`campaigns:get_all`、`campaigns:get_analytics`、`campaigns:recipients` |
| | 维护营销活动 | `campaigns:manage`、`campaigns:manage_all`、`campaigns:test` |
| | 发送营销邮件 | `campaigns:send`、`campaigns:schedule` |
| | 管理营销活动状态 | `campaigns:control` |
| | 发送跨组织公海邮件 | `campaigns:public_pool_send` |
| 模板与素材 | 查看和使用模板与素材 | `templates:get`、`media:get` |
| | 维护模板与素材 | `templates:manage`、`media:manage` |
| | 管理模板与素材共享 | `assets:share` |
| 发信与回信邮箱 | 使用发信与回信邮箱 | `mailboxes:use` |
| | 配置发信与回信邮箱 | `mailboxes:manage` |

功能权限与数据范围分别检查：所有权、当前工作区、组织成员、转移/归档状态、列表角色和 API Key scopes 仍须满足。最高管理员保留功能权限豁免，归档限制仍生效。列表角色仍可限定具体私域列表，原逐项权限 ID 保持 API 兼容。

查看、导出和敏感信息分别授权。普通所有者和组织管理员也需要明确的查看、导出和统计权限。没有 `customers:sensitive_read` 时，邮箱按当前列表设置打码或置空，UUID 与属性隐藏；Excel 客户导出、单客户资料、活动收件人报告和退信详情也执行敏感字段限制，导出表格省略不可见的敏感列。列表成员关系保留供维护使用；编辑脱敏记录时，界面不提交不可见的邮箱和属性，服务端保留省略的原字段。删除列表仍需列表管理范围，导出仍需客户查看范围；发送需活动维护与发送权限，使用模板与素材需对应查看权限。

`pools:manage` 管理当前组织的分配关系，不授予主数据维护或投放授权修改。创建本组织分配前必须已有公海投放授权；跨组织创建还需 `pools:delivery_manage`。一级公海列表、导入、联系人创建/删除和无效邮箱归档由 `pools:master_manage` 控制，只为平台一级公海扩大范围，不能扩大私域、模板或邮箱访问。公海联系人浏览、导出仍分别需要 `pools:get`、`pools:export`；除最高管理员之外，委派维护者也只获得脱敏联系人 DTO。

全局模板也需要查看和维护权限。设置/取消模板或素材共享需 `assets:share`，同时保留维护和所有权检查；未改变共享范围可以正常保存内容。上传到共享目录、移动媒体或目录涉及共享放置变化时也检查共享权限。无共享权限时新目录默认私有。共享模板的私有素材关联可扩大该素材的受众范围，但不能绕过 `media:get`。

`mailboxes:use` 允许选用当前工作区可用的发信/回信邮箱，发送测试和正式邮件；还需对应活动发送、定时或测试权限，跨组织发送还需独立公海发送权限。`mailboxes:manage` 允许配置、测试、启停和删除邮箱、SMTP 池及管理转发规则，同时保留所有者/组织管理边界。使用权限不显示连接凭据，配置权限不授予活动发送或邮箱选用。没有使用权限仍可编写草稿并保留原邮箱选择。

升级 `v6.56.0` 幂等补齐旧角色的列表删除、已有模板/素材维护者的共享、旧邮箱自助使用/配置及已有组织管理员的分配能力。不向普通角色自动授予公海主数据或投放授权权限。升级后可通过角色页面撤销任一权限；私域敏感信息和组织管理员的业务查看/导出必须按新规则显式授权。

来源：`permissions.json`、`cmd/business_permissions.go`、`cmd/workspace_permissions.go`、`internal/core/workspace*.go`、`internal/core/pools.go`、`frontend/src/utils/businessPermissions.js`、`frontend/src/views/RoleForm.vue`、`internal/migrations/v6.56.0.go`。
