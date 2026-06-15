[CmdletBinding()]
param(
  [string]$Version = 'dev',
  [string]$GoBin = ''
)

$ErrorActionPreference = 'Stop'

$repoRoot = Split-Path -Parent $PSScriptRoot
$monorepoToolEnv = Join-Path (Split-Path -Parent $repoRoot) 'monorepo\.tools\env.ps1'
if (Test-Path $monorepoToolEnv) {
  . $monorepoToolEnv
}

function Assert-Command {
  param([string]$Name)
  if (!(Get-Command $Name -ErrorAction SilentlyContinue)) {
    throw "$Name is required. Install Go first, or load the monorepo local tools with: . ..\monorepo\.tools\env.ps1"
  }
}

function Invoke-Logged {
  param(
    [string]$Command,
    [string[]]$Arguments
  )

  Write-Host "> $Command $($Arguments -join ' ')"
  & $Command @Arguments
  if ($LASTEXITCODE -ne 0) {
    throw "$Command exited with $LASTEXITCODE"
  }
}

Assert-Command go

if ([string]::IsNullOrWhiteSpace($GoBin)) {
  $goPath = (& go env GOPATH).Trim()
  $GoBin = Join-Path $goPath 'bin'
}

New-Item -ItemType Directory -Force -Path $GoBin, 'bin' | Out-Null
$env:Path = "$GoBin;$env:Path"

if (!(Get-Command oapi-codegen -ErrorAction SilentlyContinue)) {
  Write-Host 'Installing oapi-codegen v2.5.1...'
  $env:GOBIN = $GoBin
  Invoke-Logged go @('install', 'github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.5.1')
}

Write-Host 'Generating API clients...'
Invoke-Logged oapi-codegen @('-config', 'api/oapi-codegen.yaml', 'api/openapi.yaml')
Invoke-Logged oapi-codegen @('-config', 'api/dev-oapi-codegen.yaml', 'api/dev.openapi.yaml')
Invoke-Logged oapi-codegen @('-config', 'api/callback-oapi-codegen.yaml', 'api/callback.openapi.yaml')

$env:CGO_ENABLED = '0'
$ldflags = "-X github.com/highcard-dev/daemon/internal.Version=$Version"

Write-Host 'Building bin/druid.exe...'
Invoke-Logged go @('build', '-buildvcs=false', '-ldflags', $ldflags, '-o', './bin/druid.exe', './apps/druid')

Write-Host 'Building bin/druid-coldstarter.exe...'
Invoke-Logged go @('build', '-buildvcs=false', '-ldflags', $ldflags, '-o', './bin/druid-coldstarter.exe', './apps/druid-coldstarter')

Write-Host 'Druid CLI Windows binaries built in ./bin'
