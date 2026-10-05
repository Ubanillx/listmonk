# 管理端 UI/UX 审计

## 2026-09-30 组织 SMTP 发件池操作流复查

组织营销 SMTP 已收敛为“左侧选择/创建发件池，右侧编辑当前池 SMTP”的两栏流程。池列表提供明确的选中态和启用数/总数，右侧标题显示当前池名称；切换池和创建新池会检查 SMTP 草稿，避免未保存内容被切换丢弃。隔离 Cypress 覆盖了多池显示、切换空池后恢复有 SMTP 池以及活动保存流程。

## 2026-09-29 浏览器标注复查

私域客户页标注要求撤除“高级查询”及相关接口和权限。页面只保留普通搜索；全选后的批量操作仍可按搜索和客户列表筛选，后端改用普通批量接口。角色界面不再提供高级 SQL 权限；旧查询参数被拒绝，旧查询路由已撤销。

新标注要求“私域客户编码”缩写为“客户编码”、搜索兼容客户编码、去掉每行的下载数据入口。客户页和编辑表单共用的字段标签已统一；列表、全选批量目标和 CSV 导出沿用同一客户编码搜索条件，顶部导出入口保留。

后续标注要求公海客户导航展示全部公海列表的客户，并标明每行所属列表。汇总页标题不再附加上次访问的列表名；单列表管理从表格所属列表链接进入。复查发现该链接原先落到私域客户高亮的通用路由，现改为公海客户明细路由，并同步调整公海列表计数、导入结果和旧深链。本地浏览器在 8181 和构建后的 9173 验证：点击 `wsqa-pool-primary` 进入 `/admin/pool-lists/6/contacts`，公海客户高亮而私域客户不高亮；旧通用公海链接自动转入新路由，普通私域列表仍停留在私域路由。

新的标注要求公海客户汇总增加分配部门和所属公海列表筛选，公海客户列表搜索框补充 placeholder。汇总页现在用同一工具栏中的下拉框筛选，服务端在分页前过滤，并提供仅含当前工作区可见公海/部门的选项；公海与私域列表搜索框分别提示按列表名称搜索。本地浏览器确认 8181 搜索提示，9173 下拉选项和组合参数，空结果及 CSV 均遵循过滤，390px 下三个筛选填满容器且页面无横向溢出。

用户对 Dashboard、个人资料及个人 SMTP、业务审计、营销活动创建、公海客户页给出了定位截图和具体意见。已落地：淡雅白画布、无竖线导航、增强表头、控制输入宽度与浮动标签、中文年月日时间格式、活动受众分组树选择、公海客户状态下拉及移除顶部批量删除。活动树列出当前空间的常规列表、已获投放授权的一级公海，以及具有平台公海发送权限的管理员可用的一级公海；组织公海分配不作为活动受众直接选择。

本机已登录浏览器经 CDP 顺序打开 29 个管理端路由，采集标题、可见错误提示和页面级横向溢出；另在 390px 宽度抽查 9 个关键路由，并复查活动树选择、公海状态筛选和个人资料浮动标签。发现审计日志表格在 1384px 宽度溢出 485px，现由表格容器内部横向滚动承接，页面级溢出归零；活动树曾遗漏平台管理员可选的公海列表，现已修复。视觉证据还包括用户标注截图及本机登录页、活动创建移动端截图。内置浏览器连接因标签失效及认证令牌不可用而中断，当前逐路由巡检使用本机浏览器连接；不能把路由级 DOM 巡检等同于逐页人工视觉验收。


审计日期：2026-09-27。范围：`frontend/src`（Vue 2 + Buefy/Bulma 管理端）全部视图与公共组件，以及 `frontend/email-builder` 的主题色。不含服务端渲染的登录页与工作区选择页（Go 模板）。

## 方法

- **源码审计**：对全部 75 个 `.vue`/`.js` 前端文件（`frontend/src` 下 66 个 `.vue` 与 9 个 `.js`）按三个审计面（列表/表格/批量操作、表单/详情/设置、共享组件与设计系统）逐文件通读，报告中每条结论都由人工回到源码行复核，不保留无法在代码中定位的推测。
- **实测**：本地 dev 栈（`dev-backend-1`，9173）真实登录会话 + headless Chrome（CDP），桌面 1440×900 与移动 390×844 两种视口各 15 条路由，采集布局尺寸、可访问名、表单标签关联、溢出与点击目标。
  - 会话账号：`wsqa_pool_manager`（`dev/pools_e2e_seed.sql` 提供的组织 1 经理，user 角色 + 组织 manager），因此**不含** `settings:*`、`users:*`、`roles:*`、`pools:*` 权限；有权限边界含义的结论都标注了该前提。
  - 探测脚本为临时脚本（会话 scratch，未入库）；仓库内已有同类先例 `dev/email_builder_rerender_verify.js`（Node + CDP），若要把本节固化为回归门禁，建议按该模式补 `dev/ui_probe.js`。
- 文中的证据标注：**【实测】** = 在上述真实会话/DOM 上测量得到；**【源码】** = 由文件:行号直接可读出的结论；**未标注** = 源码结论且已有实测佐证。

## 结论摘要

| # | 严重度 | 问题 | 关键证据 |
| --- | --- | --- | --- |
| H1 | 高 | 无权限页面可直达，失败后永久 loading / 每 10 秒重复弹错 | 实测 `/admin/settings` 403 + 永久全屏 loading；`/admin/settings/logs` 12 秒内 2 次 403 |
| H2 | 高 | 组织/工作区页面在手机上无法使用（700px 表格塞进 390px） | 实测 `min-width:700px`，18 处 `:mobile-cards="false"` |
| H3 | 高 | 破坏性操作确认统一为“你确定吗？/确认”，批量删除可覆盖全量却不说范围 | `utils.js:148-157`；`Bounces.vue:20,191-192` |
| H4 | 高 | 批量操作不显示影响客户数；批量选择成功后不清空且各页不一致 | `CustomerBulkList.vue:59`（prop 未被渲染）；`Campaigns.vue:539-561` |
| H5 | 高 | Users 页搜索/排序静默无效（API 层丢参）且一次性渲染全部用户 | `Users.vue:273-281` → `api/index.js:798-804` |
| M1 | 中 | 空态/加载态守卫混乱（Bounces 用错 loading key；空态组件只有一种泛化文案） | `Bounces.vue:91`；`EmptyPlaceholder.vue:1-20` |
| M2 | 中 | 全局刷新按钮在多数页面静默无反应；仪表盘图表永不更新 | `App.vue:173-175`；全仓仅 12 个监听者；`Chart.vue:150-173` |
| M3 | 中 | 搜索/筛选不重置页码，越界空结果被当成“搜索无结果” | `Campaigns.vue:419-427`、`CustomerLists.vue:292-301` |
| M4 | 中 | “跨页全选”权限判定有四种写法，同一能力在不同页表现不同 | `Campaigns.vue:582-583`、`CustomerLists.vue:445-446`、`Customers.vue:1169-1170`、`Bounces.vue:240` |
| M5 | 中 | 提交按钮只有 `:loading` 没有 `:disabled`，且 loading 模型绑错 | `UserForm.vue:141`、`RoleForm.vue:122` + `constants.js:1-26` |
| M6 | 中 | `aria-disabled` 不产生任何视觉/交互变化，未选中时删除入口看起来可用 | `Customers.vue:131` + `style.scss:439-444` |
| M7 | 中 | 身份/权限变更类操作缺少确认与回滚（撤销邀请、审批申请、成员角色） | `Organizations.vue:212-215`、`ManageOrganizations.vue:99-103,201-202,434-437` |
| M8 | 中 | 客户页“删除退信”是死代码，且一旦执行会抛异常 | `CustomerForm.vue:117-118,216-218,228-238` |
| L1-L5 | 低 | 可访问性：图标按钮无可访问名、标签未关联、对比度 2.5–3.9:1、小点击目标、`!important` 击穿小标签 | 见下文实测数据 |

