# React and TypeScript migration

The live service now embeds the React application and terminal. The combined
candidate preserves the local-runtime/recovery changes through c881185. Final
project verification passed: **PASS: 7/7 steps passed (backend=web)**.

## Implemented

- React 19.3, TypeScript 5.9 strict/noUncheckedIndexedAccess, Vite 8.3.
- Typed authenticated API client, multipart/error/abort handling, query-token SSE.
- Main application navigation/hash links/saved view, token modal, global SSE
  reconnect/foreground resync, badges/toasts, command palette, live Deck panes,
  approvals, push enrollment.
- Board/task/detail/create/routines, Sessions/conversations/history/search,
  Settings/targets/projects/agents/profiles/MCP/skills are real React components.
- Actual xterm terminal engine integrated into React controls/dialogs; retained
  iframe terminal tabs, keyboard/IME/touch/scroll/search/PDF/files/history/review.
- Typed service worker with content-stamped React/font precache, offline root
  fallback, update/cache retirement, API/terminal and POST exclusions, push.
- Production Go embedding and reproducible Docker/CI frontend build stages.

## Validation and deployment

- Full Go suite passes; frontend transport and retained-secret tests pass 6/6.
- Final full browser run passes all 262 tests. The standalone-PTY fixture
  explicitly clears inherited TMUX so it tests its own terminal instead of
  opening a popup in the invoking agent's workspace.
- Clean Docker build and isolated runtime pass: health, React HTML, hashed
  assets, and service worker. Node is used only during the build.
- Live browser checks pass at 390px and 1440px, including mobile More navigation
  and all nine current session cards. No overflow or JavaScript errors.
- Deployment backs up SQLite and the previous binary, atomically replaces the
  executable, and verifies every existing tmux pane PID survives. The systemd
  unit uses KillMode=process so a web-service restart preserves interactive agents.
- Durable receipts, logs, candidates, and screenshots are under
  `.verify-artifacts/migration/`; deployment-receipt.json identifies the backup.

Build with `npm run build --prefix frontend`, then
`python3 frontend/scripts/stage.py`, followed by the Go build. Staging does not
restart the service. CI and Docker perform the frontend build before Go embeds.
