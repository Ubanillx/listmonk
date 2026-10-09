# Listmonk 工程架构与运行指南

## 业务权限分组与独立动作门（v6.56.0）

角色配置以私域客户与列表、公海客户与列表、营销活动、模板与素材、发信与回信邮箱五组呈现 20 项业务权限，不设计预设角色。`frontend/src/utils/businessPermissions.js` 只负责显示分组，服务端仍持久化原权限 ID；旧角色的部分授权不会因打开、保存而扩展。名称、映射、依赖和升级语义见 [业务权限说明](docs/content/business-permissions.md)。

新增 `customer_lists:delete`、`pools:master_manage`、`pools:delivery_manage`、`assets:share`、`mailboxes:use`、`mailboxes:manage`。组织身份和所有权决定数据范围，不能绕过查看、导出、统计或私域敏感信息权限；列表、明细、写入响应、CSV、JSON 和收件人报告一致脱敏，省略的编辑字段保留原值。全局模板也要求查看/维护权限，改变共享范围另需共享权限。发信与回信邮箱使用和配置分别授权，配置仍保留所有者/组织边界。

公海主数据授权以 `WorkspaceAccess.PoolMaster` 表达，只为平台归属、非待转移的一级 `pool` 扩大范围；工作区解析、查询、单行检查和事务锁统一传递，不能提升私域或其它资源能力，委派维护者的联系人 DTO 仍脱敏。组织分配需 `pools:manage`，本组织创建分配先检查已有投放授权，跨组织创建还需 `pools:delivery_manage`。`GET /api/pools/organizations` 只向投放授权管理者提供活动组织 ID/名称，不授予组织管理或成员身份。

迁移 `v6.56.0` 保留旧角色的列表删除、模板/素材共享、邮箱自助能力及已有组织管理员的分配能力；重复执行不增加重复授权，不自动授予公海平台级权限。回归：`cmd/business_permissions_test.go`、`cmd/org_pool_allocation_permissions_test.go`、`internal/migrations/v6.56.0_test.go`、`frontend/cypress/e2e/business-permissions.cy.js`。

## 公海客户回信地址与活动优先级（v6.55.0）

公海 CSV/XLSX 导入仍需映射姓名列，但姓名内容允许为空或仅含空白，规范化后保存为空字符串。普通导入与黑名单导入遵守同一规则；客户编号、邮箱、分配部门仍必填，姓名仍参与身份比较和重复记录判定。来源：`cmd/pools.go::parsePoolContactImportRows`、`internal/core/pools.go::ImportPoolContacts`、`internal/core/pools_import_test.go`。

统一导入支持可选 `reply_to`/“回信邮箱”，该字段必须是单个邮箱地址；公海导入的 `email` 单元格可以包含多个地址，解析器会提取地址并为每个地址复用同一行的客户编号、姓名、分配部门和回信邮箱。四列旧模板仍可导入。同身份重导入更新或清空地址而不重复创建。组织邮箱配置需 `mailboxes:manage` 与组织管理边界，联系人导入需 `pools:master_manage`。导入地址只控制 Reply-To，不创建邮箱、不保存凭据、不授予收信能力。

迁移 `internal/migrations/v6.55.0.go` 保留历史路由地址，重复运行不覆盖已有来源；`schema.sql`、安装示例、创建/更新/克隆及前端请求同步支持优先级。来源：`models/{pools,campaigns}.go`、`internal/core/{pools,pool_reply_routes,pools_tx}.go`、`queries/campaigns.sql`、`cmd/{pools,campaigns,manager_store,install}.go`、`internal/manager/manager.go`、`frontend/src/views/{Import,Customers,PoolContactForm,Campaign}.vue`。


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

本仓库的默认本地开发拓扑是“Docker 中间件 + 主机应用”：开发 Make 目标从
`dev/docker-compose.yml` 只启动 PostgreSQL、MailHog 和 Adminer；Go 使用
`dev/config.local.toml` 连接宿主机发布的 PostgreSQL 端口，由 Air 监听
Go/SQL/TOML 文件并重启进程；Node.js 运行
Vite，前端开发服务器把 API、登录、工作区选择、找回密码和 OIDC 请求代理到本机 Go 服务；
这些服务端渲染页面必须继续走 Go，不能被 Vite 的 SPA fallback 接管。需要隔离环境时，
`make dev-docker` 仍可同时运行 Compose 中的 backend 和 front 服务。

## 目录与职责

| 路径 | 职责 |
| --- | --- |
| `cmd/` | 程序入口、Echo 路由、HTTP 处理器、初始化与配置加载。 |
| `internal/core/` | 领域操作、工作区查询、资源授权及带锁的事务写入。 |
| `internal/manager/`、`internal/messenger/`、`internal/bounce/`、`internal/replyai/`、`internal/subimporter/` | 邮件调度/投递、退信、AI 回信分类、批量导入等后台能力。 |
| `models/`、`queries/`、`schema.sql`、`internal/migrations/` | Go 数据模型、具名 SQL、初始结构和版本迁移。 |
| `frontend/` | Vue 2 管理端；`src/views/` 为页面，`src/components/` 为共享组件，`src/assets/styles/` 为设计令牌与分层样式，`cypress/` 为端到端测试。设计约束见 `docs/harness/UI_DESIGN_SYSTEM.md`。 |
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

可见性为 `private`、`organization`、`global`。普通客户列表可在组织工作区内以 `organization` 共享列表元数据（包括名称和统计），个人工作区只允许 `private`；客户记录始终是所有者私有，普通列表不能全局共享，媒体不能全局公开。组织成员可以读取组织共享列表，组织经理可审查同组织成员资源及待转移资源，但只能写自己的资源，不能使用他人的私有发送资源。共享列表的可读性不赋予客户明细、导入、批量修改或活动发送权限：这些操作仍按列表权限、当前工作区和客户/列表所有者重新校验，一级公海走独立的跨工作区投放/导入授权。客户 CSV、单客户资料和审计日志均为直接 HTTP 导出，仍执行工作区、所有权和脱敏边界，不建立持久化导出任务。归档组织禁止普通写入及导出；仅平台管理员可执行受限的清理/转移流程。前端的 `$can*` 仅隐藏不允许的操作，Go 服务是唯一权威。

管理端路由自 2026-09-27 起用 `meta.permission` 显式声明页面所需权限（`frontend/src/router/index.js`：`/settings*` 需要 `settings:get`/`settings:maintain`、`/settings/audit` 需要 `audit:get`、`/users*` 需要 `users:get`/`roles:get`），`frontend/src/main.js` 的全局守卫在 `profile` 就绪后统一判定（`profileReady` 承诺消除“首次导航早于 profile 请求”的竞态，管理员管理页沿用同一机制），未授权直达会进入 `/admin/403` 说明页而不是渲染只能产生 403 的空壳；该守卫与 `$can*` 一样只属于体验层，服务端依旧逐请求校验（见本条与 `docs/harness/UI_UX_AUDIT.md`）。

组织目录由 `frontend/src/api/index.js` 的 `refreshOrganizationDirectory` 统一刷新：Vuex 的 `organizations` 是可进入的活跃组织（最高管理员取全部活跃组织，普通用户取自己的成员组织），`organizationMemberships` 仅是实际加入的组织。顶部工作区切换读取前者，公海投放授权管理另取最小组织目录，已加入组织/迁移目标及组织经理判断读取后者；页面不能用成员接口结果覆盖可进入组织。并发刷新共用一个请求承诺，两份列表成功后原子提交；失败保留上一份完整快照并显示重试入口，成功空列表正常替换旧值。完整目录不保存到浏览器，只持久化选中的组织 ID；后端持续校验权限。启动时先挂载加载/重试外壳，等待 profile、目录和工作区都校验后才挂载业务视图，避免新建表单从半初始化状态选错默认值。工作区请求仅在明确的 403/404 下回退，网络或 5xx 错误保留选择并允许重试；登录失效仍回到登录页。

平台可观测性出口与业务聚合数据的边界：`GET /api/logs` 与 `GET /api/events`（SSE 实时错误流）都是进程日志的出口，统一由 `settings:get` 控制，与 `/api/settings` 共用同一平台权限；管理端也只为持有该权限的账号建立 `EventSource`，无权限账号不会打开连接（否则浏览器会对 403 无限重试）。这两个接口不属于工作区数据面。相对地，仪表板的 `GET /api/dashboard/counts` 与 `GET /api/dashboard/charts` 不设角色权限门：任何登录用户在其当前工作区都可读取，但结果由工作区、所有权和可见性谓词收敛（只有平台管理员读取全局物化视图）。统计响应明确分为 `private_customers`（兼容别名 `customers`）、`pool_customers` 与 `pool_lists`；公海客户按一级公海成员行统计，公海列表按启用的一级公海列表统计，组织空间只计本组织可见列表和分配，个人空间两项公海统计均为零。图表没有数据只表示该工作区内没有可统计的浏览/点击行（或数据落在 30 天窗口之外），不是权限拒绝。

