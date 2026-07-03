param(
  [string]$Project = $(if ($env:PROJECT) { $env:PROJECT } else { $env:GOOGLE_CLOUD_PROJECT }),
  [string]$Location = $(if ($env:LOCATION) { $env:LOCATION } elseif ($env:GOOGLE_CLOUD_LOCATION) { $env:GOOGLE_CLOUD_LOCATION } else { "global" }),
  [string]$ServiceAccountName = $(if ($env:SERVICE_ACCOUNT_NAME) { $env:SERVICE_ACCOUNT_NAME } else { "llm-gateway-sa" }),
  [string]$ServiceAccountDisplay = $(if ($env:SERVICE_ACCOUNT_DISPLAY) { $env:SERVICE_ACCOUNT_DISPLAY } else { "Byto Gateway Production" }),
  [string]$KeyPath = $(if ($env:KEY_PATH) { $env:KEY_PATH } elseif ($env:GOOGLE_APPLICATION_CREDENTIALS) { $env:GOOGLE_APPLICATION_CREDENTIALS } else { "secrets\llm-gateway-sa.json" }),
  [string]$Model = $(if ($env:MODEL) { $env:MODEL } else { $env:VERIFY_MODEL }),
  [string]$ApiKey = $env:GATEWAY_API_KEYS,
  [switch]$NonInteractive,
  [switch]$SkipVerify,
  [switch]$InstallGo,
  [switch]$InstallGcloud,
  [switch]$ForceInstallDependencies
)

$ErrorActionPreference = "Stop"
if (Get-Variable PSNativeCommandUseErrorActionPreference -Scope Global -ErrorAction SilentlyContinue) {
  $global:PSNativeCommandUseErrorActionPreference = $false
}
$Root = Resolve-Path (Join-Path $PSScriptRoot "..")
Set-Location $Root
$SetupLogDir = Join-Path $Root ".cache\setup"

function Step($Message) {
  Write-Host ""
  Write-Host $Message -ForegroundColor Cyan
}

function Ok($Message) {
  Write-Host "OK $Message" -ForegroundColor Green
}

function Warn($Message) {
  Write-Host "WARN $Message" -ForegroundColor Yellow
}

function Fail($Message) {
  Write-Host "ERROR $Message" -ForegroundColor Red
  exit 1
}

function Prompt-Value($Label, $Default) {
  if ($NonInteractive) {
    return $Default
  }
  $value = Read-Host "$Label [$Default]"
  if ([string]::IsNullOrWhiteSpace($value)) {
    return $Default
  }
  return $value.Trim()
}

function Confirm-YesNo($Label, $Default = "yes") {
  if ($NonInteractive) {
    return $false
  }
  $answer = Read-Host "$Label [$Default]"
  if ([string]::IsNullOrWhiteSpace($answer)) {
    $answer = $Default
  }
  return $answer -match "^(y|yes)$"
}

function Select-Menu($Title, [string[]]$Options, [int]$Selected = 0) {
  if ($NonInteractive) {
    return $Selected
  }
  [Console]::CursorVisible = $false
  try {
    Write-Host $Title
    while ($true) {
      for ($i = 0; $i -lt $Options.Count; $i++) {
        if ($i -eq $Selected) {
          Write-Host ("  > {0}" -f $Options[$i]) -ForegroundColor Cyan
        } else {
          Write-Host ("    {0}" -f $Options[$i])
        }
      }
      $key = [Console]::ReadKey($true)
      if ($key.Key -eq [ConsoleKey]::Enter) {
        Write-Host ""
        return $Selected
      }
      if ($key.Key -eq [ConsoleKey]::UpArrow) {
        $Selected--
        if ($Selected -lt 0) { $Selected = $Options.Count - 1 }
      }
      if ($key.Key -eq [ConsoleKey]::DownArrow) {
        $Selected++
        if ($Selected -ge $Options.Count) { $Selected = 0 }
      }
      [Console]::SetCursorPosition(0, [Console]::CursorTop - $Options.Count)
    }
  } finally {
    [Console]::CursorVisible = $true
  }
}

