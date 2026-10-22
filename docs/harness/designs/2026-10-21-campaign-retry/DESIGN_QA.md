# Campaign Delivery Overview Design QA

Date: 2026-10-21

## Findings

No remaining actionable P0/P1/P2 findings within the implemented delivery-overview API scope.
This is an integration into the existing Listmonk design system, not a pixel-identical standalone mock.
The API limitations listed below remain explicit product limitations, not fabricated UI states.

Resolved findings:

- [P2] The audience card repeated its source/list information, increasing the two-column section height.
  Fix: combine the source label and audience names into one row and tighten fact-row spacing.
- [P2] Buefy's default green export/retry buttons and progress color differed from the blue reference.
  Fix: scope primary controls and progress values to the existing `--lm-color-primary` token.
- [P2] Failed-recipient rows were substantially taller than the reference.
  Fix: use compact desktop cell padding and 13px table text; retain larger mobile cell padding.
- [P2] The reference's header selection control was missing.
  Fix: add a labeled current-page checkbox with partial selection, disabled/error handling, and eligibility filtering.
  It never selects deferred, queued, or otherwise ineligible recipients, nor rows from other pages.
- [P2] An early mobile evidence capture included an unfinished loading transition and repeated sticky navigation.
  Fix: wait for every overview loading overlay to have `display: none`, wait for both reload requests,
  and capture the component with fixed navigation hidden only during the test screenshot.
  No production loading behavior or navigation styles were changed to produce the capture.

## Visual Truth And Implementation

All paths in this section are relative to the repository root.

Source visual truth:

- `docs/harness/designs/2026-10-21-campaign-retry/private-overview-failed-customers.png`
- `docs/harness/designs/2026-10-21-campaign-retry/pool-overview-failed-customers.png`

Rendered implementation:

- `docs/harness/designs/2026-10-21-campaign-retry/private-overview-implementation-desktop.png`
- `docs/harness/designs/2026-10-21-campaign-retry/pool-overview-implementation-desktop.png`
- `docs/harness/designs/2026-10-21-campaign-retry/private-overview-implementation-mobile.png`
- `docs/harness/designs/2026-10-21-campaign-retry/pool-overview-implementation-mobile.png`
- `docs/harness/designs/2026-10-21-campaign-retry/overview-production-desktop.png`
- `docs/harness/designs/2026-10-21-campaign-retry/overview-production-mobile.png`

Implementation sources:

- `frontend/src/components/CampaignDeliveryOverview.vue`
- `frontend/src/views/Campaign.vue`
- `frontend/cypress/e2e/campaign-delivery-overview.cy.js`

## Normalization And State

- Both source images are 1508 x 1043 pixels. Their CSS viewport/density metadata is unavailable.
  Comparison treats the raster as 1x; it does not assert exact font metrics from an unknown source density.
- Desktop browser viewport: 1280 x 720 CSS pixels, DPR 1.
  Private component capture: 913 x 1281 pixels. Pool component capture: 913 x 889 pixels.
- Source content crops: private `(268,224)-(1192,1016)`, 924 x 792 pixels;
  pool `(270,229)-(1226,1028)`, 956 x 799 pixels. Crops remove unrelated application chrome.
- Full-view comparison canvases keep both images at 1:1 raster scale without stretching to hide width differences.
  The implementation has a slightly narrower content region than the source crop.
- Mobile viewport: 390 x 844 CSS pixels, DPR 1. Component width: 356 pixels.
  Private component capture: 356 x 3367; pool component capture: 356 x 1504.
  No mobile source was supplied; these are responsive usability checks, not mobile pixel-fidelity claims.
- Source and fixture captures show paused campaigns. Private fixtures use 100 planned / 70 sent / 21 failed,
  ten rows on the captured page, and a deferred recipient. The source has different counts and three example rows.
  Pool fixtures use English, two organizations and two rows; the source uses Chinese and three organizations.
  These data/language differences explain total height and wrapping differences; they are not hidden by scaling.
- The production browser check uses the existing draft campaign at `/admin/campaigns/24#overview`.
  Its real delivery snapshots and failure list are empty; zero totals are expected, not substituted fixture data.

## Comparison Evidence And History

The source and implementation were opened together in these combined comparison images:

- `docs/harness/designs/2026-10-21-campaign-retry/private-overview-side-by-side.png`
- `docs/harness/designs/2026-10-21-campaign-retry/pool-overview-side-by-side.png`

Readable focused evidence:

- `docs/harness/designs/2026-10-21-campaign-retry/private-overview-focused-comparison.png`
- `docs/harness/designs/2026-10-21-campaign-retry/pool-overview-focused-comparison.png`
- `docs/harness/designs/2026-10-21-campaign-retry/private-overview-controls-comparison.png`
- `docs/harness/designs/2026-10-21-campaign-retry/pool-overview-controls-comparison.png`