仪表板公海列表卡片提供 `pool_lists.bound`/`unbound`/`allocations`/`organizations` 四项明细。总数保持启用的一级公海列表口径；有启用分配列表且所属组织启用才算绑定，已绑定与未绑定之和等于总数。分配数仅计绑定当前可见一级公海的启用分配，组织数去重；普通组织工作区仅计本组织的绑定，不能推断同一公海在其它组织的使用情况，普通个人工作区全为零。授权但未建立有效分配的公海计未绑定。实现：`internal/core/dashboard.go` 的 `getWorkspacePoolListDashboardCounts`；契约与归档/跨组织边界：`internal/core/dashboard_counts_db_test.go`，UI：`frontend/src/views/Dashboard.vue`。

### 角色动作细分权限（v6.38.0）

用户角色中的权限是全局功能门，不替代工作区、资源所有者、组织成员或 API Key scope 校验。业务动作按以下独立权限管理：`customers:delete`、`customers:blocklist`、`customers:membership_manage`、`customers:export`、`customers:sensitive_read`；`pools:get`、`pools:manage`、`pools:export`（2026-09-17 起，v6.45.0 回填最高管理员，见“一级公海与组织公海分配”一节）；`campaigns:send`、`campaigns:test`、`campaigns:schedule`、`campaigns:control`、`campaigns:recipients`；`bounces:delete`、`bounces:blocklist`；`users:tokens`；以及 `organizations:platform_manage`。`tx:send` 仍使用原权限 ID，但在角色界面归入事务消息组。

私域客户高级 SQL 查询已下线：`GET /api/customers` 和导出仅接受参数化的普通 `search`（匹配客户编码、姓名、邮箱）、列表和订阅状态过滤；传旧 `query` 参数返回 400。原 `/api/customers/query/*` 路由撤销，普通“选择全部结果”操作改走 `/api/customers/bulk/*`，仍执行工作区、资源管理和对应的删除、拉黑或列表成员权限校验。`customers:sql_query` 不再出现在权限清单或授权判断中；角色编辑会过滤历史保存的失效权限。服务端入口在 `cmd/customers.go`，导出实现位于 `internal/core/workspace_customer_export.go`。

`v6.36.0` 和 `v6.38.0` 通过幂等迁移为既有非 Super Admin 角色补齐细分权限，避免升级后改变原有业务能力。之后管理员可以从角色中去掉某个独立动作；创建新角色时这些高风险动作默认不勾选。平台管理员、用户/角色/设置/组织管理权限保持现有的宽平台控制，不为管理员场景继续拆分；业务细分权限不能跨越资源边界，也不能把“有某个动作”解释为获得其它动作。

组织平台管理权限只覆盖组织申请、组织生命周期、成员/邀请、组织回信邮箱、回复转发和受限资源转移；平台操作员通过明确的管理接口选择组织，不会因此获得该组织客户、活动、模板或媒体的普通工作区数据访问。平台管理员可通过 `POST /api/organizations` 在一个事务中直接创建组织并指定首位组织管理员/初始成员，也可通过 `POST /api/organizations/:id/members/bulk` 按用户名或邮箱批量加入已注册账号；批量校验失败时不写入，且组织始终至少保留一名 manager。客户导出仍执行工作区、成员、所有权、邮箱脱敏和一级公海明文限制，不因直接下载而放宽边界。个人 SMTP、个人工作区回信邮箱、API Key 自助设置、公海导入和其它自助流程仍按各自的系统边界处理；组织工作区回信邮箱从“管理组织”页面配置。

客户回信邮箱是工作区资源，选用与配置分别要求 `mailboxes:use`、`mailboxes:manage`。个人空间只列出本人邮箱，组织空间可列出全部邮箱；使用者只取得安全选择字段，配置者仍受所有权限制。`manageable` 要求所有者与配置权限，`deletable` 要求配置权限且为所有者或当前组织经理。编辑、测试与启停非本人邮箱返回 404；组织经理可清理遗留邮箱，仍在统一回信设置或活动转发规则中使用的邮箱返回 409。组织统一回信选择保留组织管理边界，个人邮箱不能由他人选用。配置权限不授予发送，使用权限不暴露连接凭据。来源：`cmd/{reply_mailboxes,reply_mailbox_campaigns,organizations}.go`。

### 媒体逻辑文件夹（v6.37.0）

活动富文本编辑器的本地图片（图片对话框上传、粘贴、拖入及图片编辑产生的 Blob）通过现有 `POST /api/media` 上传到当前工作区根目录，复用媒体维护权限及默认可见性。`RichtextEditor.vue` 在上传成功后返回带媒体 ID 的保护 URL，并经 `Editor.vue` 的 `media-selected` 事件关联活动媒体；预览、保存、测试发送、启动/排期及格式转换先调用 `prepareContent` 等待上传完成并同步正文，失败时保留图片、阻止后续动作。实际发送继续由 `internal/manager/inline_media.go` 将关联图片转换为 CID 内嵌 MIME，不向匿名访问者开放私有媒体。

`CampaignPreview.vue` 通过带工作区的登录请求获取渲染结果，只对当前活动/所选模板已关联或表单明确选择的媒体读取二进制，将图片转换为预览专用 data URL 后放入 `sandbox="allow-scripts"` 的 `srcdoc`。ID 与文件名同时匹配媒体元数据，旧文件名链接也解析为已关联的精确 ID；未关联图片与外域 URL 不触发父页面的鉴权读取。文件接口继续执行媒体角色、工作区和派生关联校验，隔离窗口不取得登录 Cookie，保存正文仍保留保护 URL，纯文本预览保留转义显示。活动列表、编辑器、归档与模板预览共用此路径。

媒体文件夹是工作区内的数据库逻辑容器，不改变 filesystem 或 S3 provider 中的对象名。这样历史邮件正文中的媒体 URL、缩略图和跨 provider 行为不受影响。`media.folder_id` 指向 `media_folders`；`NULL` 表示根目录，`parent_id` 只形成同一工作区的树。v6.51.0 增加独立 `visibility`：`private`（个人）只对当前工作区的创建者可见，`organization`（组织）对当前组织成员可见，`global`（全体）对所有登录用户跨工作区可见；平台管理员可在所选工作区审查个人目录。个人工作区不能选择组织权限。迁移将既有个人目录回填为 `private`、组织目录回填为 `organization`，重复执行保留显式权限。

`internal/core/media_folder_permissions.go` 与 `media_folders.go` 对每个目录递归检查全部祖先的可见性及组织活动状态，目录内媒体的库查询、明细、保护 URL 和发送关联使用目录权限；根目录媒体保留原资源策略，不能直接设为全局。媒体 API 返回目录对应的有效 `visibility`，但不改变媒体所有权。目录改名、权限编辑、移动和删除只允许创建者或所选工作区的平台管理员，并继续要求 `media:manage`；目录上传和嵌套创建可由本工作区的可读目录成员执行，全体共享不会赋予其它工作区写入权。发送关联在锁定媒体行后以 `FOR SHARE` 锁定祖先目录并重验权限，权限修改和移动使用 `FOR UPDATE`。模板/活动的历史派生保护 URL 仅对根目录媒体生效，目录内媒体不能借关联绕过目录权限。

`GET /api/media/folders` 返回当前工作区可见目录与全体共享目录及文件/可见子目录计数，另含 `manageable`（是否可维护目录）和 `writable`（是否可上传/新建子目录）；创建、改名、移动和删除目录沿用 `media:manage`，删除只允许空目录，移动会拒绝自身或子孙目录。`PUT /api/media/:id/folder` 将媒体移入目录或根目录，但仍执行媒体资源原所有者的 `manage` 边界，组织经理不能借文件夹权限修改他人媒体。上传与 `GET /api/media` 支持 `folder_id`，旧请求不带该参数时返回全部可读媒体（包含全体目录），媒体和目录写入仍严格限定源/目标工作区一致；归档工作区保留平台清理读取能力，但不开放普通写入。

目录写入在 `internal/core/media_folders.go` 与工作区事务中重验活动组织、成员资格、所有权和目录边界；归档工作区禁止普通目录/媒体写入。用户物理删除在同一事务中清理其个人目录，目录中的媒体通过外键回到根目录；组织目录保留，仅将已删除创建者引用置空。初始结构和 `v6.37.0` 幂等迁移同步创建目录表、媒体外键、根目录级大小写不敏感唯一约束和索引。管理端 `Media.vue` 提供面包屑、嵌套目录、新建/改名/空目录删除，以及媒体/目录和本地文件拖放上传。

