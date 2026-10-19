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