## 修复进展（2026-09-27）

本节记录同一轮修复的落地结果与验证方式。文中的“修复后实测”来自同一套 CDP 探测脚本、同一账号（`wsqa_pool_manager`）与同一视口（1440×900 / 390×844）在修复前后的两次运行，以及针对样式与路由的定点复测。

| 项 | 状态 | 修复内容 | 修复后证据 |
| --- | --- | --- | --- |
| H1 | 已修 | `router/index.js` 为 `/settings`、`/settings/logs`、`/settings/audit`、`/settings/maintenance`、`/users`、`/users/roles/*` 声明 `meta.permission`；`main.js` 的守卫在 profile 就绪后才判定（`profileReady` 承诺），未授权跳转 `/admin/403`；新增 `views/403.vue`；`Settings.vue`/`Logs.vue` 补失败态与重试 | 组织经理访问 `/admin/settings` 与 `/admin/settings/logs` 均落到 `/admin/403`（“你没有访问此页面的权限。”+ 返回仪表盘），无 loading 遮罩；日志页不再每 10 秒重复弹 403 |
| H2 | 已修 | 删除 `MyOrganizations.vue` 的 `min-width: 700px`，13 处 `:mobile-cards="false"` 恢复默认并包 `table-scroll`（`overflow-x: auto`），CSV 预览表保留横滚容器 | 390px 下“我参与的组织”表格 700→345px、“管理组织”成员表 633→343px，全部路由 `wideInMain = 0` |
| H3 | 已修 | `utils.confirm(msg, fn, onCancel, { type })` 支持危险色、自定义确认文案与默认聚焦取消；退信删除/拉黑、媒体删除、模板删除与下架、客户删除、活动启动/暂停/取消、API Key 撤销、维护清理等改用带对象与数量的文案 + `is-danger` | 源码级：确认框颜色与文案由 `Dialog.confirm` 的 `type`/`confirmText` 驱动 |
| H4 | 已修 | `CustomerBulkList.vue` 渲染 `customers.bulkAffectedCount` 并对移除/退订二次确认；`Campaigns.vue`/`CustomerLists.vue` 批量删除成功后重置选择并缓存数量用于提示 | 源码级 |
| H5 | 已修 | `Users.vue` 改为前端过滤 + 前端排序（后端 `/api/users` 无搜索/排序能力），列 `field` 对齐真实键名 | 源码级：搜索与排序不再是无效控件 |
| M1 | 已修 | `Bounces.vue` 空态守卫改为 `!loading.bounces`；组织、批量导入、客户等页面补 `v-if="!loading.x"` | 源码级 |
| M2 | 已修 | `Chart.vue` 增加 `data` watcher 与 `beforeDestroy` 销毁；`Dashboard.vue` 取数改 `try/finally`；刷新按钮按路由 `meta.refreshable` 提示“此页面没有可刷新的内容” | 源码级 |
| M3 | 已修 | `Campaigns.vue`/`CustomerLists.vue` 搜索与排序统一重置页码为 1 | 源码级 |
| M4 | 部分 | 新增 `$isPlatformAdmin()` 并替换 `CustomerLists.vue` 的魔法数字；四处“跨页全选”的语义差异（活动/名单=平台管理员、客户=非组织经理、退信=巡查权限）保留并加注释，未强行统一以免放宽删除范围 | 源码级 |
| M5 | 已修 | `UserForm.vue` 改用 `loading.users`；`RoleForm.vue` 按类型取 `userRoles`/`customerListRoles`；提交按钮统一 `:loading` + `:disabled`（含维护、API Key、资料保存） | 源码级 |
| M6 | 已修 | 池联系人删除入口补 `data-disabled`（置灰并禁点）与未选中提示 | 源码级 |
| M7 | 已修 | 撤销邀请、审批组织申请（拒绝走 `$utils.prompt` 收集理由并写入请求 `note`）、成员角色变更（确认 + 失败回滚显示） | 源码级 |
| M8 | 已修 | 客户页“删除退信”从死代码改为退信 Tab 内可用入口，方法签名改为无参并提示数量 | 源码级 |
| L1/L2 | 已修 | 新增 `a11y.js`（在 `main.js` 安装）：为 Buefy 字段控件补齐 `aria-label`（字段标签或 placeholder 兜底），为分页 prev/next 补名字；各列表搜索输入与提交按钮、复制控件补 `aria-label` | 未命名控件：活动新建 12→0、个人资料 5→0、营销统计 8→0、导入 4→0、管理组织 8→5（剩行内选择器）；无可访问名图标按钮：退信 4→0、客户列表 5→0、统计 4→0、媒体 1→0、客户 5→1 |
| L3 | 已修 | 状态标签改用同色相加深文字（`darken`） | private 2.51→10.74、blocklisted 2.88→6.75、public 3.05→5.88、finished 3.26→6.83、默认 3.70→10.42，全部 ≥ 4.5:1 |
| L4 | 已修 | `.tag` 与 `.tag.is-small` 的 padding 同时加权，既保留基础 20px 胶囊外观，又让小标签真正变小 | 实测基础 `.tag` 为 `0 20px`、`.tag.is-small` 为 `3px 5px`（修复前小标签同样是 `0 20px`） |
| L5 | 已修 | `.copy-text` 补 `:focus`/`:focus-visible`；行内动作与单元格链接设 24px 最小命中区；复制控件补 `title` | CSS 级 |
| L6 | 已修 | 维护页 “Data”、客户列表表单 “Opt-in type”、模板更新提示、个人资料二维码 alt、可视化编辑器 iframe title、应用重启提示统一走 `$t()` | 源码级 |
| L7 | 已修 | `Navigation.vue` 的“客户列表”分组 key 统一为 `customerLists` | 源码级 |
| L8 | 已修 | 四个表单弹窗底部按钮由“关闭”改为“取消”（含客户表单） | 源码级 |
| L11 | 已修 | 用户下拉改用 `tag="router-link"`/`tag="a"` 消除 `<a>` 嵌套 `<a>`；退出登录加确认 | 源码级 |

