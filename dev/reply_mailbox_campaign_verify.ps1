# Verifies the campaign reply-mailbox permission boundary in a running
# development stack: an organization member without any organization-management
# permission may SELECT a shared organization reply mailbox for a campaign, but
# may not MANAGE that mailbox (edit, disable, re-enable, connection test).
#
# Before the fix this failed twice over: GET /api/profile/reply-mailboxes
# answered {"data":[]} for such a member (the query filtered on user_id), and a
# submitted reply_mailbox_id of a mailbox owned by somebody else answered
# 403 "reply mailbox is not owned by this account".
#
# The script expects the standard workspace QA fixtures (dev/wsqa_fixtures.sql)
# and an active organization reply mailbox owned by another member of the same
# organization. It needs `docker exec psql` to resolve fixture ids and the
# backend of the dev docker suite (make dev-docker) to be running.
#
#   $env:LISTMONK_QA_BASE_URL = 'http://localhost:9173'   # optional
#   $env:REPLY_MAILBOX_QA_PASSWORD = 'Test@1234'          # optional
#   pwsh -File dev/reply_mailbox_campaign_verify.ps1
#
# Every assertion aborts the script on failure, so a clean run is the evidence.

$ErrorActionPreference = 'Stop'
$base = if ($env:LISTMONK_QA_BASE_URL) { $env:LISTMONK_QA_BASE_URL } else { 'http://localhost:9173' }
$password = if ($env:REPLY_MAILBOX_QA_PASSWORD) { $env:REPLY_MAILBOX_QA_PASSWORD } else { 'Test@1234' }
$memberUsername = if ($env:REPLY_MAILBOX_QA_MEMBER) { $env:REPLY_MAILBOX_QA_MEMBER } else { 'wsqa_noperm' }
$organizationID = if ($env:REPLY_MAILBOX_QA_ORGANIZATION_ID) { [int]$env:REPLY_MAILBOX_QA_ORGANIZATION_ID } else { 1 }
$dbContainer = if ($env:POOL_QA_DB_CONTAINER) { $env:POOL_QA_DB_CONTAINER } else { 'dev-db-1' }
$dbUser = if ($env:POOL_QA_DB_USER) { $env:POOL_QA_DB_USER } else { 'lmkdev_M7q2Xr8N' }
$dbName = if ($env:POOL_QA_DB_NAME) { $env:POOL_QA_DB_NAME } else { 'lmkdev_M7q2Xr8N' }

function Check($label, $condition) {
  if ($condition) { Write-Host "PASS  $label" } else { throw "FAIL  $label" }
}

function DbScalar($sql) {
  # An empty result set is not an error here: the script probes for optional
  # fixtures (a mailbox of another organization) with the same helper.
  $rows = @(& docker exec $dbContainer psql -X -A -t -v ON_ERROR_STOP=1 -U $dbUser -d $dbName -c $sql)
  if ($LASTEXITCODE -ne 0) { throw "database assertion failed" }
  if ($rows.Count -eq 0) { return '' }
  return ([string]$rows[0]).Trim()
}

function DbInt($sql) {
  $value = DbScalar $sql
  if (-not $value) { return 0 }
  return [int]$value
}

function LoginUser($username, $loginPassword) {
  $jar = Join-Path $env:TEMP ("reply-mailbox-campaign-{0}-{1}.jar" -f $username, $PID)
  Remove-Item $jar -ErrorAction SilentlyContinue
  curl.exe -s -c $jar "$base/admin/login" | Out-Null
  $html = [string](curl.exe -s -b $jar -c $jar "$base/admin/login")
  $nonce = [regex]::Match($html, 'name="nonce" value="([^"]+)"').Groups[1].Value
  Check "$username login nonce" ($nonce.Length -gt 0)
  curl.exe -s -o NUL -b $jar -c $jar -X POST -d "username=$username&password=$loginPassword&nonce=$nonce&next=/admin" "$base/admin/login" | Out-Null
  return $jar
}

function Api($method, $path, $jar, $workspaceID, $body) {
  $out = Join-Path $env:TEMP ("reply-mailbox-campaign-response-{0}.json" -f $PID)
  Remove-Item $out -ErrorAction SilentlyContinue
  $args = @('-s', '-o', $out, '-w', '%{http_code}', '-b', $jar,
    '-H', "X-Listmonk-Organization-ID: $workspaceID")
  if ($null -ne $body) {
    $bodyFile = Join-Path $env:TEMP ("reply-mailbox-campaign-body-{0}.json" -f $PID)
    Set-Content -Path $bodyFile -Value ($body | ConvertTo-Json -Compress) -NoNewline -Encoding Ascii
    $args += @('-H', 'Content-Type: application/json', '-X', $method, '--data-binary', "@$bodyFile")
  } else {
    $args += @('-X', $method)
  }
  $status = (& curl.exe @args "$base$path").Trim()
  $raw = if (Test-Path $out) { Get-Content -Raw -Path $out } else { '' }
  $json = $null
  if ($raw) { try { $json = $raw | ConvertFrom-Json } catch { } }
  return [pscustomobject]@{ Status = [int]$status; Raw = $raw; Json = $json }
}

