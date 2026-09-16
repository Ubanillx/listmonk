# 技术架构

## 权威入口

系统架构、目录职责、技术栈、权限控制算法、构建测试和部署脚本的唯一详细说明是 [`docs/ARCHITECTURE.md`](../ARCHITECTURE.md)。修改模块边界或运行命令时，先更新该文件，再在本台账记录状态或技术债。

## 模块地图

| 层 | 主要目录 | 责任 |
| --- | --- | --- |
| HTTP/API | `cmd/` | Echo 路由、认证接入、请求校验和响应。 |
| 领域与持久化 | `internal/core/`、`models/`、`queries/`、`schema.sql` | 业务操作、工作区授权、SQL 查询和事务写入。 |
| 后台能力 | `internal/manager/`、`messenger/`、`bounce/`、`replyai/`、`subimporter/` | 调度发送、SMTP/Postback、退信、AI 回信分类和批量导入。 |
| 直接导出 | `cmd/customers.go`、`cmd/audit.go`、`internal/core/` | 直接流式返回客户 CSV、客户资料 JSON 和审计 CSV，不创建导出任务或持久化文件。 |
| Web 客户端 | `frontend/`、`frontend/email-builder/` | Vue 2 管理端与 React/TypeScript 邮件编辑器。 |
| 运行与交付 | `dev/`、`deploy/`、`.github/`、`Jenkinsfile` | 本地 Compose、离线包、CI 和 systemd/GoReleaser 发布。 |

跨层变更必须同时检查 API、权限、迁移、测试和部署文档，不在此复制易过时的实现细节。

## 角色权限注册与动作边界（v6.36.0）

- `permissions.json` 是角色编辑器和服务端配置的权限注册表；`internal/auth/models.go` 保存处理器使用的稳定权限常量，`RoleForm.vue` 通过 i18n 将权限 ID 显示为中文/英文自然语言。
- `cmd/handlers.go` 的路由门和处理器内的资源门必须同时存在。`customers:*`、`campaigns:*`、`bounces:*` 等细分动作只收窄功能，不替代 `WorkspaceAccess`、Core ownership 或 API Key scope。
- `organizations:platform_manage` 只用于平台组织管理路由；对活跃组织的成员/邀请/回复转发等管理调用使用显式路径白名单，避免平台操作员被当成普通资源管理员。`users:tokens` 独立控制集成令牌 API。
- `internal/migrations/v6.36.0.go` 只为历史宽权限用户角色补齐新动作，迁移必须幂等；撤销动作时由角色数组直接生效。新增后台任务或下载接口必须在执行时重新读取权限和工作区状态。
- 直接客户导出由 `cmd/customers.go` 执行 `customers:export` 和工作区/所有权/脱敏检查；审计导出由 `cmd/audit.go` 执行 `audit:get` 和相同工作区边界。前端只负责发起下载，不能作为安全边界。

## Jenkins filesystem media persistence

`Jenkinsfile` 的 systemd 发布使用 `<DEPLOY_DIR>/releases/<release-id>` 保存
不可变版本，并将 `<DEPLOY_DIR>/uploads` 作为版本外的持久化 filesystem media
目录。每个新版本创建 `uploads` 符号链接，systemd 从稳定的 `<DEPLOY_DIR>` 运行，
因此数据库中默认的相对路径 `uploads` 不会随 `current` 切换而改变解析位置。
首次使用修复后的流水线时，部署脚本会在停机窗口内把旧版本的实际
`current/uploads` 内容以不覆盖方式迁移到持久目录；失败回滚只清理新版本和符号
链接，不清理持久媒体目录。

## 媒体逻辑文件夹（v6.37.0）

- `media_folders` 保存工作区目录树，`media.folder_id IS NULL` 为根目录；provider 文件名保持平铺，不执行物理搬移，因此 filesystem、S3 和历史媒体 URL 都兼容。
- `cmd/media_folders.go` 注册目录查询、创建、改名、移动、空目录删除和媒体归属移动接口；目录操作使用 `media:manage`，媒体移动额外经过 `RequireManageResource` 的所有者边界。
- `internal/core/media_folders.go` 负责名称校验、工作区过滤、目录计数、唯一名称、循环检测和带组织锁的事务写入；活动工作区中的平台管理员媒体/目录查询也按当前选择收敛，归档平台清理保留宽读取；`internal/migrations/v6.37.0.go` 与 `schema.sql` 保持安装/升级一致。
- `frontend/src/views/Media.vue` 以面包屑和当前层级网格展示目录，支持目录嵌套、媒体拖入目录、本地文件拖入目录上传，以及改名和空目录删除。`frontend/src/api/index.js` 只发送 snake_case 写入字段。

## 公海能力边界（已实施）

