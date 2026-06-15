[CmdletBinding()]
param(
  [string]$DruidExe = '.\bin\druid.exe',
  [string]$PullImage = 'druid:local',
  [string]$Kubeconfig = '',
  [string]$ManagementListen = '127.0.0.1:8081',
  [string]$PublicListen = '127.0.0.1:8082'
)

$ErrorActionPreference = 'Stop'

$repoRoot = Split-Path -Parent $PSScriptRoot
$monorepoRoot = Join-Path (Split-Path -Parent $repoRoot) 'monorepo'
$monorepoToolEnv = Join-Path $monorepoRoot '.tools\env.ps1'
if (Test-Path $monorepoToolEnv) {
  . $monorepoToolEnv
}

if (!(Test-Path $DruidExe)) {
  throw "Druid executable not found at $DruidExe. Run .\scripts\build-windows.ps1 first."
}

function Get-EnvOrDefault {
  param(
    [string]$Name,
    [string]$Default
  )

  $value = [Environment]::GetEnvironmentVariable($Name)
  if ([string]::IsNullOrEmpty($value)) {
    return $Default
  }
  return $value
}

$localKubeconfig = Join-Path $monorepoRoot '.tools\kubeconfig-druid-gs.yaml'
if ([string]::IsNullOrWhiteSpace($Kubeconfig)) {
  $Kubeconfig = Get-EnvOrDefault 'DRUID_K8S_KUBECONFIG' (Get-EnvOrDefault 'KUBECONFIG' '')
}
if ([string]::IsNullOrWhiteSpace($Kubeconfig) -and (Test-Path $localKubeconfig)) {
  $Kubeconfig = $localKubeconfig
}
if (![string]::IsNullOrWhiteSpace($Kubeconfig)) {
  $Kubeconfig = (Resolve-Path $Kubeconfig).Path
  $env:KUBECONFIG = $Kubeconfig
}

$args = @(
  'daemon',
  '--runtime', 'kubernetes',
  '--listen', $ManagementListen,
  '--public-listen', $PublicListen,
  '--unsafe-allow-unauthenticated-management',
  '--unsafe-allow-unauthenticated-public',
  '--worker-daemon-url', "http://$ManagementListen",
  '--k8s-pull-image', $PullImage
)

if (![string]::IsNullOrWhiteSpace($Kubeconfig)) {
  $args += @('--k8s-kubeconfig', $Kubeconfig)
}

$args += @(
  '--k8s-ui-s3-bucket', (Get-EnvOrDefault 'DRUID_K8S_UI_S3_BUCKET' 'druid-ui'),
  '--k8s-ui-s3-public-base-url', (Get-EnvOrDefault 'DRUID_K8S_UI_S3_PUBLIC_BASE_URL' 'http://127.0.0.1:9000/druid-ui'),
  '--k8s-ui-s3-region', (Get-EnvOrDefault 'DRUID_K8S_UI_S3_REGION' 'us-east-1'),
  '--k8s-ui-s3-endpoint', (Get-EnvOrDefault 'DRUID_K8S_UI_S3_ENDPOINT' 'http://host.docker.internal:9000'),
  '--k8s-ui-s3-credentials-secret', (Get-EnvOrDefault 'DRUID_K8S_UI_S3_CREDENTIALS_SECRET' 'druid-ui-s3')
)

& $DruidExe @args
