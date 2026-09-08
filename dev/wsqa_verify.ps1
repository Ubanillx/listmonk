$ErrorActionPreference = 'Continue'
$base = 'http://localhost:9173'

function Check($label, $cond) {
  if ($cond) { Write-Host "PASS  $label" } else { Write-Host "FAIL  $label" }
}

# ---------- B2: wsqa_noperm select-workspace: single org -> auto enter org 1 ----------
$jar = "$env:TEMP\wsqa.jar"
$html = curl.exe -s -b $jar "$base/admin/select-workspace"
$raw = [string]$html
Check 'B2 select page reachable' ($raw -match 'app-shell-card')
Check 'B2 auto-enter org 1 script' ($raw -match 'enterWorkspace\( 1 ')
Check 'B2 NOT auto-enter personal (org 0)' (-not ($raw -match 'enterWorkspace\( 0 '))
Check 'B2 no manual space options (single space)' (-not ($raw -match 'data-org='))
Check 'B2 no blocked error' (-not ($raw -match 'noWorkspace'))

# ---------- B3: API capability gate with org 0 ----------
# Personal workspace resolve (write-ish path) must be 403.
$code = curl.exe -s -o NUL -w '%{http_code}' -b $jar -H 'X-Listmonk-Organization-ID: 0' "$base/api/workspace"
Check 'B3 GET /api/workspace org=0 -> 403' ($code -eq '403')

# Migration list read exemption: GET /api/customer-lists with explicit org 0 must be 200 and list the retained personal list.
$lists = curl.exe -s -b $jar -H 'X-Listmonk-Organization-ID: 0' "$base/api/customer-lists?per_page=all&status=active"
Check 'B3 GET /api/customer-lists org=0 -> 200 (read exemption)' ($lists -match '"total"')
Check 'B3 retained personal list visible' ($lists -match 'wsqa-personal-list')

# Non-exempt read path: customer export with org 0 must stay 403.
$code = curl.exe -s -o NUL -w '%{http_code}' -b $jar -H 'X-Listmonk-Organization-ID: 0' "$base/api/customers/export"
Check 'B3 GET /api/customers/export org=0 -> 403' ($code -eq '403')

# Write to the personal workspace must be 403.
$code = curl.exe -s -o NUL -w '%{http_code}' -b $jar -H 'X-Listmonk-Organization-ID: 0' -H 'Content-Type: application/json' -X POST -d '{\"name\":\"wsqa-forbidden-list\",\"type\":\"private\",\"optin\":\"single\",\"tags\":[]}' "$base/api/customer-lists"
Check 'B3 POST /api/customer-lists org=0 -> 403 (write blocked)' ($code -eq '403')

# ---------- B4: organization workspace still works for the same user ----------
$code = curl.exe -s -o NUL -w '%{http_code}' -b $jar -H 'X-Listmonk-Organization-ID: 1' "$base/api/workspace"
Check 'B4 org 1 workspace OK' ($code -eq '200')

# The v3 browser compatibility window is closed. When a request carries both
# a current session and stale legacy BasicAuth, the explicit Authorization
# header is evaluated and must not be silently ignored in favor of the cookie.
$legacy = [Convert]::ToBase64String([Text.Encoding]::ASCII.GetBytes('legacy-v3-user:legacy-v3-password'))
$code = curl.exe -s -o NUL -w '%{http_code}' -b $jar -H "X-Listmonk-Organization-ID: 1" -H "Authorization: Basic $legacy" "$base/api/workspace"
Check 'B4 stale BasicAuth is rejected when session cookie is also present' ($code -eq '403')

# ---------- B5: migrate retained personal list into org 1 (move) ----------
# The migration source (personal workspace) is capability-exempt; the handler
# resolves the target from the ACTIVE workspace header first, exactly like the
# SPA sends it (a revoked account can never be in the personal workspace).
$idMatch = [regex]::Match($lists, '"id":\s*(\d+)[\s\S]*?"name":\s*"wsqa-personal-list"')
if (-not $idMatch.Success) {
  $idMatch = [regex]::Match($lists, '"id":(\d+)[\s\S]*?wsqa-personal-list')
}
$listID = if ($idMatch.Success) { $idMatch.Groups[1].Value } else { '0' }
Write-Host "migrating list id=$listID"
$body = "{`"customer_list_ids`":[$listID],`"mode`":`"move`",`"target_organization_id`":1}"
Set-Content -Path "$env:TEMP\wsqa_body.json" -Value $body -NoNewline -Encoding Ascii
$resp = curl.exe -s -b $jar -H 'Content-Type: application/json' -H 'X-Listmonk-Organization-ID: 1' -X POST --data-binary "@$env:TEMP\wsqa_body.json" "$base/api/organizations/resources/customer-lists/migrate"
Check 'B5 migrate move -> ok' ($resp -match '"data"')
$lists2 = curl.exe -s -b $jar -H 'X-Listmonk-Organization-ID: 0' "$base/api/customer-lists?per_page=all&status=active"
Check 'B5 personal list gone after move' (-not ($lists2 -match 'wsqa-personal-list'))
