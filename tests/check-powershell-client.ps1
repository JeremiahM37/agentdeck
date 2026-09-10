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
