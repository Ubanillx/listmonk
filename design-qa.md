# BestEDM 发送分析 — 设计与功能验收

日期：2026-10-20

**Findings**

无待修复的 P0/P1/P2 问题。按用户选定的第 2 张左右联动效果图接入现有 Listmonk，批次输入按用户新要求改为下拉选择。

**Evidence And Normalization**

- Source visual truth: `C:/Users/topmax/.codex/generated_images/01a15951-752e-7463-b84a-341716ead0b8/exec-863f8992-c01f-4a3c-99a8-f1eeaef8718c.png`，1487 × 1058 像素。
- Implementation: `C:/Users/topmax/Documents/Codex/2026-10-20/https-www-bestedm-net-apidoc-base/outputs/bestedm-analysis/发送分析-桌面.png`，1473 × 1045 像素，来自生产构建后的 9173 页面。
- CSS viewport：1488 × 1056；实测 `innerWidth=1488`、`innerHeight=1056`、DPR 约 1。截图工具输出比 CSS 视口略小，已保持宽高比统一缩放后比较，不据此判定字号或像素偏移。
- State：中文、演示数据、发送分析、选中批次、失败明细已加载。参考图选中最后一个虚构批次，实现选中第一个示例批次；指标跟随选中范围，数值不要求与生图一致。
- Full-view comparison：`outputs/bestedm-analysis/效果图对照.png`；来源和实现同时放入同一图，完整构图分别等比缩放至 1100px 宽后观察。
- Focused comparison：`outputs/bestedm-analysis/明细区域对照.png`；左右统计、选择状态、错误标签、搜索框、表头、操作与分页同图比较。来源裁切 (244,455)-(1460,1009)，实现裁切 (290,450)-(1420,1020)，分别等比缩放至 900px 宽。
- Mobile：`outputs/bestedm-analysis/发送分析-手机.png`；CSS 视口 390 × 844，双栏改为上下布局；实测根页面无横向溢出，全部筛选/分页控件边界位于视口内。宽表格在自己的容器内滚动。

**Required Fidelity Surfaces**

- Fonts and typography：沿用 Listmonk 字体及中文回退；标题、数字、次要说明的层级保留。长批次使用较小字号保持单行；数字使用等宽数字排版，缺失值显示“—”。不声称生成图中的字体有可恢复的精确字体文件。
- Spacing and layout rhythm：共享查询栏、四张指标卡、左右统计/明细顺序与参考一致；修复了批次换行造成的过高行距。保留既有导航宽度和标准分页（含页码跳转），表格独立滚动，手机改为上下排列。
- Colors and tokens：发送蓝、成功绿、失败珊瑚红、无效/未发送紫；浅色背景、蓝色选中行、红/黄分类标签，未使用深色指标背景。标签同时有文字，含义不只靠颜色。
- Image quality and assets：参考为数据控制台，无需生成照片或插画；使用现有 Listmonk 标识和 MDI 图标。未把整个效果图作为界面图片，也未制作替代图形资产。
- Copy and content：顶层为总览/发送分析/投递日志/账号设置。批次来自所选日期内统计记录去重，不允许手工输入，不把统计记录 ID 当发送任务 ID。无效/未发送采用上游现有字段含义，不计算虚构比率。

**Accepted Product Constraints**

- 保留现有应用外框、标准控件和分页，不全局修改导航宽度或字号。
- 保留一次 Excel 导出入口，移除效果图中的重复导出按钮。
- 统计卡跟随当前批次；左侧保留所选日期内的批次以便切换。
- 服务商返回的重复记录不去重；仅批次候选去重。同批次跨日期可高亮多行。
- SMTP 无效地址不能可靠归属批次，提供“按日期查看”；错误类型 1/2/3 无批次时禁用。
- 失败明细不提供收件人发送日期，“查日志”只预填邮箱和候选日期并提示确认，不自动消耗有额度限制的日志调用。

**Comparison History**

1. 初版对照：长批次换行、行高偏大，错误分类标签实色过重，为 P2；页头说明使纵向密度偏松。
2. 修复：加宽左侧比例，缩小批次字号，收紧表格行距；分类标签使用浅红/浅黄，压缩页头和指标间距；限制双表高度并保留独立滚动。
3. 对照复验：同时检查完整比较图和明细比较图；批次不再换行，左右层级、分类颜色和选中反馈清楚，控件不碰撞。生产构建后再次保存同状态截图并比较，无待修复 P0/P1/P2。

