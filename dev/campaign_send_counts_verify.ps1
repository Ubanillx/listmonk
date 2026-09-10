# Dev-only verification for the unified campaign send counts.
#
# The UI projections, the sender's send-state query and the progress sync used to
# compute "how many recipients are left" with three different copies of the rule
# (some ignoring pool recipients, some skipping the ownership checks). They now
# all read campaign_send_counts. This script extracts the *committed* expression
# and statement text from queries/campaigns.sql, builds fixtures - including
# synthetic pool recipients, which the dev database has none of - and checks the
# view against an independent recomputation from the base tables. Everything runs
# in a transaction that is rolled back.
#
# Usage: pwsh -File dev/campaign_send_counts_verify.ps1

param(
    [string]$DbContainer = "dev-db-1",
    [string]$DbUser = "lmkdev_M7q2Xr8N",
    [string]$DbName = "lmkdev_M7q2Xr8N"
)

$ErrorActionPreference = "Stop"
Set-Location (Split-Path -Parent $PSScriptRoot)

function Invoke-Sql {
    param([string]$Sql)
    ($Sql | docker exec -i $DbContainer psql -U $DbUser -d $DbName -v ON_ERROR_STOP=1 -t -A 2>&1) -join "`n"
}

function Get-QueryBody {
    param([string]$Path, [string]$Name)

    $lines = Get-Content -LiteralPath $Path
    $start = -1
    for ($i = 0; $i -lt $lines.Count; $i++) {
        if ($lines[$i] -match "^-- name:\s+$([regex]::Escape($Name))\s*$") { $start = $i + 1; break }
    }
    if ($start -lt 0) { throw "query '$Name' not found in $Path" }

    $body = @()
    for ($i = $start; $i -lt $lines.Count; $i++) {
        if ($lines[$i] -match '^-- name:\s') { break }
        $body += $lines[$i]
    }
    return (($body -join "`n").Trim() -replace ';\s*$', '')
}

