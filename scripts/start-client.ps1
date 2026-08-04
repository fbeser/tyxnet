param(
    [string]$Config = "client-state/client.yaml",
    [switch]$Local,
    [switch]$Lan
)

$ErrorActionPreference = "Stop"
$repoRoot = Split-Path -Parent $PSScriptRoot
Set-Location -LiteralPath $repoRoot
if ($Local -and $Lan) { throw "Use either -Local or -Lan, not both." }

$principal = New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent())
if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    Write-Host "Requesting Administrator access for the TyxNet client adapter..." -ForegroundColor Yellow
    $scriptPath = $MyInvocation.MyCommand.Path
    $modeArgument = if ($Local) { " -Local" } elseif ($Lan) { " -Lan" } else { "" }
    $arguments = "-NoProfile -ExecutionPolicy Bypass -File `"$scriptPath`" -Config `"$Config`"$modeArgument"
    Start-Process -FilePath "powershell.exe" -Verb RunAs -ArgumentList $arguments
    exit
}

New-Item -ItemType Directory -Force -Path "bin" | Out-Null
New-Item -ItemType Directory -Force -Path "logs" | Out-Null
Write-Host "Building TyxNet Client..." -ForegroundColor Cyan
go build -o "bin/tyxnet-client.exe" ./cmd/tyxnet-client
go build -ldflags "-H=windowsgui" -o "bin/tyxnet-tray.exe" ./cmd/tyxnet-tray

if (-not (Test-Path -LiteralPath "bin/wintun.dll")) {
    & "$PSScriptRoot/install-wintun.ps1" -Destination "bin"
}

$clientArguments = @("run", "--config", $Config)
if ($Lan) { $clientArguments += "--lan-web" }
if ($Local) { $clientArguments += "--local-web" }

$clientURL = if ($Local) { "http://127.0.0.1:9070" } else { "http://<LAN-IP>:9070" }
Write-Host ""
Write-Host "TyxNet Client setup is available at $clientURL" -ForegroundColor Green
Write-Host "Use the tray menu to open or stop the client." -ForegroundColor DarkGray
Write-Host ""

$env:TYXNET_TRAY_TOKEN = [Guid]::NewGuid().ToString("N") + [Guid]::NewGuid().ToString("N")
if (-not (Get-Process -Name "tyxnet-client" -ErrorAction SilentlyContinue)) {
    Start-Process -FilePath (Resolve-Path "bin/tyxnet-client.exe") -ArgumentList $clientArguments -WorkingDirectory $repoRoot -WindowStyle Hidden -RedirectStandardOutput (Join-Path $repoRoot "logs/client.log") -RedirectStandardError (Join-Path $repoRoot "logs/client-error.log")
}
if (-not (Get-Process -Name "tyxnet-tray" -ErrorAction SilentlyContinue)) {
    Start-Process -FilePath (Resolve-Path "bin/tyxnet-tray.exe") -WindowStyle Hidden
}
Write-Host "TyxNet Client is running in the background. Use the tray icon to open or quit it." -ForegroundColor Green
