$ErrorActionPreference = 'Stop'
Set-Location -LiteralPath (Split-Path -Parent $PSScriptRoot)

if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
    throw 'Docker Desktop is required. See dev/README.md.'
}

& docker compose -f dev/docker-compose.yml up -d --wait db mailhog adminer
if ($LASTEXITCODE -ne 0) {
    throw 'The development middleware failed to start.'
}

Write-Host 'PostgreSQL: localhost:5437; MailHog: http://localhost:8265; Adminer: http://localhost:8171'
