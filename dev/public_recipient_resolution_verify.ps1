# Dev-only verification for the shared public-recipient resolution.
#
# The four public bearer paths (archive render, view registration, link click,
# bearer unsubscribe) each carried their own copy of the
# "campaign UUID + customer UUID -> actual recipient" CTE and had already
# diverged on how an empty customer reference is handled. They now all consume
# resolve_campaign_recipient(). This script extracts the *committed* statement
# text from queries/*.sql (so the verification cannot drift from the code),
# substitutes fixture literals for the placeholders, and exercises every branch
# against real rows inside a transaction that is rolled back.
#
# Usage: pwsh -File dev/public_recipient_resolution_verify.ps1

param(
    [string]$DbContainer = "dev-db-1",
    [string]$DbUser = "lmkdev_M7q2Xr8N",
    [string]$DbName = "lmkdev_M7q2Xr8N"
)

$ErrorActionPreference = "Stop"
Set-Location (Split-Path -Parent $PSScriptRoot)

function Invoke-Sql {
    param([string]$Sql)
    $Sql | docker exec -i $DbContainer psql -U $DbUser -d $DbName -v ON_ERROR_STOP=1 -t -A 2>&1
}

# Extracts one goyesql statement body: everything after "-- name: <name>" up to
# the next "-- name:" marker, trailing semicolon removed.
function Get-QueryBody {
    param([string]$Path, [string]$Name, [hashtable]$Params)

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

    $text = ($body -join "`n").Trim() -replace ';\s*$', ''
    foreach ($key in $Params.Keys) {
        $text = $text.Replace($key, $Params[$key])
    }
    if ($text -match '\$\d') { throw "query '$Name' still has unsubstituted placeholders" }
    return $text
}