function New-GatewayKey {
  $bytes = New-Object byte[] 32
  $rng = [System.Security.Cryptography.RandomNumberGenerator]::Create()
  try {
    $rng.GetBytes($bytes)
  } finally {
    $rng.Dispose()
  }
  return "byto_" + [Convert]::ToBase64String($bytes).TrimEnd("=").Replace("+", "-").Replace("/", "_")
}

function Add-PathIfExists([string[]]$Paths) {
  foreach ($path in $Paths) {
    if ($path -and (Test-Path $path) -and (($env:Path -split ";") -notcontains $path)) {
      $env:Path = "$path;$env:Path"
    }
  }
}

function Add-CommonGoPath {
  $programFilesX86 = [Environment]::GetEnvironmentVariable("ProgramFiles(x86)")
  Add-PathIfExists @("$env:ProgramFiles\Go\bin", "$programFilesX86\Go\bin", "$env:USERPROFILE\go\bin")
}

function Add-CommonGcloudPath {
  $programFilesX86 = [Environment]::GetEnvironmentVariable("ProgramFiles(x86)")
  Add-PathIfExists @(
    "$env:ProgramFiles\Google\Cloud SDK\google-cloud-sdk\bin",
    "$programFilesX86\Google\Cloud SDK\google-cloud-sdk\bin",
    "$env:LOCALAPPDATA\Google\Cloud SDK\google-cloud-sdk\bin"
  )
}

function Add-CommonChocolateyPath {
  Add-PathIfExists @("$env:ProgramData\chocolatey\bin", "$env:ChocolateyInstall\bin")
}

function Test-IsAdmin {
  $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
  $principal = New-Object Security.Principal.WindowsPrincipal($identity)
  return $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
}

function Install-Chocolatey {
  Step "Installing Chocolatey"
  if (-not (Test-IsAdmin)) {
    Warn "Chocolatey installation requires an elevated PowerShell window."
    Write-Host "Open PowerShell as Administrator, then rerun this setup."
    return $false
  }
  Set-ExecutionPolicy Bypass -Scope Process -Force
  [System.Net.ServicePointManager]::SecurityProtocol = [System.Net.ServicePointManager]::SecurityProtocol -bor 3072
  Invoke-Expression ((New-Object System.Net.WebClient).DownloadString("https://community.chocolatey.org/install.ps1"))
  Add-CommonChocolateyPath
  return [bool](Get-Command choco -ErrorAction SilentlyContinue)
}

function Ensure-PackageManager([bool]$AutoInstall) {
  if (Get-Command winget -ErrorAction SilentlyContinue) {
    return "winget"
  }
  Add-CommonChocolateyPath
  if (Get-Command choco -ErrorAction SilentlyContinue) {
    return "choco"
  }

  Warn "Neither winget nor Chocolatey is installed."
  $choice = 1
  if ($AutoInstall) {
    $choice = 0
  } elseif (-not $NonInteractive) {
    $choice = Select-Menu "Choose how to continue:" @("Install Chocolatey now", "Skip for now", "Abort setup") 0
  }
  if ($choice -eq 0) {
    if (Install-Chocolatey) {
      return "choco"
    }
    return ""
  }
  if ($choice -eq 2) {
    Fail "Setup aborted before installing a package manager."
  }
  return ""
}

function Test-GoVersion {
  if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    return $false
  }
  $versionText = (& go version 2>&1) -join "`n"
  $match = [regex]::Match($versionText, "go(\d+)\.(\d+)")
  if (-not $match.Success) {
    return $false
  }
  $major = [int]$match.Groups[1].Value
  $minor = [int]$match.Groups[2].Value
  return ($major -gt 1 -or ($major -eq 1 -and $minor -ge 22))
}

