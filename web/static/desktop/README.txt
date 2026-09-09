AgentDeck + WezTerm on Windows

1. Install WezTerm from https://wezterm.org/install/windows.html
   Or in PowerShell:
     winget install --id wez.wezterm -e

2. Keep Tailscale connected. Configure this SSH alias in
   %USERPROFILE%\.ssh\config (create the file if needed):

     Host agentdeck
       HostName aiserver
       User admin
       ServerAliveInterval 30
       ServerAliveCountMax 3

   Use your existing desktop SSH key/account for AIServer. Do not copy a
   server's private key onto the desktop. If no desktop key exists, run
   ssh-keygen -t ed25519 and add its .pub file to admin's authorized_keys
   on AIServer. Test: ssh agentdeck true

   This SSH alias is the desktop's one connection to the control plane.
   AgentDeck resolves local/LXC/SSH/WSL targets from there automatically.
   Other AgentDeck installations: replace HostName and User with your own
   control-plane host and account. Configure AGENTDECK_PORT/AUTH_TOKEN in
   that account's environment if the service uses a nondefault port/token.

3. Download setup-agentdeck.ps1 beside this document. Run in PowerShell:
     powershell -NoProfile -ExecutionPolicy Bypass -File .\setup-agentdeck.ps1
   No administrator privileges needed. Existing WezTerm settings are untouched.

4. AgentDeck > Attach > Desktop > Open desktop terminal.
   Your browser may ask whether to open the AgentDeck link; allow it.
   This opens the SAME tmux session. Close the window or detach with
   Ctrl+B then D; the session continues and stays available in the browser.

Optional appearance: merge these into your existing .wezterm.lua config table:
   font_size = 14.0,
   color_scheme = 'Builtin Solarized Dark',
   scrollback_lines = 100000,
   enable_scroll_bar = true,

WezTerm defaults: Ctrl+Shift+C/V copy/paste; Ctrl+Shift+F search;
Ctrl+Shift+Space quick select; Ctrl+Shift+L launcher.

Files: use AgentDeck's browser terminal to drop local files or paste screenshots.
It transfers the bytes to the session's machine and inserts the remote path.
Because both views attach to the same tmux session, the inserted path appears
in WezTerm too. A native terminal drop alone may only insert a LOCAL filename.

Manual connection (also works in Kitty on Linux/macOS):
   ssh -t agentdeck /usr/local/bin/agentdeck attach session SESSION_ID
Copy the exact command from the Desktop panel; tasks use attempt IDs.

The browser's Split shell starts an independent persistent shell in the same
workspace. Hiding it disconnects that view, preserving its work. No exit or
interrupt is sent to the agent. Pause view freezes a readable snapshot while
output continues; Resume/Live returns to the current terminal.

The file drawer is scoped to the saved session directory or task worktree.
Files outside it can still be handled using your shell. File previews render
text as text (including HTML/SVG source); images and PDF have dedicated views.
Downloads and uploads are limited to 25 MiB per file.

Uninstall desktop links (PowerShell):
   Remove-Item HKCU:\Software\Classes\agentdeck -Recurse
   Remove-Item "$env:LOCALAPPDATA\AgentDeck" -Recurse
