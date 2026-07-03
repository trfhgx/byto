param(
  [string]$Project = $env:GOOGLE_CLOUD_PROJECT,
  [string]$Location = $(if ($env:GOOGLE_CLOUD_LOCATION) { $env:GOOGLE_CLOUD_LOCATION } else { "global" }),
  [string]$ApiKey = $env:GATEWAY_API_KEYS,
  [switch]$Open,
  [switch]$Protected,
  [switch]$NonInteractive,
  [switch]$SkipTests,
  [switch]$Run
)

$ErrorActionPreference = "Stop"
$Root = Resolve-Path (Join-Path $PSScriptRoot "..")
Set-Location $Root

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

function Write-EnvFile {
  if (Test-Path ".env") {
    $backup = ".env.backup.$(Get-Date -Format yyyyMMddHHmmss)"
    Copy-Item ".env" $backup
    Ok "Backed up existing .env to $backup"
  }

  $content = @"
GOOGLE_CLOUD_PROJECT=$Project
GOOGLE_CLOUD_LOCATION=$Location
MODEL_CATALOG_PATH=config/models.json
MODEL_CATALOG_REFRESH_ON_START=true
ALLOWED_MODELS=
ALLOW_ANY_GEMINI_MODEL=false
GATEWAY_API_KEYS=$ApiKey
GATEWAY_ALLOW_UNAUTHENTICATED=$GatewayOpen
VERTEX_BASE_URL=https://aiplatform.googleapis.com
PORT=8080
LOG_PATH=logs/requests.jsonl
REQUEST_TIMEOUT_SECONDS=180
VERTEX_RETRY_MAX_ATTEMPTS=3
VERTEX_RETRY_INITIAL_MS=250
VERTEX_RETRY_MAX_MS=2000

# Optional auth overrides:
# VERTEX_ACCESS_TOKEN=
# GOOGLE_APPLICATION_CREDENTIALS=
"@
  Set-Content -Path ".env" -Value $content -Encoding utf8
  Ok "Wrote .env"
}

function Ensure-Go {
  $go = Get-Command go -ErrorAction SilentlyContinue
  if (-not $go) {
    Fail "Go 1.22+ is required. Install it from https://go.dev/dl/ and open a new PowerShell window."
  }
  $versionText = (& go version)
  if ($versionText -notmatch "go(\d+)\.(\d+)") {
    Fail "Could not parse Go version: $versionText"
  }
  $major = [int]$Matches[1]
  $minor = [int]$Matches[2]
  if ($major -lt 1 -or ($major -eq 1 -and $minor -lt 22)) {
    Fail "$versionText found, but Go 1.22+ is required."
  }
  Ok $versionText
}

function Ensure-Gcloud {
  $gcloud = Get-Command gcloud -ErrorAction SilentlyContinue
  if (-not $gcloud) {
    Warn "Google Cloud CLI is not installed. Install it from https://cloud.google.com/sdk/docs/install-sdk#windows for live Vertex auth."
    return $false
  }
  Ok "gcloud CLI found"
  return $true
}

function Configure-GatewayAuth {
  if ($Open) {
    $script:GatewayOpen = "true"
    $script:ApiKey = ""
    Warn "Gateway will accept /v1 requests without Authorization. Use only behind a private boundary."
    return
  }
  if (-not $Protected -and -not $NonInteractive) {
    $choice = Select-Menu "Gateway access:" @("Protect with API key", "Open access, no gateway API key") 0
    if ($choice -eq 1) {
      $script:GatewayOpen = "true"
      $script:ApiKey = ""
      Warn "Gateway will accept /v1 requests without Authorization. Use only behind a private boundary."
      return
    }
  }
  $script:GatewayOpen = "false"
  if ([string]::IsNullOrWhiteSpace($ApiKey) -or $ApiKey -eq "dev-local-key" -or $ApiKey -eq "dev-local-key-change-me") {
    $script:ApiKey = New-GatewayKey
    Ok "Generated gateway API key"
  }
}

function Configure-GoogleAuth($HasGcloud) {
  if (-not $HasGcloud) {
    Warn "Skipping Google auth because gcloud is missing."
    return
  }
  if ($NonInteractive) {
    return
  }
  $choice = Select-Menu "Google Cloud auth:" @("Run full Google auth now", "Set gcloud project only", "Skip auth") 0
  if ($choice -eq 0) {
    & gcloud auth login
    & gcloud auth application-default login
    & gcloud config set project $Project
    & gcloud auth application-default set-quota-project $Project
    & gcloud services enable aiplatform.googleapis.com --project $Project
    Ok "Google auth configured"
  } elseif ($choice -eq 1) {
    & gcloud config set project $Project
    & gcloud auth application-default set-quota-project $Project
    Ok "gcloud project configured"
  }
}

Step "Byto Windows Setup"
Read-EnvFile

Step "Checking Dependencies"
Ensure-Go
$hasGcloud = Ensure-Gcloud

Step "Configuring Local Environment"
if ([string]::IsNullOrWhiteSpace($Project)) {
  $Project = Prompt-Value "Google Cloud project ID" "your-project-id"
}
$Location = Prompt-Value "Vertex AI location" $Location
Configure-GatewayAuth
Write-EnvFile

if ($Project -eq "your-project-id") {
  Warn ".env still uses the placeholder project ID. Edit GOOGLE_CLOUD_PROJECT before live Vertex calls."
}

Step "Google Cloud Auth"
Configure-GoogleAuth $hasGcloud

if (-not $SkipTests) {
  Step "Running Local Verification"
  & go test ./...
  if ($LASTEXITCODE -ne 0) {
    Fail "go test ./... failed"
  }
}

Ok "Setup complete"
Write-Host ""
Write-Host "Start the gateway:"
Write-Host "  go run ./cmd/gateway"
if ($GatewayOpen -ne "true") {
  Write-Host ""
  Write-Host "Gateway API key:"
  Write-Host "  $ApiKey"
}

if ($Run) {
  & go run ./cmd/gateway
}