function Install-Go {
  Step "Installing Go"
  $manager = Ensure-PackageManager $InstallGo
  if ($manager -eq "winget") {
    & winget install --id GoLang.Go --exact --source winget --accept-package-agreements --accept-source-agreements
  } elseif ($manager -eq "choco") {
    & choco install golang -y --no-progress
  } else {
    Warn "No supported Windows package manager is available for automatic Go install."
    Write-Host "Install Go from https://go.dev/dl/ and open a new PowerShell window."
    return $false
  }
  Add-CommonGoPath
  return (Test-GoVersion)
}

function Ensure-Go {
  Add-CommonGoPath
  if ($ForceInstallDependencies) {
    if (-not (Install-Go)) {
      Fail "Go 1.22+ is required before setup can continue."
    }
  }
  if (-not (Test-GoVersion)) {
    if (Get-Command go -ErrorAction SilentlyContinue) {
      Warn "$(& go version) found, but Go 1.22+ is required."
    } else {
      Warn "Go 1.22+ is not installed."
    }
    $choice = 1
    if ($InstallGo) {
      $choice = 0
    } elseif (-not $NonInteractive) {
      $choice = Select-Menu "Choose how to continue:" @("Install Go now", "Skip for now", "Abort setup") 0
    }
    if ($choice -eq 0) {
      if (Install-Go) {
        Ensure-Go
        return
      }
      Fail "Go 1.22+ is required before setup can continue."
    }
    if ($choice -eq 2) {
      Fail "Setup aborted before installing Go."
    }
    Fail "Go 1.22+ is required before setup can continue."
  }
  Ok (& go version)
}

function Install-Gcloud {
  Step "Installing Google Cloud CLI"
  $manager = Ensure-PackageManager $InstallGcloud
  if ($manager -eq "winget") {
    & winget install --id Google.CloudSDK --exact --source winget --accept-package-agreements --accept-source-agreements
  } elseif ($manager -eq "choco") {
    & choco install gcloudsdk -y --no-progress
  } else {
    Warn "No supported Windows package manager is available for automatic Google Cloud CLI install."
    Write-Host "Install it from https://cloud.google.com/sdk/docs/install-sdk#windows and open a new PowerShell window."
    return $false
  }
  Add-CommonGcloudPath
  return [bool](Get-Command gcloud -ErrorAction SilentlyContinue)
}

function Ensure-Gcloud {
  Add-CommonGcloudPath
  if ($ForceInstallDependencies) {
    if (-not (Install-Gcloud)) {
      Fail "Production setup needs Google Cloud CLI."
    }
  }
  if (Get-Command gcloud -ErrorAction SilentlyContinue) {
    Ok "gcloud CLI found"
    return
  }
  Warn "gcloud is not installed. It is required for production setup."
  $choice = 1
  if ($InstallGcloud) {
    $choice = 0
  } elseif (-not $NonInteractive) {
    $choice = Select-Menu "Install Google Cloud CLI now?" @("Install Google Cloud CLI", "Skip for now", "Abort setup") 0
  }
  if ($choice -eq 0) {
    if (Install-Gcloud) {
      Ensure-Gcloud
      return
    }
    Fail "Production setup needs Google Cloud CLI."
  }
  if ($choice -eq 2) {
    Fail "Setup aborted before installing gcloud."
  }
  Fail "Production setup needs Google Cloud CLI."
}

function Read-EnvFile {
  if (-not (Test-Path ".env")) {
    return
  }
  Get-Content ".env" | ForEach-Object {
    $line = $_.Trim()
    if (-not $line -or $line.StartsWith("#") -or -not $line.Contains("=")) {
      return
    }
    $parts = $line.Split("=", 2)
    if (-not [Environment]::GetEnvironmentVariable($parts[0], "Process")) {
      [Environment]::SetEnvironmentVariable($parts[0], $parts[1], "Process")
    }
  }
}

function Test-ServiceKeyValid {
  param([string]$Path)
  if (-not (Test-Path $Path)) {
    return $false
  }
  try {
    $json = Get-Content $Path -Raw | ConvertFrom-Json
    return [bool]($json.client_email -and $json.private_key)
  } catch {
    return $false
  }
}

function Convert-ToRootedPath([string]$Path) {
  if ([System.IO.Path]::IsPathRooted($Path)) {
    return $Path
  }
  return Join-Path $Root $Path
}

