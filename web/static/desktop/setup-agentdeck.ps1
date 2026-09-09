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
$wezterm = (Get-Command wezterm-gui.exe -ErrorAction SilentlyContinue).Source
if (-not $wezterm) {
    $candidates = @(
        (Join-Path $env:ProgramFiles 'WezTerm\wezterm-gui.exe'),
        (Join-Path $env:LOCALAPPDATA 'Programs\WezTerm\wezterm-gui.exe')
    )
    $wezterm = $candidates | Where-Object { Test-Path $_ } | Select-Object -First 1
}
if (-not $wezterm) { throw 'Install WezTerm first: winget install --id wez.wezterm -e' }
# Start-Process receives only fixed words and validated kind/numeric ID.
Start-Process -FilePath $wezterm -ArgumentList @('start','--','ssh','-t','agentdeck','/usr/local/bin/agentdeck','attach',$kind,$sessionId)
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
Write-Host 'In AgentDeck: Attach > Desktop > Open desktop terminal.'
Write-Host 'Remove later: Remove-Item HKCU:\Software\Classes\agentdeck -Recurse'
