# Terminal workspace

Attach opens an xterm.js workspace connected to the existing ttyd WebSocket and
tmux session. ttyd still owns the PTY bridge and reconnects resolve by attachment
identity. All JavaScript, fonts, PDF rendering, and terminal add-ons ship in the
binary; clients do not contact a CDN.

- Drop files or paste screenshots to upload original bytes into the session
  directory (or the current attempt's worktree). A successful upload pastes a
  shell-quoted target path with **no Enter**. Transfer failures stay visible.
- Find searches the terminal buffer. History captures up to 100,000 retained
  tmux lines (final 8 MiB cap), with search and a text download. It works for
  output generated before the browser attached, including alternate-screen CLIs.
- Pause view keeps a selectable snapshot while the live buffer continues to
  consume output. Live returns to current output. It does not stop the agent.
- Split shell starts a separate persistent tmux session in the same workspace.
  Hiding the shell closes only its browser connection. Reopening attaches to it.
- Files lists the workspace with parent-folder navigation, path insertion,
  raster-image/PDF/text previews, and byte-preserving downloads. Symlinks out of
  the workspace, nonregular files, and files over 25 MiB are refused. HTML/SVG
  render as source text. PDF.js renders pages without a browser plugin.
- Appearance preferences (font size, spacing, theme) persist per device.
- Desktop offers an `agentdeck://attach/<kind>/<id>` link, a manual command,
  and the Windows setup script. `agentdeck attach <kind> <id>` on the control
  plane resolves the same target and invokes the same terminal command.

The desktop launcher uses the user's `agentdeck` SSH alias to the control plane.
It accepts only known attachment kinds and a positive numeric ID. It neither
accepts arbitrary commands/hosts in URLs nor copies control-plane SSH keys to
clients. Installation instructions are served at `/desktop/README.txt`.

SSH attachment respects the target's port and command prefix. Wrappers that
consume stdin (Windows SSH → WSL in particular) use `script -qefc` and
`/dev/tty` to relay a real Unix PTY to tmux. A plain `tmux attach` inside the
base64 pipe fails with `not a terminal`; redirecting to `/dev/tty` alone fails
with `can't use /dev/tty`. `script` is provided by util-linux on these targets.

Token mode gates terminal tokens/WebSockets as well as workspace APIs. The
terminal reads the same `adk-token` local storage value as the board. Terminal
pages and live terminal traffic bypass the PWA cache.

## Verification

`e2e/test_terminal_workspace.py` starts a real, isolated AgentDeck, ttyd, tmux
server and filesystem. It covers drop/paste without submission, exact upload and
download bytes, shell persistence, history search, appearance persistence,
phone layout, frozen views, simultaneous clients, the desktop CLI over a real
PTY, image/PDF rendering, failed-upload recovery, and ttyd death/reconnection.

Go tests cover real filesystem reads, symlink/special-file/size boundaries,
attempt worktree routing, token enforcement, and SSH command quoting/wrappers.
The existing terminal proxy tests also cover service restart and two clients.

## Deployment verification — 2026-09-08

Deployed on AIServer; all 10 pre-existing tmux sessions survived the restart.
`verify`: **PASS: 6/6 steps passed (backend=web)**, including the full Go suite
and 68 browser tests. No error-priority service journal entries after deployment.

A portable Windows WezTerm 20240203-110809-5046fc22 trial displayed a tmux
session also attached through the deployed browser terminal. Its native
`wezterm cli get-text` returned the marker entered in the browser. Windows
PowerShell parsed the setup and launcher scripts and rejected malformed links.
The native desktop connection still requires the operator's SSH alias/key setup;
the trial did not install WezTerm or the URI handler permanently.

The desktop's WSL instance subsequently stopped and returned
`Wsl/Service/E_UNEXPECTED` to new invocations. No WSL restart or configuration
change was performed. The production local-target browser check separately
verified keyboard input and upload/path insertion. Existing remote-upload tests
remain in the suite; do not mistake a stopped WSL instance for a ttyd failure.