function Invoke-Step {
  param(
    [string]$Label,
    [scriptblock]$Command
  )
  New-Item -ItemType Directory -Force -Path $SetupLogDir | Out-Null
  $safe = ($Label.ToLowerInvariant() -replace "[^a-z0-9_-]+", "-").Trim("-")
  $log = Join-Path $SetupLogDir ("{0}-{1}.log" -f (Get-Date -Format yyyyMMddHHmmss), $safe)
  Write-Host "  RUN $Label"
  try {
    $global:LASTEXITCODE = 0
    & $Command *> $log
    if ($LASTEXITCODE -ne 0) {
      throw "Command exited with $LASTEXITCODE"
    }
    Ok $Label
  } catch {
    Write-Host "  FAIL $Label" -ForegroundColor Red
    Write-Host "Log: $log" -ForegroundColor Yellow
    if (Test-Path $log) {
      Get-Content $log -Tail 80
    }
    throw
  }
}

function Copy-KeyToClipboard {
  if ([string]::IsNullOrWhiteSpace($ApiKey)) {
    return
  }
  try {
    Set-Clipboard -Value $ApiKey
    Ok "Copied gateway API key to clipboard"
  } catch {
    Warn "Could not copy API key to clipboard"
  }
}

function Get-ActiveGcloudMember {
  $account = (& gcloud auth list "--filter=status:ACTIVE" "--format=value(account)" | Select-Object -First 1)
  if ([string]::IsNullOrWhiteSpace($account)) {
    Fail "No active gcloud account. Run gcloud auth login first."
  }
  if ($account.EndsWith(".gserviceaccount.com")) {
    return "serviceAccount:$account"
  }
  return "user:$account"
}

function Grant-OrgPolicyAdmin {
  $member = Get-ActiveGcloudMember
  $ancestors = (& gcloud projects get-ancestors $Project "--format=value(type,id)")
  $ancestorType = ""
  $ancestorID = ""
  foreach ($line in $ancestors) {
    $parts = $line -split "\s+"
    if ($parts.Count -ge 2 -and ($parts[0] -eq "folder" -or $parts[0] -eq "organization")) {
      $ancestorType = $parts[0]
      $ancestorID = $parts[1]
      break
    }
  }
  if ([string]::IsNullOrWhiteSpace($ancestorType) -or [string]::IsNullOrWhiteSpace($ancestorID)) {
    Fail "Could not find a folder or organization ancestor for this project. Grant roles/orgpolicy.policyAdmin manually where this project is governed."
  }
  if ($ancestorType -eq "folder") {
    Invoke-Step "Grant roles/orgpolicy.policyAdmin" {
      & gcloud resource-manager folders add-iam-policy-binding $ancestorID "--member=$member" "--role=roles/orgpolicy.policyAdmin" --quiet
    }
  } else {
    Invoke-Step "Grant roles/orgpolicy.policyAdmin" {
      & gcloud organizations add-iam-policy-binding $ancestorID "--member=$member" "--role=roles/orgpolicy.policyAdmin" --quiet
    }
  }
}

function Allow-ServiceAccountKeyCreation {
  Invoke-Step "Enable Organization Policy API" { & gcloud services enable orgpolicy.googleapis.com "--project=$Project" }
  try {
    Invoke-Step "Allow service account key creation" {
      & gcloud resource-manager org-policies disable-enforce constraints/iam.disableServiceAccountKeyCreation "--project=$Project" --quiet
    }
    return
  } catch {
    Warn "Your active gcloud account cannot change org policy yet."
    Write-Host "Missing permission: setOrgPolicy"
    Write-Host "Needed role: roles/orgpolicy.policyAdmin"
    if (Confirm-YesNo "Grant roles/orgpolicy.policyAdmin to the active gcloud account and retry?" "yes") {
      Grant-OrgPolicyAdmin
      Invoke-Step "Allow service account key creation" {
        & gcloud resource-manager org-policies disable-enforce constraints/iam.disableServiceAccountKeyCreation "--project=$Project" --quiet
      }
      return
    }
    throw
  }
}