另修一项（复核中被实测证伪的回归）：`Campaign.vue` 的 `isUnsaved()` 对**未保存过**的新建活动会误判为“有未保存修改”——实测未做任何编辑时其 `onbeforeunload` 返回 `true`（`wouldPrompt = true`），离开或刷新都会弹浏览器的离开确认；现在未保存活动直接返回 `false`，`beforeDestroy` 也会清除 `window.onbeforeunload`，复测同一页面返回 `null`（`wouldPrompt = false`）。

### 本轮未修复（仍留在台账）

- 只读态的可视化编辑器：`email-builder` 没有只读模式，HTML/Markdown/纯文本编辑器已禁用、保存入口已隐藏，但可视化画布仍可输入。
- `Organizations.vue` 的平台成员角色下拉与平台审批按钮未加确认/回滚（`ManageOrganizations.vue` 的同类入口已修）。
- 无深色模式；2026-09-29 已将管理端 Sass 主色与 CSS 令牌收进 `styles/_variables.scss`，图表色同步到 `constants.js`，但 `components/editor-theme.js` 与 `frontend/email-builder/src/theme.ts` 仍需后续统一。
- 断点体系未收敛（JS `window.innerWidth <= 768` vs SCSS 850/1024/1100/1500）。
- 移动端表格排序按钮与 `b-numberinput` 的 ± 按钮仍无 `aria-label`（Buefy 上游标记，且缺少可用的 i18n 键）。
- “管理组织”页 5 个行内 `select` 无程序化标签；营销统计页移动端仍有 3px 横向溢出。
- 维护页 “Vacuum” 标题与客户属性校验提示仍为英文硬编码（缺对应 i18n 键）。

验证方式：`yarn lint` 与 `yarn build` 通过；`docker restart dev-backend-1` 后 `no upgrades to run`、`http://localhost:9173` 返回 200；CDP 探测修复前后各一次（15 条路由 × 2 视口），另有针对标签样式与 `/admin/403` 的定点复测。

### 2026-09-29 视觉规范续进

新增 `UI_DESIGN_SYSTEM.md`，将令牌、基础排版、布局、组件和仪表盘样式分层放入 `frontend/src/assets/styles/`。统一中性色、细边框、多层微阴影、焦点环、表格密度、分页状态和软背景状态标签；Dashboard 数字、图标和图表使用同一蓝色系，并把无效的 `<label for="#">` 数字标记改为语义正确的 `<strong>`。Dashboard 当前没有上期对比数据，因此没有显示增长率。`yarn lint`、`yarn build`、文档检查通过，构建产物已在 9173 返回 200；本轮未重新执行浏览器截图复测。
## 一、高危

### H1 受限页面可直达，且失败后没有错误态（永久 loading / 重复弹错）

- 位置：`frontend/src/router/index.js:148-188`（`/settings*`、`/users*` 只有 `meta.title`，无权限 meta）；`frontend/src/main.js:43-54`（`beforeEach` 只拦截 `meta.organizationManager`，全仓唯一一条）；`frontend/src/views/Settings.vue:4,224-256`；`frontend/src/views/Logs.vue:29-33,44`；`frontend/src/api/index.js:104-112`。
- 证据
  - 【实测】以组织 1 经理会话访问 `/admin/settings`：`GET /api/settings` → `403 {"message":"permission denied: settings:get"}`；页面正文只有标题“设置 (dev)”（8 字符），`input/select/textarea` 计数 0、`.box` 计数 0，而 `.loading-overlay` 的 class 为 `loading-overlay is-active is-full-page`、`display:flex`、`visibility:visible` 且含 spinner——全屏遮罩永久存在，没有错误文案、没有重试入口，3 秒后连错误 toast 都已消失（实测 toast 计数 0）。
  - 【实测】访问 `/admin/settings/logs`：12 秒内记录到 2 次 `GET /api/logs`（403），日志区 `.line` 计数 0，页面上存在一条 toast “permission denied: settings:get”——轮询不会停，用户停留多久就被重复报错多久。
  - 【源码】`Settings.vue:4`：`<b-loading :is-full-page="true" v-if="loading.settings || isLoading" active />`；`getSettings()` 只在 `.then` 里复位 `isLoading`，且 `JSON.parse(JSON.stringify(data))` 的 `try` 命中时 `return` 也不复位（`:228-233`）。`Logs.vue:29-33` 的 `getLogs()` 同样没有 `.catch/.finally`；`Logs.vue:44` 每 10 秒轮询一次。失败只由 `api/index.js:104-112` 弹出一条 3 秒 danger toast。
- 影响：菜单里看不到的页面，输入 URL 就能进入；进去后是一个永远转圈或反复报错的空壳。用户无法区分“没有权限”“请求失败”“真的没有数据”，也不知道该找管理员还是该刷新。
- 建议：① 路由表补 `meta.permission`（如 `settings:get`、`roles:get`、`audit:get`、`settings:maintain`），在 `main.js:43-54` 的 `beforeEach` 里统一判定并重定向到统一的 403 视图或仪表盘；② 所有取数函数用 `.catch/.finally` 复位 loading，并渲染“加载失败 + 重试”状态；③ 轮询类页面（Logs）在连续失败后停止轮询并显示错误态；④ 参考 `Customers.vue:243-249` 已有的 `pending/error` 状态机与 `Customers.vue:360` 的空态写法。
- 验证：以无 `settings:get` 的账号访问 `/admin/settings`，应看到 403 说明与返回入口，而不是 loading 遮罩；访问 `/admin/settings/logs` 不应出现反复 toast。

### H2 组织/工作区页面在窄屏不可用（实测 700px 表格塞进 390px 视口）

