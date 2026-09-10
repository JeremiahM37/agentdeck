# Run in regular PowerShell; this installs only a per-user URI handler.
# The existing SSH alias 'agentdeck' owns host/user/key configuration.
$ErrorActionPreference = 'Stop'
$installDir = Join-Path $env:LOCALAPPDATA 'AgentDeck'
New-Item -ItemType Directory -Force -Path $installDir | Out-Null
$launcher = @'
param([Parameter(Mandatory=$true)][string]$Uri)
$ErrorActionPreference = 'Stop'
# No arbitrary commands, hosts, options or query strings from a web page.
if ($Uri -notmatch '^agentdeck://attach/(session|attempt|project|session-shell|attempt-shell)/([1-9][0-9]*)/?$') {
    throw 'Invalid AgentDeck terminal link'
}
$kind = $Matches[1]
$sessionId = $Matches[2]
# Launch a console application normally. Windows hosts it in the user's
# default terminal application (Windows Terminal, Console Host, or another host).
$ssh = (Get-Command ssh.exe -ErrorAction Stop).Source
Start-Process -FilePath $ssh -ArgumentList @('-t','agentdeck','/usr/local/bin/agentdeck','attach',$kind,$sessionId)
'@
$launcherPath = Join-Path $installDir 'open-terminal.ps1'
if (Test-Path $launcherPath) { Copy-Item $launcherPath ($launcherPath + '.bak') -Force }
Set-Content -LiteralPath $launcherPath -Value $launcher -Encoding UTF8
$reg = 'HKCU:\Software\Classes\agentdeck'
New-Item -Path $reg -Force | Out-Null
Set-Item -Path $reg -Value 'URL:AgentDeck terminal'
New-ItemProperty -Path $reg -Name 'URL Protocol' -Value '' -PropertyType String -Force | Out-Null
New-Item -Path "$reg\shell\open\command" -Force | Out-Null
$command = 'powershell.exe -NoProfile -ExecutionPolicy Bypass -File "' + $launcherPath + '" -Uri "%1"'
Set-Item -Path "$reg\shell\open\command" -Value $command
Write-Host 'AgentDeck desktop links are installed for this Windows user.'
Write-Host 'Next: configure the agentdeck SSH alias, then test: ssh agentdeck true'
Write-Host 'In AgentDeck: Attach > Desktop > Open in terminal.'
Write-Host 'Remove later: Remove-Item HKCU:\Software\Classes\agentdeck -Recurse'