**Interaction Verification**

- `go test ./internal/bestedm ./cmd -count=1`：346 项通过，包括邮箱大小写/空白搜索与实际 Excel 内容一致的验证。
- `node dev/run-cypress.js --spec cypress/e2e/delivery-provider.cy.js`：隔离数据库 10/10 通过，覆盖草稿保留、批次下拉/点击联动、已应用日期、类型约束、搜索导出、日志按需、延迟响应隔离、日期刷新清除失效批次、旧 failures URL、手机控件和凭据工作流。
- 初次回归的手机整页截图工具超时，控件断言通过；改为视口截图后整套 10/10 通过，另用浏览器保存手机整页截图检查。
- 前端 lint / 生产构建通过；保留已有地图变量 warning。生产资源同步后重启后端，9173 返回 HTTP 200，启动成功。
- 在 8181 与 9173 页面检查实际渲染和交互，未见浏览器 console error。
- 当前已配置真实账号：日期统计返回 7 条记录，批次下拉含 5 个去重批次；选择一个批次后同批次跨日 2 行高亮，失败明细正常加载，导出入口可用，无接口错误。未将真实账号、邮箱明细或截图保存到交付文件。

**Open Questions And Residual Gaps**

- 服务商 SMTP 实际发送、数据分类交叉口径、服务商时区和刷新延迟仍是独立联调项。
- 未进行完整读屏、多浏览器或 200% 缩放审计。

**Implementation Checklist**

- [x] 合并发送统计与失败地址。
- [x] 从统计结果提供批次下拉，并兼容重复批次跨日记录。
- [x] 筛选、搜索、导出、按需日志和过期响应隔离。
- [x] 桌面/手机截图对照与真实账号只读检查。
- [x] 同步用户/API/架构文档，生产资源构建及后端重启。

final result: passed

---

以下保留此前客户导入页面验收记录。

# Customer import issue report - design QA

Date: 2026-10-19

## Findings

No actionable P0/P1/P2 findings remain in the compared result panel. This is an
integration into the existing admin UI, not a pixel-identical standalone page.

## Evidence And Normalization

- Source visual truth: `C:/Users/topmax/.codex/generated_images/01a1537a-a469-7831-bbff-cc451bbf2f7a/call_ab998f06fb49481f96c8107ef84b2325.png` (1672 x 941 pixels).
- Implementation: `frontend/cypress/screenshots/pool-multi-email-import.cy.js/pool-import-errors-desktop-panel.png` (955 x 1291 pixels).
- Desktop viewport: 1280 x 720 CSS pixels; the screenshot captures the full result element, including content below the viewport, at CSS-pixel density.
- Mobile viewport: 390 x 844 CSS pixels; `frontend/cypress/screenshots/pool-multi-email-import.cy.js/pool-import-errors-mobile-panel.png` (358 x 1704 pixels).
- Full-view comparison: `.codex-artifacts/import-issues-design-comparison.png`. Both artifacts are in the same image, each scaled to 800 pixels wide with aspect ratio preserved.
- Focused comparison: `.codex-artifacts/import-issues-design-focus.png`. Source crop (75,65)-(1595,595) and implementation crop (0,0)-(955,600) each scale to 760 pixels wide with aspect ratio preserved. Inspected together for summary cards, heading, filters, table headings, icons, and type labels.
- State: completed public-pool import, all issue types selected, first page. The source uses illustrative counts and seven example rows; the implementation uses a real isolated import with 253 records, 252 issues, and ten rows per page. Do not infer pixel-identical density or page height from these different data sets and source frame widths.

## Required Fidelity Surfaces

