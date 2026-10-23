# Documentation map

This repository contains the source for the static website https://listmonk.app, the user documentation, and the engineering records.

- Website source: `docs/site` (Hugo; config `docs/site/config.toml`). The repository does not vendor the Hugo toolchain, and CI pins the Hugo version in `.github/workflows/github-pages.yml`; to preview locally, run `hugo serve` inside `docs/site`.
- User documentation: `docs/docs` (mkdocs: `mkdocs.yml` + `content/`). From the repository root, preview with `pip install -r docs/docs/requirements.txt` and `mkdocs serve -f docs/docs/mkdocs.yml`.
- Engineering architecture: `docs/ARCHITECTURE.md` is the authoritative document; engineering tracking records (TODOs, plans, status, technical debt, architecture, business logic) are in `docs/harness/`.
- API specification: `docs/swagger/`.
- README figures: `docs/readme/` holds the diagrams and UI screenshots used by the root `README.md`, with the capture and regeneration workflow described in `docs/readme/README.md`.
- Translation workbench: `docs/i18n/` (served at https://listmonk.app/i18n), which edits the language packs in `i18n/`.
- OpenClaw marketing skill: `skills/listmonk-openclaw-marketing` (SKILL.md + `references/` + Python CLI + unittest suite). It packages this deployment's REST contracts and permission boundaries for agent platforms such as OpenClaw; it is a toolset, not product code, and the root `README.md` exposes it in its 智能体接入 section.

- Documentation is validated with `python scripts/check_docs.py` (nav reachability, in-page anchors, relative links) and `mkdocs build --strict`; both run in `.github/workflows/docs-sanity.yml` on every push to a pull request that touches `docs/**`.
