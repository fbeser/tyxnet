param(
    [string]$Destination = "bin"
)

$ErrorActionPreference = "Stop"
$version = "0.14.1"
$expectedSha256 = "07c256185d6ee3652e09fa55c0b673e2624b565e02c4b9091c79ca7d2f24ef51"
$downloadUrl = "https://www.wintun.net/builds/wintun-$version.zip"
$repoRoot = Split-Path -Parent $PSScriptRoot
$destinationPath = Join-Path $repoRoot $Destination

$architecture = switch ($env:PROCESSOR_ARCHITECTURE) {
    "AMD64" { "amd64" }
    "ARM64" { "arm64" }
    default { throw "Unsupported Windows architecture: $env:PROCESSOR_ARCHITECTURE" }
}

$temporaryRoot = Join-Path ([System.IO.Path]::GetTempPath()) ("tyxnet-wintun-" + [guid]::NewGuid())
$archivePath = Join-Path $temporaryRoot "wintun.zip"
$extractPath = Join-Path $temporaryRoot "extract"

try {
    New-Item -ItemType Directory -Force -Path $temporaryRoot, $extractPath, $destinationPath | Out-Null
    Write-Host "Downloading Wintun $version from the official distribution..." -ForegroundColor Cyan
    Invoke-WebRequest -Uri $downloadUrl -OutFile $archivePath

    $actualSha256 = (Get-FileHash -LiteralPath $archivePath -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($actualSha256 -ne $expectedSha256) {
        throw "Wintun archive checksum mismatch. Expected $expectedSha256, received $actualSha256."
    }

    Expand-Archive -LiteralPath $archivePath -DestinationPath $extractPath
    Copy-Item -LiteralPath (Join-Path $extractPath "wintun\bin\$architecture\wintun.dll") -Destination (Join-Path $destinationPath "wintun.dll") -Force
    Copy-Item -LiteralPath (Join-Path $extractPath "wintun\LICENSE.txt") -Destination (Join-Path $destinationPath "wintun-LICENSE.txt") -Force
    Write-Host "Verified Wintun installed beside TyxNet Server." -ForegroundColor Green
}
finally {
    if (Test-Path -LiteralPath $temporaryRoot) {
        Remove-Item -LiteralPath $temporaryRoot -Recurse -Force
    }
}