- 位置：`frontend/src/views/organizations/MyOrganizations.vue:37,637-642`；`ManageOrganizations.vue:43,90,170,195,210`；`Organizations.vue:14,160,203,260,271,374`；`CreateOrganization.vue:23`；另 `views/Customers.vue:106`、`views/Import.vue:136`、`views/UserBulkImport.vue:39`、`views/OrganizationMemberBulkImport.vue:34`、`views/Users.vue:152`；`frontend/src/assets/style.scss:2205-2208`。
- 证据
  - 【实测】390×844 视口下“我参与的组织”：`section.organizations` 宽 700px、页头 `.columns.page-header` 宽 723px、`.column.is-10` 宽 723px、表格宽 700px（`min-width` 生效），均远超 390px 视口；同一会话下普通表格（客户页 9 列）会被压到 345px，说明差异来自这里显式关闭了 Buefy 的卡片化。
  - 【实测】“管理组织”页在同一视口下 `section` 宽 635px、成员表宽 633px。
  - 【源码】`MyOrganizations.vue:641`：`.org-table table { min-width: 700px; }`；`MyOrganizations.vue:37`：`<b-table :data="organizations" :mobile-cards="false" class="org-table">`。全仓 `:mobile-cards="false"` 共 18 处（`grep` 全量），全部落在本分支新增的组织/工作区/客户/导入/用户表格上，而 `style.scss` 只在 `@media (max-width:1100px)` 给 `html, body` 加了 `overflow-x: auto`，表格自身没有横向滚动容器（对比 `Import.vue` 预览表用了 `preview-table-wrap{overflow-x:auto}`）。
- 影响：手机上组织页必须整页横向拖动才能看到“进入/管理成员/转移资源/退出组织”等操作列；行内角色下拉在窄屏被挤压。这是本分支新增模块最系统性的可用性退化点。
- 建议：① 删掉 `min-width: 700px`（或降到 `min-width: 0`）并让 `:mobile-cards` 恢复默认 `true`；② 若坚持宽表，给表格外层补 `overflow-x:auto` 容器（复用 `Import.vue` 的 `preview-table-wrap` 模式）；③ 操作列在窄屏改为图标按钮 + `aria-label`。
- 验证：390px 下打开上述 5 个页面，`document.documentElement.scrollWidth` 不应超过视口宽度，关键操作无需横向拖动。

### H3 破坏性操作的确认语义被统一成最弱的一种

- 位置：`frontend/src/utils.js:148-157`；`frontend/src/views/Bounces.vue:20,23,187-202`；`frontend/src/views/Media.vue:142`；`frontend/src/views/Templates.vue:93,103`；`frontend/src/views/Customers.vue:1003-1005`；`frontend/src/components/CustomerForm.vue:229`。
- 证据
  - 【源码】`utils.js:148-157`：`confirm = (msg, onConfirm, onCancel) => Dialog.confirm({ message: !msg ? t('globals.messages.confirm') : escapeHTML(msg), confirmText: t('globals.buttons.ok'), cancelText: t('globals.buttons.cancel'), ... })`；i18n 原文为 `globals.messages.confirm = "你确定吗？"`、`globals.buttons.ok = "确认"`（en: “Are you sure?” / “Ok”）。没有类型（danger/warning）、没有默认按钮、没有焦点策略。
  - 【源码】大量不可逆操作以 `confirm(null, …)` 调用：`Bounces.vue:20`（删除退信）、`Bounces.vue:23`（拉黑）、`Media.vue:142`（删除媒体）、`Templates.vue:93,103`（删除模板/清空）、`Customers.vue:1003-1005`（删除客户）、`CustomerForm.vue:229`。
  - 【源码】`Bounces.vue:187-202`：当用户先点“全选”时 `params.all = true`（删除**当前查询下的全部**退信），但确认框只说“你确定吗？”，既不显示数量也不说明范围；同一产品里已经有更好的现成文案 `globals.messages.confirmDelete = "删除 {num} 个 {name}？"`（`Customers.vue:1108`、`CustomerLists.vue:358` 在用）。
- 影响：删除模板、媒体、全部退信、吊销 API Key、拉黑客户这类不可逆动作，视觉与措辞跟普通确认完全一致，确认按钮还是绿色主色“确认”；用户连续操作时误点成本很高，且事后无法判断影响面。
- 建议：`confirm()` 增加第三参数 `{ type: 'is-danger', confirmText }`，危险操作固定红色主按钮 + 具体数量/名称文案；默认焦点放在“取消”；`Bounces.vue` 的全量删除再加一次显式二次确认。
- 验证：删除模板/媒体/全部退信时确认框应包含对象名称与数量，主按钮为危险色。

### H4 批量操作：不显示影响范围，且选择状态在成功后不清空（各页行为不一致）

- 位置：`frontend/src/views/CustomerBulkList.vue:59`；`frontend/src/views/Customers.vue:367`；`frontend/src/views/Campaigns.vue:539-561`；`frontend/src/views/CustomerLists.vue:333-355`；`frontend/src/views/Bounces.vue:161-167`；`frontend/node_modules/buefy/src/components/table/Table.vue:875-879`。
- 证据
  - 【源码】`CustomerBulkList.vue:59` `numCustomers: { type: Number, default: 0 }`——全文件仅此一处出现（`grep` 确认），模板完全没渲染；`Customers.vue:367` 传入 `:num-customers="this.numSelectedCustomers"`。弹窗让用户选“添加/移除/标记退订 + 目标列表”，全程不告知影响多少客户，提交按钮是“保存”，点下即执行。
  - 【源码】`Campaigns.vue:553-561` 与 `CustomerLists.vue:347-355`：删除成功回调只做 `getX()` + toast，`bulk.checked`/`bulk.all` 不重置；而 Buefy 只在 `checkedRows` prop 变化时同步内部数组（`Table.vue:875-879` `checkedRows(rows) { this.newCheckedRows = [...rows] }`），数据刷新不会清空勾选。对照：`Bounces.vue:161-167` 与 `Customers.vue:916,998` 会清空。
- 影响：批量改列表/批量删除这种“一按就影响很多人”的动作没有范围提示；删除后工具栏仍显示“已选择 N”，再次点击会提交已不存在的 ID 或按旧条件再删一次。
- 建议：① `CustomerBulkList` 顶部显示“将对 {numCustomers} 位客户执行…”，`remove/unsubscribe` 追加确认；② 所有批量操作成功回调统一重置 `bulk = { checked: [], all: false }`；③ 工具栏补“清除选择”；④ 抽一个 `useBulkSelection` mixin 收敛四处重复实现。
- 验证：选中若干行→执行批量操作→工具栏选择计数应归零；打开批量弹窗应能看到受影响客户数。

### H5 Users 页搜索与排序静默无效，且一次性渲染全部用户

