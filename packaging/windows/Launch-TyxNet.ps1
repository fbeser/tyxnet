param(
    [Parameter(Mandatory = $true)]
    [ValidateSet("client", "server")]
    [string]$Mode
)

$ErrorActionPreference = "Stop"
$principal = New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent())
if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    $arguments = "-NoProfile -ExecutionPolicy Bypass -WindowStyle Hidden -File `"$PSCommandPath`" -Mode $Mode"
    Start-Process -FilePath "powershell.exe" -Verb RunAs -ArgumentList $arguments
    exit
}

$installDirectory = $PSScriptRoot
$dataDirectory = Join-Path $env:ProgramData "TyxNet"
$logDirectory = Join-Path $dataDirectory "logs"
New-Item -ItemType Directory -Force -Path $dataDirectory, $logDirectory | Out-Null

$env:TYXNET_TRAY_TOKEN = [Guid]::NewGuid().ToString("N") + [Guid]::NewGuid().ToString("N")
if ($Mode -eq "client") {
    $coreName = "tyxnet-client"
    $trayName = "tyxnet-tray"
    $configPath = Join-Path $dataDirectory "client.yaml"
    $coreArguments = @("run", "--config", $configPath, "--local-web")
    $trayArguments = @("--client-url", "http://127.0.0.1:9070")
    $webURL = "http://127.0.0.1:9070"
} else {
    $coreName = "tyxnet-server"
    $trayName = "tyxnet-server-tray"
    $configPath = Join-Path $dataDirectory "server.yaml"
    if (-not (Test-Path -LiteralPath $configPath)) {
        Copy-Item -LiteralPath (Join-Path $installDirectory "server.yaml") -Destination $configPath
    }
    $coreArguments = @("run", "--config", $configPath, "--local-web")
    $trayArguments = @("--server-url", "http://127.0.0.1:8443")
    $webURL = "http://127.0.0.1:8443"
}

$coreExecutable = Join-Path $installDirectory ($coreName + ".exe")
$trayExecutable = Join-Path $installDirectory ($trayName + ".exe")
if (Get-Process -Name $coreName -ErrorAction SilentlyContinue) {
    Start-Process $webURL
    exit
}
Start-Process -FilePath $coreExecutable -ArgumentList $coreArguments -WorkingDirectory $dataDirectory -WindowStyle Hidden -RedirectStandardOutput (Join-Path $logDirectory ($coreName + ".log")) -RedirectStandardError (Join-Path $logDirectory ($coreName + "-error.log"))
if (-not (Get-Process -Name $trayName -ErrorAction SilentlyContinue)) {
    Start-Process -FilePath $trayExecutable -ArgumentList $trayArguments -WorkingDirectory $dataDirectory -WindowStyle Hidden
}