# ---------------------------------------------------------------------------
# Fixtures. Ids are resolved by query, never assumed.
# ---------------------------------------------------------------------------
$memberID = DbInt "SELECT id FROM users WHERE username='$memberUsername'"
if ($memberID -lt 1) {
  throw "fixture user $memberUsername is missing; load dev/wsqa_fixtures.sql into the development database."
}

# The mailbox a plain member should be able to select and must not be able to
# manage: an active organization mailbox of the same organization that belongs
# to another member.
$sharedMailboxID = DbInt "SELECT m.id FROM reply_mailboxes m WHERE m.organization_id=$organizationID AND m.status='active' AND m.user_id <> $memberID ORDER BY m.is_default DESC, m.id LIMIT 1"
if ($sharedMailboxID -lt 1) {
  throw "organization $organizationID has no active reply mailbox owned by another member; seed one (for example dev/pools_e2e_seed.sql) before running this script."
}
$sharedMailboxEmail = DbScalar "SELECT email FROM reply_mailboxes WHERE id=$sharedMailboxID"
$sharedMailboxOwner = DbInt "SELECT user_id FROM reply_mailboxes WHERE id=$sharedMailboxID"

# A mailbox of a different organization, to prove the workspace boundary holds.
# The script seeds its own fixture, marked with a dedicated address, and removes
# every row carrying that address again at the end, so the boundary is exercised
# on every run and a leftover fixture from an aborted run is cleaned up too.
$foreignMarkerEmail = 'foreign-org-replies@example.test'
$foreignMailboxID = DbInt "SELECT id FROM reply_mailboxes WHERE email='$foreignMarkerEmail' ORDER BY id LIMIT 1"
if ($foreignMailboxID -lt 1) {
  $foreignOwnerID = DbInt "SELECT user_id FROM organization_members WHERE organization_id <> $organizationID AND removed_at IS NULL ORDER BY organization_id, user_id LIMIT 1"
  if ($foreignOwnerID -gt 0) {
    $foreignMailboxID = DbInt "INSERT INTO reply_mailboxes (user_id, organization_id, email, status, verified_at) SELECT $foreignOwnerID, om.organization_id, '$foreignMarkerEmail', 'active', NOW() FROM organization_members om WHERE om.user_id = $foreignOwnerID AND om.organization_id <> $organizationID AND om.removed_at IS NULL ORDER BY om.organization_id LIMIT 1 RETURNING id"
  }
}

$jar = LoginUser $memberUsername $password
$listing = Api 'GET' '/api/profile/reply-mailboxes' $jar $organizationID $null
if ($listing.Status -ne 200) { throw "$memberUsername session is not authenticated (HTTP $($listing.Status))." }

$rows = @($listing.Json.data)
$shared = $rows | Where-Object { $_.id -eq $sharedMailboxID }
Check 'shared organization mailbox is listed for a member without organization management permission' ($null -ne $shared)
Check 'listed shared mailbox exposes the organization company address' ($shared.email -eq $sharedMailboxEmail)
Check 'listed shared mailbox is active (selectable)' ($shared.status -eq 'active')
Check 'listed shared mailbox is not manageable for a non-owner' ($shared.manageable -eq $false)
Check 'listed shared mailbox still reports its real owner' ([int]$shared.user_id -eq $sharedMailboxOwner)
if ($foreignMailboxID -gt 0) {
  Check 'a mailbox of another organization is not listed' (-not ($rows | Where-Object { $_.id -eq $foreignMailboxID }))
}

# ---------------------------------------------------------------------------
# Selection: the member may attach the organization's shared mailbox.
# ---------------------------------------------------------------------------
# The probe creates its own customer list instead of reusing a fixture: a
# development database may only contain pooled lists and a retained personal
# list, neither of which an organization member can send to.
$probeListName = "reply-mailbox-permission-list-$PID"
$probeList = Api 'POST' '/api/customer-lists' $jar $organizationID ([pscustomobject]@{
  name = $probeListName; type = 'private'; optin = 'single'; tags = @()
})
Check 'member can create a probe customer list in the organization' ($probeList.Status -eq 200)
$probeListID = [int]$probeList.Json.data.id
Check 'probe customer list has an id' ($probeListID -gt 0)

$templates = @((Api 'GET' '/api/templates?per_page=all' $jar $organizationID $null).Json.data)
Check 'member can read at least one template' ($templates.Count -gt 0)

