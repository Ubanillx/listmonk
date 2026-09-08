# 工作状态

快照日期：2026-09-07

- 公海二级列表模板下载与单条维护表格体验优化（2026-09-07）：管理弹窗的“下载模板”现在生成 `pool-segment-allocation-templates.zip`，同时包含 CSV 与 XLSX 两份 `customer_code,email` 模板；单条维护改为工具栏 + 结果计数 + 状态/组织剔除标签的后台表格，状态统一显示“正常 / 已归档 / 已移除”，空结果使用固定高度空状态，邮箱列省略显示；不再在弹窗打开时全量读取联系人，必须先按客户编码查询。验证：`cd frontend && yarn lint && yarn build`；浏览器实际打开公海管理页，已确认目标组织的二级列表中显示新版工具栏、3 条查询结果和标签列；重启 `dev-backend-1` 后 `http://localhost:9173` 返回 HTTP 200，日志显示无待执行迁移。

## 已完成

- 用户列表 API 标签改用已安装 MDI 图标库中的 `code-tags`，修复原 `code` 图标名无对应字形导致的空白（`frontend/src/views/Users.vue`，2026-09-07）。验证：`yarn lint`、`yarn build` 通过；后端容器已重启，9173 与最新 Users 脚本均返回 HTTP 200，脚本包含 `code-tags`。
- 客户/客户列表命名重构已完成：后端 API 使用 `/api/customers`、`/api/customer-lists`，数据库表、列、枚举和权限同步改名；v6.23.0/v6.24.0 迁移保留现有数据并重建客户列表统计视图。`namesalutation` 技术字段保持不变。验证：`go test ./...`、`cd frontend && yarn lint && yarn build`，Docker 重启后 9173 返回 HTTP 200；旧 `/api/lists` 返回 404。