- 位置：`frontend/src/views/Users.vue:22-37,273-281`；`frontend/src/api/index.js:798-804`。
- 证据
  - 【源码】`Users.vue:273-281` 把参数传给 API：`this.$api.queryUsers({ query: …, order_by: …, order: … })`；而 `api/index.js:798-804` 的实现是 `export const queryUsers = () => http.get('/api/users', { loading: models.users, store: models.users })`——形参被丢弃，请求恒定发往 `/api/users`（无参数）。
  - 【源码】`Users.vue:22-23`：`:data="users"` + `backend-sorting @sort="onSort"`，但表格没有 `paginated/backend-pagination/total`，`:278-280` 又直接 `this.users = resp`，即整表一次性渲染。
- 影响：输入关键字回车/点放大镜后列表毫无变化（用户会认为“搜索坏了”），点表头排序同理；用户量大时首屏渲染全部行。
- 建议：`queryUsers = (params) => http.get('/api/users', { params, loading: models.users, store: models.users })`，并给该表补后端分页与总数。
- 验证：Users 页搜索命中后行数应变化；`/api/users?query=…` 请求应带参数。

## 二、中危

### M1 空态与加载态守卫混乱，空态组件只有一种泛化文案

- 位置：`frontend/src/views/Bounces.vue:91`；对照 `frontend/src/views/Customers.vue:360`、`frontend/src/views/Audit.vue:140`；`frontend/src/components/EmptyPlaceholder.vue:1-20`；`frontend/src/views/CustomFields.vue:56`；`frontend/node_modules/buefy/src/components/table/Table.vue:355,374`。
- 证据
  - 【源码】`Bounces.vue:91`：`<template #empty v-if="!loading.templates">`——守卫的是与本页无关的 `loading.templates`（页面自身的加载键是 `loading.bounces`，同文件 `:11` 用的是对的）。Buefy 在 `v-if="!visibleData.length"`（`Table.vue:355`）时渲染 `#empty` 插槽、在 `loading` 时另行渲染加载槽（`:374`），两者互不排斥：首次加载会同时出现 spinner 与“暂无内容”；若刚访问过模板页使 `loading.templates` 为 true，则空态被移除，数据为空时只剩 spinner。
  - 【源码】`EmptyPlaceholder.vue:1-20` 只有 `icon`（默认加号）与 `label`（默认 `globals.messages.emptyState = "暂无内容"`）两个 prop，没有加载/错误分支、没有插槽、没有操作按钮；`CustomFields.vue:56` 则自写 `<div class="has-text-centered has-text-grey py-6">`，`Forms.vue:9-11` 又是另一种写法。
- 建议：统一模板 `:loading="loading.x"` + `#empty v-if="!loading.x"`；把 `EmptyPlaceholder` 扩展为 `TableState`（loading/empty/error + 主操作按钮 + 插槽），并纳入编码规范。
- 验证：任何列表在首次加载期间不得同时出现 spinner 与空态文案。

### M2 全局刷新按钮在多数页面静默无反应；仪表盘图表永不更新

- 位置：`frontend/src/App.vue:36-41,173-175`；`frontend/src/components/Chart.vue:150-173`；`frontend/src/views/Dashboard.vue:181-195`。
- 证据
  - 【源码】`App.vue:173-175` 只发全局事件 `this.$root.$emit('page.refresh')`；全仓 `page.refresh` 监听者只有 12 处（CreateOrganization、JoinOrganization、ManageOrganizations、MyOrganizations、Bounces、Campaigns、CustomerLists、Customers、Dashboard、Media、Templates、Users），即 Settings、Logs、Audit、Maintenance、Roles、CustomFields、Import、活动编辑页、个人资料、403/404 等页面点刷新按钮**完全没有反应**（无 toast、无 loading）。
  - 【源码】`Chart.vue` 只在 `mounted()` 里 `new Chart(ctx, conf)`（`:150-173`），没有 `data` watcher、也没有 `beforeDestroy` 销毁图表；`Dashboard.vue:224` 把 `fetchData` 挂到 `page.refresh`，于是刷新后数字变了而图表仍是旧图，同时旧实例泄漏。
- 建议：① 给 `page.refresh` 增加“无监听者”提示或改为按路由注册的刷新注册表；② `Chart.vue` 增加 `watch.data`（调 `chart.update()` 或重建）与 `beforeDestroy` 中的 `chart.destroy()`；③ 取数统一 `.finally` 复位 loading（`Dashboard.vue:181-195` 无 catch）。
- 验证：在 Settings/Logs/Audit 等页点导航栏刷新应有反馈；仪表盘点刷新后图表与数字同步变化。

### M3 搜索/筛选不重置页码，越界空结果被当成“搜索无结果”

- 位置：`frontend/src/views/Campaigns.vue:27,419-427`；`frontend/src/views/CustomerLists.vue:292-301`；对照 `frontend/src/views/Customers.vue:968-974`。
- 证据：【源码】`Campaigns.vue` 的搜索表单直接 `@submit.prevent="getCampaigns"`，而 `getCampaigns()` 用 `page: this.queryParams.page`；`CustomerLists.vue` 同构。在第 3 页搜索只命中 1 页的结果时请求仍带 `page=3`，后端返回空页 → 界面显示“暂无内容”。而 `Customers.vue:968-974` 的 `onSubmit()` 明确 `queryCustomers({ page: 1 })`，`Media.vue`、`Audit.vue` 也重置页码——同一交互在不同列表页行为不一致。
- 建议：搜索/筛选/排序统一走一个 `setQueryParamsAndFetch({ page: 1, … })`。
- 验证：翻到第 3 页后搜索，结果不应为空。

### M4 “跨页全选”权限判定有四种写法

- 位置：`frontend/src/views/Campaigns.vue:582-583`；`frontend/src/views/CustomerLists.vue:445-446, 449-451`；`frontend/src/views/Customers.vue:1169-1171`；`frontend/src/views/Bounces.vue:240`。
- 证据：【源码】四处依次为 `Number(this.profile.userRole.id) === 1`、`Number(this.profile.userRole.id) === 1`、`!(this.workspace.organizationId && this.workspace.role === 'manager')`、`!this.$canInspectOrganization() || this.isPlatformAdmin`。同一语义（能否跨页全选）用了硬编码角色 id 与两种工作区判定混写；`CustomerLists.vue` 同文件内还有 `isPlatformAdmin()` 的第三种写法（`:449-451`）。
- 影响：同一个用户在客户页能全选、在活动页不能全选，会怀疑权限配置出错；角色 id 是魔法数字，权限模型一改就静默失效。
- 建议：收敛为后端下发的显式能力位或单一的 `$can*` 判定（参照 `main.js` 已有的一组 helper），删除所有 `userRole.id === 1` 判定。

