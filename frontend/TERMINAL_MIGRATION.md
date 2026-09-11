# React terminal

Source lives in `src/terminal/`. Build with `npm run build`, then stage the complete app and
terminal via `python3 scripts/stage.py`. This changes the next Go
binary's embedded terminal; it does not deploy or replace the live binary.

- `engine.ts`: real xterm/fit/search/web-links. Existing ttyd token fetch,
  authenticated WebSocket, generation guards, queue drain before reset, bounded
  reconnect backoff, binary input and pause/resume flow control retained.
- `scroll.ts`: typed browser integration for negotiated mouse protocols, native
  scrollback, retained tmux history, touch momentum and middle autoscroll.
- `App.tsx`: React pane lifecycle, split shell, paused view, keybar application
  cursor mode, upload/drop/clipboard, toolbar and iframe layout.
- `layout.ts`: captures the parent iframe's initial layout message before React
  effects mount. Without this, a fast parent load hides mobile controls.
- `dialogs.tsx`: actual React appearance/history/workspace files, typed browser
  PDF canvas adapter to the existing vendored PDF.js, desktop setup and protocol
  handoff. Uploaded paths are quoted and pasted without executing Enter.
- `Review.tsx`: reusable read-only working/staged/repository diff viewer,
  generation-safe loading and error clearing, line numbers and persisted wrap.
- Shared SavedConversations and NativeSearch preserve real backend contracts,
  pagination, pending/error states, cancellation, native resume and explicit fork
  confirmation. Search and reader dialogs are React components.
- TerminalTabs retains mounted iframe identities across navigation and persists
  its validated paths to sessionStorage plus a durable localStorage mirror.

The terminal is deployed with the React application. The original terminal,
workspace, native-history/search, review, touch, resize and reconnect tests pass
in the combined browser gate. Current verification and deployment receipts live
under `.verify-artifacts/migration/`; see MIGRATION.md for the aggregate result.