- 客户管理界面已按业务确认调整：客户编码成为勾选框后的首个数据列，并位于新建/编辑表单首项；客户的“名称”显示为“称呼”；“客户列表”列显示可点击的列表名称，跳转至相应客户列表的客户筛选页。覆盖项已更新至 `frontend/cypress/e2e/customers.cy.js`；`cd frontend && yarn lint` 与中英文语言包 JSON 解析通过。生产管理端已通过 `cd frontend && yarn build` 构建并重启后端，`http://localhost:9173` 及其最新客户脚本均返回 HTTP 200（2026-09-04）。
- 修复客户命名重构后超级管理员仪表板仍读取旧 `subscribers/lists` 物化视图的问题：新增 v6.26.0 迁移重建 `mat_dashboard_counts`，统一返回 `customers/customerLists`，避免前端读取 `counts.customerLists.total` 时渲染异常。Docker 已执行迁移并重启；仪表板接口返回 200、页面显示客户列表 18、客户 26，浏览器刷新后无新的渲染错误（2026-09-04）。
- AI 入站回信分类已完成端到端验证（2026-09-04）。功能：全局 `reply_ai` 设置（OpenAI 兼容 base_url/模型/密钥/超时/最低置信度，密钥打码往返）、回信邮箱逐邮箱 AI 开关、POP3 非破坏扫描 + `reply_ai_events` 持久队列（邮箱+Message-ID/内容哈希去重、租约令牌、3 次重试、终态清除正文）、仅在工作区内唯一匹配发件人时动作（`unsubscribe`/`complaint` 全局 blocklisted + 全部名单退订；投诉额外写入 `source=reply_ai` 的退信并携带事件 ID/模型/置信度/原因），其余一律 ignored 审计。验证：`go test ./...`、前端 lint + `yarn build`、Docker 重启后 `v6.25.0` 迁移应用且 9173 返回 200；QA 脚本见 `dev/reply_ai_mock_openai.js`、`dev/reply_ai_e2e_seed.sql`、`dev/reply_ai_settings_verify.js`、`dev/reply_ai_enable_e2e.js`、`dev/reply_ai_reset.js`——退订拉黑、投诉退信（meta 含 event_id/confidence/model/reason）、低置信度忽略、unmatched/跨工作区发件人忽略、重复消息去重、密钥掩码与空值保留、空 base_url 校验 400 全部断言 PASS。管理端：设置页新增「回信 AI 分类」标签页；客户行为记录页新增 AI 分类表；`i18n` en/zh-CN/zh-TW 补齐；文档更新 `docs/ARCHITECTURE.md`、`docs/harness/TECH_ARCHITECTURE.md`、`docs/harness/BUSINESS_LOGIC.md`、`docs/docs/content/bounces.md`。
- 建立 `docs/ARCHITECTURE.md`，记录目录结构、系统架构、技术栈、权限算法以及构建、测试、CI、部署命令。
- 建立本 `docs/harness/` 台账，并在 `AGENTS.md` 中要求代码变更同步更新相关文档。
- 修正开发容器前端端口、文档预览路径，以及 API 示例中的后端端口说明。
- 补充工作区、资源可见性、所有权和组织经理只读边界的用户权限文档。
- 安装 `ezwinports.make 4.4.1`（`winget`），并记录 Windows 下的 Docker 开发启动流程；将 Git for Windows 的 POSIX 工具目录置于 Make 之前后，`make --version`、`make -n dev-docker` 和 `make build-dev-docker` 成功。验证 `docker compose -f dev/docker-compose.yml up --build -d` 后 `docker compose ... ps` 显示 5 个服务运行，管理端、后端根路径、Adminer 和 MailHog 均返回 HTTP 200（2026-09-03）。
- 新增个人空间能力控制与登录后空间选择：`permissions.json` 增加 `workspaces:personal`（Super Admin 豁免）；`cmd/organizations.go` 的 `workspaceAccessForOrganizationWithPersonal` 在 `organization_id=0` 处门控，个人资源迁移端点与四个迁移列表 GET 端点豁免（数据保留但可迁移；明细/导出/写入仍 403）；密码/2FA/OIDC/重置密码成功后进入 `/admin/select-workspace`（`cmd/select_workspace.go` + `static/public/templates/select-workspace.html`，单个空间自动进入，客户端同步 localStorage/Cookie；选择页为浅色全屏外壳，整屏只呈现空间选择）。按用户确认，登录/2FA/重置等页面**保持经典布局不做视觉重做**（曾试做深蓝全屏外壳后回退，`git checkout` 还原五个登录系模板，`style.css` 全屏样式改为经典浅色 `#f9f9f9` + 白卡）。SPA 在 `App.vue`/`main.js` 隐藏个人空间入口并按能力回退。验证：`go build ./...`、`go test ./...`、模板冒烟测试（`cmd/render_tpl_smoke_test.go`，含 html/template 的 `<script>` 双转义回归）全部通过。
- Docker 端到端验证（dev 栈，`dev-backend-1` 重启加载新代码）：fixtures 见 `dev/wsqa_fixtures.sql`（密码 `Test@1234`），脚本见 `dev/wsqa_verify.ps1` / `dev/wsqa_verify_cd.ps1`，全部断言 PASS——登录 302 → `/admin/select-workspace`；无 `workspaces:personal` 用户单组织自动进入组织、多组织渲染选择页且无个人空间选项；有权限且无组织用户自动进入个人空间；`GET /api/workspace?organization_id=0`、个人空间写入、客户导出均 403；`GET /api/customer-lists`（org 0）只读豁免且可见保留的个人客户列表；迁移 move 成功将个人客户列表归入目标组织（2026-09-03）。
- 选择页常驻「邀请码加入组织」帮助区（用户确认：入口对所有用户常驻、可直接输码加入）：`static/public/templates/select-workspace.html` 在空间列表与"无可用空间"阻断态下方渲染 `.join-help`（标题/说明/邀请码输入 + 加入按钮），`fetch POST /api/organizations/join` 成功后写入工作区 localStorage/Cookie 并直接进入该组织；自动进入页不渲染帮助区；`style.css` 增加浅色样式；`i18n` en/zh-CN 增加 `users.noOrgTitle/noOrgHint/inviteCode/joinOrg`。验证：模板冒烟测试（`cmd/render_tpl_smoke_test.go`）与 Docker 端到端（`dev/wsqa_verify_join.ps1`，用户 `wsqa_noorg` + 邀请 `WSQA-JOIN-TEST`）：多空间页含帮助表单、阻断页含帮助表单、无效码 404、有效码加入 org 1 后单空间自动进入（2026-09-03）。
- 一级公海与组织二级列表已完成并完成全链路回归（2026-09-04）：v6.27.0 提供独立 `pool`/`pool_segment` 类型、允许重复导入客户编码的公海联系人、组织级分配/逻辑剔除/审计和组织二级回件邮箱；非最高管理员接口仅返回安全字段和脱敏客户邮箱，内部回件邮箱始终明文。活动可选择一级或二级受众，服务端按组织二级分配解析真实收件人和回件邮箱、按 pool contact ID 去重并在快照中保留来源；退订、退信和回复 AI 仅回写实际投递组织。`dev/pools_e2e_verify.ps1`、`dev/pools_smtp_e2e_verify.ps1` 和 Cypress 浏览器矩阵均 PASS，覆盖最高管理员明文例外、组织管理员脱敏、未授权组织无入口且一级详情 403、组织隔离、剔除、草稿阻断、去重、MailHog 实投与 `finished|3|3` 计数。夹具和验证脚本按名称动态解析数据库 ID，可安全重复加载。
- 一级公海二级列表已收敛为“从一级拆分并绑定”单一路径（2026-09-07）：最高管理员或目标组织管理员在一级公海管理窗口创建二级列表时，列表、组织投放授权和绑定关系一次事务完成；通用客户列表表单不再提供 `pool_segment`，待绑定/二级合并一级接口已移除。每个“一级公海 × 组织”仅允许一个二级列表，重复创建返回明确冲突；普通客户列表导入保留为独立维护接口。
- 公海目标组织改为独立于工作区和成员关系的分配上下文（2026-09-07）：最高管理员在一级公海管理窗口选择目标组织后，`GET /api/pools/:id/management-target` 返回该组织已绑定二级列表及仅含名称/邮箱的内部回件邮箱选项；新建二级列表请求显式指向目标组织并在一次事务中完成创建、授权和绑定。此过程不切换当前工作区，也不新增 `organization_members` 记录。`PoolManager.vue` 明确标注这一语义，不再提供待绑定候选或合并入口。验证：`go test ./...`、`cd frontend && yarn lint && yarn build`、打包后的 `http://localhost:9173` 返回 200，Cypress `pools.cy.js` 5 项通过。
- 一级公海运营表单已按任务流程简化（2026-09-07）：管理窗口只保留“选择组织 → 创建/查看二级列表 → 分配联系人”，移除合并标签、待绑定候选和无效的高级空白区；已绑定列表集中展示所属组织、回件邮箱和联系人分配入口。弹窗保持 800px 宽度，避免右侧无意义空白。
- 二级列表联系人分配已改为文件批量导入（2026-09-07）：新增 `POST /api/pool-segments/:id/import-members`（兼容 `/api/pools/segments/:id/import-members`），支持 CSV/XLSX 的 `customer_code,email` 首行格式；服务端在当前一级公海内按双字段唯一匹配、去重并事务化新增/恢复成员，返回新分配、恢复、已存在、未匹配、歧义和无效行统计，响应不含真实邮箱。管理端新增模板下载、拖拽上传、异常行提示；原查询/勾选区折叠为单条维护入口。验证：`go test ./cmd ./internal/core ./models`、`cd frontend && yarn lint && yarn build` 通过。
- 回件邮箱配置已从最高管理员公海拆分弹窗移除（2026-09-07）：最高管理员创建二级列表时不再代填组织回件邮箱，管理窗口只展示“由目标组织配置”的状态；组织管理员仍可在自己的组织工作区维护二级列表邮箱，发送前校验和来源解析保持不变。创建接口拒绝平台管理员显式传入邮箱，更新接口仅允许组织管理员调用。