# ---------------------------------------------------------------------------
# Fixtures.
# ---------------------------------------------------------------------------
$mixed = (Invoke-Sql @"
SELECT c.id::text || '|' || COALESCE(c.organization_id::text, 'NULL') || '|' || c.status
FROM campaigns c
WHERE (SELECT count(*) FROM campaign_recipients cr WHERE cr.campaign_id = c.id) > 0
ORDER BY (SELECT count(*) FROM campaign_recipients cr2 WHERE cr2.campaign_id = c.id) DESC
LIMIT 1;
"@).Trim()
if (-not $mixed) { throw "no campaign with recipient snapshot rows found" }
$mixedId, $mixedOrg, $mixedStatus = $mixed -split '\|'

$emptyId = (Invoke-Sql @"
SELECT c.id::text FROM campaigns c
WHERE NOT EXISTS (SELECT 1 FROM campaign_recipients cr WHERE cr.campaign_id = c.id)
  AND NOT EXISTS (SELECT 1 FROM campaign_pool_recipients cpr WHERE cpr.campaign_id = c.id)
LIMIT 1;
"@).Trim()
if (-not $emptyId) { throw "no campaign without recipient snapshot rows found" }

Write-Host "fixtures: mixed campaign=$mixedId (org=$mixedOrg, status=$mixedStatus) empty campaign=$emptyId"

$orgExpr = if ($mixedOrg -eq 'NULL') { 'NULL' } else { $mixedOrg }

# The committed projection expression (identical text in every projection).
$statusBody = Get-QueryBody -Path "queries/campaigns.sql" -Name "get-campaign-status"
$exprMatch = [regex]::Match($statusBody, 'COALESCE\(\(SELECT sc\.unsent_count FROM campaign_send_counts sc WHERE sc\.campaign_id = campaigns\.id\), 0\) AS unsent_count')
if (-not $exprMatch.Success) { throw "could not find the shared unsent_count expression in get-campaign-status" }
$uiExpr = $exprMatch.Value -replace ' AS unsent_count$', ''

$sendState = Get-QueryBody -Path "queries/campaigns.sql" -Name "get-campaign-send-state"
$sendState = $sendState.Replace('$1', $mixedId).Replace('$2', 'CURRENT_DATE')

$syncProgress = Get-QueryBody -Path "queries/campaigns.sql" -Name "sync-campaign-progress"
$syncProgress = $syncProgress.Replace('$1', $mixedId)

$statusQuery = $statusBody.Replace('$1', "'$mixedStatus'")

$sql = @"
\pset pager off
BEGIN;

-- Synthetic pool recipients for the mixed campaign: the dev database has pool
-- contacts and pool customer_lists but no campaign_pool_recipients rows, so the
-- pool branch of the view would otherwise go untested.
INSERT INTO campaign_pool_recipients (campaign_id, pool_contact_id, pool_id, organization_id, status, email_snapshot)
SELECT $mixedId, pc.id, (SELECT id FROM customer_lists WHERE type = 'pool' ORDER BY id LIMIT 1),
       $orgExpr, s.status, pc.email
FROM (SELECT id, email, row_number() OVER (ORDER BY id) AS rn FROM pool_contacts LIMIT 3) pc
JOIN (VALUES (1, 'pending'::campaign_recipient_status), (2, 'queued'::campaign_recipient_status), (3, 'sent'::campaign_recipient_status)) AS s(rn, status)
  ON s.rn = pc.rn;

-- A campaign with no snapshot rows keeps using the persisted totals.
UPDATE campaigns SET to_send = 10, sent = 4 WHERE id = $emptyId;

\echo '=== 1. campaign_send_counts equals an independent recomputation ==='
SELECT '1a one view row per campaign' AS check,
    CASE WHEN (SELECT count(*) FROM campaign_send_counts) = (SELECT count(*) FROM campaigns)
        THEN 'PASS' ELSE 'FAIL' END AS result;

SELECT '1b mixed campaign counts customer and pool rows' AS check,
    CASE WHEN (SELECT unsent_count FROM campaign_send_counts WHERE campaign_id = $mixedId) =
              (SELECT COALESCE(cu.unsent, 0) + COALESCE(po.unsent, 0)
               FROM campaigns c
               LEFT JOIN LATERAL (
                   SELECT COUNT(*) FILTER (WHERE cr.status = ANY('{pending,queued,deferred}'::campaign_recipient_status[])) AS unsent
                   FROM campaign_recipients cr JOIN customers s ON s.id = cr.customer_id
                   WHERE cr.campaign_id = c.id
                     AND s.organization_id IS NOT DISTINCT FROM c.organization_id
                     AND s.owner_user_id = c.owner_user_id
                     AND s.transfer_pending_at IS NULL) cu ON TRUE
               LEFT JOIN LATERAL (
                   SELECT COUNT(*) FILTER (WHERE cpr.status = ANY('{pending,queued,deferred}'::campaign_recipient_status[])) AS unsent
                   FROM campaign_pool_recipients cpr
                   WHERE cpr.campaign_id = c.id
                     AND cpr.organization_id IS NOT DISTINCT FROM c.organization_id) po ON TRUE
               WHERE c.id = $mixedId)
        THEN 'PASS' ELSE 'FAIL' END AS result;

SELECT '1c mixed campaign queued count spans both relations' AS check,
    CASE WHEN (SELECT queued_count FROM campaign_send_counts WHERE campaign_id = $mixedId) =
              (SELECT (SELECT COUNT(*) FROM campaign_recipients cr JOIN customers s ON s.id = cr.customer_id
                       JOIN campaigns c ON c.id = cr.campaign_id
                       WHERE cr.campaign_id = $mixedId AND cr.status = 'queued'
                         AND s.organization_id IS NOT DISTINCT FROM c.organization_id
                         AND s.owner_user_id = c.owner_user_id AND s.transfer_pending_at IS NULL)
                    + (SELECT COUNT(*) FROM campaign_pool_recipients cpr JOIN campaigns c ON c.id = cpr.campaign_id
                       WHERE cpr.campaign_id = $mixedId AND cpr.status = 'queued'
                         AND cpr.organization_id IS NOT DISTINCT FROM c.organization_id))
        THEN 'PASS' ELSE 'FAIL' END AS result;

SELECT '1d mixed campaign reports a snapshot and pool-only unsent is not the fallback' AS check,
    CASE WHEN (SELECT has_snapshot FROM campaign_send_counts WHERE campaign_id = $mixedId)
              AND (SELECT unsent_snapshot_count FROM campaign_send_counts WHERE campaign_id = $mixedId) > 0
              AND (SELECT unsent_count FROM campaign_send_counts WHERE campaign_id = $mixedId)
                  = (SELECT unsent_snapshot_count FROM campaign_send_counts WHERE campaign_id = $mixedId)
        THEN 'PASS' ELSE 'FAIL' END AS result;

SELECT '1e a campaign without snapshot rows falls back to to_send - sent' AS check,
    CASE WHEN NOT (SELECT has_snapshot FROM campaign_send_counts WHERE campaign_id = $emptyId)
              AND (SELECT unsent_count FROM campaign_send_counts WHERE campaign_id = $emptyId) = 6
        THEN 'PASS' ELSE 'FAIL' END AS result;

\echo '=== 2. the committed UI projection matches the view for every campaign ==='
SELECT '2a every campaign agrees with the shared expression' AS check,
    CASE WHEN NOT EXISTS (
        SELECT 1 FROM campaigns
        WHERE $uiExpr IS DISTINCT FROM (SELECT sc.unsent_count FROM campaign_send_counts sc WHERE sc.campaign_id = campaigns.id))
        THEN 'PASS' ELSE 'FAIL' END AS result;

SELECT '2b the whole committed get-campaign-status statement runs and agrees' AS check,
    CASE WHEN NOT EXISTS (
        SELECT 1 FROM ($statusQuery) t
        LEFT JOIN campaign_send_counts sc ON sc.campaign_id = t.id
        WHERE t.unsent_count IS DISTINCT FROM COALESCE(sc.unsent_count, 0))
        THEN 'PASS' ELSE 'FAIL' END AS result;

\echo '=== 3. the sender reads the same definition ==='
SELECT '3a sender queued_count equals the view' AS check,
    CASE WHEN (SELECT queued_count FROM ($sendState) t) =
              (SELECT queued_count FROM campaign_send_counts WHERE campaign_id = $mixedId)
        THEN 'PASS' ELSE 'FAIL' END AS result;

SELECT '3b sender unsent count equals the view snapshot count (no fallback)' AS check,
    CASE WHEN (SELECT unsent_count FROM ($sendState) t) =
              (SELECT unsent_snapshot_count FROM campaign_send_counts WHERE campaign_id = $mixedId)
        THEN 'PASS' ELSE 'FAIL' END AS result;

\echo '=== 4. the progress sync writes the same definition ==='
$syncProgress;
SELECT '4a sync-campaign-progress stores the view totals' AS check,
    CASE WHEN (SELECT to_send FROM campaigns WHERE id = $mixedId) = (SELECT total_count FROM campaign_send_counts WHERE campaign_id = $mixedId)
              AND (SELECT sent FROM campaigns WHERE id = $mixedId) = (SELECT sent_count FROM campaign_send_counts WHERE campaign_id = $mixedId)
        THEN 'PASS' ELSE 'FAIL' END AS result;

SELECT '4b the persisted totals then agree with the effective recipient set' AS check,
    CASE WHEN (SELECT to_send - sent FROM campaigns WHERE id = $mixedId)
              = (SELECT unsent_snapshot_count FROM campaign_send_counts WHERE campaign_id = $mixedId)
        THEN 'PASS' ELSE 'FAIL' END AS result;

ROLLBACK;
\echo '=== every check above must read PASS ==='
"@

$tmp = Join-Path $env:TEMP "campaign_send_counts_verify.sql"
Set-Content -LiteralPath $tmp -Value $sql -Encoding UTF8

$out = Get-Content -LiteralPath $tmp -Raw | docker exec -i $DbContainer psql -U $DbUser -d $DbName -v ON_ERROR_STOP=1 2>&1
$out | Where-Object { $_ -match 'PASS|FAIL|ERROR|===' } | ForEach-Object { $_ }

$failed = @($out | Where-Object { $_ -match '\|\s+FAIL\s*$' -or $_ -match '^ERROR:' -or $_ -match '^psql:' })
if ($failed.Count -gt 0) {
    Write-Host "FAILED:"
    $failed | ForEach-Object { Write-Host "  $_" }
    exit 1
}
Write-Host "ALL CHECKS PASS"
exit 0