### M5 提交按钮只有 `:loading` 没有 `:disabled`，且 loading 模型绑错

- 位置：`frontend/src/views/UserForm.vue:140-143`；`frontend/src/views/RoleForm.vue:122`；`frontend/src/constants.js:1-26`；`frontend/node_modules/buefy/src/components/button/Button.vue:7-17`。
- 证据
  - 【源码】`UserForm.vue:141`：`:loading="loading.customer_lists"`（该表单操作的是用户，`models.users` 才对；其它请求触碰 `customer_lists` 时这个保存按钮会跟着转圈）；`RoleForm.vue:122`：`:loading="loading.roles"`，而 `constants.js:1-26` 的 `models` 里根本没有 `roles` 键，值恒为 `undefined`。
  - 【源码】Buefy 的 `Button` 只把 `loading` 映射成 `is-loading` class（`Button.vue:7-17`），不禁用按钮；两处保存按钮都没有 `:disabled`。
- 影响：保存中既没有可靠进度提示，也能被重复点击 → 重复创建用户/角色。
- 建议：`UserForm` 改 `loading.users`，`RoleForm` 用 `loading.userRoles`/`loading.customerListRoles`（与 `Roles.vue:105` 一致），并统一 `:loading="x" :disabled="x"`；建议加一条编码规则：提交按钮必须同时绑定 loading 与 disabled。
- 验证：快速双击保存只产生一次请求。

### M6 `aria-disabled` 不产生任何视觉/交互变化

- 位置：`frontend/src/views/Customers.vue:131`；`frontend/src/assets/style.scss:439-444`。
- 证据：【源码】`Customers.vue:131`：`data-cy="btn-delete-pool-contacts" :aria-disabled="poolBulk.checked.length === 0"`；`style.scss:439-444` 只对 `.actions a[data-disabled]`、`.actions .icon[data-disabled]` 做置灰与 `pointer-events: none`，`aria-disabled` 没有对应样式，也没有阻止点击。未选中任何行时该删除入口看起来完全可用，点击后被方法里的空判断静默 `return`（无 toast）。
- 建议：改用 `:disabled`/`data-disabled`（或 `v-if` 隐藏），并在空选择时给出提示；`[aria-disabled=true]` 建议在全局样式里补一套统一表现（含 `pointer-events`）。

### M7 身份/权限变更类操作缺少确认与回滚

- 位置：`frontend/src/views/Organizations.vue:166,212-215,590`；`frontend/src/views/organizations/ManageOrganizations.vue:50,99-103,201-202,434-437,505-508`。
- 证据
  - 【源码】撤销邀请直接落库：`Organizations.vue:212-215` 与 `ManageOrganizations.vue:99-103` 的 `@click="revokeInvite(props.row)"` → `async revokeInvite(invite) { await this.$api.revokeOrganizationInvite(invite.id); await this.refresh(); }`，同文件里移除成员（`ManageOrganizations.vue:440`）却走了确认框，说明不是有意为之。
  - 【源码】审批组织创建申请同样无确认，且拒绝理由硬编码为空：`ManageOrganizations.vue:201-202` 两个同级按钮（批准为 `is-primary`、拒绝为 `is-text`）直接调用 `reviewRequest`，`:505-508` 为 `reviewOrganizationRequest(request.id, { approve, note: '' })`；而 `CreateOrganization.vue` 专门有 `requestNote` 列展示该字段，永远是 `-`。
  - 【源码】成员角色“改即存”：`ManageOrganizations.vue:50`/`Organizations.vue:166` 用 `:value` + `@input="changeMemberRole(...)"`，`:434-437` 直接 `updateOrganizationMember`，无确认、无 loading、失败时也不回滚本地显示值（只有 `await this.refresh()`）。
- 建议：撤销邀请/审批加确认（拒绝时用 `$utils.prompt` 填写理由）；角色变更加确认 + 失败回滚 + 提交期禁用。
- 验证：撤销邀请应出现确认框；拒绝申请应能填写理由并出现在申请方列表里。

### M8 客户页“删除退信”是死代码，一旦执行会抛异常

- 位置：`frontend/src/views/CustomerForm.vue:117-121,207,216-218,228-238`。
- 证据：【源码】模板 `v-if="isBounceVisible && canDeleteBounces"`（`:118`，同标签还带了拼写错误的 `disabed="true"`，`:117`）；`isBounceVisible` 的初值为 `false`（`:207`），唯一能翻转它的 `toggleBounces()`（`:216-218`）在全仓没有任何调用点（`grep` 仅命中定义），因此该入口永不渲染。即使渲染，绑定为 `@click.prevent="deleteBounces"`（无参），而方法签名是 `deleteBounces(sub)` 且回调里用 `sub.name`（`:234`）——会抛 `TypeError`，删除成功反被报错打断。
- 建议：要么删除这段死代码与其状态，要么接到退信 Tab 的激活态上并把签名改为无参、用客户名或 `globals.messages.deletedCount` 生成提示。

## 三、低危：一致性与可访问性（多数带实测数据）

### L1 纯图标控件缺少可访问名

- 位置示例：`views/Campaigns.vue:33` 与 `views/Customers.vue:107`、`views/CustomerLists.vue`、`views/Media.vue`、`views/Users.vue:33`（搜索提交按钮，只有 `icon-left="magnify"`）；`views/Campaigns.vue` 行内动作；`views/Templates.vue` 的预览/克隆。
- 证据
  - 【实测】无可访问名的可见图标控件计数：营销活动 7、客户 5、客户列表 5、退信 4、媒体 1、营销活动新建页 2、移动端每个页面还有 1（返回顶部）；典型样本：`button.button.is-primary <mdi mdi-magnify>`、`a.pagination-link.pagination-previous <mdi mdi-chevron-left mdi-24px>`、`a.copy-text <mdi mdi-file-multiple-outline>`、`a[data-cy=btn-start] <mdi mdi-rocket-launch-outline>`、`a[data-cy=btn-preview] <mdi mdi-file-find-outline>`。
  - 【源码】全仓自闭合的纯图标 `b-button` 共 12 个，其中仅 1 个带 `aria-label`；Buefy 的 `Tooltip` 不提供任何 ARIA 属性，因此“有 tooltip”不等于“有无障碍名”。
- 建议：所有纯图标控件补 `:aria-label="$t(...)"`（tooltip 仅作视觉补充），并把“图标按钮必须有可访问名”写进评审清单。

### L2 表单控件与可见标签没有程序化关联

