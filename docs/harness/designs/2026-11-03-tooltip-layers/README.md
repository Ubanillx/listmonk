# 全平台 tooltip 裁切整改

日期：2026-11-03。范围：既有运营后台的提示气泡显示与清理，不调整页面结构、品牌、业务动作、权限或统计口径。

## 诊断与修复

用户指出仪表盘「投递与退订分析」的提示被容器截断。真实组件复现确认：`DashboardChart.vue` 的 `overflow: hidden` 与 `contain: strict` 会裁切内部 HTML 提示，提高内部 z-index 不能解决。选择页面级 body 浮层，保留原有图表隔离和容器滚动，加入视口边缘避让和长文本换行。

| 检查入口 | 结果与处理 |
| --- | --- |
| 仪表盘七个 ECharts 图表 | 共用 `utils/chartTooltip.js`，body 挂载、1000 层级、420px 最大宽度、8px 安全边距；七图桌面实际坐标检查通过，投递图另验 390px 长标题。 |
| `CampaignGeoHeatmap.vue` | 采用同一配置，实际广州数据点的城市/地区/国家/次数提示验证通过；销毁后气泡移除。 |
| 11 个文件中的 `b-tooltip` | 在 `main.js` 安装统一 Buefy 扩展 `AppTooltip.js`，覆盖客户、活动、列表、模板、用户、角色、个人 API Key、顶部刷新等入口，无须逐页面配置。320px 上限、翻转、夹紧、键盘 focus/Escape，保留原 slots/props。 |
| `IconTip.vue` | 已有 fixed 定位仍可能受 paint containment/transform 限制，改为 body 挂载，补充销毁清理与 Escape。 |
| `Chart.vue` / `BarChart.vue` | 旧统计、营销报告及发件服务仍采用 Chart.js；画布内提示不能越过 canvas 边界。共用 `canvasTooltip.js`，保留格式化标题、正文、百分比和颜色，以文本节点输出 body 浮层，销毁时清理。 |
| 邮件编辑器 MUI、富文本编辑器与公开页面 | 源码审查未发现关闭 portal 的 MUI 配置、同类自制 tooltip 或需要此次修正的挂载策略；未宣称逐一操作全部编辑器状态。 |

指针图表提示在页面/容器滚动或缩放时关闭；Buefy 键盘提示随仍可见的焦点重新定位，避免聚焦造成的自动滚动立即关闭气泡。触发器离开视口后关闭。统一销毁节点和监听器。

## 验证与证据

- 前端 lint 通过；保留原有 `CampaignGeoHeatmap.vue` 的 `vue/no-template-shadow` 警告。
- 生产前端构建通过；按项目要求重启 `dev-backend-1`，以 9173 HTTP 200 和启动日志确认本地同步。未发布线上站点。
- 隔离 Cypress（`listmonk-cypress`、9273，非本地开发数据库）运行 `frontend/cypress/e2e/tooltip-layers.cy.js`：4/4 通过，覆盖桌面/390px 长标题、页面级提示、键盘/Escape、canvas 百分比及导航清理。测试等待实际布局帧完成后触发提示，避免原生滚动/ResizeObserver 在触发后才完成。
- 浏览器补测：七个仪表盘图表、手机长标题、横向滚动/paint containment、弹窗层级、地理图提示、IconTip 与两种图表销毁清理。
- 独立视觉评审两次（桌面一轮，手机补充复核），均通过，不打分；交互由主执行者实测。没有新增业务流程或动效，没有删除界面文字。
- 使用宿主浏览器截图，无截图工具默认做法提示；没有保留需解释的提示项。数据是隔离测试内容，不是线上业务数据。
- 更新既有 `UI_DESIGN_SYSTEM.md` 的浮层规范及 `ARCHITECTURE.md` 的共用组件说明，无待确认设计条目。

截图：[整改前](before-delivery.png)、[桌面整改后](after-delivery.png)、[手机投递提示](after-mobile.png)、[手机表格提示](after-table-mobile.png)、[手机旧图表提示](after-canvas-mobile.png)。

上述图片使用真实 Vue/Buefy/ECharts/Chart.js 组件与既有样式，加载隔离模拟数据。其他页面的覆盖来自共用入口审查及代表性运行验证，不等同于全部页面、全部权限和全部状态的逐项截图验收。
