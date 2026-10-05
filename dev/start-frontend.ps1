$ErrorActionPreference = 'Stop'
Set-Location -LiteralPath (Split-Path -Parent $PSScriptRoot)

foreach ($command in @('node', 'yarn', 'make')) {
    if (-not (Get-Command $command -ErrorAction SilentlyContinue)) {
        throw "Missing required command: $command. See dev/README.md."
    }
}

if (Get-NetTCPConnection -State Listen -LocalPort 8181 -ErrorAction SilentlyContinue) {
    try {
        $page = Invoke-WebRequest -Uri 'http://127.0.0.1:8181/admin/' -UseBasicParsing -TimeoutSec 3
        if ($page.StatusCode -eq 200 -and $page.Content.Contains('/admin/@vite/client')) {
            Write-Host 'Vite frontend is already running on http://localhost:8181/admin/ .'
            exit 0
        }
    } catch { }
    throw 'Port 8181 is occupied by another service.'
}

Write-Host 'Starting Vite hot reload on http://localhost:8181/admin/ . Press Ctrl+C to stop.'
& make run-frontend-local
exit $LASTEXITCODE
