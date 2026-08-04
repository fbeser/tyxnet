param(
    [string]$Config = "configs/server.yaml",
    [string]$WebURL = "http://127.0.0.1:8443",
    [switch]$Local,
    [switch]$Lan
)

$ErrorActionPreference = "Stop"
$repoRoot = Split-Path -Parent $PSScriptRoot
Set-Location -LiteralPath $repoRoot
if ($Local -and $Lan) { throw "Use either -Local or -Lan, not both." }

$principal = New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent())
if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    Write-Host "Requesting Administrator access for the TyxNet virtual adapter..." -ForegroundColor Yellow
    $scriptPath = $MyInvocation.MyCommand.Path
    $modeArgument = if ($Local) { " -Local" } elseif ($Lan) { " -Lan" } else { "" }
    $arguments = "-NoProfile -ExecutionPolicy Bypass -File `"$scriptPath`" -Config `"$Config`" -WebURL `"$WebURL`"$modeArgument"
    Start-Process -FilePath "powershell.exe" -Verb RunAs -ArgumentList $arguments
    exit
}

New-Item -ItemType Directory -Force -Path "bin" | Out-Null
New-Item -ItemType Directory -Force -Path "logs" | Out-Null
Write-Host "Building TyxNet Server..." -ForegroundColor Cyan
go build -o "bin/tyxnet-server.exe" ./cmd/tyxnet-server
go build -ldflags "-H=windowsgui" -o "bin/tyxnet-server-tray.exe" ./cmd/tyxnet-server-tray

if (-not (Test-Path -LiteralPath "bin/wintun.dll")) {
    & "$PSScriptRoot/install-wintun.ps1" -Destination "bin"
}

Write-Host ""
$serverURL = if ($Local) { "http://127.0.0.1:8443" } else { "http://<LAN-IP>:8443" }
Write-Host "TyxNet will be available at $serverURL" -ForegroundColor Green
Write-Host "Open that address in your browser or use the tray menu." -ForegroundColor DarkGray
Write-Host ""

$env:TYXNET_TRAY_TOKEN = [Guid]::NewGuid().ToString("N") + [Guid]::NewGuid().ToString("N")
$serverArguments = @("run", "--config", $Config)
if ($Lan) { $serverArguments += "--lan-web" }
if ($Local) { $serverArguments += "--local-web" }

if (-not (Get-Process -Name "tyxnet-server" -ErrorAction SilentlyContinue)) {
    Start-Process -FilePath (Resolve-Path "bin/tyxnet-server.exe") -ArgumentList $serverArguments -WorkingDirectory $repoRoot -WindowStyle Hidden -RedirectStandardOutput (Join-Path $repoRoot "logs/server.log") -RedirectStandardError (Join-Path $repoRoot "logs/server-error.log")
}
if (-not (Get-Process -Name "tyxnet-server-tray" -ErrorAction SilentlyContinue)) {
    Start-Process -FilePath (Resolve-Path "bin/tyxnet-server-tray.exe") -ArgumentList @("--server-url", $WebURL) -WindowStyle Hidden
}
Write-Host "TyxNet Server is running in the background. Use the tray icon to open or quit it." -ForegroundColor Green