function Write-ProductionEnv {
  if (Test-Path ".env") {
    $backup = ".env.backup.$(Get-Date -Format yyyyMMddHHmmss)"
    Copy-Item ".env" $backup
    Ok "Backed up existing .env to $backup"
  }

  $content = @"
GOOGLE_CLOUD_PROJECT=$Project
GOOGLE_CLOUD_LOCATION=$Location
GATEWAY_API_KEYS=$ApiKey
GATEWAY_ALLOW_UNAUTHENTICATED=false
MODEL_CATALOG_PATH=config/models.json
MODEL_CATALOG_REFRESH_ON_START=true
ALLOW_ANY_GEMINI_MODEL=false
MODEL_ALIASES=
VERTEX_BASE_URL=https://aiplatform.googleapis.com
GOOGLE_APPLICATION_CREDENTIALS=$KeyPath
VERTEX_ACCESS_TOKEN=
PORT=8080
LOG_PATH=logs/requests.jsonl
REQUEST_TIMEOUT_SECONDS=180
VERTEX_RETRY_MAX_ATTEMPTS=3
VERTEX_RETRY_INITIAL_MS=250
VERTEX_RETRY_MAX_MS=2000
"@
  Set-Content -Path ".env" -Value $content -Encoding utf8
  Ok "Wrote .env"
}

function New-ServiceAccountKey {
  $keyDir = Split-Path -Parent $KeyPath
  if ($keyDir) {
    New-Item -ItemType Directory -Force -Path $keyDir | Out-Null
  }
  New-Item -ItemType Directory -Force -Path $SetupLogDir | Out-Null
  $log = Join-Path $SetupLogDir ("{0}-create-service-account-key.log" -f (Get-Date -Format yyyyMMddHHmmss))
  & gcloud iam service-accounts keys create $KeyPath "--iam-account=$script:ServiceAccountEmail" *> $log
  if ($LASTEXITCODE -eq 0) {
    Ok "Create service account key"
    return
  }
  if (Test-Path $KeyPath) {
    Remove-Item $KeyPath -Force
  }
  $logText = ""
  if (Test-Path $log) {
    $logText = Get-Content $log -Raw
  }
  if ($logText -match "constraints/iam.disableServiceAccountKeyCreation") {
    Warn "Your Google organization policy does not allow service account key creation."
    Write-Host "Blocked policy: constraints/iam.disableServiceAccountKeyCreation"
    if (Confirm-YesNo "Change this project policy now so setup can create the key?" "yes") {
      Allow-ServiceAccountKeyCreation
      & gcloud iam service-accounts keys create $KeyPath "--iam-account=$script:ServiceAccountEmail" *> $log
      if ($LASTEXITCODE -eq 0) {
        Ok "Create service account key"
        return
      }
    }
  }
  Write-Host "Log: $log" -ForegroundColor Yellow
  if (Test-Path $log) {
    Get-Content $log -Tail 80
  }
  Fail "Could not create service account key"
}

Step "Byto Windows Production Setup"
Write-Host "Creates/reuses a Google service account, downloads an ignored key file, and writes .env."

Read-EnvFile
if ([string]::IsNullOrWhiteSpace($Project)) { $Project = $env:GOOGLE_CLOUD_PROJECT }
if ([string]::IsNullOrWhiteSpace($Location)) { $Location = $(if ($env:GOOGLE_CLOUD_LOCATION) { $env:GOOGLE_CLOUD_LOCATION } else { "global" }) }
if ([string]::IsNullOrWhiteSpace($ApiKey)) { $ApiKey = $env:GATEWAY_API_KEYS }
if ([string]::IsNullOrWhiteSpace($KeyPath) -and $env:GOOGLE_APPLICATION_CREDENTIALS) { $KeyPath = $env:GOOGLE_APPLICATION_CREDENTIALS }
if ([string]::IsNullOrWhiteSpace($Project) -or $Project -eq "your-project-id" -or $Project -eq "test-project") {
  $Project = Prompt-Value "Google Cloud project" $Project
}
$Location = Prompt-Value "Vertex location" $Location
$ServiceAccountName = Prompt-Value "Service account name" $ServiceAccountName
$KeyPath = Prompt-Value "Service account key path" $KeyPath
if ([string]::IsNullOrWhiteSpace($ApiKey) -or $ApiKey -eq "dev-local-key" -or $ApiKey -eq "dev-local-key-change-me") {
  $ApiKey = New-GatewayKey
  Ok "Generated gateway API key"
}
if ([string]::IsNullOrWhiteSpace($Project)) {
  Fail "Google Cloud project is required"
}