#### 两层策略的维护规则（强制）

工作区资源授权目前**同时**存在于两层：`cmd/workspace_permissions.go`（HTTP 边界的 `workspaceReadException`/`workspaceCopyException`/`canCopyWorkspaceResource`/`canCopyWorkspaceCampaign`）与 `internal/core/workspace.go`（Core 的 `read`/`use`/`copy`/`manage` 判定），其中 `cmd` 侧的活动复制策略刻意与 Core 的 `CanCopyCampaign` 互为镜像。两层重复是已知技术债，因此：

- 任何接触工作区资源的新接口必须先取 `WorkspaceAccess`，再按需叠加传统角色/客户列表权限；不得只做其中一层。
- 修改任一层的工作区规则时，必须同时检查另一层的对应函数，并在 `cmd/campaign_copy_policy_test.go`（记录了两层当前已知差异）或 `cmd/workspace_permissions_test.go` 中补边界测试。
- 不要在已有门之后追加“若上一层不通过则回退到传统角色”的兜底分支：这类分支在硬门存在时恒不可达（`cmd/campaigns.go` 的 `CloneCampaign` 曾因此留下一段死代码，2026-09-10 删除），而且复活它只会**放宽**权限。若确实需要更宽的行为，应修改唯一一处策略函数并补测试。

### 直接数据导出

客户列表的 `GET /api/customers/export` 直接流式返回 CSV；单客户资料的 `GET /api/customers/:id/export` 直接返回 JSON；审计日志的 `GET /api/audit-events/export` 直接返回当前工作区 CSV。客户/黑名单及单客户资料导出由 `customers:export` 控制（平台管理员和组织管理员仍按工作区规则处理），并继续执行客户所有权、工作区、列表权限及邮箱脱敏边界。公开订阅者自助资料导出保持独立。系统不创建导出任务或持久化文件，不运行导出 worker，也没有导出中心。

### 业务审计日志（v6.32.0）

`audit_events` 是低频、可检索的业务操作日志，与 `campaign_recipients`、`campaign_views`、`link_clicks`、`bounces` 和回信队列表中的业务事实分工：打开/点击等高频事实继续写专用表，不复制到审计表；活动发送器只记录开始、结束、暂停、取消、延迟和启动失败等生命周期事件。`cmd/audit.go` 的认证 API 中间件按稳定动作名记录活动、客户/名单、导入、退信、模板、SMTP、回复邮箱/转发、事务邮件及导出等操作，并保存工作区、用户/API Key、结果、原因码、请求 ID、IP、User-Agent 和小型非敏感元数据。模板、活动、媒体、客户列表、用户和角色等低频写操作会额外保存截断后的 `object_details` 摘要，认证操作者保存可读的 `actor_details`；审计查询在保留 `actor_user_id` 的历史记录上关联 `users` 返回当前用户名/名称。登录成功在建立会话前绑定用户，失败登录只保存尝试的用户名；媒体移动记录保存文件名及源/目标目录摘要。这些快照和关联字段用于审计页面展示，不替代稳定 ID。密码、令牌、邮件正文、附件和完整收件人集合不得进入元数据。

`audit:get` 只允许查看当前工作区的审计记录；`organization_id = 0` 明确表示个人空间，组织历史保留原组织 ID，不能因组织删除而落入个人查询。`GET /api/audit-events` 支持动作、结果、对象和服务端分页筛选，单页最多 100 条；`GET /api/audit-events/:id` 同样执行工作区边界。`GET /api/audit-events/export` 复用相同的工作区和筛选边界，支持当前页勾选 ID 的选择导出，以及忽略分页限制的全量 CSV 流式导出；选择导出最多接受 1000 个 ID，导出动作本身也写入审计日志。后台活动发送器、退信 webhook、回信转发器和回信 AI 处理器使用 `system`/`webhook` 操作者类型补写自动动作，失败结果使用固定原因码而不记录底层错误正文。审计写入是业务提交后的独立 best-effort 写入，写入失败只进入运行日志，不改变原业务请求结果；后续需要强一致的核心事务可复用 `internal/audit.Writer.RecordTx`。

第二阶段将同一模型扩展到三组业务边界：公共订阅/退订/Opt-in 与客户自助导出/擦除使用 `customer` 操作者并在可解析时绑定客户 UUID 和所属工作区；公海联系人、媒体、自定义字段记录安全对象 ID 和批量结果摘要；用户、角色、组织、API Key、2FA、登录/登出、密码重置和 OIDC 记录成功、失败或拒绝结果。业务函数通过 Echo context 提供对象、组织、动作、原因码和白名单元数据，避免把请求体、邮箱、凭据或 token 原文交给通用中间件；公共未认证路由也挂载审计中间件，但只有明确登记的低频写操作才产生事件。

公海转私域保留手动流程，不提供独立导出或自动匹配统计。移除时由操作者填写“转入私有”，公海页面展示实际记录；恢复分配后不展示旧原因。

### 订阅者客户编码与邮箱打码（v6.21.0+）

- 普通客户批量导入仅支持邮箱、姓名、客户编码映射；CSV（包括 ZIP 内 CSV）固定逗号分隔，XLSX 保持支持。导入 API 不再接受属性映射，也不再读取 `delim` 参数。覆盖用户信息仅更新姓名与客户编码，保留已有属性。选择一级公海列表时，同一入口切换到公海专用导入分支，不走普通客户写入流程。

- `customers.customer_code`（v6.21.0 迁移新增）：客户编码，必填但不唯一。仅管理端新增/编辑（`cmd/customers.go`）与导入路径（`internal/subimporter`）校验必填；公开订阅入口可选。列允许空串并带普通索引。
- `customer_lists.mask_emails`（v6.21.0 迁移新增）：客户列表级“打码邮箱”开关。无敏感数据访问权的查看者，在当前查看客户列表开启打码时看到打码邮箱；客户列表未开启或无上下文时维持原置空行为。打码覆盖客户列表/详情、API 响应及范围 CSV 导出，搜索仍按完整邮箱匹配。CSV 导出额外输出 `customer_code` 列。

### 一级公海与组织公海分配（已实施）