# ---------------------------------------------------------------------------
# Fixtures from live rows.
# ---------------------------------------------------------------------------
$pair = (Invoke-Sql @"
SELECT c.uuid::text || '|' || s.uuid::text
FROM campaign_recipients cr
JOIN campaigns c ON c.id = cr.campaign_id
JOIN customers s ON s.id = cr.customer_id
WHERE (SELECT count(*) FROM campaign_recipients x WHERE x.campaign_id = c.id) > 0
LIMIT 1;
"@).Trim()
if (-not $pair) { throw "no campaign/recipient fixture found in the dev database" }
$campaignUuid, $recipientUuid = $pair -split '\|'

$strangerUuid = (Invoke-Sql @"
SELECT s.uuid::text FROM customers s
WHERE NOT EXISTS (
    SELECT 1 FROM campaign_recipients cr
    WHERE cr.campaign_id = (SELECT id FROM campaigns WHERE uuid = '$campaignUuid')
      AND cr.customer_id = s.id)
LIMIT 1;
"@).Trim()
if (-not $strangerUuid) { throw "no non-recipient customer fixture found" }

# A campaign with no recipient snapshot and at least one list, i.e. the legacy
# fallback branch.
$legacyUuid = (Invoke-Sql @"
SELECT c.uuid::text FROM campaigns c
WHERE NOT EXISTS (SELECT 1 FROM campaign_recipients cr WHERE cr.campaign_id = c.id)
  AND EXISTS (SELECT 1 FROM campaign_customer_lists cl WHERE cl.campaign_id = c.id)
LIMIT 1;
"@).Trim()
$legacyMemberUuid = ""
if ($legacyUuid) {
    $legacyMemberUuid = (Invoke-Sql @"
SELECT s.uuid::text
FROM customer_list_memberships sl
JOIN customers s ON s.id = sl.customer_id
WHERE sl.customer_list_id IN (
    SELECT customer_list_id FROM campaign_customer_lists
    WHERE campaign_id = (SELECT id FROM campaigns WHERE uuid = '$legacyUuid'))
LIMIT 1;
"@).Trim()
}

Write-Host "fixture campaign=$campaignUuid recipient=$recipientUuid stranger=$strangerUuid legacy=$legacyUuid legacyMember=$legacyMemberUuid"

$recipientParams = @{ '$1' = "'$campaignUuid'"; '$2' = "'$recipientUuid'" }
$strangerParams = @{ '$1' = "'$campaignUuid'"; '$2' = "'$strangerUuid'" }
$emptyParams = @{ '$1' = "'$campaignUuid'"; '$2' = "''" }

$qRecipient = Get-QueryBody -Path "queries/campaigns.sql" -Name "get-public-campaign-recipient" -Params $recipientParams
$qRecipientStranger = Get-QueryBody -Path "queries/campaigns.sql" -Name "get-public-campaign-recipient" -Params $strangerParams
$qRecipientEmpty = Get-QueryBody -Path "queries/campaigns.sql" -Name "get-public-campaign-recipient" -Params $emptyParams

$qView = Get-QueryBody -Path "queries/campaigns.sql" -Name "register-campaign-view" -Params $recipientParams
$qViewEmpty = Get-QueryBody -Path "queries/campaigns.sql" -Name "register-campaign-view" -Params $emptyParams
$qViewStranger = Get-QueryBody -Path "queries/campaigns.sql" -Name "register-campaign-view" -Params $strangerParams

$clickParams = @{ '$1' = "'bbbbbbbb-2222-4222-8222-bbbbbbbbbbbb'"; '$2' = "'$campaignUuid'"; '$3' = "'$recipientUuid'" }
$clickEmptyParams = @{ '$1' = "'bbbbbbbb-2222-4222-8222-bbbbbbbbbbbb'"; '$2' = "'$campaignUuid'"; '$3' = "''" }
$clickStrangerParams = @{ '$1' = "'bbbbbbbb-2222-4222-8222-bbbbbbbbbbbb'"; '$2' = "'$campaignUuid'"; '$3' = "'$strangerUuid'" }
$qClick = Get-QueryBody -Path "queries/links.sql" -Name "register-link-click" -Params $clickParams
$qClickEmpty = Get-QueryBody -Path "queries/links.sql" -Name "register-link-click" -Params $clickEmptyParams
$qClickStranger = Get-QueryBody -Path "queries/links.sql" -Name "register-link-click" -Params $clickStrangerParams

$unsubParams = @{ '$1' = "'$campaignUuid'"; '$2' = "'$recipientUuid'"; '$3' = 'FALSE' }
$unsubStrangerParams = @{ '$1' = "'$campaignUuid'"; '$2' = "'$strangerUuid'"; '$3' = 'FALSE' }
$unsubEmptyParams = @{ '$1' = "'$campaignUuid'"; '$2' = "''"; '$3' = 'FALSE' }
$qUnsub = Get-QueryBody -Path "queries/customers.sql" -Name "unsubscribe-by-campaign" -Params $unsubParams
$qUnsubStranger = Get-QueryBody -Path "queries/customers.sql" -Name "unsubscribe-by-campaign" -Params $unsubStrangerParams
$qUnsubEmpty = Get-QueryBody -Path "queries/customers.sql" -Name "unsubscribe-by-campaign" -Params $unsubEmptyParams

$legacyChecks = ""
if ($legacyUuid -and $legacyMemberUuid) {
    $legacyChecks = @"

\echo '=== 1f legacy campaign fallback (no snapshot) ==='
SELECT '1f a list member of a snapshot-less campaign resolves' AS check,
    CASE WHEN (SELECT count(*) FROM resolve_campaign_recipient('$legacyUuid'::uuid, '$legacyMemberUuid')) = 1
        THEN 'PASS' ELSE 'FAIL' END AS result;

SELECT '1g a non-member of a snapshot-less campaign resolves to nothing' AS check,
    CASE WHEN (SELECT count(*) FROM resolve_campaign_recipient('$legacyUuid'::uuid, '$strangerUuid')) = 0
        THEN 'PASS' ELSE 'FAIL' END AS result;
"@
} else {
    $legacyChecks = "`n\echo '=== 1f/1g SKIPPED: no snapshot-less campaign with lists in this database ==='"
}

$sql = @"
\pset pager off
BEGIN;

\echo '=== 1. resolve_campaign_recipient ==='
SELECT '1a snapshot recipient resolves' AS check,
    CASE WHEN (SELECT count(*) FROM resolve_campaign_recipient('$campaignUuid'::uuid, '$recipientUuid')) = 1
        THEN 'PASS' ELSE 'FAIL' END AS result;
SELECT '1b non-recipient resolves to nothing' AS check,
    CASE WHEN (SELECT count(*) FROM resolve_campaign_recipient('$campaignUuid'::uuid, '$strangerUuid')) = 0
        THEN 'PASS' ELSE 'FAIL' END AS result;
SELECT '1c empty customer reference resolves to nothing without a cast error' AS check,
    CASE WHEN (SELECT count(*) FROM resolve_campaign_recipient('$campaignUuid'::uuid, '')) = 0
        THEN 'PASS' ELSE 'FAIL' END AS result;
SELECT '1d unknown campaign resolves to nothing' AS check,
    CASE WHEN (SELECT count(*) FROM resolve_campaign_recipient('00000000-0000-0000-0000-000000000000'::uuid, '$recipientUuid')) = 0
        THEN 'PASS' ELSE 'FAIL' END AS result;
INSERT INTO customer_uuid_aliases (uuid, customer_id)
SELECT 'aaaaaaaa-1111-4111-8111-aaaaaaaaaaaa'::uuid, id FROM customers WHERE uuid = '$recipientUuid';
SELECT '1e an aliased customer UUID resolves to the same recipient' AS check,
    CASE WHEN (SELECT customer_id FROM resolve_campaign_recipient('$campaignUuid'::uuid, 'aaaaaaaa-1111-4111-8111-aaaaaaaaaaaa'))
              = (SELECT customer_id FROM resolve_campaign_recipient('$campaignUuid'::uuid, '$recipientUuid'))
        THEN 'PASS' ELSE 'FAIL' END AS result;
$legacyChecks

\echo '=== 2. get-public-campaign-recipient (archive render) ==='
SELECT '2a recipient matched' AS check,
    CASE WHEN (SELECT count(*) FROM ($qRecipient) AS t) = 1 THEN 'PASS' ELSE 'FAIL' END AS result;
SELECT '2b non-recipient not matched' AS check,
    CASE WHEN (SELECT count(*) FROM ($qRecipientStranger) AS t) = 0 THEN 'PASS' ELSE 'FAIL' END AS result;
SELECT '2c empty customer reference not matched, no cast error' AS check,
    CASE WHEN (SELECT count(*) FROM ($qRecipientEmpty) AS t) = 0 THEN 'PASS' ELSE 'FAIL' END AS result;

\echo '=== 3. register-campaign-view (open tracking) ==='
$qView;
SELECT '3a recipient view recorded with the customer id' AS check,
    CASE WHEN EXISTS (SELECT 1 FROM campaign_views v JOIN campaigns c ON c.id = v.campaign_id
        WHERE c.uuid = '$campaignUuid' AND v.customer_id IS NOT NULL) THEN 'PASS' ELSE 'FAIL' END AS result;
$qViewEmpty;
SELECT '3b aggregate view recorded with a NULL customer id' AS check,
    CASE WHEN EXISTS (SELECT 1 FROM campaign_views v JOIN campaigns c ON c.id = v.campaign_id
        WHERE c.uuid = '$campaignUuid' AND v.customer_id IS NULL) THEN 'PASS' ELSE 'FAIL' END AS result;
CREATE TEMP TABLE view_before AS SELECT count(*) AS n FROM campaign_views;
$qViewStranger;
SELECT '3c non-recipient records no view' AS check,
    CASE WHEN (SELECT count(*) FROM campaign_views) = (SELECT n FROM view_before) THEN 'PASS' ELSE 'FAIL' END AS result;

\echo '=== 4. register-link-click (click tracking) ==='
INSERT INTO links (uuid, url) VALUES ('bbbbbbbb-2222-4222-8222-bbbbbbbbbbbb'::uuid, 'https://example.com/tracked');
INSERT INTO campaign_links (campaign_id, link_id)
SELECT c.id, l.id FROM campaigns c, links l
WHERE c.uuid = '$campaignUuid' AND l.uuid = 'bbbbbbbb-2222-4222-8222-bbbbbbbbbbbb'::uuid;
$qClick;
SELECT '4a recipient click recorded with the customer id' AS check,
    CASE WHEN EXISTS (SELECT 1 FROM link_clicks lc JOIN campaigns c ON c.id = lc.campaign_id
        WHERE c.uuid = '$campaignUuid' AND lc.customer_id IS NOT NULL) THEN 'PASS' ELSE 'FAIL' END AS result;
$qClickEmpty;
SELECT '4b aggregate click recorded with a NULL customer id' AS check,
    CASE WHEN EXISTS (SELECT 1 FROM link_clicks lc JOIN campaigns c ON c.id = lc.campaign_id
        WHERE c.uuid = '$campaignUuid' AND lc.customer_id IS NULL) THEN 'PASS' ELSE 'FAIL' END AS result;
CREATE TEMP TABLE click_before AS SELECT count(*) AS n FROM link_clicks;
$qClickStranger;
SELECT '4c non-recipient records no click' AS check,
    CASE WHEN (SELECT count(*) FROM link_clicks) = (SELECT n FROM click_before) THEN 'PASS' ELSE 'FAIL' END AS result;

\echo '=== 5. unsubscribe-by-campaign (bearer unsubscribe) ==='
$qUnsub;
SELECT '5a recipient unsubscribed from the campaign lists' AS check,
    CASE WHEN EXISTS (
        SELECT 1 FROM customer_list_memberships sl
        WHERE sl.customer_id = (SELECT id FROM customers WHERE uuid = '$recipientUuid')
          AND sl.status = 'unsubscribed') THEN 'PASS' ELSE 'FAIL' END AS result;
CREATE TEMP TABLE subs_before AS SELECT count(*) AS n FROM customer_list_memberships WHERE status = 'unsubscribed';
$qUnsubStranger;
SELECT '5b non-recipient changes nothing' AS check,
    CASE WHEN (SELECT count(*) FROM customer_list_memberships WHERE status = 'unsubscribed') = (SELECT n FROM subs_before)
        THEN 'PASS' ELSE 'FAIL' END AS result;
$qUnsubEmpty;
SELECT '5c empty customer reference changes nothing, no cast error' AS check,
    CASE WHEN (SELECT count(*) FROM customer_list_memberships WHERE status = 'unsubscribed') = (SELECT n FROM subs_before)
        THEN 'PASS' ELSE 'FAIL' END AS result;

ROLLBACK;
\echo '=== every check above must read PASS ==='
"@

$tmp = Join-Path $env:TEMP "public_recipient_resolution_verify.sql"
Set-Content -LiteralPath $tmp -Value $sql -Encoding UTF8

$out = Get-Content -LiteralPath $tmp -Raw | docker exec -i $DbContainer psql -U $DbUser -d $DbName -v ON_ERROR_STOP=1 2>&1
$out | Where-Object { $_ -match 'PASS|FAIL|ERROR|===' } | ForEach-Object { $_ }

# Match the result column and psql's own error prefix precisely: several check
# descriptions legitimately contain the word "error".
$failed = @($out | Where-Object { $_ -match '\|\s+FAIL\s*$' -or $_ -match '^ERROR:' -or $_ -match '^psql:' })
if ($failed.Count -gt 0) {
    Write-Host "FAILED checks:"
    $failed | ForEach-Object { Write-Host "  $_" }
    exit 1
}
Write-Host "ALL CHECKS PASS"
exit 0
