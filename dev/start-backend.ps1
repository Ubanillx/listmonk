$ErrorActionPreference = 'Stop'
Set-Location -LiteralPath (Split-Path -Parent $PSScriptRoot)

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    throw 'Go is required. See dev/README.md.'
}

if (Get-NetTCPConnection -State Listen -LocalPort 9173 -ErrorAction SilentlyContinue) {
    try {
        $page = Invoke-WebRequest -Uri 'http://127.0.0.1:9173/admin/login' -UseBasicParsing -TimeoutSec 3
        if ($page.StatusCode -eq 200 -and $page.Content.Contains('name="nonce"')) {
            Write-Host 'Go backend is already running on http://localhost:9173 .'
            exit 0
        }
    } catch { }
    throw 'Port 9173 is occupied by another service.'
}

$env:CGO_ENABLED = '0'
& go run ./cmd --install --idempotent --yes --config dev/config.local.toml
if ($LASTEXITCODE -ne 0) { throw 'Database install failed. Start the Docker middleware first.' }

& go run ./cmd --upgrade --yes --config dev/config.local.toml
if ($LASTEXITCODE -ne 0) { throw 'Database upgrade failed.' }

Write-Host 'Starting Go with Air hot reload on http://localhost:9173 . Press Ctrl+C to stop.'
& go run github.com/air-verse/air@v1.67.4 -c dev/.air.toml
exit $LASTEXITCODE