- 一级公海是独立的客户池类型，不等同于允许匿名订阅的 `public` 列表。公海联系人保存导入的客户编码（允许重复）、公司名称和真实邮箱；编码不承担唯一键职责。
- 一级公海列表本身固定为平台级 `global` 资源，组织投放权限单独保存在授权表；组织公海分配的业务归属由 `organization_id`/`organization_name` 表示，创建人字段仅用于审计和所有权校验。
- 公海导入统一使用 `POST /api/import/customers`：`customer_list_ids` 必须只包含一个一级 `pool` 列表，首个 CSV/XLSX 工作表必须能映射 `customer_code`、`name`、`email`、`allocation_department` 四列；兼容模板中的 `客户编号`/`客户编码`、`姓名`、`邮箱`、`分配部门`，其他列（例如注册名称、品牌、客户等级、`分表1`）只作为模板信息忽略。`email` 单元格支持多个地址，解析器从分号、逗号、换行、斜线、竖线分隔或附带备注的文本中提取地址，每个地址生成一条联系人记录并复用该源行的其他字段；重复地址仍由统一去重逻辑处理，无法解析的片段按原行号报告；统计与 100000 上限按展开后记录数计算。模板同时含“部门”和“分配部门”时，自动映射优先“分配部门”。`分配部门` 必须匹配一个启用中的 `organizations.name`；不存在或已归档的部门按行拒绝，不创建组织、不写入公海。若该组织已绑定该一级公海的公海分配，导入会自动写入该公海分配；若公海分配后创建，创建事务会回填已有的同部门联系人。
- 管理端 `/admin/customers/import` 用私域/公海两个页签明确区分导入路径；公海页签只对具备 `pools:master_manage` 的用户显示并只允许选择一个一级公海列表，选中后在同页复用 `PoolManager.vue` 为组织查看或创建绑定的公海分配。绑定界面为左侧可搜索组织及已绑定/未绑定状态、右侧当前组织的分配详情或创建表单；自动选择当前组织（个人工作区选择首个组织），分配名称按组织预填，切换组织保留各自名称草稿，创建后刷新左右状态。绑定加载失败时只提供重试，不展示创建表单。普通分配管理者只显示当前工作区组织，投放授权管理者可选择跨组织目标（创建还需分配管理权限），选择不会切换工作区。两种页签都可直接打开客户列表创建弹窗；保存后回到导入表单并自动选中新列表。带 `customer_list_id` 的导入深链在列表加载后自动切到对应页签，服务端权限和导入接口规则保持权威。
- 公海统一导入支持 `subscribe` 与 `blocklist` 两种模式，均使用四个必填列和可选回信邮箱列的模板及启用组织校验；黑名单按所选一级公海中的邮箱匹配（忽略大小写），匹配联系人及新联系人保存 `pool_contacts.status='blocklisted'`。v6.54.0 扩展状态约束且保留旧数据。联系人状态影响其所有组织分配和共享同一联系人记录的其他池，其他池独立记录不受影响。普通导入保留黑名单，并将所选池同邮箱的新身份记录标黑；组织移除/恢复不解除该状态。既有收件人解析、快照与投递领取均要求联系人 active，自动排除黑名单。导入结果与审计保存模式及去重后的黑名单联系人数量；页面显示黑名单状态。
- 一级公海文件导入、联系人创建/删除与清除邮箱由 `pools:master_manage` 控制，包括统一导入与普通列表导入一级公海。组织分配成员导入、分配、移除和恢复由 `pools:manage` 控制，不能更改主数据或投放授权。公海管理窗口只负责目标组织、投放授权和分配创建/绑定；主数据在独立客户页面维护。
- 公海分配仅保存一级公海联系人到组织的分配关系（不再保存回件邮箱），不复制联系人主数据。组织在公海分配手动移除联系人时，一级公海保留该联系人并显示该组织的逻辑剔除标记；该组织后续选择一级公海投放时也必须过滤该标记，其他组织不受影响。
- 公海和公海分配的客户计数及查看入口使用 `pool_members`/`org_pool_allocation_members` 专用查询；管理端不会把 `pool_contacts` 伪装成普通 `customers`，也不会让普通客户批量操作或导出路径接触公海数据。一级列表对组织用户只返回本组织已分配的安全 DTO，最高管理员可查看一级/二级完整记录；组织公海分配显示本组织的逻辑移除状态和原因，一级公海总表汇总各组织的移除记录，并向最高管理员展示对应组织、原因和可恢复的分配 ID。
- 公海联系人复用 `frontend/src/views/Customers.vue`，旧的 `PoolContacts.vue` 已删除。`/admin/pool` 展示所有可见一级公海的联系人并显示所属列表；`/admin/pool-lists/:id/contacts` 展示单个公海或组织公海分配的联系人。两种视图复用客户表格布局、服务端分页与排序、搜索、状态筛选和独立 CSV 导出；汇总页支持按来源公海执行已授权的跨公海批量操作，单列表按权限提供管理操作。数据仍保存在独立的 `pool_contacts` 中，普通客户批量操作与导出不接触公海数据。“归档无效联系人”只清除邮箱并标记 archived；一级公海联系人的永久删除要求 `pools:master_manage`，经 `DELETE /api/pools/:id/contacts/:contact_id` 执行，分配列表 ID 被拒绝。
- 当前管理端导航以“客户”为一级折叠菜单，二级顺序为“公海客户”、“公海客户列表”、“私域客户”、“私域客户列表”、“导入”、“退信”，表单保留在其后。`/admin/pool-lists` 仅展示一级 `pool` 与 `org_pool_allocation`，一级公海创建及公海分配管理入口均在此页；`/admin/customer-lists` 仅展示普通 `private`/`public` 列表，创建表单不提供 `pool` 类型。前端用 `GET /api/customer-lists?type_group=pool|private` 分组；`cmd/customer_list_filters.go` 统一解析分组，`queryReadableWorkspaceLists` 对跨工作区追加的已授权公海应用同一组搜索、类型、状态、订阅方式及标签条件，并在权限过滤后统一排序、分页，保证总数与列表一致。查询式批量删除复用该可见结果，再与可管理 ID 取交集；前端同时传递 `type_group` 与 `status`，防止跨页面或跨归档状态删除。不带参数的既有 API 行为不变。公海汇总页 `/admin/pool` 与公海单列表页 `/admin/pool-lists/:id/contacts` 都高亮“公海客户”；私域客户和私域列表使用各自路由。旧 `/admin/pool/:id` 重定向至汇总页，旧 `/admin/customers/pool-lists/:id` 重定向至公海单列表页。
- 列表类型是资源边界：`private` 与 `public` 可互相转换；一级 `pool` 和 `org_pool_allocation` 必须保留创建时的类型。`internal/core/workspace_resource_writes.go` 在持有列表行锁的更新事务内核验旧类型和请求类型，禁止通用 `PUT /api/customer-lists/:id` 将普通列表转成公海或公海分配，也禁止反向转换；公海分配只能走专用拆分事务。
- 一级公海和组织公海分配的客户视图使用下拉筛选「正常客户」与「已移除客户」，分别按 `status=active` 和 `status=removed` 请求；一级公海的已移除客户按联系人 ID 汇总所有组织未恢复的移除/剔除记录，未分配客户仍属正常，组织用户的一级公海视图只在本组织范围内分类。移除成员不会混入正常客户列表，已移除客户保留恢复分配入口；不带 `status` 的旧 API 请求仍返回全部联系人。
- 历史二级成员导入 API 仍保留兼容路由，但不再是管理端主入口，且其写操作与联系人维护都由服务端限制为最高管理员；统一客户导入接口是新增公海数据的唯一产品入口。组织统一回件邮箱由组织经理在组织工作区设置，或由具备 `organizations:platform_manage` 的平台组织操作员在管理组织页面选定目标组织后设置（`PUT /api/organizations/:id/reply-mailbox`）。
- 每个“一级公海 × 组织”至多绑定一个分配。`CreateOrgPoolAllocation` 要求 `pools:manage`；普通成员可在当前组织已有投放授权下创建分配，组织身份不自动授予动作权限。跨组织创建还需 `pools:delivery_manage`，该路径可在事务内创建授权；普通分配管理不得借创建新增或恢复授权。Core 锁定目标组织后重验活动状态、成员与授权。创建分配不携带回信邮箱。
- 公海权限自 v6.56.0 分为查看 `pools:get`、组织分配 `pools:manage`、主数据维护 `pools:master_manage`、投放授权 `pools:delivery_manage` 和导出 `pools:export`，互不隐含。普通查看/导出限本组织分配；主数据权限只扩大平台一级池范围，委派者仍使用脱敏 DTO。授权管理不授予客户数据或组织成员身份。最高管理员保留功能豁免，迁移不向普通角色授予主数据或投放授权。
- 分配与主数据维护独立：恢复、分配、移除和分配成员导入要求 `pools:manage`，普通调用者只操作本组织分配；联系人新增、清邮、删除与统一导入要求 `pools:master_manage`。组织经理也需明确授权。统一回信邮箱配置要求 `mailboxes:manage` 与组织经理/平台组织管理范围，分配级配置端点已删除。历史脚本断言须按功能授权解释，不能由 manager 身份推导写权限。
- 公海客户导航固定进入 `/admin/pool` 汇总页；`GET /api/pools/contacts` 与导出接口在服务端按一级公海成员关系跨列表搜索、状态、来源 `pool_id` 和精确分配部门过滤，再统一排序和分页，每行返回来源 `pool_id`/`pool_name`。`GET /api/pools/contacts/filters` 按同一组织边界返回可选公海/部门组合，不依赖当前页数据。同一联系人属于两个公海时显示两行，不合并来源；平台管理员可看全部公海，组织用户只看本组织分配内的成员且邮箱继续脱敏。点击“所属公海列表”进入 `/admin/pool-lists/:id/contacts` 单列表管理页，并保持“公海客户”导航高亮；公海客户列表计数和导入结果也指向该路由。旧 `/admin/pool/:id` 入口重定向汇总页，旧 `/admin/customers/pool-lists/:id` 和指向公海列表的通用客户列表路由重定向至单列表管理页。
- 投放授权管理者使用独立组织目录选择跨组织目标，查看分配并授予/撤销投放授权；普通分配管理者固定当前组织。创建请求不切换工作区、不新增成员关系。分配不能经通用列表表单创建，也不存在二级合并一级流程。弹窗不配置邮箱；组织经理或平台组织操作员配置邮箱时另需 `mailboxes:manage`。
- 活动只允许选择一级公海作为受众，服务端按活动范围解析组织公海分配。创建、保存与测试发送均通过 `splitCampaignAudienceIDs` 区分公海和私域列表：公海走投放授权/组织分配，不能按普通列表要求属于当前工作区或拥有主数据维护权限；私域仍执行工作区与列表权限校验。测试发送取已保存的不可变 `pool_scope`，请求不能扩大或缩小范围；全组织活动仍拒绝私域及显式分配列表。测试收件人仍由 `GetManagedWorkspaceCustomersByEmails` 限制为当前工作区可管理的客户，不启动公海批量投递。v6.55.0 新增 `pool_contacts.reply_to` 与 `campaigns.pool_reply_priority`：默认 `contact_first`（客户回信邮箱 → 组织统一回信邮箱），可选 `organization_first`（组织 → 客户），逐客户使用第一个可用地址；组织地址必须 active，AI 模式还需通过收件连接验证，普通地址无需收件凭据。公海受众不使用活动级或个人回信邮箱。回信地址是内部路由地址，在公海、分配和 CSV 中展示；客户收件地址继续脱敏。
- 公海受众在预览/发送被阻断时，错误信息必须可自查：`ValidatePoolCampaignAudience`（`internal/core/pools.go`）先按当前配置刷新路由，再逐条列出未解析受众的完整解析链 `pool list "<公海列表>" -> organization allocation "<组织公海分配>" (organization "<组织>")` 与首个失败条件（无目标组织 / 该组织未绑定公海分配 / 组织未配置统一回件邮箱 / 该邮箱已停用或 AI 收件连接未验证），并以 `Fix: ` 给出可照做步骤（先在“客户列表 → 公海管理”绑定该组织的公海分配（如需要），再由该组织经理在“管理组织 → 组织回信邮箱”保存一个可用回信地址作为组织统一回件邮箱，AI 收件连接须验证，最后重试预览/发送）；活动编辑页对未解析受众只读展示同一结论，不提供任何回信配置操作。该诊断只报告，不改变解析规则：公海收件人的 Reply-To 始终取投递快照中按活动优先级解析的客户回信邮箱或组织回退地址（`internal/manager/manager.go` 仅对公海收件人使用 `campaign_pool_recipients.reply_mailbox_id`），仅公海受众隐藏活动级邮箱；混选公海与私域时保留该字段，私域逐收件人保存实际回信地址，公海仍使用独立快照。
- 公海投递快照使用 `campaign_pool_recipients` 与联系人内部 ID 去重；公海退订、退信和回复 AI 事件写入 `org_pool_allocation_exclusions` 的组织维度逻辑状态，并在 `bounces`/`reply_ai_events` 保留来源池、公海分配和组织字段，禁止改变一级主数据或其他组织分配。收件人判定（活跃公海联系人 × 有效二级分配 × 本组织未剔除）只在 `internal/core/pools.go` 的 `poolRecipientMembershipSQL` 定义一次，一级解析、二级解析与快照写入共用同一片段，因此三条路径不可能给出不同收件人集合。快照刷新采用 `DO UPDATE` 并清理本组织范围内、已不再可投递且尚未交给投递的 `pending`/`deferred` 行；已 `queued`/`sent`/`cancelled` 的行属于投递历史，不重写也不删除，退队路径另按 `org_pool_allocation_exclusions` 重查剔除。
- 邮件打开像素与点击链接的公开 URL 可以携带普通客户或公海联系人的 UUID；`resolve_campaign_tracking_recipient` 必须先验证活动收件人快照，点击还要验证链接属于活动。`campaign_views`/`link_clicks` 用互斥的 `customer_id`、`pool_contact_id` 保存事件，匿名聚合模式继续写两者均为空的事件。公海成功投递写 `campaign_pool_recipients.sent_at`，迁移 v6.46.0 将旧 `sent` 行的 `updated_at` 作为近似历史时间回填。活动和工作区报表的发送分母、唯一打开与点击人数均纳入公海；收件人明细还要求 `campaigns:recipients`、普通客户查看权限及公海行的 `pools:get`，非平台管理员只能看到本组织公海行，邮箱必须脱敏且不得返回公海 UUID。
- 打开像素在启用跟踪时由 `cmd/geoip.go` 使用本地 GeoIP2 City 数据库对请求 IP 做一次近似定位，`campaign_views` 只保存国家代码、国家、地区、城市和近似经纬度，不保存原始 IP；数据库未配置或查无结果时地理字段为空。迁移 v6.47.0 为旧库增加这些字段，历史事件无法补定位。`GET /api/campaigns/:id/report/geo` 与 `/api/campaigns/report/geo` 在工作区可读活动范围内按日期汇总，`located_opens` 为有坐标的打开数，`unknown_opens` 为其余打开数。管理端以本地全球边界数据及 ECharts 绘制城市热力图；邮件服务商或代理代取像素时，位置可能指向代理而非收件人。
- `CampaignGeoHeatmap.vue` 的世界底图与定位数据独立显示：加载、没有定位点、未配置 GeoIP 或请求失败时仍保留地图和相应状态提示；只有有效定位点才显示热力色标。刷新为空或失败会清除旧热力点，跨活动统计与单活动报告共用这一组件。
- 地理报告支持全球/国家切换、城市标记及明细、点击城市聚焦和重置视图。`frontend/src/utils/campaignGeo.js` 将 ISO 国家码与本地世界边界对应，按国家、地区、城市聚合次数；同名异地城市不合并，没有城市名明确显示未知。国家图聚焦主要陆地及当前定位点，跨日期变更线的坐标展开到同一范围；没有独立边界的国家/地区使用世界底图上的坐标范围。跨活动报告刷新时以 `country` URL 参数保留选择，条件未变也重新请求报告。筛选仅消费当前报告已授权的聚合，不发起外部地图请求、不扩展 API 或数据权限。缺少定位的历史打开不能补出城市，城市坐标仍是 GeoIP 近似值。
- 客户回复、退订和投诉只能对实际投递来源组织的二级分配执行逻辑剔除，保留一级主数据和历史快照；最高管理员可跨组织审计，组织用户只能看本组织安全字段。