- Fonts and typography: retained the admin UI's existing font family/fallbacks. Dark semibold headings, prominent summary counts, muted labels, and blue actions preserve the source hierarchy. Live Chinese labels are legible, with no broken glyphs. Longer reasons wrap within their cells rather than colliding with actions. Exact font identity cannot be recovered from the generated reference and is not claimed.
- Spacing and layout rhythm: summary cards precede the tinted issue header, filter/search/range toolbar, issue table, and pagination. Existing application control sizes and table row spacing are intentionally retained instead of shrinking the UI to the reference's wide compact composition. Six cards fit on desktop; mobile uses two per row and stacks the toolbar. The table scrolls within its own region, not the page; pagination wraps and remains reachable.
- Colors and tokens: neutral total/duplicate cards, green created, blue reused, orange conflict, and red invalid states preserve semantic grouping. The light red issue header and blue export/details actions match the reference direction while using the current application palette. Type badges supplement color with readable text.
- Image quality and asset fidelity: no photography, logo, or decorative illustration is required in this panel. Icons use the existing MDI library; no handcrafted replacement art, rasterized UI, or placeholder imagery is used. Icons remain sharp at desktop and mobile sizes.
- Copy and content: labels and Excel headers are localized. Row number, customer code, allocation department, type, and reason reflect the actual API. The mockup's invented phone/name/email columns are deliberately omitted to preserve the existing privacy boundary. Conflict copy correctly explains that the contact was created separately; duplicate copy explains that the duplicate was skipped.

## Accepted Product Constraints

- Keep the reused-contact summary and blocklist result count, even though the illustrative reference omits these existing import outcomes.
- Keep this result panel in the current import workflow rather than introducing the mockup's separate page, invented timestamp, or navigation.
- Reuse shared `AppPagination`, including page-size selection and page jump, rather than recreating a second pagination design.
- Ten real rows and wrapped reasons make the panel taller than the seven-row mockup. Normal vertical page scrolling is expected; persistent controls do not cause page-level horizontal overflow.

## Interaction Verification

The final isolated Cypress run passed 9/9 tests across import, pagination, and
public-pool lists. Import coverage includes category-card filtering, select
filtering, row/code/department search, detail dialog, page jump, size changes,
last-page ranges, no-match state, no-issue state, reimport, and blocklist behavior.
It checks desktop/mobile document overflow and downloads a real workbook.

Excel checks confirm all 252 issues export even when one row is visible after
filtering, localized column headings, preserved formula-like values as text,
and no email leakage. The API returns more than 200 issues without truncation.

Evidence: `.codex-artifacts/import-issues-final-cypress.log`.

## Comparison History

- Final handoff comparison: inspected the combined full-view and focused images, then the mobile panel capture. No new P0/P1/P2 difference required a visual fix during this pass. Earlier functional test repairs are not represented as visual-QA iterations.

## Open Questions And Residual Gaps

- No unresolved design decision blocks delivery.
- Automated coverage and visual inspection are not a full screen-reader, keyboard-only, 200% zoom, or multi-browser accessibility audit.
- Extremely large reports still travel as a complete import response; displayed rows are paginated client-side. This change does not introduce a persistent server-side report resource or report-history page.
- The existing development database has a separate transactional-template compilation warning for template 3 (`RootURL` undefined). Backend startup and HTTP 200 succeed; that unrelated template is not modified here.

## Implementation Checklist

- [x] Compare the source and rendered implementation in combined images.
- [x] Inspect typography, spacing, colors, assets, and copy.
- [x] Verify desktop/mobile layout and primary report interactions.
- [x] Verify complete Excel output and privacy/text safety.
- [x] Run full Go tests, focused database tests, isolated browser tests, build, documentation checks, and whitespace checks.
- [x] Restart the local backend after the latest production frontend build; verify HTTP 200 and no pending database upgrades.

final result: passed

---

# Organization pool allocation workbench - design QA

Date: 2026-10-19

## Findings

No unresolved P0/P1/P2 design findings remain in the reviewed workbench.
The result integrates selected AI concept 3 into the existing admin UI; it is
not a pixel-identical recreation of invented mockup data.

## Evidence And Normalization

- Source: `concept-3-organization-workbench.png`, 1442 x 1091 generated pixels.
- Actual application: `implementation-desktop.jpg`, captured from the production asset server at port 9173, with measured CSS viewport 1442 x 1091.
- Combined source/result: `design-comparison.png`. Both full captures are scaled proportionally to fit 1050 x 820 panels; no crop or stretch is used.
- Mobile: `implementation-mobile.jpg`, measured viewport 390 x 844. Card top 16, bottom 828; fixed footer top 753, bottom 828. Measured document width 375 <= viewport 390 (native scrollbar gutter).
- Large desktop: measured 1644 x 1244; workbench width 1180, x 224.33..1404.33, y 137.70..1106.30; document width 1629 <= viewport 1644.
- Visual state difference: source illustrates a delegated operator and two removed contacts; live development data contains a highest administrator and three active sample contacts. Thus remove replaces restore, and the separately authorized delivery action is visible. Cypress additionally captures the Chinese removed-contact state with a delegated UI profile. These states must not be mistaken for a fidelity failure or permission inference.
- Browser console: no warnings or errors captured in the reviewed production tab. Temporary viewport override reset after QA; no live development customer/allocation mutation made for screenshots.

