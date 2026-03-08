$ErrorActionPreference = "Stop"
$env:GOOS = "linux"
$env:GOARCH = "amd64"
$binDir = "bin"
$out = Join-Path $binDir "netgate"
if (-not (Test-Path $binDir)) { New-Item -ItemType Directory -Path $binDir | Out-Null }
go build -o $out .
Write-Host "Linux binary: $out"