$campaignBody = [pscustomobject]@{
  name              = "reply-mailbox-permission-$PID"
  subject           = 'reply mailbox permission check'
  customer_list_ids = @($probeListID)
  type              = 'regular'
  content_type      = 'richtext'
  body              = '<p>reply mailbox permission check</p>'
  messenger         = 'email'
  from_email        = 'noreply@example.test'
  reply_mailbox_id  = $sharedMailboxID
  template_id       = [int]$templates[0].id
}

$created = Api 'POST' '/api/campaigns' $jar $organizationID $campaignBody
Check 'member can CREATE a campaign with the organization reply mailbox' ($created.Status -eq 200)
$campaignID = [int]$created.Json.data.id
if ($campaignID -lt 1) { throw 'campaign creation returned no id' }
Check 'created campaign keeps the shared mailbox' ([int]$created.Json.data.reply_mailbox_id -eq $sharedMailboxID)

# The create response is the raw inserted row; the email is rendered by the
# campaign read, which is what the editor shows after a save.
$fedBack = Api 'GET' "/api/campaigns/$campaignID" $jar $organizationID $null
Check 'selection survives a reload of the campaign' ($fedBack.Status -eq 200 -and [int]$fedBack.Json.data.reply_mailbox_id -eq $sharedMailboxID)
Check 'reloaded campaign renders the organization company address' ($fedBack.Json.data.reply_mailbox_email -eq $sharedMailboxEmail)

# The campaign editor sends the whole form on save, so re-submitting must work
# too (this is the request that answered 403 before the fix).
$updated = Api 'PUT' "/api/campaigns/$campaignID" $jar $organizationID $campaignBody
Check 'member can SAVE an existing campaign with the organization reply mailbox' ($updated.Status -eq 200)


if ($foreignMailboxID -gt 0) {
  $foreignBody = $campaignBody | Select-Object *
  $foreignBody.reply_mailbox_id = $foreignMailboxID
  $foreign = Api 'PUT' "/api/campaigns/$campaignID" $jar $organizationID $foreignBody
  Check 'a mailbox of another organization stays rejected' ($foreign.Status -eq 403)
}

# ---------------------------------------------------------------------------
# Management: selecting the shared mailbox grants nothing else.
# ---------------------------------------------------------------------------
$edit = Api 'PUT' "/api/profile/reply-mailboxes/$sharedMailboxID" $jar $organizationID ([pscustomobject]@{
  email = 'hijacked@example.test'; username = 'hijack'; imap_host = 'imap.example.test'; imap_port = 993; folder = 'INBOX'
})
Check 'member cannot edit a mailbox they do not own' ($edit.Status -eq 404)

$disable = Api 'DELETE' "/api/profile/reply-mailboxes/$sharedMailboxID" $jar $organizationID $null
Check 'member cannot disable a mailbox they do not own' ($disable.Status -eq 404)

$enable = Api 'PUT' "/api/profile/reply-mailboxes/$sharedMailboxID/enable" $jar $organizationID $null
Check 'member cannot re-enable a mailbox they do not own' ($enable.Status -eq 404)

$test = Api 'POST' '/api/profile/reply-mailboxes/test' $jar $organizationID ([pscustomobject]@{
  id = $sharedMailboxID; email = $sharedMailboxEmail; imap_host = 'imap.example.test'; imap_port = 993
})
Check 'member cannot connection-test a mailbox they do not own' ($test.Status -eq 404)

$stillActive = DbScalar "SELECT status FROM reply_mailboxes WHERE id=$sharedMailboxID"
Check 'the shared mailbox was not modified by the member' ($stillActive -eq 'active')

# ---------------------------------------------------------------------------
# Cleanup: the probe campaign and list are removed again; the mailbox is left
# untouched.
# ---------------------------------------------------------------------------
$deleted = Api 'DELETE' "/api/campaigns/$campaignID" $jar $organizationID $null
Check 'probe campaign removed' ($deleted.Status -eq 200)
Check 'probe campaign is gone' ((Api 'GET' "/api/campaigns/$campaignID" $jar $organizationID $null).Status -eq 404)

$listDeleted = Api 'DELETE' "/api/customer-lists/$probeListID" $jar $organizationID $null
Check 'probe customer list removed' ($listDeleted.Status -eq 200)

if ($foreignMailboxID -gt 0) {
  DbScalar "DELETE FROM reply_mailboxes WHERE email='$foreignMarkerEmail'" | Out-Null
  Check 'foreign-organization probe fixture removed' ((DbInt "SELECT id FROM reply_mailboxes WHERE email='$foreignMarkerEmail'") -eq 0)
}
Write-Host ''
Write-Host 'All campaign reply-mailbox permission checks passed.'
