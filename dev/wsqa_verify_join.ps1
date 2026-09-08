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

# ---------- F: multi-space user sees the persistent join help ----------
$jarM = "$env:TEMP\wsqa_m.jar"
LoginUser 'wsqa_multi' $jarM
$html = GetPageUtf8 "$base/admin/select-workspace" $jarM
Check 'F1 list page has join help' ($html -match 'join-form')
Check 'F2 join posts to /api/organizations/join' ($html -match '/api/organizations/join')
Check 'F3 join input has name code' ($html -match 'name="code"')

# ---------- G: blocked (no space at all) user sees help on the blocker ----------
$jarN = "$env:TEMP\wsqa_n.jar"
LoginUser 'wsqa_noorg' $jarN
$html = GetPageUtf8 "$base/admin/select-workspace" $jarN
Check 'G1 blocked page rendered (heading, no options, no auto CALL)' (($html -match '<h1>') -and (-not ($html -match 'data-org=')) -and (-not ($html -match 'enterWorkspace\( \d')))
Check 'G2 blocked page has join help' ($html -match 'join-form')

# ---------- H: joining with the invite code ----------
# Invalid code first -> 404 with message (body from file to dodge shell quoting).
Set-Content -Path "$env:TEMP\wsqa_bad.json" -Value '{"code":"WSQA-NOPE"}' -NoNewline -Encoding Ascii
$codeResp = curl.exe -s -o NUL -w '%{http_code}' -b $jarN -H 'Content-Type: application/json' -X POST --data-binary "@$env:TEMP\wsqa_bad.json" "$base/api/organizations/join"
Check 'H1 invalid code rejected (404)' ($codeResp -eq '404')

# Valid code -> 200 with the organization.
Set-Content -Path "$env:TEMP\wsqa_join.json" -Value '{"code":"WSQA-JOIN-TEST"}' -NoNewline -Encoding Ascii
$resp = curl.exe -s -b $jarN -H 'Content-Type: application/json' -X POST --data-binary "@$env:TEMP\wsqa_join.json" "$base/api/organizations/join"
Check 'H2 valid code joins org 1' ($resp -match '"id":1' -or $resp -match '"data"')

# After joining, the select page auto-enters the single space (org 1).
$html = GetPageUtf8 "$base/admin/select-workspace" $jarN
Check 'H3 single space now auto-enters org 1' ($html -match 'enterWorkspace\( 1 ')
Check 'H4 no blocker anymore' (-not ($html.Contains('没有可访问的工作空间')))