Iteration 1 found duplicate audience information and default green primary controls. Those findings blocked acceptance.
Iteration 2 re-captured the corrected cards/controls, then found excessive desktop table density and missing header selection.
Iteration 3 re-captured compact rows and header selection, and exercised selection exclusion/partial state in Cypress.
The mobile capture was then corrected and re-opened to remove evidence-only loading/sticky-chrome artifacts.
The final combined desktop comparisons and clean mobile captures are the post-fix evidence.

## Required Fidelity Surfaces

- Fonts/typography: retain the application's existing `Inter, "Helvetica Neue", sans-serif` stack and Chinese fallback.
  Section headings are 18px/600; totals are 28px with tabular numbers; table text is 13px.
  Labels, secondary customer codes, and percentages remain visually distinct. Source raster text is not used as an exact font specification.
- Spacing/layout: preserve four summary metrics, private two-column progress/audience cards, pool organization table,
  and the failed-recipient action area. Mobile uses two metric columns, stacked cards and form controls,
  responsive recipient cards, and local horizontal scrolling only for the organization table.
  Production DOM checks found document width 1265 at 1280px and 375 at 390px; no document overflow.
- Colors/tokens: use existing primary `#155eef`, border `#eaecf0`, surface and text tokens.
  Export is blue outlined, retry is blue filled, and eligibility uses soft green/orange status tags with text labels.
  Disabled controls remain visibly disabled. No new global theme or parallel-task stylesheet changes were introduced.
- Image quality/assets: the target contains ordinary editable UI, not photographs or illustrations.
  Existing Material Design Icons supply refresh/search/download icons; no rasterized UI, placeholder artwork or custom SVG substitutes.
- Copy/content: three locales cover the overview and selection controls. Failed recipients are distinct currently unsent customers;
  failed attempts remain historical event counts. The list never invents unsubscribe/allocation causes from a generic status.
  Source mock names/counts are not hardcoded into the production interface.

## Functional And Runtime Verification

- Combined isolated Cypress run: overview 2/2 and historical send-error report 3/3 passed.
  An earlier historical-report run hit a screenshot timeout; the complete rerun passed without changing its business assertions.
- The checked-in visual evidence remains available above. Routine overview E2E runs omit explicit screenshot commands:
  Electron timed out on different captures in two exact-index attempts even though both business tests passed separately.
  This keeps visual acceptance evidence separate from the nonvisual behavior regression gate.
- Final isolated overview rerun: 2/2 passed, including disabled select-all after a list failure and clean mobile screenshots.
- Verified organization filtering, applied-filter export rather than unsubmitted search text, exact selected retry identities,
  409 retry failure followed by successful re-selection, confirmation, pagination/size selection, selection reset,
  current-page select-all eligibility, read-only controls, empty state, 503 recovery and URL hash reload.
- Go full suite: 747 passed across 28 packages. Real PostgreSQL targeted tests: 5 passed across 2 packages.
- Frontend lint: 0 errors, one existing `CampaignGeoHeatmap.vue` template-shadow warning.
  The new Cypress spec also passes its explicit ESLint check with 0 errors/warnings.
- Full `make -B build-frontend` completed, including the email builder; subsequent source changes were rebuilt by the isolated runner.
  `make build-frontend` then reported current assets. Generated assets were not manually edited.
- Restarted `dev-backend-1` after the last asset build. Port 9173 returned HTTP 200;
  logs reported no pending upgrades and a listening HTTP server, with no new startup errors.
- The production page was reopened after restart to verify fresh dynamic chunks, real API rendering, refresh, empty state,
  labeled disabled selection, and 390px export-control bounds. Browser error logs were empty after the final reload/navigation.
- Locale JSON/key parity, documentation checks and `git diff --check` pass.
- No real marketing email was sent and no production/dev customer data was reset. Cypress used its isolated database.

## Explicit Limitations And Follow-up Polish

- The delivery-overview API does not supply the source-pool/allocation description or per-organization SMTP/readiness state
  illustrated in the pool reference. Those columns are not shown; this QA pass does not claim those features are integrated.
  Adding authoritative fields and their permission-aware API contract is separate work; the UI must not infer readiness from progress.
- The reference's exact typography, selected sample rows, count values and English/Chinese fixture line breaks are not pixel-identical.
  Existing application tokens and pagination are intentional integration constraints.
- [P3] The reference's small link chevrons and exact tag shape can be refined without changing the completed core workflow.
- This is not a full screen-reader/keyboard accessibility certification or a real SMTP delivery acceptance test.

## Implementation Checklist

- [x] Correct primary colors, card duplication and excessive row density.
- [x] Add eligible-only current-page selection and stale/error selection safeguards.
- [x] Re-capture and inspect combined source/render comparisons plus clean mobile evidence.
- [x] Verify interfaces, failure recovery, authorization behavior and selected retry payloads.
- [x] Pass lint, targeted/full tests, documentation checks and production asset sync.
- [x] Verify the real production-served page after the final restart.

final result: passed
