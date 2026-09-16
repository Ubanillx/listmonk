# @listmonk/email-builder

The visual email editor used by the listmonk admin UI. It is a standalone React 18 + TypeScript subproject of `frontend/`, built as a UMD library around the EmailBuilder.js block components (`@usewaypoint/*`).

The Vue admin UI loads the built bundle into an iframe and drives it through the global `EmailBuilder` object (`render`, `resetDocument`, `isRendered`); see `frontend/src/components/VisualEditor.vue`.

This subproject uses yarn (`packageManager` is pinned to yarn 1.22.22). From this directory:

- `yarn install` - install dependencies.
- `yarn dev` - start the Vite playground on http://localhost:5173.
- `yarn build` - build the library to `dist/email-builder.umd.js`.
- `yarn preview` - serve the production build locally.

There are no lint or test scripts in this subproject.

## Integration with the admin UI

`make build-email-builder` runs `yarn build` here and copies `dist/*` into `frontend/public/static/email-builder/`. `make build-frontend` builds both this editor and the Vue admin UI. At runtime the bundle is served at `/admin/static/email-builder/email-builder.umd.js` and injected into the editor iframe by `VisualEditor.vue`.

See `../dev/README.md` for the local development suite and `../AGENTS.md` for repository conventions.