- 证据：【实测】无可访问名的可见表单控件计数：营销活动新建页 12（`name`、`subject`、多个 select）、管理组织 8（含角色/邮箱选择）、营销分析 8–9、个人资料 5（`email`、`name`、密码）、导入 4（文件 + select）。这些字段都有可见文字，但 label 没有 `for`/包裹关系，也没有 `aria-label`。
- 建议：统一改用 Buefy `b-field :label` 的正确绑定或补 `aria-label`/`aria-labelledby`。

### L3 状态标签对比度不足

- 证据：【实测】按计算样式（前景/背景）实测对比度：`.tag.private` 2.51:1、`.tag.blocklisted` 2.88:1、`.tag.public` 3.05:1、`.tag.finished` 3.26:1、默认 `.tag` 3.70:1——全部低于 WCAG AA 正文 4.5:1；来源为 `style.scss:1458-1511` 的浅底浅字配色（如 `#ed7b00` 在 `lighten(#ed7b00,47%)` 底上）。
- 建议：状态标签改深色文字或加深底色使对比度 ≥4.5:1；同时不要只靠颜色区分状态（配合图标/文字）。

### L4 `.tag` 的 `!important` 击穿了 `.tag.is-small`

- 证据：【源码】`style.scss:1458-1469`：`.tag { border-radius: 30px !important; padding: 0 20px !important; &.is-small { font-size: 0.65rem; padding: 3px 5px; … } }`。【实测】注入 `.tag.is-small` 后计算 padding 为 `0px 20px`（与基础 `.tag` 相同，`3px 5px` 未生效），font-size 生效为 9.75px。即“小标签”只小在字号，横向内边距仍是 20px，在表格窄列里被挤压/撑宽。
- 建议：去掉 `padding` 上的 `!important`，或把 `is-small` 规则提到具有更高特异性的选择器下（`.tag.is-small`）。

### L5 小点击目标与仅 hover 可见的复制图标

- 证据：【实测】移动端（390px）与桌面均存在的微小点击目标：`a[data-cy=btn-preview]` 15×23、`a.copy-text` 15×19、`a "0"` 7×14、客户列表“2 查看”23×39、`a[data-cy=btn-advanced-search]` 79×19。另【源码】`style.scss:181-193` 的 `.copy-text .icon { visibility: hidden }` 只在 `:hover` 显示，没有 `:focus` 规则 → 键盘用户看不到复制图标。
- 建议：行内操作统一最小 24–32px 命中区（图标可小、点击区不可小）；图标显示条件补 `&:hover, &:focus-visible`。

### L6 硬编码英文文案绕过 `$t()`

- 证据：【源码】`views/Maintenance.vue:18,47,79` `label="Data"`（同文件其它字段已 i18n）；`views/CustomerListForm.vue:45` `placeholder="Opt-in type"`；`views/TemplateForm.vue:229` `toast(\`'${d.name}' updated\`)`（同一函数 `:209` 的创建却用 `globals.messages.created`）；`views/UserProfile.vue:96` `alt="QR Code"`；`components/VisualEditor.vue:4` `title="Visual email editor"`；`App.vue:179` `toast('Reloading app ...')`。
- 建议：逐条替换为已有 i18n 键；可考虑加一条 lint 规则（模板裸字符串）防止回归。

### L7 导航“客户列表”分组永不自动展开/高亮

- 位置：`frontend/src/components/Navigation.vue:9-16`；`frontend/src/router/index.js:23,29,35`；`frontend/src/App.vue:154-164`。
- 证据：【源码】分组读取 `activeGroup.customer_lists`（`:9-10`），但路由 meta 写的是 `group: 'customerLists'`，`App.vue:159` 按 `to.meta.group` 写入 `activeGroup.customerLists`；同一标签 `@update:active="(state) => toggleGroup('customerLists', state)"` 提交的又是另一种 key。其余 5 个分组 key 一致、均正常。
- 影响：从其它入口跳到客户列表/表单页时，菜单不会展开也不会高亮，用户定位不到自己在哪。
- 建议：三处统一为 `customerLists`（或统一为 `customer_lists`）。

### L8 弹窗底部“关闭/取消”语义不一致

- 证据：【源码】`UserForm.vue:138`、`RoleForm.vue:120`、`TemplateForm.vue:96`、`CustomerListForm.vue:75` 用 `globals.buttons.close`，`PoolContactForm.vue:29` 用 `globals.buttons.cancel`；两者行为完全相同（`$parent.close()` 直接丢弃未保存修改）。
- 建议：统一文案，并在有未保存修改时给出确认。

### L9 主题 token 缺失、无深色模式

- 证据：【源码】主色 `#0055d4` 分别写在 `assets/style.scss:26`、`constants.js:53`、`components/editor-theme.js:16,18,21,28`；`email-builder/src/theme.ts:6` 又用 `#0079CC`；全仓没有 `prefers-color-scheme`/`data-theme` 规则。另 `theme.ts:6` 的字体栈把通用族 `sans-serif` 放在最前，与后台使用的 Inter 不一致。
- 建议：主色收敛为单一来源（CSS 变量 + 一处 JS 常量），为深色模式预留 `:root[data-theme=dark]` 覆盖；对齐编辑器的字体栈与品牌色。

### L10 断点体系不统一

- 证据：【源码】JS 判定用 `window.innerWidth <= 768`（`App.vue:270-272`），SCSS 另有 850/1024/1100/1500 四档（`style.scss:2192-2315`），其中 1100 档同时承担表格工具栏换行、侧栏标题隐藏、标签间距等互不相关的职责。
- 建议：定义一套命名断点（与 Bulma `$tablet` 对齐）供 JS/SCSS 共用。

### L11 导航用户下拉是 `<a>` 嵌套 `<a>`，退出登录无确认

- 证据：【源码】`App.vue:56-63`：`<b-navbar-item href="#"><router-link to="/user/profile">…` 与 `<b-navbar-item href="#"><a href="#" @click.prevent="doLogout">`；【实测】DOM 中确认存在嵌套（外层 `A[href="#"]` 内含 `A[href="/admin/user/profile"]`），实测点击内层链接可正常跳转（`/admin/customers → /admin/user/profile`，地址栏无残留 `#`），因此这是非法标记与辅助技术风险，而非功能故障。退出登录（`App.vue:192-196`）无确认。
- 建议：把外层 `b-navbar-item` 改为 `tag="div"`（或直接让它承担路由跳转），退出登录加确认。

## 四、与既有台账的重叠（不重复计数）

`docs/harness/TECH_DEBT.md` 已登记以下前端条目，本次审计补充了更精确的失效条件或证据，**不再重复登记**：

