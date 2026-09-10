# Windows terminal client: run the console on the server over OpenSSH.
param([string]$Server = 'agentdeck')
$ErrorActionPreference = 'Stop'
if ($Server -notmatch '^[a-zA-Z0-9_@.:-]+$' -or $Server.StartsWith('-')) { throw 'Invalid SSH alias' }
Get-Command ssh -ErrorAction Stop | Out-Null
$dir = Join-Path $env:LOCALAPPDATA 'AgentDeck\cli'
New-Item -ItemType Directory -Force $dir | Out-Null
$launcher = @'
$server = '__SERVER__'
function Quote-Sh([string]$value) { $q = [char]39; $d = [char]34; return "$q" + $value.Replace("$q", "$q$d$q$d$q") + "$q" }
if ($args.Count -eq 0) { & ssh -tt $server /usr/local/bin/agentdeck console; exit $LASTEXITCODE }
# Transfer local context files before invoking the server's upload command.
if ($args[0] -eq 'upload') {
  if ($args.Count -ne 4) { throw 'Usage: agentdeck upload KIND ID FILE' }
  if ($args[1] -notin @('session','attempt','project','task') -or $args[2] -notmatch '^[1-9][0-9]*$') { throw 'Invalid attachment' }
  $file = Get-Item -LiteralPath $args[3]
  $extension = $file.Extension
  if ($extension -notmatch '^\.[a-zA-Z0-9]+$') { $extension = '' }
  $remote = (& ssh $server 'mktemp -d /tmp/agentdeck-upload-XXXXXXXX').Trim()
  if ($LASTEXITCODE -ne 0 -or $remote -notmatch '^/tmp/agentdeck-upload-[a-zA-Z0-9]+$') { throw 'Cannot stage upload' }
  try {
    & scp -- $file.FullName "${server}:$remote/context$extension"
    if ($LASTEXITCODE -ne 0) { throw 'Transfer failed' }
    & ssh $server "/usr/local/bin/agentdeck upload $($args[1]) $($args[2]) $(Quote-Sh "$remote/context$extension")"
    $result = $LASTEXITCODE
  } finally { & ssh $server "rm -rf -- $remote" | Out-Null }
  exit $result
}
# Download to a server-side staging file, then copy to the requested local path.
if ($args[0] -eq 'download') {
  if ($args.Count -ne 5) { throw 'Usage: agentdeck download KIND ID REMOTE LOCAL' }
  if ($args[1] -notin @('session','attempt','project') -or $args[2] -notmatch '^[1-9][0-9]*$') { throw 'Invalid attachment' }
  if (Test-Path -LiteralPath $args[4]) { throw 'Destination already exists' }
  $remote = (& ssh $server 'mktemp -d /tmp/agentdeck-download-XXXXXXXX').Trim()
  if ($LASTEXITCODE -ne 0 -or $remote -notmatch '^/tmp/agentdeck-download-[a-zA-Z0-9]+$') { throw 'Cannot stage download' }
  try {
    & ssh $server "/usr/local/bin/agentdeck download $($args[1]) $($args[2]) $(Quote-Sh $args[3]) $remote/artifact"
    if ($LASTEXITCODE -ne 0) { throw 'Download failed' }
    & scp "${server}:$remote/artifact" $args[4]
    $result = $LASTEXITCODE
  } finally { & ssh $server "rm -rf -- $remote" | Out-Null }
  exit $result
}
$command = '/usr/local/bin/agentdeck ' + (($args | ForEach-Object { Quote-Sh $_ }) -join ' ')
if ($args[0] -in @('console','tui','attach')) { & ssh -tt $server $command }
else { & ssh $server $command }
exit $LASTEXITCODE
'@
$launcher.Replace('__SERVER__', $Server) | Set-Content (Join-Path $dir 'agentdeck.ps1')
'@powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0agentdeck.ps1" %*' | Set-Content (Join-Path $dir 'agentdeck.cmd')
$userPath = [Environment]::GetEnvironmentVariable('Path','User')
if (($userPath -split ';') -notcontains $dir) { [Environment]::SetEnvironmentVariable('Path', "$userPath;$dir", 'User') }
$env:Path += ";$dir"
Write-Host 'Installed. Run agentdeck from a new terminal. SSH uses your existing keys and host verification.'