### 独立组织营销 SMTP 与活动发件来源

管理端入口 `frontend/index.html` 的 favicon、custom.css 和 custom.js 使用 `/admin/` 绝对路径，避免嵌套组织页刷新时将回退 HTML 当脚本加载。组织营销配置位于 `frontend/src/views/organizations/ManageOrganizations.vue` 的独立 SMTP 标签，复用带组织 owner 参数的 `PersonalSMTPSettings.vue`。

自 v6.50.0 起，“管理组织”可配置一个或多个组织自有营销 SMTP，与个人 SMTP、系统通知 SMTP 独立。`user_smtp_servers` 复用连接字段与额度存储，但 `user_id` / `organization_id` 必须且只能存在一个；组织行不附着任何成员账号。迁移保留所有个人行、UUID、密码与使用量，旧活动的 `campaigns.smtp_source` 默认 `personal`。

自 v6.52.0 起，组织 SMTP 再按组织划分为多个发件池（`organization_smtp_pools`）。每个组织可创建、重命名和删除空池；SMTP 行通过 `smtp_pool_id` 严格归属一个同组织池，旧组织 SMTP 和组织来源活动迁移到“默认发件池”。活动保存 `campaigns.smtp_pool_id`，组织轮询只读取该池；删除有 SMTP 或活动引用的池会被拒绝。

全组织公海活动不绑定单个 `smtp_pool_id`：组织来源读取每个目标组织在组织管理中配置的全部启用 SMTP，覆盖该组织的各发件池；个人来源只读取该目标组织有效成员的个人 SMTP，两种来源不混用。`Campaign.vue` 在全组织/个人来源模式清空隐藏的池选择，并在创建、保存、测试及概览请求中按实际范围规范化池 ID；单组织池选择在加载完成前禁用。

- `cmd/organization_smtp.go` 的 `/api/organizations/smtp` GET/PUT、`/:id` DELETE、`/test` POST 由组织经理或平台组织管理权限授权，归档组织不可编辑。内部将正账号 ID / 负组织 ID 作为可信 owner key 复用经过校验的保存、删除、连接测试与使用量查询；数据库以独占 owner CHECK 和各 owner 名称唯一索引约束隔离。批量保存同时锁住 owner 行与 SMTP 行，防止空池并发覆盖；密码空值或掩码保留旧值，客户端不能改变 UUID 或归属。
- 活动保存 `smtp_source=personal|organization`，个人工作区不可选择组织来源（全组织公海范围除外）。个人来源轮询活动所有者的启用 SMTP，组织来源轮询活动所属组织选定发件池的独立 SMTP。组织缓存以负池 ID 与账号缓存分开，共用发送读写锁；配置变更关闭并失效对应池及所有公海单发件池，后续消息重新解析。没有可用池时暂停/回草稿，额度用尽时延迟；不回退到其他来源或系统 SMTP。移出已归档组织到个人工作区的活动重置个人来源；克隆至组织保留来源、克隆至个人重置个人来源。
- `GET /api/campaigns/smtp-overview?source=...`（新建）与 `GET /api/campaigns/:id/smtp-overview?source=...`（已有活动）返回启用的发件邮箱、名称、今日使用量、每日额度，绝不查询/返回 host、用户名、密码或连接设置。普通组织成员可按活动权限读取；已有活动使用活动 owner/组织，而不是查看者的 SMTP。新建全组织公海预览还传 `pool_scope=all_organizations` 和逗号分隔的 `customer_list_ids`，服务端验证专用发送权限及受众可用范围。
- 全组织公海活动选组织来源时，准备检查、分配器、剩余容量和概览都取目标组织的自有 SMTP；选个人来源保留历史的“目标组织有效成员的个人 SMTP”语义。组织发件快照的 `sender_user_id` 为空，`sender_smtp_uuid`/from 快照仍保留。系统通知与事务邮件路由保持原有所有权；两种营销来源都应用 `smtp_delivery` 的单 SMTP 性能、随机延迟及邮件头，TLS 取各 SMTP 自身配置。