## 后续 / 持续维护

- v3→v4 BasicAuth/session Cookie 兼容窗口已按用户确认于 2026-09-06 结束。认证中间件已移除“存在 session Cookie 时忽略 Authorization”特例；请求同时携带旧 BasicAuth 与当前 session 时，显式 Authorization 按当前 API 认证规则处理。`dev/wsqa_verify.ps1` 新增回归断言，确认失效旧 BasicAuth 不会借当前 Cookie 绕过认证。
- 文档 strict 构建已接入 `.github/workflows/build-sanity.yml`，贡献者可复用该 workflow 的依赖和命令。

状态变更时保留验证命令、结果和日期，避免只写主观进度。

## 本轮复核（2026-09-04）

- `go test ./...`：PASS。
- `cd frontend && yarn lint`、`yarn build`：PASS（仅既有 Vite/Sass 弃用警告）。
- `cd docs/docs && mkdocs build --strict --clean`：PASS（仅既有未纳入导航页面与外链提示）。
- `CYPRESS_BASE_URL=http://localhost:8181 npx cypress run --spec cypress/e2e/pools.cy.js --env POOL_E2E=true --browser electron`：3 passing（组织管理员脱敏、最高管理员明文、未授权组织隐藏入口及详情 403）。
- `dev/pools_e2e_verify.ps1`、`dev/pools_smtp_e2e_verify.ps1`：全部断言 PASS；按名称动态解析一级、二级和回件邮箱 fixture ID，脚本可在重复加载夹具后稳定运行并清理临时活动/SMTP。`make test` 在当前 PowerShell 环境不可用（未安装/未加入 PATH），由等价 `go test ./...` 完成后端验证。
- `dev/wsqa_verify.ps1`：认证升级回归新增“session Cookie + 失效旧 BasicAuth 返回 403”断言；与登录、工作区选择、个人空间迁移断言一并通过（2026-09-06）。
