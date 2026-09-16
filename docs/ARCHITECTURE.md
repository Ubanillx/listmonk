# Listmonk 工程架构与运行指南

本文档描述当前仓库的实现边界、运行方式和安全约束，是贡献者理解代码的工程入口。用户使用说明位于 `docs/docs/content/`；当实现、命令、部署方式或权限规则变化时，必须在同一变更中同步更新本文及受影响的用户文档。

## 系统全景

```text
浏览器 / API 客户端
        │  HTTPS、会话 / Basic / Bearer 认证
        ▼
Vue 2 管理端 ──────── REST `/api/*` ───────► Go + Echo 服务 (`cmd/`)
  Vite 开发服务器                                │
                                                  ├─ Core：业务、工作区与事务锁
                                                  ├─ Manager：活动批次与 SMTP/Postback 发送
                                                  ├─ Importer、Cron、Bounce（Webhook / POP3）
                                                  ▼
                                           PostgreSQL（数据、会话、迁移）
                                                  │
                                   静态资源、邮件模板、i18n、文件/S3 媒体
```

生产构建把 Go 二进制、`frontend/dist`、SQL、静态文件和语言包用 `stuffbin` 打包为单一 `listmonk` 可执行文件。开发模式则让 Go 服务从磁盘读取 `frontend/dist`，并可由 Vite 独立提供前端热更新。

## 目录与职责

| 路径 | 职责 |
| --- | --- |
| `cmd/` | 程序入口、Echo 路由、HTTP 处理器、初始化与配置加载。 |
| `internal/core/` | 领域操作、工作区查询、资源授权及带锁的事务写入。 |
| `internal/manager/`、`messenger/`、`bounce/`、`replyai/`、`subimporter/` | 邮件调度/投递、退信、AI 回信分类、批量导入等后台能力。 |
| `models/`、`queries/`、`schema.sql`、`internal/migrations/` | Go 数据模型、具名 SQL、初始结构和版本迁移。 |
| `frontend/` | Vue 2 管理端；`src/views/` 为页面，`src/components/` 为共享组件，`cypress/` 为端到端测试。 |
| `frontend/email-builder/` | 独立的 React 18 + TypeScript 邮件编辑器。 |
| `static/`、`i18n/` | 公开页面、邮件模板、媒体静态资产和语言包。 |
| `dev/`、`deploy/`、`.github/`、`Jenkinsfile` | 本地容器、离线部署包、GitHub Actions 与 Jenkins 流水线。 |

## 技术栈

- 后端：Go 1.26.1、Echo v4、`sqlx`/`goyesql`、Koanf、PostgreSQL。
- 认证：PostgreSQL 持久化会话、传统 API 用户令牌、个人 Bearer API Key，以及可选 OIDC/OAuth2 与 2FA。
- 前端：Vue 2、Vue Router、Vuex、Axios、Buefy/Bulma、Vite；依赖由 Yarn 1 锁定。邮件编辑器采用 React 18、MUI、TypeScript 和 Vite。
- 运维：Docker Compose、systemd、GoReleaser；测试使用 Go `testing`、ESLint 和 Cypress。

## 权限控制算法

### 首次管理员引导与 OIDC 身份绑定

- 首次安装的管理员引导在数据库事务内串行化：`Core.FirstTimeSetup`（`internal/core/first_time_setup.go`）在同一事务中先取 `pg_advisory_xact_lock`，再检查是否已有用户并创建 Super Admin 角色、首个管理员及其资源归属，因此只有一个调用方能成功。并发失败方返回 `ErrFirstTimeSetupDone`，`cmd/auth.go` 的 `LoginSetupPage` 清除 `needsUserSetup` 后回退到普通登录流程（等价于“已完成设置”的行为），不会产生第二个 `role_id=1` 用户。`App.needsUserSetup` 只是内存提示，不承担互斥职责。
- OIDC 登录只在 provider 明确断言 `email_verified: true` 时才绑定账号：`auth.VerifyOIDCEmailClaims`（`internal/auth/auth.go`）是纯函数，ID token 与 userinfo fallback 合并后的 claims 都必须通过它；`false` 和缺失（provider 未提供该 claim）一律拒绝，`cmd/auth.go` 的 OIDC 回调在登录与自动建号前再次校验。部署侧配置见 `docs/docs/content/oidc.md`。

### 认证兼容边界（v6.27.0+）

v3→v4 浏览器 BasicAuth/session Cookie 升级兼容窗口已结束。请求携带 `Authorization` 时，`internal/auth` 始终按显式 API 凭据认证；即使同时存在有效 `session` Cookie，也不会静默回退至 Cookie 会话。浏览器客户端必须移除缓存的旧 BasicAuth 凭据；失效凭据按 API 认证规则拒绝。Cookie 会话继续仅用于不携带 `Authorization` 的浏览器请求，`Basic`/`token` 和 Bearer 继续是 API 认证方式。

每个受保护请求必须同时通过下列边界，前一层通过不代表后一层自动通过：

1. **认证与令牌约束**：`internal/auth` 验证会话、Basic/`token` API 凭据或 Bearer Key。个人 Key 仅可使用创建者身份、绑定的个人/组织工作区和显式 scopes；它只能缩小能力，不能扩大角色、成员资格或资源所有权。密码/2FA/OIDC 认证成功后，浏览器流程先进入 `/admin/select-workspace`（`cmd/select_workspace.go`）：服务端为普通用户计算个人空间与活跃组织成员空间，为最高管理员列出全部活跃组织；仅一个空间时页面自动进入，多个时由用户选择，选择结果写入 localStorage 与工作区 Cookie 后再进入管理端。该选择仅设置当前工作区；最高管理员向组织分配公海时在公海管理窗口另行选择目标组织，不需进入或加入该组织。登录、2FA、重置密码等服务端页面保持经典布局不变；仅选择页使用浅色全屏外壳（`header-app`/`footer-app`，`static/public/templates/index.html`），整屏只呈现空间选择。
2. **传统角色授权**：`permissions.json` 定义功能权限；用户角色提供全局功能权限，客户列表角色提供单个客户列表的 `get`/`manage` 权限。内置 Super Admin 可绕过此层，但仍受归档工作区的常规写入限制。`workspaces:personal` 是个人空间入口权限：无此权限的用户（Super Admin 豁免）在个人工作区（`organization_id=0`、缺失工作区头或绑定个人空间的 Key）的写入/明细/导出请求一律 403，个人数据保留但常规 UI 隐藏；唯一例外是迁移 UI 依赖的四个客户列表 GET 端点（`/api/customer-lists`、`/api/templates`、`/api/campaigns`、`/api/media`，个人空间只含本人资源）与个人资源迁移端点（`MigratePersonal*` 经 `workspaceAccessForOrganizationWithPersonal(..., false)` 豁免），仅用于把留存数据搬入组织。
3. **工作区解析**：`cmd/organizations.go` 从 `X-Listmonk-Organization-ID`（浏览器可使用查询参数/受控 Cookie 回退）解析工作区，重新验证组织仍活动且用户仍是成员，并构建不可跨请求共享的 `WorkspaceAccess`。
4. **资源边界**：每行带有 `organization_id`、`owner_user_id`、原始所有者、可见性和转移状态。`internal/core/workspace.go` 分别判断 `read`、`use`、`copy`、`manage` 和敏感数据访问，不能以“可读”推导出“可发送、导出或修改”。
5. **事务重验**：写入在事务内按稳定顺序锁定组织与资源，重验组织状态、成员资格、所有权和待转移状态，消除“检查后状态变化”的越权窗口。

可见性为 `private`、`organization`、`global`。名单和订阅者始终是所有者私有的；媒体不能全局公开。组织成员可以读取组织共享资源，组织经理可审查同组织成员资源及待转移资源，但只能写自己的资源，不能使用他人的私有发送资源。v6.28.0 的专用导出 API 允许组织管理员导出本组织成员名单及营销数据，仍执行邮箱脱敏；它不扩大普通写入或发送权限。归档组织禁止普通写入及导出；仅平台管理员可执行受限的清理/转移流程。前端的 `$can*` 仅隐藏不允许的操作，Go 服务是唯一权威。

### 媒体逻辑文件夹（v6.37.0）

媒体文件夹是工作区内的数据库逻辑容器，不改变 filesystem 或 S3 provider 中的对象名。这样历史邮件正文中的媒体 URL、缩略图和跨 provider 行为不受影响。`media.folder_id` 指向 `media_folders`；`NULL` 表示根目录，`parent_id` 只形成同一工作区的树。个人文件夹按 `owner_user_id` 隔离，组织文件夹按 `organization_id` 对组织成员可见。

`GET /api/media/folders` 返回当前工作区可见的目录及文件/子目录计数；创建、改名、移动和删除目录沿用 `media:manage`，删除只允许空目录，移动会拒绝自身或子孙目录。`PUT /api/media/:id/folder` 将媒体移入目录或根目录，但仍执行媒体资源原所有者的 `manage` 边界，组织经理不能借文件夹权限修改他人媒体。上传与 `GET /api/media` 支持 `folder_id`，旧请求不带该参数时继续返回工作区内全部媒体。

目录写入在 `internal/core/media_folders.go` 与工作区事务中重验活动组织、成员资格、所有权和目录边界；归档工作区禁止普通目录/媒体写入。初始结构和 `v6.37.0` 幂等迁移同步创建目录表、媒体外键、根目录级大小写不敏感唯一约束和索引。管理端 `Media.vue` 提供面包屑、嵌套目录、新建/改名/空目录删除，以及媒体/目录和本地文件拖放上传。

#### 两层策略的维护规则（强制）

工作区资源授权目前**同时**存在于两层：`cmd/workspace_permissions.go`（HTTP 边界的 `workspaceReadException`/`workspaceCopyException`/`canCopyWorkspaceResource`/`canCopyWorkspaceCampaign`）与 `internal/core/workspace.go`（Core 的 `read`/`use`/`copy`/`manage` 判定），其中 `cmd` 侧的活动复制策略刻意与 Core 的 `CanCopyCampaign` 互为镜像。两层重复是已知技术债，因此：

- 任何接触工作区资源的新接口必须先取 `WorkspaceAccess`，再按需叠加传统角色/客户列表权限；不得只做其中一层。
- 修改任一层的工作区规则时，必须同时检查另一层的对应函数，并在 `cmd/campaign_copy_policy_test.go`（记录了两层当前已知差异）或 `cmd/workspace_permissions_test.go` 中补边界测试。
- 不要在已有门之后追加“若上一层不通过则回退到传统角色”的兜底分支：这类分支在硬门存在时恒不可达（`cmd/campaigns.go` 的 `CloneCampaign` 曾因此留下一段死代码，2026-09-10 删除），而且复活它只会**放宽**权限。若确实需要更宽的行为，应修改唯一一处策略函数并补测试。

### 管理员数据导出（v6.28.0）

`cmd/exports.go` 提供 `/api/exports` 创建/分页查询、`/api/exports/options` 筛选名称、`/api/exports/:id/download` 下载；`internal/dataexport` 负责固定字段查询、参数化筛选、后台队列、脱敏和 CSV/Excel 流式生成。`data_export_jobs` 保存任务与下载审计，`data_export_chunks` 保存文件块，完成后保留 7 天。单 worker 使用 PostgreSQL 会话锁，异常退出后重试未完成任务；新 schema 和 v6.28.0 迁移同步维护。前端 `ExportButton.vue` 在各业务页面提供导出弹窗，内置最近文件、生成进度、失败提示及下载，不设独立页面和导航。关闭弹窗或切换工作区停止前端轮询，后台生成继续。

生成和下载均重查当前账号及组织管理员资格，文件仅创建人可见；打码与追踪设置变化使旧文件不可下载。最高管理员通过工作区选择器跨组织操作，组织管理员仅限本组织。旧管理端客户 CSV/JSON 导出也增加管理员门槛，公开隐私自助下载保持独立。个人 API Key 默认拒绝新导出端点。

### 业务审计日志（v6.32.0）

`audit_events` 是低频、可检索的业务操作日志，与 `campaign_recipients`、`campaign_views`、`link_clicks`、`bounces` 和回信队列表中的业务事实分工：打开/点击等高频事实继续写专用表，不复制到审计表；活动发送器只记录开始、结束、暂停、取消、延迟和启动失败等生命周期事件。`cmd/audit.go` 的认证 API 中间件按稳定动作名记录活动、客户/名单、导入、退信、模板、SMTP、回复邮箱/转发、事务邮件及导出等操作，并保存工作区、用户/API Key、结果、原因码、请求 ID、IP、User-Agent 和小型非敏感元数据。密码、令牌、邮件正文、附件和完整收件人集合不得进入元数据。

`audit:get` 只允许查看当前工作区的审计记录；`organization_id = 0` 明确表示个人空间，组织历史保留原组织 ID，不能因组织删除而落入个人查询。`GET /api/audit-events` 支持动作、结果、对象和服务端分页筛选，单页最多 100 条；`GET /api/audit-events/:id` 同样执行工作区边界。`GET /api/audit-events/export` 复用相同的工作区和筛选边界，支持当前页勾选 ID 的选择导出，以及忽略分页限制的全量 CSV 流式导出；选择导出最多接受 1000 个 ID，导出动作本身也写入审计日志。后台活动发送器、退信 webhook、回信转发器和回信 AI 处理器使用 `system`/`webhook` 操作者类型补写自动动作，失败结果使用固定原因码而不记录底层错误正文。审计写入是业务提交后的独立 best-effort 写入，写入失败只进入运行日志，不改变原业务请求结果；后续需要强一致的核心事务可复用 `internal/audit.Writer.RecordTx`。

第二阶段将同一模型扩展到三组业务边界：公共订阅/退订/Opt-in 与客户自助导出/擦除使用 `customer` 操作者并在可解析时绑定客户 UUID 和所属工作区；公海联系人、媒体、自定义字段记录安全对象 ID 和批量结果摘要；用户、角色、组织、API Key、2FA、登录/登出、密码重置和 OIDC 记录成功、失败或拒绝结果。业务函数通过 Echo context 提供对象、组织、动作、原因码和白名单元数据，避免把请求体、邮箱、凭据或 token 原文交给通用中间件；公共未认证路由也挂载审计中间件，但只有明确登记的低频写操作才产生事件。

公海转私域保留手动流程，不提供独立导出或自动匹配统计。移除时由操作者填写“转入私有”，公海页面和公海分配名单导出的“移除原因”展示实际记录；恢复分配后不展示旧原因。指标历史缺失、容量限制与部署容量说明见 `docs/docs/content/data-exports.md`。

### 订阅者客户编码与邮箱打码（v6.21.0+）

- 客户批量导入仅支持邮箱、姓名、客户编码映射；CSV（包括 ZIP 内 CSV）固定逗号分隔，XLSX 保持支持。导入 API 不再接受属性映射，也不再读取 `delim` 参数。覆盖用户信息仅更新姓名与客户编码，保留已有属性。

- `customers.customer_code`（v6.21.0 迁移新增）：客户编码，必填但不唯一。仅管理端新增/编辑（`cmd/customers.go`）与导入路径（`internal/subimporter`）校验必填；公开订阅入口可选。列允许空串并带普通索引。
- `customer_lists.mask_emails`（v6.21.0 迁移新增）：客户列表级“打码邮箱”开关。无敏感数据访问权的查看者，在当前查看客户列表开启打码时看到打码邮箱；客户列表未开启或无上下文时维持原置空行为。打码覆盖客户列表/详情、API 响应及范围 CSV 导出，搜索仍按完整邮箱匹配。CSV 导出额外输出 `customer_code` 列。

### 一级公海与组织二级列表（已实施）

- 一级公海是独立的客户池类型，不等同于允许匿名订阅的 `public` 列表。公海联系人保存导入的客户编码（允许重复）、公司名称和真实邮箱；编码不承担唯一键职责。
- 二级列表仅保存一级公海联系人到组织的分配关系和组织回件邮箱，不复制联系人主数据。组织在二级列表手动移除联系人时，一级公海保留该联系人并显示该组织的逻辑剔除标记；该组织后续选择一级公海投放时也必须过滤该标记，其他组织不受影响。
- 二级列表联系人批量分配使用 `POST /api/pool-segments/:id/import-members`（兼容 `/api/pools/segments/:id/import-members`）上传 CSV/XLSX。文件首行必须包含 `customer_code` 与 `email`；服务端在事务内仅对当前一级公海按“客户编码 + 邮箱（去空格、忽略大小写）”唯一匹配，重复行去重，未匹配/歧义行返回安全的行号和客户编码，不回传上传的真实邮箱。普通组织管理员只能导入本组织二级列表，最高管理员可导入任意目标组织；单条查询/移除/恢复仍保留为维护入口。
- 每个“一级公海 × 组织”至多绑定一个二级列表，以便一级公海投放能唯一解析该组织的回件邮箱。最高管理员或目标组织管理员在目标组织尚未绑定时创建并绑定二级列表；回件邮箱由组织管理员在组织工作区单独配置，创建二级列表时不要求最高管理员代填。
- 最高管理员在一级公海管理窗口通过独立目标组织选择器查看该组织的现有二级列表；创建请求显式指向目标组织，不切换当前工作区，也不创建组织成员关系。普通组织用户没有跨组织目标选择能力，二级列表不能通过通用客户列表表单创建，也不存在二级合并一级流程。二级列表回件邮箱的维护入口只对组织工作区管理员开放。
- 活动选择一级公海时，一级列表可作为受众选择项，但不授予详情、导出或客户明文邮箱访问；服务端按目标组织的二级分配解析收件人和统一回件邮箱，并在发送快照中记录一级/二级来源、组织和最终邮箱来源。活动选择二级列表时直接使用该二级列表的回件邮箱。回件邮箱是公司内部地址，可在管理端明文展示，不纳入客户邮箱脱敏。
- 公海投递快照使用 `campaign_pool_recipients` 与联系人内部 ID 去重；公海退订、退信和回复 AI 事件写入 `pool_segment_exclusions` 的组织维度逻辑状态，并在 `bounces`/`reply_ai_events` 保留来源池、二级列表和组织字段，禁止改变一级主数据或其他组织分配。收件人判定（活跃公海联系人 × 有效二级分配 × 本组织未剔除）只在 `internal/core/pools.go` 的 `poolRecipientMembershipSQL` 定义一次，一级解析、二级解析与快照写入共用同一片段，因此三条路径不可能给出不同收件人集合。快照刷新采用 `DO UPDATE` 并清理本组织范围内、已不再可投递且尚未交给投递的 `pending`/`deferred` 行；已 `queued`/`sent`/`cancelled` 的行属于投递历史，不重写也不删除，退队路径另按 `pool_segment_exclusions` 重查剔除。
- 客户回复、退订和投诉只能对实际投递来源组织的二级分配执行逻辑剔除，保留一级主数据和历史快照；最高管理员可跨组织审计，组织用户只能看本组织安全字段。

## AI 入站回信处理

退信设置的 `POST /api/settings/bounce/mailbox/test` 复用 `settings:manage`，对未保存表单执行连接/登录/读取/解析四步只读检测，最多读取当前 POP 会话最高序号的一封邮件（30 秒、5 MiB 上限），不调用 `Scan`、不删除邮件、不入队或改变客户状态。`internal/bounce/mailbox/test.go` 负责连接诊断，`preview.go` 负责外层摘要与 DSN 失败收件人解析。`bounce.mailboxes[].starttls` 默认 false，与 `tls_enabled` 互斥；后台扫描也支持 STLS，旧配置保持原行为，无数据库迁移。接口和日期/识别语义见 `docs/docs/content/bounces.md`。

全局 `reply_ai` 设置保存 OpenAI 兼容接口的端点、模型、密钥、超时和最低置信度；密钥在读取设置时打码，更新时空值表示保留。每个客户回信邮箱还必须显式启用 AI 处理，避免将未选择的邮箱内容发送到第三方模型。

对接聚合网关（new-api、one-api、LiteLLM 等）时模型清单由网关决定，因此设置页提供两个只读探测端点：`POST /api/settings/reply-ai/models`（`GET {root}/models`，根地址未带 `/v1` 时回退尝试 `/v1/models`，返回模型清单、是否可对话的提示和实际生效的根地址）与 `POST /api/settings/reply-ai/test`（依次执行配置校验、网关连通、模型是否在清单中、真实分类往返四步，返回 `success`/`warning`/`failed` 与每步原因码）。两者都接受未保存的表单值，`api_key` 为空或全掩码时复用已保存密钥，密钥只出现在出站 `Authorization` 头中，不落库、不回显、不入日志；探测始终使用已配置的根地址发起分类调用，网关只在 `/v1` 上响应时通过 `suggested_base_url` 提示操作者改正地址，而不是静默替换。实现见 `internal/replyai/gateway.go` 与 `cmd/reply_ai_settings.go`，两者共用 `internal/replyai/client.go` 的提示词与响应校验。

后台 worker 使用 POP3 非破坏性轮询已验证且已启用的回信邮箱；每轮通过 LIST 先获取邮件大小，仅处理最新的有限批次并跳过超大邮件，把邮件按“邮箱 + Message-ID/内容哈希”写入持久化队列。邮箱扫描走固定大小的 worker 池（`replyAIMaxConcurrent`，不按邮箱创建 goroutine），因此单个挂起的 POP 服务器最多占用一个 worker，其余邮箱仍会在同一轮内被扫描。由于 `go-pop3` 不提供读超时或 context，`cmd/reply_ai.go` 通过 `pop3.Opt.Dialer` 注入包装过的 `net.Conn`，为每次读写重新设置空闲读/写截止时间（`replyAIMailboxReadTimeout`，默认 30 秒），并在整轮邮箱扫描超过 `replyAIMailboxTimeout`（2 分钟）时强制关闭该连接，释放 worker 而不是泄漏 goroutine；日志会带上邮箱 ID 与服务器地址以区分“挂起”和“失败”。只会在发件人地址能唯一匹配到该邮箱所属工作区、所有者的客户时调用模型；自动回复、未匹配地址、低置信度和非明确意图都只留下审计记录，不修改客户。模型返回固定结构的 `unsubscribe`、`complaint` 或 `other` 意图，邮件正文被视为不可信数据，终态记录会清除可发送给模型的正文，仅保留哈希和最小化审计字段。

明确退订和明确垃圾/滥用投诉均在同一工作区将匹配客户设为 `blocklisted` 并退订其全部名单；投诉另外以 `source=reply_ai` 写入 `bounces`，保留事件 ID、模型、置信度和原因代码。处理事务会再次锁定并验证工作区、成员资格和客户所有权；队列租约与唯一事件键保证重试不会重复投诉计数或跨工作区操作。

## 客户回信转发

`cmd/reply_forwarder.go` 以 POP3 非破坏性轮询保留（`retained`）回信邮箱，把客户回信转发到规则目标地址，源邮件永不删除。去重键 `reply_forward_messages(rule_id, message_key)`（Message-ID + 原文哈希）保证同一封回信只转发一次：每轮扫描先用单条 `INSERT ... ON CONFLICT DO UPDATE` 抢占该行（置 `pending`、递增 `attempts`），抢占提交后才投递，投递返回后才写终态，因此 `forwarded` 行永不会被再次抢占，而抢占行本身就是租约。

投递失败（队列积压、管理器关闭或重启）记为 `failed`，下一轮按 `replyForwardRetryBackoff` 起的指数退避重试，直到 `replyForwardMaxAttempts`（默认 5 次）；到达上限后保持 `failed` 且不再重试，成为可查询的终态（`status = 'failed' AND attempts >= 上限`）并写日志，避免永久不可达的回信被无限重试或被静默吞掉。抢占后进程退出的行仍是 `pending`，只有超过 `replyForwardClaimLease`（默认 5 分钟）才会被下一轮接管；租约远长于单次投递（`PushMessage` 3 秒超时），所以租约过期只可能意味着持有者已死，从而在不重复投递的前提下恢复中断的转发。状态机与终态查询同时写在 `schema.sql` 的表注释里，未新增列、不需要迁移；回归测试见 `cmd/reply_forwarder_test.go`。

## 构建、测试与开发

在 Bash 兼容终端运行以下命令：

| 命令 | 用途 |
| --- | --- |
| `make build` | 编译后端为 `./listmonk`。 |
| `make build-frontend` | 构建 Vue 管理端与邮件编辑器。 |
| `make dist` | 构建前后端并将运行资源嵌入单一二进制。 |
| `make run` / `make run-frontend` | 分别运行后端和端口 8080 的 Vite 前端。 |
| `make test` | 运行全部 Go 单元测试（`go test ./...`）。 |
| `cd frontend && yarn lint` | 执行 Vue/JavaScript ESLint。 |
| `cd frontend && yarn cypress run` | 运行端到端测试；它会重置并启动本地服务，只能在隔离环境使用。 |
| `make init-dev-docker`、`make dev-docker` | 初始化并启动开发 Compose 套件；管理端映射到 `http://localhost:8181`。 |
| `make rm-dev-docker` | 删除开发容器及其数据库卷，数据不可恢复。 |

Go 测试放在实现附近的 `*_test.go`；修改工作区、权限、导入、发送或迁移行为时，必须补充对应的边界测试。前端可见行为变化应更新 `frontend/cypress/e2e/` 测试。

## 发布与部署脚本

- v6.23.0 术语迁移在重命名统计物化视图后同步重命名输出列，再创建索引；兼容旧 `list_id/subscriber_count` 并保留数据。迁移回归测试使用 `MIGRATION_TEST_DSN` 指向测试 PostgreSQL，创建并清理独立测试 schema，覆盖重复执行。

- 根目录 `docker-compose.yml` 是常规 Compose 部署：应用启动时幂等安装、执行迁移，再开始服务；所有密钥使用运行环境变量或 `LISTMONK_*_FILE`，不可提交真实配置。
- `deploy/package_bundle.sh` 先执行 `make dist`，打包官方镜像、本地镜像、PostgreSQL 镜像与 Compose 脚本。将生成包复制到目标机后，依次运行包内 `scripts/load-images.sh`，配置 `env/runtime.env`，再运行 `start-online.sh` 或 `start-local.sh`；`stop.sh` 用于停止。
- `Jenkinsfile` 执行 `make test` 和 `make dist`，归档二进制、前端压缩包及 SHA-256，再通过 SSH 和 systemd 进行可回滚部署。Jenkins 发布将 filesystem 媒体固定保存在 `<DEPLOY_DIR>/uploads`（版本目录之外），每个版本的 `uploads` 都链接到该目录；首次修复发布会从旧 `current/uploads` 迁移历史文件。`listmonk@.service` 是强化隔离的模板，`listmonk-simple.service` 兼容旧系统。
- GitHub Actions 的 PR build-sanity 执行 `make dist`；标签 `v*` 使用 GoReleaser 发布多架构二进制与 Docker 镜像，nightly 工作流发布每日快照。

## 文档同步规则（强制）

提交前逐项检查：目录职责或模块移动更新本文；路由、数据模型、工作区/权限语义变化更新本文及 `docs/docs/content/roles-and-permissions.md` 或相关 API 文档；构建、测试、CI、Docker、Jenkins 或部署脚本变化更新本文、`docs/docs/content/developer-setup.md` 和/或 `deploy/README.md`。不得保留与代码、`Makefile`、Compose 或流水线不一致的命令、端口、服务名、权限描述或示例。


## 模板称呼兜底（v6.29.0）

`templates.name_fallback` 是模板所属工作区内的 JSON 配置（enabled/value/invalid_values）；`models.NameFallback` 负责校验及完整值匹配。模板更新在原工作区事务内写入，API 未传字段保留已有配置。`internal/manager/message.go` 和 `models/messages.go` 在渲染用的客户副本上应用规则，不改变数据库、投递信封或追踪对象。活动查询按与 template_body 相同的授权条件读取 template_name_fallback；HTML 活动随模板加载，visual 活动使用 `campaigns.name_fallback` 导入快照。模板/活动复制、工作区迁移复制保留配置。初始 schema 与 v6.29.0 幂等迁移均默认 `{}`，保持旧行为。入口为 `TemplateForm.vue` / `NameFallbackSettings.vue`，预览接受未保存规则和空姓名样本。