### 平台级公海营销（全量组织受众 + 组织 SMTP 池轮询，已实施）

平台设置自 v6.49.0 起只保留一个用于系统通知的 SMTP；营销活动使用账号或组织发件池，事务邮件仍使用账号 SMTP。`settings.smtp_delivery` 统一定义每个 SMTP 的连接数、重试、超时、随机延迟和邮件头：`cmd/manager_store.go` 每次解析时应用它，系统通知 SMTP 保存时同步这些字段。TLS 协议和证书校验属于各 SMTP 的连接配置。v6.53.0 将旧全局 TLS 的实际生效值复制到各 SMTP，转换延迟单位，并回填活动频率；后续迁移重跑不覆盖单 SMTP TLS 或活动频率修改。设置保存后需重启，运行中的活动按既有规则延迟重启。

统一投递配置的 `send_delay_min` / `send_delay_max` 是整数毫秒，满足 `0 ≤ 下限 ≤ 上限 ≤ 3600000`；两者为 0 时关闭，兼容读取旧 `2s` 等时长字符串并按原时长转换。每封 SMTP 邮件（包括首封）发送前按整数毫秒均匀抽样。`internal/messenger/email/send_delay.go` 在进程内按 SMTP UUID 共享可取消的发送门与连接容量，避免同一 SMTP 被多个缓存池重复放大并发；开启随机延迟时同 SMTP 依次等待并投递，不同 SMTP 可并行。网络重试属于同一发送尝试，不重新抽样。暂停/取消或关闭可中断等待，释放 SMTP 每日额度预占；连接测试跳过随机延迟。

投递限速分三层：`campaigns.smtp_rate_limit` 先约束单活动所有 SMTP 的合计发送频率（封/分钟），个人来源默认 20、组织来源默认 100，用户可设 1–1000000；以活动 UUID 共享平滑间隔，全组织公海也共用该活动额度。平台 `app.message_rate` 是所有 SMTP 合计的每秒硬上限，滑动窗口同样为平台总量；`app.concurrency` 同时控制工作线程数和所有 SMTP 的实际投递总并发。超限等待、不丢弃，线程数不乘大发送频率。`internal/messenger/email/delivery_limiter.go` 在实际 SMTP Send 前统一取得活动、平台频率及并发许可，完成或失败时释放并发许可并唤醒队列；`internal/manager/manager.go` 给系统通知、个人/组织缓存池、公海单发件池及临时连接测试挂同一 limiter，因此直接发送的系统通知也受平台限制。单 SMTP 连接、重试、超时和随机延迟仍独立生效。所有限速与发送门为进程内状态，重启重置，多个活动工作进程之间不共享；需要平台合计上限时采用一个活动发送进程。

- 营销活动勾选一级公海时，默认仍是单组织范围（`campaigns.pool_scope = 'organization'`，按当前工作区组织的公海分配解析）。持有专用权限 `campaigns:public_pool_send` 的账号可以把活动创建为 `all_organizations`：受众是该一级公海下**所有活跃组织**的公海分配并集，只接受一级公海列表（显式公海分配列表与普通客户列表都会被拒绝），权限常量在 `internal/auth/models.go`、清单在 `permissions.json`（`campaigns` 组），前端以 `$can('campaigns:public_pool_send')` 控制入口。
- 组织工作区的新活动通过“公海发送范围”显式选择本组织或全部目标组织，默认本组织；选择全部目标组织后禁止混选私域列表。个人工作区仅允许纯公海受众进入全组织路径。已有活动范围不可修改。发送条件状态绑定已保存活动的发件来源、受众 ID 集合及回信优先级；编辑后隐藏旧状态并提示保存，成功保存后重新检查，异步旧响应不能覆盖新状态。已有全组织活动的 SMTP 概览也按表单传入的 `customer_list_ids` 解析，省略该参数才使用已保存受众。
- 多公海活动的组织集合取全部选中公海分配的并集。就绪接口按组织去重、对同组织每个分配的回信条件取逻辑与；发送校验逐公海/组织报告问题，任何未绑定组织的选中公海均阻止发送。组织轮询保留既有顺序并追加新目标组织（包括旧活动此前漏掉的目标组织），不重排已有组织；邮箱概览、校验、调度和剩余额度使用同一受众范围。
- 收件人解析在 `internal/core/pools.go` 的全组织规则中保持活跃成员、联系人和剔除边界，并按持久化组织顺序去重。单组织和全组织快照共用 `poolReplySnapshotRouteSQL`：写入实际 `reply_to_snapshot`、`reply_to_source`，仅当实际地址匹配目标组织启用的邮箱时记录该 `reply_mailbox_id`，AI 模式还需收件连接已验证，外部地址不得归到回退或其他组织邮箱。关联普通地址 ID 不会自动开启收信。`pending`/`deferred` 允许刷新，`queued`/`sent`/`cancelled` 路由保持投递历史；发送队列与分配器只读取地址快照。
- 组织顺序持久化在 `campaign_pool_org_orders`：活动首次被调度（`cmd/manager_store.go` 的分配器）时按活跃分配组织随机打散写入，不因暂停/重启/次日续发而重排；不再持有该公海分配的组织其顺序行会被清理。`campaigns.pool_next_org_index` 保存轮转位置，分配器每领取一封就推进一次，因此并发批次、多 worker 与恢复后的活动继续轮转。
- 发件侧不再使用活动所有者的个人 SMTP：分配器在同一个数据库事务里按“组织轮转 → 组织内稳定顺序（user_id, smtp id）”领取收件人，并用组织级持久化游标 `org_pool_smtp_cursors.next_smtp_uuid`（`FOR UPDATE` 行锁）选择发件 SMTP，同时把 `sender_smtp_uuid`/`sender_user_id`/`sender_from_snapshot`/`sender_assigned_at` 写入投递快照。组织池成员动态过滤（组织 active、`organization_members.removed_at IS NULL`、用户 enabled、SMTP enabled），因此成员离组、账号停用、SMTP 停用立即生效。已分配且仍可用（在池中且有剩余额度）的发件人会被复用，使失败重试不跨账号。
- 发送链路按已分配的 SMTP UUID 解析：`manager.Config.PoolSMTP`（`cmd/init.go` 注入，`cmd/manager_store.go` 的 `GetPoolSMTPServerByUUID` 在每次缓存未命中时重新校验账户状态）返回**单服务器** `email.Emailer`，`resolveMessenger` 优先于 owner 路径按 UUID 解析，缓存与失效复用 `personalSMTPSendMut`/`personalSMTPMut` 锁；SMTP 配置变更（`WithPersonalSMTPUpdate`）、成员移除、账号启停都会失效组织池缓存。发送失败仍按既有语义把收件人置回 `pending` 并保留发件人分配；`ErrPoolSMTPUnavailable`（组织结构性缺少可用 SMTP）暂停活动，`ErrSMTPQuotaExceeded` 按既有 `daily_resume_time` 延迟。
- 额度语义保持不变并叠加：活动 `daily_send_limit` 仍是硬上限，每个 SMTP 行的 `daily_limit` 在所有公海活动之间共享；`get-campaign-pool-smtp-remaining` 只提供批量大小的建议值，最终上限由 `smtpQuotaTracker.ReserveServer` 在发送时原子预占。
- 校验与状态：`ValidatePoolCampaignAudience` 与全组织发送状态逐组织检查活跃分配、完整回信路由以及 SMTP。客户都有导入地址时可以不配置组织邮箱；无客户地址的有效收件人必须有组织回退，否则阻止预览/发送；空分配保留组织邮箱配置检查。`GetCampaignPoolSendStatus` 复用相同路由就绪规则，不返回凭据。
- 与发送快照相关的既有投影同步放宽：`campaign_send_counts`、`has-campaign-recipients`、`queue-campaign-pool-customers` 对 `all_organizations` 活动不再要求池行所属组织等于活动组织，因此未发送计数、完成判定与调度器读取的收件人集合在两条部署路径下一致。
- 迁移 `internal/migrations/v6.44.0.go`（`cmd/upgrade.go` 注册）幂等新增 `campaigns.pool_scope`/`pool_next_org_index`、`campaign_pool_recipients` 的四个发件快照列、`campaign_pool_org_orders`、`org_pool_smtp_cursors`，并以新谓词重建 `campaign_send_counts`；对全新 `schema.sql` 库为 no-op。