| 已登记条目 | 本次补充 |
| --- | --- |
| 无界轮询与脆弱的 iframe 握手（`App.vue:184` 每 500ms；`VisualEditor.vue:85-97`） | `VisualEditor.vue:84-97` 的定时器初值 `let n = 10` 配合 `n += 1; if (n > 10)`，实际只重试 **1 次**（100ms）就放弃；慢加载时“导入可视化模板”不会生效且无提示。 |
| 活动只读权限未贯彻到编辑控件（`Campaign.vue:261`、`Editor.vue:69-76`） | `Editor.vue:61-76` 中只有 richtext 传了 `:disabled`，visual/code(markdown/html)/plain textarea 均未传；`Campaign.vue` 其它字段全部 `:disabled="!canEdit"`（如 `:67,72,80,85,95,110,159,176`），只读用户仍可在编辑器里输入无法保存的内容。 |
| Campaign 未保存检测只比较 2 字段、`beforeDestroy` 未清 `onbeforeunload`（`Campaign.vue:602-605,1244,1314-1316`） | 【实测】探测会话离开 `/admin/campaigns/new` 时浏览器记录了 beforeunload 拦截（`Blocked attempt to show a 'beforeunload' confirmation panel…`），说明该守卫在“未做任何编辑”的新建活动页就已生效；同时它从未被释放，会持续影响后续 SPA 会话。 |
| 表单生成器 JSON/元数据解析无捕获路径（`Campaign.vue` 归档保存 `JSON.parse`） | 现为 `Campaign.vue:912` 的 `archive_meta: JSON.parse(this.form.archiveMetaStr)`，其后的 `.then`（`:916-918`）也无 catch，失败时按钮无 loading、无 toast，用户以为已保存。 |
| 同 model 并发请求用单布尔 loading | 与 M5 的 `UserForm.vue:141`（错绑 `loading.customer_lists`）、`RoleForm.vue:122`（`loading.roles` 不存在于 `constants.js` 的 `models`）叠加：这两个保存按钮的进度提示永远不会出现。 |
| 活动/分析域长组件与重复、管理端 Vue 2 双栈 | 佐证见本文件“一致性”部分（四处批量选择实现、四种全选判定、三种空态写法）。 |

## 五、建议的收敛方案（按投入产出排序）

1. **统一状态组件**：把 `EmptyPlaceholder` 升级为 `TableState`（`loading`/`empty`/`error` + 主操作 + 插槽），所有 `b-table` 统一 `:loading` + `#empty v-if="!loading.x"`。这条同时解决 M1、H1 的一半，并消除现有三种空态写法。
2. **路由权限元数据 + 403 视图**：`router/index.js` 每条受限路由声明 `meta.permission`，`main.js` 的 `beforeEach` 统一判定；失败态一律渲染可重试错误页。解决 H1。
3. **危险操作统一封装**：`$utils.confirm(msg, fn, { type, confirmText, focus })` + 强制具体文案（数量/名称），并把 `confirm(null, …)` 列为禁止写法。解决 H3、M7。
4. **批量选择 mixin**：`bulk = { checked, all }` 的获取/重置/全选权限收敛到一处（含 `$canCrossPageSelect`）。解决 H4、M4。
5. **移动端规则**：禁止新增 `:mobile-cards="false"`（除非包 `overflow-x:auto` 容器），禁止表格 `min-width` 超过 480px。解决 H2。
6. **可访问性底线**：图标按钮必须 `aria-label`；表单控件必须有程序化标签；状态标签对比度 ≥4.5:1；点击目标 ≥24px。解决 L1–L5。
7. **编码规则补两条**：提交按钮必须 `:loading="x" :disabled="x"`；模板可见文案必须走 `$t()`。解决 M5、L6。

## 六、验收与回归清单

- 权限：以无 `settings:get`/`roles:get`/`audit:get`/`users:get` 的账号访问对应 URL，应重定向或显示 403 说明，不得出现永久 loading 或反复 toast。
- 列表：任何列表在加载期间不得同时出现 spinner 与空态；失败必须出现错误态与重试入口。
- 批量：批量操作前可见影响范围；成功后选择计数归零；跨页全选入口在四个列表页表现一致。
- 危险操作：删除/清空/撤销/角色变更的确认框含对象名称与数量，主按钮为危险色，取消为默认焦点。
- 移动端：390px 下 5 个组织/工作区页面无需整页横向拖动；`document.documentElement.scrollWidth <= innerWidth`。
- 可访问性：列表页纯图标控件均有可访问名；主表单（活动新建、管理组织、导入、个人资料）无未命名控件；状态标签对比度 ≥4.5:1。
- 建议把上述检查做成可重复门禁：复用 `dev/email_builder_rerender_verify.js` 的 Node+CDP 模式补 `dev/ui_probe.js`（登录 → 逐路由采集上面的量化指标 → 断言），并纳入 CI 或发布前清单。

## 七、未覆盖与不确定性

- 本次实测会话是**组织 1 经理**（无 `settings:*`/`users:*`/`roles:*`/`pools:*`）。因此 Users/Roles/Audit/Settings/维护页的**正向**交互（有权限时的表现）未逐项实测，相关结论均为源码结论；H1 的失败态恰因缺少权限而实测到。
- 平台管理员（`root`）口令本机未知（与 `STATUS.md` 既有记录一致），超级管理员视角的页面（池管理弹窗、组织归档、角色表单等）未做浏览器实测。
- 布局类断言在隐藏的工作面板浏览器里不可靠（viewport 为 0），本文的布局数据全部来自独立的 headless Chrome（强制 1440×900 与 390×844）。
- 无 `prefers-color-scheme` 支持、断点不统一等结论来自全仓文本检索，未做跨浏览器（Safari/Firefox）渲染对比。
- 未评估 200+ 行的大组件拆分方案（`Campaign.vue` 1211 行、`CampaignAnalyticsReport.vue` 927 行等），该部分已由 `TECH_DEBT.md` 的长组件条目覆盖。

来源：`frontend/src/{App.vue,main.js,router/index.js,utils.js,constants.js,api/index.js}`、`frontend/src/assets/style.scss`、`frontend/src/components/{Navigation.vue,EmptyPlaceholder.vue,Editor.vue,VisualEditor.vue,Chart.vue,LogView.vue}`、`frontend/src/views/**`、`frontend/email-builder/src/theme.ts`、`frontend/node_modules/buefy/src/components/{table/Table.vue,button/Button.vue}`、`i18n/{zh-CN,en}.json`、`dev/pools_e2e_seed.sql`。实测环境：`dev-backend-1`（9173）+ headless Chrome（CDP）。
