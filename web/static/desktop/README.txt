AgentDeck native terminals

Open in terminal uses your device's default terminal. AgentDeck does not require
Kitty, WezTerm, or another specific terminal, and does not override its appearance.

Linux

Download setup-agentdeck-terminal.sh, then run:
  bash setup-agentdeck-terminal.sh

The handler asks xdg-terminal-exec or your desktop's x-terminal-emulator to open
SSH. Minimal window managers can use their existing TERMINAL environment setting;
when no default helper is available, it uses an installed terminal automatically.
The older setup-agentdeck-kitty.sh download now installs this same generic handler.

Windows

Download setup-agentdeck.ps1, then run in PowerShell:
  powershell -NoProfile -ExecutionPolicy Bypass -File .\setup-agentdeck.ps1

This starts OpenSSH in the terminal host chosen by Windows's Default terminal
application setting. No separate terminal installation is required. OpenSSH must
be installed. Original open-terminal.ps1 is retained as a .bak file.

Connection (both platforms)

Configure your existing SSH key and this alias in ~/.ssh/config (Windows:
%USERPROFILE%\.ssh\config), using your server's host and account:
  Host agentdeck
    HostName YOUR_SERVER
    User YOUR_USER
    ServerAliveInterval 30
    ServerAliveCountMax 3

Connect once with ssh agentdeck true and verify its host key. Then click
Open in terminal in AgentDeck. The browser may ask to allow the external link.
The same tmux session remains available in both views. Ctrl+B then D detaches.
Tools > Terminal connection setup contains installation and manual instructions.

Original Linux launcher/MIME files are saved under ~/.local/state/agentdeck/setup-*.
To uninstall, restore the saved MIME settings and remove
~/.local/bin/agentdeck-terminal and
~/.local/share/applications/agentdeck-terminal.desktop.

Files: use AgentDeck's browser terminal to drop local files or paste screenshots.
It transfers the bytes to the session's machine and inserts the remote path.
Because both views attach to the same tmux session, the inserted path appears
in your terminal too. A native terminal drop alone may only insert a LOCAL filename.

Manual connection (any SSH terminal):
   ssh -t agentdeck /usr/local/bin/agentdeck --hosted-attach attach session SESSION_ID
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

MANAGE WITHOUT THE WEB UI

On the AgentDeck server: agentdeck console
On Linux: download install-agentdeck-cli.sh from this directory, then:
  bash install-agentdeck-cli.sh --server agentdeck --api https://YOUR_SERVER:8443
On Windows: download install-agentdeck-cli.ps1 and run in PowerShell:
  powershell -ExecutionPolicy Bypass -File .\install-agentdeck-cli.ps1 -Server agentdeck

Run agentdeck to open the console; agentdeck --help lists scripting commands.
Manage sessions, tasks, routines, projects, targets, approvals and settings.
Use agentdeck upload session ID ./document.pdf to add context from your machine.