- 一级公海主数据、一级成员关系、组织二级分配、组织剔除记录、一级投放授权和二级回件邮箱属于独立的领域边界；不得通过复制 `customers` 或现有普通 `customer_lists` 行来实现。
- 列表/详情/API 层使用安全 DTO；真实邮箱只允许进入服务端活动受众解析、投递快照和回信处理。导出、浏览器响应、日志和审计字段不得携带明文邮箱给非最高管理员。
- 活动受众关系需要同时记录一级公海来源、二级组织分配和最终回件邮箱解析结果。发送 worker 以联系人内部 ID 去重，并在查询时过滤组织级剔除状态。
- 二级列表手动移除与回复剔除写入可审计的逻辑状态；一级公海列表查询按调用者显示组织维度标记，最高管理员跨组织可见，组织用户只可见本组织。
- 二级回件邮箱由组织二级列表配置，且只能由组织工作区管理员维护。一级公海活动不得回退到全局默认邮箱；缺少或存在冲突的有效二级邮箱时，活动应在发送前阻断并提示配置来源。回件邮箱属于公司内部地址，可在组织管理端明文展示，但必须与客户真实邮箱 DTO 和导出字段分离。
- 最高管理员分配公海时使用 `GET /api/pools/:id/management-target` 读取显式目标组织的现有二级列表状态，不在公海拆分弹窗代配回件邮箱。创建拆分请求显式携带目标组织信息，由服务端在单一事务中创建列表、授权和绑定；该端点不从当前工作区推导目标组织，平台管理员路径不检查该组织成员资格。
- 公海联系人新增统一通过 `POST /api/import/customers`：请求只允许一个一级 `pool` 列表，后端在首个 CSV/XLSX 工作表解析 `customer_code`、`name`、`email`、`allocation_department` 四个字段，额外模板列丢弃；`allocation_department` 必须匹配启用中的 `organizations.name`，未知或已归档组织的行记为无效并跳过写入；合法值才写入 `pool_contacts`，且不触发组织创建或二级绑定。该同步分支由 `poolImportMu` 串行化并在事务内按完整规范化记录幂等，编码相同但比较字段不同则写冲突审计。
- `pool` 导入、联系人维护及二级列表创建/绑定的写路由统一由最高管理员守卫；普通用户不能通过伪造前端请求绕过。公海管理组件只调用 `management-target`、`pool-segments` 和 `POST /api/pool-segments`，不再上传或维护联系人。历史 `import-members`/`pools/import` 路由仅为兼容保留，不作为产品入口。
- 数据库唯一约束保证每个 `(pool_id, organization_id)` 只有一个已绑定二级列表，因此同一组织内的公海联系人只有唯一归属和回件邮箱来源；若产品放开重叠归属，受众解析必须要求显式二级列表选择，不能静默猜测回件邮箱。一级公海活动可保存草稿但缺少有效归属/邮箱时不得预览或发送。回件邮箱作为公司内部地址明文保留和展示，不进入客户联系方式脱敏策略。

## 退信邮箱检测（2026-09-08）

- `cmd/bounce_mailbox.go`：未保存配置检测、按 UUID 复用已保存密码、统一响应包裹；路由沿用 `settings:manage`。
- `internal/bounce/mailbox/test.go`：30 秒只读 POP 探测与 STLS 连接适配；`preview.go`：递归 MIME / DSN 解析；`pop.go` 后台扫描复用 STLS 适配但保留原处理行为。
- `frontend/src/views/settings/bounces.vue`：三种连接模式、检测按钮、四步结果与邮件摘要。

## 回信 AI 网关探测（2026-09-10）

- `cmd/reply_ai_settings.go`：`POST /api/settings/reply-ai/models` 与 `POST /api/settings/reply-ai/test`，均为 `settings:manage`；接受未保存表单，空值/全掩码 `api_key` 复用已保存密钥，按网关状态码映射 400/502 并输出 i18n 文案，密钥不出站到响应或日志。
- `internal/replyai/gateway.go`：`/models` 发现（根地址未带 `/v1` 时回退该挂载并返回 `suggested_base_url`）、模型清单解析与排序（对话模型优先、非对话模型标记）、四步模型测试（配置 → 网关 → 模型 → 分类往返）与原因码；`client.go` 抽出 `apiRoot`/`chatEndpointURL`/`resolveTimeout`/`chat`/`decodeDecision` 供探测与生产分类共用。
- `frontend/src/views/settings/inbound-replies.vue`：三步式设置流（地址与密钥 → 获取模型列表 → 选择模型 → 测试模型），探测状态不进入设置表单，模型用 `b-autocomplete` 从网关清单选择并允许手填未列出的 ID；意图文案统一走 `settings.inboundReplies.intent.{unsubscribe,complaint,product_complaint,other}`，下拉与结果面板共用 `intentLabel()`。
- 验证：`internal/replyai/gateway_test.go`、`cmd/reply_ai_settings_test.go`、`cmd/i18n_frontend_keys_test.go`（扫描前端 `$t()` 字面量与模板字符串前缀，防止缺失 key 直接渲染成原始字符串）、`dev/reply_ai_gateway_probe_verify.js`（27 项断言）、`frontend/cypress/e2e/reply-ai-settings.cy.js`（`REPLY_AI_E2E` 开关，`REPLY_AI_SESSION` 可复用已登录会话）。
