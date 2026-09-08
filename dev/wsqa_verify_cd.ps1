$ErrorActionPreference = 'Continue'
$base = 'http://localhost:9173'

function Check($label, $cond) {
  if ($cond) { Write-Host "PASS  $label" } else { Write-Host "FAIL  $label" }
}

function GetPageUtf8($url, $jar) {
  $tmp = "$env:TEMP\wsqa_page.html"
  curl.exe -s -b $jar -o $tmp $url | Out-Null
  return [System.IO.File]::ReadAllText($tmp, [System.Text.Encoding]::UTF8)
}

function LoginUser($username, $jar) {
  curl.exe -s -c $jar "$base/admin/login" | Out-Null
  $html = [string](curl.exe -s -b $jar -c $jar "$base/admin/login")
  $nonce = [regex]::Match($html, 'name="nonce" value="([^"]+)"').Groups[1].Value
  curl.exe -s -o NUL -b $jar -c $jar -X POST -d "username=$username&password=Test@1234&nonce=$nonce&next=/admin" "$base/admin/login" | Out-Null
}

# ---------- C: wsqa_perm — has personal capability, no orgs ----------
$jarC = "$env:TEMP\wsqa_c.jar"
LoginUser 'wsqa_perm' $jarC
$html = GetPageUtf8 "$base/admin/select-workspace" $jarC
Check 'C select page rendered' ($html -match 'app-shell-card')
Check 'C auto-enter personal (org 0)' ($html -match 'enterWorkspace\( 0 ')
Check 'C entering state with spinner' ($html -match 'app-shell-spinner')
$code = curl.exe -s -o NUL -w '%{http_code}' -b $jarC -H 'X-Listmonk-Organization-ID: 0' "$base/api/workspace"
Check 'C GET /api/workspace org=0 -> 200' ($code -eq '200')

# ---------- D: wsqa_multi — no personal capability, two orgs ----------
$jarD = "$env:TEMP\wsqa_d.jar"
LoginUser 'wsqa_multi' $jarD
$html = GetPageUtf8 "$base/admin/select-workspace" $jarD
Check 'D select page rendered' ($html -match 'app-shell-card')
Check 'D two org buttons rendered' (($html -match 'data-org="1"') -and ($html -match 'data-org="2"'))
Check 'D org 2 name rendered' ($html.Contains('Smoke Org A 20260829'))
Check 'D no auto-enter CALL' (-not ($html -match 'enterWorkspace\( \d'))
Check 'D no personal-space option' (-not ($html -match 'data-org="0"'))
Check 'D personal space not advertised' (-not ($html -match '个人空间'))

# Organization workspaces are fully usable for D.
$code = curl.exe -s -o NUL -w '%{http_code}' -b $jarD -H 'X-Listmonk-Organization-ID: 1' "$base/api/customer-lists?per_page=all"
Check 'D org 1 lists readable' ($code -eq '200')
$code = curl.exe -s -o NUL -w '%{http_code}' -b $jarD "$base/api/workspace?organization_id=0"
Check 'D personal via query param -> 403' ($code -eq '403')

# ---------- E: 2FA-capable login chain still reaches selection ----------
# wsqa_noperm already proved the plain-password chain. Verify the login page
# itself carries the fullscreen shell and the form target.
$login = GetPageUtf8 "$base/admin/login" $jarD
Check 'E login page fullscreen shell' ($login -match 'app-shell')
Check 'E login form present' ($login -match 'action="/admin/login"')