公海客户汇总页与单公海列表共用 `frontend/src/views/Customers.vue` 的勾选和批量操作：跨公海分配按来源公海分别选择目标分配，组织移除/恢复按当前组织的公海分配解析，归档请求携带各行来源公海 ID；永久删除仅对平台管理员开放，并按联系人 ID 去重（同一联系人可以属于多个公海）。公海 CSV 的重复 `contact` 参数只缩小已授权查询结果，汇总导出按 `(pool_id, contact_id)` 匹配，单列表导出使用 `0:<contact_id>`；省略参数保持全量筛选导出，非平台管理员仍返回脱敏 DTO。私域与公海的工具栏、弹窗和确认文案使用语言键。

营销活动配置页 `frontend/src/views/Campaign.vue` 按基本信息、收件客户、发件与回信、发送安排分组，桌面双栏、窄屏顺序堆叠。详细规则和标签/发送渠道/邮件头使用可展开区域，测试发送只在活动保存后出现。公海回信优先级的界面名称为“列表回信邮箱 → 组织回信邮箱”及其反向顺序；列表来源仍逐条读取导入记录的 `reply_to`，`contact_first` / `organization_first` 和后端路由契约保持兼容。条件回信控件使用独立组件 key，防止受众切换时沿用旧邮箱值。

## AI 入站回信处理

回信邮箱配置分为“回信地址”和“AI 收件连接”。`ai_enabled=false` 时只需邮箱地址，保存即为 `active`、`verified_at` 保持空值，不要求收件账号和密码；营销活动与公海统一回信路由认可该普通地址。开启 AI 时才要求收件配置与已保存或新输入的密码，未验证的连接为 `pending`；后台查询另要求 `verified_at IS NOT NULL`。关闭 AI 保留已有收件配置，恢复普通地址使用；停用和离组保留状态不能通过保存隐式重启。读取仅返回 `has_password`，不会返回密码。

`POST /api/profile/reply-mailboxes/test` 只接受已保存邮箱 ID，按所有者和工作区加载凭据并测试，拒绝地址模式；页面有未保存修改时禁用测试按钮。成功写入验证状态前比较数据库连接快照，遇到并发修改返回 409，避免旧连接测试验证新配置。测试成功保留 `disabled`/`retained` 生命周期，停用邮箱仍需显式启用，前端保持相同状态。来源：`cmd/reply_mailboxes.go`、`queries/replies.sql`、`frontend/src/components/ReplyMailboxSettings.vue`。

`reply_ai.scan_interval` 同时控制 AI 扫描和离组邮箱转发，默认 `60s`，范围 `10s`–`24h`；`models.ReplyScanInterval` 统一解析，设置保存与启动均校验，旧设置缺失字段自动兜底，不需要迁移。设置页“回信邮箱检查间隔”可在 AI 关闭时编辑，生效沿用自动 reload/发送活动延迟重启流程。离组转发仅扫描保留且具备已验证连接和密码的邮箱，纯地址邮箱无法由系统代收或转发。

退信设置的 `POST /api/settings/bounce/mailbox/test` 复用 `settings:manage`，对未保存表单执行连接/登录/读取/解析四步只读检测，最多读取当前 POP 会话最高序号的一封邮件（30 秒、5 MiB 上限），不调用 `Scan`、不删除邮件、不入队或改变客户状态。`internal/bounce/mailbox/test.go` 负责连接诊断，`preview.go` 负责外层摘要与 DSN 失败收件人解析。`bounce.mailboxes[].starttls` 默认 false，与 `tls_enabled` 互斥；后台扫描也支持 STLS，旧配置保持原行为，无数据库迁移。接口和日期/识别语义见 `docs/docs/content/bounces.md`。

全局 `reply_ai` 设置保存 OpenAI 兼容接口的端点、模型、密钥、超时和最低置信度；密钥在读取设置时打码，更新时空值表示保留。每个客户回信邮箱还必须显式启用 AI 处理，避免将未选择的邮箱内容发送到第三方模型。

对接聚合网关（new-api、one-api、LiteLLM 等）时模型清单由网关决定，因此设置页提供两个只读探测端点：`POST /api/settings/reply-ai/models`（`GET {root}/models`，根地址未带 `/v1` 时回退尝试 `/v1/models`，返回模型清单、是否可对话的提示和实际生效的根地址）与 `POST /api/settings/reply-ai/test`（依次执行配置校验、网关连通、模型是否在清单中、真实分类往返四步，返回 `success`/`warning`/`failed` 与每步原因码）。两者都接受未保存的表单值，`api_key` 为空或全掩码时复用已保存密钥，密钥只出现在出站 `Authorization` 头中，不落库、不回显、不入日志；探测始终使用已配置的根地址发起分类调用，网关只在 `/v1` 上响应时通过 `suggested_base_url` 提示操作者改正地址，而不是静默替换。实现见 `internal/replyai/gateway.go` 与 `cmd/reply_ai_settings.go`，两者共用 `internal/replyai/client.go` 的提示词与响应校验。

后台 worker 使用真实 IMAP 非破坏性轮询，连接原始主机/端口及 TLS 设置，以 EXAMINE 选择配置文件夹，以 BODY.PEEK 读取正文，不标已读、不删除消息。`reply_mailbox_scan_cursors` 按邮箱和消费者保存连接配置摘要、UIDVALIDITY 和最后入库 UID，AI 从最旧未处理 UID 每轮推进最多 200 封，重启延续进度；UIDVALIDITY 或连接配置变化重置进度，持久队列继续去重。单封读取或入库失败不推进该消息；超过 5 MiB 的邮件保存 `message_too_large` 忽略记录并报告扫描异常，再推进后续消息。固定 4 个邮箱 worker、30 秒空闲读写超时、2 分钟整轮超时与 socket 强制关闭保持隔离；`cmd/reply_imap.go` 的连接测试和扫描共用同一实现。

格式损坏且无法解析的邮件先持久化 `malformed_message` 忽略事件，再推进 UID，不让单封永久坏信阻塞后续回信；该记录入库失败仍重试原 UID，不保存原文。回归同时验证后续正常消息和重复扫描去重。

回复来源由 `internal/core/reply_ai_delivery.go` 在实际收件邮箱内同时查私域与公海的 sent 投递记录，以投递邮箱快照匹配发件人。组织共享邮箱可解析其他成员的真实投递，不能把邮箱创建者当作客户所有者。新发邮件设置由活动 UUID 和私域/公海收件人 ID 组成的 Message-ID；入库保存 In-Reply-To 与 References，优先解析实际引用，无引用的历史邮件仅在来源唯一时处理。任何未发送、错误邮箱、跨组织或歧义来源均忽略。私域保存实际 Reply-To 快照；历史 NULL 快照仅以已有活动邮箱关联作保守回退，迁移不伪造旧地址。

AI 调用前重验全局开关；副作用事务按组织、设置、事件、邮箱和资源边界重验全局与逐邮箱 AI 开关、邮箱验证状态、所有者启用状态、组织/成员资格、sent 来源及租约。私域使用实际客户当前所有者的工作区事务；公海锁定组织和来源后只写实际组织排除。平台管理员配置的组织邮箱不要求管理员额外加入组织，但组织仍须活动。停用保留 AI 配置意图；启用只恢复运行状态。私域活动启动/排期、调度和实际发送均检查邮箱当前可用性，发送期间失效立即暂停并保留待发送记录。

模型返回 `unsubscribe`、`complaint`、`product_complaint` 或 `other`，仅明确退订/营销投诉且达到置信度时动作。自动邮件、低置信度和其余意图只保留审计；正文视作不可信数据，终态清除正文。迁移 `v6.57.0` 增加私域地址快照、邮件引用字段和 IMAP 进度表，幂等且不覆写历史/扫描进度。回归：`cmd/{reply_chain_regression,reply_imap,reply_mailbox_test_connection}_test.go`、`internal/core/reply_ai_lease_test.go`、`internal/manager/pool_reply_test.go`、`internal/migrations/v6.57.0_test.go`。