## Required Fidelity Surfaces

- Typography: preserve the existing application font/fallbacks, semibold headings, muted helper text, and clear identity/email hierarchy. Exact generated font identity is not claimed. Member identity is a single block on mobile, preventing email/name side-by-side compression.
- Spacing/layout: centered modal with 240px organization rail, three tabs and right-hand content. Header close remains at top right; add-existing is at footer left, close at footer right. Search/filter, member rows, pagination, and a distinct delivery summary retain the reference hierarchy. Three actual rows and the separate allocation-name line change vertical density intentionally.
- Colors: existing --lm-* blue/white/neutral semantic tokens, blue row actions and selection, green active/authorization badges, light border and surface hierarchy. No decorative gradients or new theme.
- Assets: retained native Listmonk logo and MDI icons; no rasterized UI or replacement artwork in production. Generated images are design evidence only.
- Copy: localized English/Simplified/Traditional Chinese labels. Management, shared master data and delivery authorization are explicitly distinct. Lifecycle state is separate from contact status; restoring a relationship does not clear a blocklist or grant delivery.

## Comparison And Repair History

1. Initial rendered capture revealed the global modal header display:block moving close below the title. Applied explicit scoped flex layout and verified top-right close.
2. Toolbar controls all flexed equally. Restricted flex growth to search, used primary search action, and blue text row operations.
3. Default modal-content wrapper plus a full-height card caused nested mobile overflow and put the footer below the viewport. Switched this modal to Buefy has-modal-card, bounded card height, and kept only the body scrollable. Mobile bounds now have automated assertions.
4. Global button spacing defeated margin:auto on add-existing. Increased the local footer selector specificity; automated desktop assertion verifies a large horizontal separation from close.
5. Wrapped member identity as a single mobile cell block; inspected desktop and mobile evidence again.

## Interaction And Permission Evidence

- Workbench Cypress: 9/9, including relation remove/restore, lifecycle, URL reload, dirty-close, persistent failure, mobile bounds, delegated controls, no read-permission customer request, late organization responses, and Chinese removed-contact capture.
- Public-pool hierarchy: 4/4. Existing full-page screenshot was timing out in Electron; viewport capture preserves UI evidence without removing any business assertion.
- Business permissions: 8/8 after fixing the newly added permission to be default-disabled and high-risk in RoleForm. Partial permission save still does not expand grants.
- Real PostgreSQL and HTTP tests cover scope, safe DTO, transactions, version conflict, membership race, queued source/transfer/state/grant/blocklist checks, audit rollback and migration. This is not an external SMTP delivery test.

## Residual Limits

- Not a full screen-reader, 200% zoom or multi-browser audit.
- Four broader preexisting fixture/migration failures remain documented in INTERACTION_SPEC.md section 12; this visual pass does not mark the broad database suite as passed.
- Existing development transactional template 3 RootURL compilation error remains unrelated and unchanged. Startup and HTTP 200 still succeed.

## Implementation Checklist

- [x] Compare selected source and rendered implementation together.
- [x] Inspect typography, spacing, colors, assets and copy.
- [x] Fix all observed P0/P1/P2 visual issues and recapture.
- [x] Verify 1442/1644 desktop and 390 mobile measurements.
- [x] Verify interaction, permission boundaries and default-grant safety.
- [x] Rebuild production assets and synchronize backend runtime.

final result: passed

## Allocation Tab Continuity Follow-Up (2026-10-19)

The root cause was the fullPath router-view key, not a necessary reload of allocation metadata. Only internal allocation-management query parameters are excluded on pool list/detail routes. The existing organization-management key behavior stays intact. Tabs retain data/filter/selection/draft state in the current target, mutations invalidate data, and the modal now has a stable 90vh frame. Live production/Vite checks confirm unchanged height (648px at the current viewport), draft and selection retention, and no console errors. Evidence: `.codex-artifacts/allocation-tabs-info.jpg`; Cypress directly compares modal/organization DOM identity, height and network counts.