Step "Checking Dependencies"
Ensure-Go
Ensure-Gcloud

$script:ServiceAccountEmail = "$ServiceAccountName@$Project.iam.gserviceaccount.com"

Step "Google Cloud"
Invoke-Step "Set gcloud project" { & gcloud config set project $Project }
$activeAccount = (& gcloud auth list "--filter=status:ACTIVE" "--format=value(account)" 2>$null | Select-Object -First 1)
if ([string]::IsNullOrWhiteSpace($activeAccount)) {
  if ($NonInteractive) {
    Fail "No active gcloud account. Run gcloud auth login first."
  }
  Invoke-Step "Authenticate gcloud" { & gcloud auth login }
}
Invoke-Step "Enable Google APIs" { & gcloud services enable aiplatform.googleapis.com iam.googleapis.com serviceusage.googleapis.com cloudresourcemanager.googleapis.com }

$global:LASTEXITCODE = 0
& gcloud iam service-accounts describe $script:ServiceAccountEmail *> $null
if ($LASTEXITCODE -eq 0) {
  Ok "Service account exists: $script:ServiceAccountEmail"
} else {
  Invoke-Step "Create service account" { & gcloud iam service-accounts create $ServiceAccountName "--display-name=$ServiceAccountDisplay" }
}

Step "IAM"
foreach ($role in @("roles/aiplatform.user", "roles/serviceusage.serviceUsageConsumer")) {
  Invoke-Step "Grant $role" {
    & gcloud projects add-iam-policy-binding $Project "--member=serviceAccount:$script:ServiceAccountEmail" "--role=$role" --quiet
  }
}

Step "Service Account Key"
if (Test-ServiceKeyValid $KeyPath) {
  Ok "Using existing key: $KeyPath"
} else {
  if (Test-Path $KeyPath) {
    Warn "Ignoring invalid or empty key file at $KeyPath"
    Remove-Item $KeyPath -Force
  }
  New-ServiceAccountKey
}

Write-ProductionEnv
Copy-KeyToClipboard

if (-not $SkipVerify -and -not [string]::IsNullOrWhiteSpace($Model)) {
  Step "Live Verification"
  $absoluteKeyPath = Convert-ToRootedPath $KeyPath
  Invoke-Step "Verify gateway with service account" {
    $env:GOOGLE_CLOUD_PROJECT = $Project
    $env:GOOGLE_CLOUD_LOCATION = $Location
    $env:GATEWAY_API_KEYS = $ApiKey
    $env:GATEWAY_ALLOW_UNAUTHENTICATED = "false"
    $env:GOOGLE_APPLICATION_CREDENTIALS = $absoluteKeyPath
    $env:VERTEX_ACCESS_TOKEN = ""
    $env:MODEL_CATALOG_PATH = ""
    $env:ALLOW_ANY_GEMINI_MODEL = "true"
    $env:RUN_LIVE_VERTEX_TESTS = "1"
    $env:LIVE_VERTEX_MODEL = $Model
    & go test ./test/e2e -run TestLiveVertexGenerateExplicitModel -count=1 -v
  }
} elseif (-not $SkipVerify) {
  Warn "Skipped live generation verification because no model was provided"
}

Step "Done"
Ok "Production setup complete"
Write-Host "Service account: $script:ServiceAccountEmail"
Write-Host "Key path: $KeyPath"
