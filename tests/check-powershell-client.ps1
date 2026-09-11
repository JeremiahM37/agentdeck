$ErrorActionPreference = 'Stop'
$source = Join-Path $PSScriptRoot '../web/static/desktop/install-agentdeck-cli.ps1'
$tokens = $null; $errors = $null
[System.Management.Automation.Language.Parser]::ParseFile($source,[ref]$tokens,[ref]$errors) | Out-Null
if ($errors.Count) { throw ($errors | Out-String) }
$text = Get-Content -Raw $source
$launcher = ($text -split "(?m)^\`$launcher = @'\r?\n",2)[1] -split "(?m)^'@",2 | Select-Object -First 1
$launcher = $launcher.Replace('__SERVER__','test-server')
$ast = [System.Management.Automation.Language.Parser]::ParseInput($launcher,[ref]$tokens,[ref]$errors)
if ($errors.Count) { throw ($errors | Out-String) }
$quoteFunction = $ast.Find({param($node) $node -is [System.Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -eq 'Quote-Sh'}, $true)
Invoke-Expression $quoteFunction.Extent.Text
foreach ($value in @('simple', 'space and $dollar', "apostrophe's", '{"text":"say hello"}', '$(touch unwanted)')) {
  $actual = & /bin/sh -c ('printf %s ' + (Quote-Sh $value))
  if ($actual -cne $value) { throw "Shell argument changed: $value" }
}
'PASS: PowerShell installer/launcher parse and POSIX argument round trips'

# Exercise the desktop URI handler without launching a Windows console here.
$desktopSource = Join-Path $PSScriptRoot '../web/static/desktop/setup-agentdeck.ps1'
[System.Management.Automation.Language.Parser]::ParseFile($desktopSource,[ref]$tokens,[ref]$errors) | Out-Null
if ($errors.Count) { throw ($errors | Out-String) }
$text = Get-Content -Raw $desktopSource
$desktopHandler = ($text -split "(?m)^\`$launcher = @'\r?\n",2)[1] -split "(?m)^'@",2 | Select-Object -First 1
$handler = [scriptblock]::Create($desktopHandler)
function Get-Command { param($Name) if ($Name -ne 'ssh.exe') { throw 'Must use the default console host' }; [pscustomobject]@{Source='ssh.exe'} }
function Start-Process { param($FilePath,$ArgumentList) $script:started=@{FilePath=$FilePath;Arguments=$ArgumentList} }
& $handler -Uri 'agentdeck://attach/session/42'
if ($started.FilePath -ne 'ssh.exe' -or ($started.Arguments -join '|') -ne '-t|agentdeck|/usr/local/bin/agentdeck|--hosted-attach|attach|session|42') { throw 'Wrong default-terminal launch' }
$script:started=$null
$rejected=$false
try { & $handler -Uri 'agentdeck://attach/session/42?command=bad' } catch { $rejected=$true }
if (-not $rejected -or $started) { throw 'Unsafe desktop link was launched' }
'PASS: Windows URI handler launches SSH in the default terminal and rejects invalid links'
