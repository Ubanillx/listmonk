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
