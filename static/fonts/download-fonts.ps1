# Run with: powershell -ExecutionPolicy Bypass -File download-fonts.ps1
# Downloads DM Sans woff2 from Google Fonts into this folder (requires internet).

$ErrorActionPreference = "Stop"
$base = $PSScriptRoot
$latin = "https://fonts.gstatic.com/s/dmsans/v17/rP2Yp2ywxg089UriI5-g4vlH9VoD8Cmcqbu0-K4.woff2"
$latinExt = "https://fonts.gstatic.com/s/dmsans/v17/rP2Yp2ywxg089UriI5-g4vlH9VoD8Cmcqbu6-K6h9Q.woff2"

Write-Host "Downloading DM Sans (latin)..."
Invoke-WebRequest -Uri $latin -OutFile (Join-Path $base "dm-sans-latin.woff2") -UseBasicParsing
Write-Host "Downloading DM Sans (latin-ext)..."
Invoke-WebRequest -Uri $latinExt -OutFile (Join-Path $base "dm-sans-latin-ext.woff2") -UseBasicParsing
Write-Host "Done. Fonts saved in $base"