明确退订和明确垃圾/滥用投诉均在同一工作区将匹配客户设为 `blocklisted` 并退订其全部名单；垃圾/滥用投诉另外以 `source=reply_ai` 写入 `bounces`，保留事件 ID、模型、置信度和原因代码。产品/服务投诉只作为非动作分类留存。处理事务会再次锁定并验证工作区、成员资格和客户所有权；队列租约与唯一事件键保证重试不会重复投诉计数或跨工作区操作。

## 客户回信转发

`cmd/reply_forwarder.go` 以相同 IMAP 连接和只读文件夹设置非破坏性轮询保留（`retained`）回信邮箱，把客户回信转发到规则目标地址，源邮件永不删除；转发每轮重访源消息以保留持久化重试/退避语义，不使用 AI 的增量消费进度。去重键 `reply_forward_messages(rule_id, message_key)`（Message-ID + 原文哈希）保证同一封回信只转发一次：每轮扫描先用单条 `INSERT ... ON CONFLICT DO UPDATE` 抢占该行（置 `pending`、递增 `attempts`），抢占提交后才投递，投递返回后才写终态，因此 `forwarded` 行永不会被再次抢占，而抢占行本身就是租约。

投递失败（队列积压、管理器关闭或重启）记为 `failed`，下一轮按 `replyForwardRetryBackoff` 起的指数退避重试，直到 `replyForwardMaxAttempts`（默认 5 次）；到达上限后保持 `failed` 且不再重试，成为可查询的终态（`status = 'failed' AND attempts >= 上限`）并写日志，避免永久不可达的回信被无限重试或被静默吞掉。抢占后进程退出的行仍是 `pending`，只有超过 `replyForwardClaimLease`（默认 5 分钟）才会被下一轮接管；租约远长于单次投递（`PushMessage` 3 秒超时），所以租约过期只可能意味着持有者已死，从而在不重复投递的前提下恢复中断的转发。状态机与终态查询同时写在 `schema.sql` 的表注释里，未新增列、不需要迁移；回归测试见 `cmd/reply_forwarder_test.go`。

## 构建、测试与开发

### OpenClaw 营销技能

`skills/listmonk-openclaw-marketing/` 是外部自动化工具集，不进入产品前后端构建。
`SKILL.md` 提供任务路由与工作流，`references/api-reference.md` 按实际
`cmd/handlers.go`、`cmd/api_keys.go` 和处理器维护参数、scope、返回结构与工作区边界；
`references/rest-workflow.md` 维护 CLI 参数和操作示例。个人 Key 可读跨活动报表，
但仍不能使用 dashboard、邮箱配置、pools 或 org-pool-allocations 路径。
Python 草稿创建支持显式 SMTP 来源/组织池/频率与回信邮箱，内容蓝本不继承这些发送配置；
报表支持单活动、多个 ID 或工作区授权集合、可选 geo 及指定收件人页。
测试运行 `python -m pytest skills/listmonk-openclaw-marketing/tests -q`；不需要真实 Key 或发送邮件。

在 Bash 兼容终端运行以下命令：

| 命令 | 用途 |
| --- | --- |
| `make build` | 编译后端为 `./listmonk`。 |
| `make build-frontend` | 构建 Vue 管理端与邮件编辑器。 |
| `make dist` | 构建前后端并将运行资源嵌入单一二进制。 |
| `make run` / `make run-frontend` | 通用的 Go 后端与端口 8080 Vite 前端入口。 |
| `make test` | 运行全部 Go 单元测试（`go test ./...`）。 |
| `cd frontend && yarn lint` | 执行 Vue/JavaScript ESLint。 |
| `node dev/run-cypress.js --spec cypress/e2e/customer-lists.cy.js` | 在独立的 `listmonk-cypress` Compose 项目运行端到端测试；临时 PostgreSQL 与开发数据库隔离，重置任务只接受 9273 测试服务。 |
| `make init-dev-local`、`make dev-middleware` | 启动 Docker 中的 PostgreSQL、MailHog、Adminer，并用 `dev/config.local.toml` 初始化本机开发库。 |
| `make run-backend-local`、`make run-frontend-local` | 分别用 Air 在主机热更新 Go（`http://localhost:9173`）和用 Vite 在主机热更新 Vue（`http://localhost:8181/admin/`）。 |
| `dev/start-middleware.ps1`、`dev/start-backend.ps1`、`dev/start-frontend.ps1` | Windows 下分别启动 Docker 中间件、本机 Go/Air 与本机 Node.js/Vite；后两者在各自终端持续运行。 |
| `make init-dev-docker`、`make dev-docker` | 运行兼容的全 Docker 开发套件；默认本机热更新流程不调用这些目标。 |
| `make rm-dev-docker` | 删除开发容器及其数据库卷，数据不可恢复。 |

Go 测试放在实现附近的 `*_test.go`；修改工作区、权限、导入、发送或迁移行为时，必须补充对应的边界测试。前端可见行为变化应更新 `frontend/cypress/e2e/` 测试。

## 发布与部署脚本

- v6.23.0 术语迁移在重命名统计物化视图后同步重命名输出列，再创建索引；兼容旧 `list_id/subscriber_count` 并保留数据。迁移回归测试使用 `MIGRATION_TEST_DSN` 指向测试 PostgreSQL，创建并清理独立测试 schema，覆盖重复执行。

- 根目录 `docker-compose.yml` 是常规 Compose 部署：应用启动时幂等安装、执行迁移，再开始服务；所有密钥使用运行环境变量或 `LISTMONK_*_FILE`，不可提交真实配置。
- `deploy/package_bundle.sh` 先执行 `make dist`，打包官方镜像、本地镜像、PostgreSQL 镜像与 Compose 脚本。将生成包复制到目标机后，依次运行包内 `scripts/load-images.sh`，配置 `env/runtime.env`，再运行 `start-online.sh` 或 `start-local.sh`；`stop.sh` 用于停止。
- `Jenkinsfile` 执行 `make test` 和 `make dist`，归档二进制、前端压缩包及 SHA-256，再通过 SSH 和 systemd 进行可回滚部署。Jenkins 发布将 filesystem 媒体固定保存在 `<DEPLOY_DIR>/uploads`（版本目录之外），每个版本的 `uploads` 都链接到该目录；首次修复发布会从旧 `current/uploads` 迁移历史文件。`listmonk@.service` 是强化隔离的模板，`listmonk-simple.service` 兼容旧系统。
- GitHub Actions 的 PR build-sanity 执行 `make dist`；`docs-sanity` 在 PR 的每次 push（`docs/**` 或 `scripts/check_docs.py` 变化时触发）执行 `python scripts/check_docs.py`（校验 nav 可达性、页内锚点与相对链接）和 `mkdocs build --strict --clean`；标签 `v*` 使用 GoReleaser 发布多架构二进制与 Docker 镜像，nightly 工作流发布每日快照。

## 文档同步规则（强制）

提交前逐项检查：目录职责或模块移动更新本文；路由、数据模型、工作区/权限语义变化更新本文及 `docs/docs/content/roles-and-permissions.md` 或相关 API 文档；构建、测试、CI、Docker、Jenkins 或部署脚本变化更新本文、`docs/docs/content/developer-setup.md` 和/或 `deploy/README.md`。不得保留与代码、`Makefile`、Compose 或流水线不一致的命令、端口、服务名、权限描述或示例。

提交文档改动前运行 `python scripts/check_docs.py`：它校验 nav 可达性、页内锚点和相对链接——这三类漂移 `mkdocs build --strict` 不会报告（nav 未引用的页面只提示 INFO，失效锚点与失效相对链接不检查）。


## 模板称呼兜底（v6.29.0）

`templates.name_fallback` 是模板所属工作区内的 JSON 配置（enabled/value/invalid_values）；`models.NameFallback` 负责校验及完整值匹配。模板更新在原工作区事务内写入，API 未传字段保留已有配置。`internal/manager/message.go` 和 `models/messages.go` 在渲染用的客户副本上应用规则，不改变数据库、投递信封或追踪对象。活动查询按与 template_body 相同的授权条件读取 template_name_fallback；HTML 活动随模板加载，visual 活动使用 `campaigns.name_fallback` 导入快照。模板/活动复制、工作区迁移复制保留配置。初始 schema 与 v6.29.0 幂等迁移均默认 `{}`，保持旧行为。入口为 `TemplateForm.vue` / `NameFallbackSettings.vue`，预览接受未保存规则和空姓名样本。
