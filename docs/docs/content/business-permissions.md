# 业务权限（含 v6.61.0 跨组织分配）

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
| | 跨组织管理公海分配 | `pools:allocation_platform_manage` |
| | 维护公海主数据 | `pools:master_manage` |
| | 编辑公海客户全部字段 | `pools:contacts_edit` |
| | 仅编辑公海客户名称 | `pools:contacts_name_edit` |
| | 管理公海投放授权 | `pools:delivery_manage` |
| | 导出公海客户数据 | `pools:export` |
| 营销活动 | 查看营销活动与统计 | `campaigns:get`、`campaigns:get_all`、`campaigns:get_analytics`、`campaigns:recipients` |
| | 查看私域发送错误详情 | `campaigns:private_send_error_details` |
| | 维护营销活动 | `campaigns:manage`、`campaigns:manage_all`、`campaigns:test` |
| | 发送营销邮件 | `campaigns:send`、`campaigns:schedule` |
| | 管理营销活动状态 | `campaigns:control` |
| | 发送跨组织公海邮件 | `campaigns:public_pool_send` |
| 模板与素材 | 查看和使用模板与素材 | `templates:get`、`media:get` |
| | 维护模板与素材 | `templates:manage`、`media:manage` |
| | 管理模板与素材共享 | `assets:share` |
| 发件服务 | 查看发件服务 | `delivery_provider:get` |
| | 拉黑失败地址 | `delivery_provider:blocklist` |
| 发信与回信邮箱 | 使用发信与回信邮箱 | `mailboxes:use` |
| | 配置发信与回信邮箱 | `mailboxes:manage` |

“拉黑失败地址”（`delivery_provider:blocklist`）只控制失败地址行的黑名单动作，不扩大任何客户或公海范围：每个私域目标仍需 `customers:blocklist` 且可维护，每个公海目标仍需 `pools:master_manage`；缺少对应权限的目标会跳过并显示原因。默认不向旧角色授予。

“查看发件服务”是独立的平台级只读权限，允许账号资料、安全配置状态、发送统计、失败邮箱、投递日志和 Excel 导出；不受当前组织工作区限制。菜单与直接路由、后端接口同步校验，无权限返回 403。该权限默认不勾选、不向旧角色自动授予，普通系统设置/邮箱/组织管理权限不隐含授权；账号保存与连接测试仍仅限最高管理员。个人 API Key 的路径 allowlist 未开放发件服务，不因角色授权扩大。

功能权限与数据范围分别检查：所有权、当前工作区、组织成员、转移/归档状态、列表角色和 API Key scopes 仍须满足。最高管理员保留功能权限豁免，归档限制仍生效。列表角色仍可限定具体私域列表，原逐项权限 ID 保持 API 兼容。

查看、导出和敏感信息分别授权。普通所有者和组织管理员也需要明确的查看、导出和统计权限。没有 `customers:sensitive_read` 时，邮箱按当前列表设置打码或置空，UUID 与属性隐藏；Excel 客户导出、单客户资料、活动收件人报告和退信详情也执行敏感字段限制，导出表格省略不可见的敏感列。活动私域发送错误的具体诊断另由 `campaigns:private_send_error_details` 控制；不给该权限仍可看错误分类、状态码和计数，但不显示、搜索或导出原始诊断。该权限不解锁邮箱地址或公海诊断。列表成员关系保留供维护使用；编辑脱敏记录时，界面不提交不可见的邮箱和属性，服务端保留省略的原字段。删除列表仍需列表管理范围，导出仍需客户查看范围；发送需活动维护与发送权限，使用模板与素材需对应查看权限。

`pools:manage` 管理当前组织的分配关系，不授予主数据维护或投放授权修改。创建本组织分配前必须已有公海投放授权；跨组织管理使用独立 `pools:allocation_platform_manage`，不通过投放权限推导。跨组织管理者可预建未授权分配，创建、停用、移除、恢复和解除都不会隐含授予或撤销投放授权。一级公海列表、导入、联系人创建/删除和无效邮箱归档由 `pools:master_manage` 控制，只为平台一级公海扩大范围，不能扩大私域、模板或邮箱访问。`pools:contacts_edit` 仅允许编辑公海客户编码、名称、邮箱、回信邮箱和分配部门；`pools:contacts_name_edit` 仅允许编辑名称，服务端会忽略其他字段。主数据管理员兼容地拥有全部编辑能力，但新编辑权限不会授予创建、删除、导入或归档。公海联系人浏览、导出仍分别需要 `pools:get`、`pools:export`；除最高管理员之外，委派维护者也只获得脱敏联系人 DTO。

全局模板也需要查看和维护权限。设置/取消模板或素材共享需 `assets:share`，同时保留维护和所有权检查；未改变共享范围可以正常保存内容。上传到共享目录、移动媒体或目录涉及共享放置变化时也检查共享权限。无共享权限时新目录默认私有。共享模板的私有素材关联可扩大该素材的受众范围，但不能绕过 `media:get`。

`mailboxes:use` 允许选用当前工作区可用的发信/回信邮箱，发送测试和正式邮件；还需对应活动发送、定时或测试权限，跨组织发送还需独立公海发送权限。`mailboxes:manage` 允许配置、测试、启停和删除邮箱、SMTP 池及管理转发规则，同时保留所有者/组织管理边界。使用权限不显示连接凭据，配置权限不授予活动发送或邮箱选用。没有使用权限仍可编写草稿并保留原邮箱选择。

升级 `v6.56.0` 幂等补齐旧角色的列表删除、已有模板/素材维护者的共享、旧邮箱自助使用/配置及已有组织管理员的分配能力。不向普通角色自动授予公海主数据或投放授权权限。升级后可通过角色页面撤销任一权限；私域敏感信息和组织管理员的业务查看/导出必须按新规则显式授权。

来源：`permissions.json`、`cmd/business_permissions.go`、`cmd/workspace_permissions.go`、`internal/core/workspace*.go`、`internal/core/pools.go`、`frontend/src/utils/businessPermissions.js`、`frontend/src/views/RoleForm.vue`、`internal/migrations/v6.56.0.go`。

## 跨组织分配工作台与升级（v6.61.0）

- 最高管理员沿用跨组织范围；委派人员需显式“跨组织管理公海分配”，升级不自动授予。只有旧分配权限与投放授权权限的组合不再允许跨组织创建，请按业务需要单独授权。
- 分配工作台只管理绑定、名称、备注、生命周期与成员关系，不编辑、导入或永久删除主数据。来源和组织只读；普通列表编辑/归档/删除入口拒绝组织分配。
- 无公海查看权限仍可管理最小元数据，但不能浏览成员/候选、查看客户数量或导出；有查看权限的委派人员仍获得脱敏行，候选限匹配目标组织部门。工作台的成员响应也不会因最高管理员身份直接展示明文邮箱。
- 投放状态独立展示；只有投放授权权限才显示授权/撤销动作，只有审计权限才显示目标分配操作记录。跨组织分配能力不授予全平台审计、组织成员身份、邮箱配置或活动发送能力。
- 解除要求原因、精确组织名与无未结束活动引用；逻辑解除保留历史，恢复绑定默认停用，启用仍不自动开通投放。黑名单、主数据和其他组织成员关系保持不变。
- 新管理接口仍受认证方式与 scope 约束。个人 API Key 的 `/api/pools/*` 前缀尚未开放，不能凭新角色权限调用；不要把管理 UI 能力描述成个人 Key API 能力。
