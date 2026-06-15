[CmdletBinding()]
param(
  [string]$Version = 'dev',
  [string]$Image = 'druid:local',
  [string]$Cluster = 'druid-gs'
)

$ErrorActionPreference = 'Stop'

$repoRoot = Split-Path -Parent $PSScriptRoot
$monorepoToolEnv = Join-Path (Split-Path -Parent $repoRoot) 'monorepo\.tools\env.ps1'
if (Test-Path $monorepoToolEnv) {
  . $monorepoToolEnv
}

if ($IsWindows -or $env:OS -eq 'Windows_NT') {
  if ([string]::IsNullOrWhiteSpace($env:DOCKER_HOST)) {
    $env:DOCKER_HOST = 'npipe:////./pipe/dockerDesktopLinuxEngine'
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

Invoke-Logged docker @('build', '.', '-f', 'Dockerfile', '--build-arg', "VERSION=$Version", '-t', $Image)

$previousErrorActionPreference = $ErrorActionPreference
$ErrorActionPreference = 'Continue'
docker rm -f "k3d-$Cluster-tools" *> $null
$ErrorActionPreference = $previousErrorActionPreference

Invoke-Logged k3d @('image', 'import', $Image, '-c', $Cluster)
