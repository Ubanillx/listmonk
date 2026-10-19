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
