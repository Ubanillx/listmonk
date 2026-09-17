$ErrorActionPreference = 'Stop'
$base = if ($env:LISTMONK_QA_BASE_URL) { $env:LISTMONK_QA_BASE_URL } else { 'http://localhost:9173' }
$password = if ($env:POOL_QA_PASSWORD) { $env:POOL_QA_PASSWORD } else { 'Test@1234' }
$superPassword = if ($env:POOL_QA_SUPER_PASSWORD) { $env:POOL_QA_SUPER_PASSWORD } else { $password }
$dbContainer = if ($env:POOL_QA_DB_CONTAINER) { $env:POOL_QA_DB_CONTAINER } else { 'dev-db-1' }
$dbUser = if ($env:POOL_QA_DB_USER) { $env:POOL_QA_DB_USER } else { 'lmkdev_M7q2Xr8N' }
$dbName = if ($env:POOL_QA_DB_NAME) { $env:POOL_QA_DB_NAME } else { 'lmkdev_M7q2Xr8N' }

function Check($label, $condition) {
  if ($condition) { Write-Host "PASS  $label" } else { throw "FAIL  $label" }
}

function DbScalar($sql) {
  $value = (& docker exec $dbContainer psql -X -A -t -v ON_ERROR_STOP=1 -U $dbUser -d $dbName -c $sql).Trim()
  if ($LASTEXITCODE -ne 0) { throw "database assertion failed" }
  return $value
}

function LoginUser($username, $loginPassword = $password) {
  $jar = Join-Path $env:TEMP ("pool-e2e-{0}-{1}.jar" -f $username, $PID)
  Remove-Item $jar -ErrorAction SilentlyContinue
  curl.exe -s -c $jar "$base/admin/login" | Out-Null
  $html = [string](curl.exe -s -b $jar -c $jar "$base/admin/login")
  $nonce = [regex]::Match($html, 'name="nonce" value="([^"]+)"').Groups[1].Value
  Check "$username login nonce" ($nonce.Length -gt 0)
  curl.exe -s -o NUL -b $jar -c $jar -X POST -d "username=$username&password=$loginPassword&nonce=$nonce&next=/admin" "$base/admin/login" | Out-Null
  return $jar
}

function Api($method, $path, $jar, $organizationID, $body) {
  $out = Join-Path $env:TEMP ("pool-e2e-response-{0}.json" -f $PID)
  $args = @('-s', '-o', $out, '-w', '%{http_code}', '-b', $jar,
    '-H', "X-Listmonk-Organization-ID: $organizationID")
  if ($null -ne $body) {
    $bodyFile = Join-Path $env:TEMP ("pool-e2e-body-{0}.json" -f $PID)
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

# Fixture IDs are intentionally resolved by name. The fixture is idempotent,
# and development databases do not promise any particular sequence values.
$poolID = if ($env:POOL_QA_POOL_ID) { [int]$env:POOL_QA_POOL_ID } else { [int](DbScalar "SELECT id FROM customer_lists WHERE name='wsqa-pool-primary' AND type='pool'") }
$allocationID = if ($env:POOL_QA_SEGMENT_ID) { [int]$env:POOL_QA_SEGMENT_ID } else { [int](DbScalar "SELECT ps.id FROM org_pool_allocations ps JOIN customer_lists l ON l.id=ps.list_id WHERE l.name='wsqa-org-pool-allocation' AND ps.pool_id=$poolID") }
# Every pool audience of the organization replies through the organization's
# single unified reply mailbox; the allocation has no mailbox of its own.
$replyMailboxID = [int](DbScalar "SELECT reply_mailbox_id FROM organizations WHERE id=1")
$removableContactID = [int](DbScalar "SELECT id FROM pool_contacts WHERE email='beta-pool@example.test'")

# v6.45.0: public-pool contacts are governed by the configurable permissions
# pools:get / pools:manage / pools:export. The fixture manager role does not
# hold them by default (the migration only backfills the Super Admin role), so
# this script grants them for its own lifetime and restores the exact previous
# grant no matter how it exits.
function DbExec($sql) {
  & docker exec $dbContainer psql -X -v ON_ERROR_STOP=1 -U $dbUser -d $dbName -c $sql | Out-Null
  if ($LASTEXITCODE -ne 0) { throw "database execution failed" }
}

$script:managerRoleBackup = ''
function GrantManagerPoolPermissions {
  $script:managerRoleBackup = DbScalar "SELECT array_to_string(permissions,',') FROM roles WHERE id=4"
  DbExec "UPDATE roles SET permissions = ARRAY(SELECT DISTINCT p FROM unnest(permissions || ARRAY['pools:get','pools:manage','pools:export']) AS p) WHERE id=4"
}
function RestoreManagerRole {
  if (-not $script:managerRoleBackup) { return }
  $quoted = ($script:managerRoleBackup -split ',' | ForEach-Object { "'" + $_.Trim() + "'" }) -join ','
  DbExec "UPDATE roles SET permissions = ARRAY[$quoted]::TEXT[] WHERE id=4"
  $script:managerRoleBackup = ''
}

trap {
  RestoreManagerRole
  throw
}

GrantManagerPoolPermissions
$root = LoginUser 'root' $superPassword
$manager = LoginUser 'wsqa_pool_manager'
$otherOrg = LoginUser 'wsqa_multi'

# A failed login leaves a session-less cookie jar, which would otherwise surface
# as a confusing 403 in the assertions below. Probe both sessions up front.
$managerProbe = Api 'GET' "/api/pools/$poolID/allocations" $manager 1 $null
if ($managerProbe.Status -ne 200) { throw "wsqa_pool_manager session is not authenticated (HTTP $($managerProbe.Status)); load dev/pools_e2e_seed.sql into the development database." }
$rootProbe = Api 'GET' '/api/settings' $root 1 $null
if ($rootProbe.Status -ne 200) { throw "highest-administrator session is not authenticated (HTTP $($rootProbe.Status)); set POOL_QA_SUPER_PASSWORD to the password of the 'root' account." }

$contacts = Api 'GET' "/api/pools/$poolID/contacts" $root 1 $null
Check 'highest administrator can read pool contacts' ($contacts.Status -eq 200)
Check 'highest administrator sees source email' ($contacts.Raw.Contains('alpha-pool@example.test'))

$dup = Api 'GET' "/api/pools/$poolID/contacts?customer_code=DUP-001" $manager 1 $null
$dupRows = @($dup.Json.data.results)
Check 'duplicate imported customer code returns two records' ($dup.Status -eq 200 -and $dupRows.Count -eq 2)
Check 'pool contacts are server-paginated' ($dup.Json.data.page -eq 1 -and $dup.Json.data.per_page -ge 2 -and $dup.Json.data.total -eq 2)
Check 'ordinary manager receives safe contact DTO only' (-not ($dup.Raw.Contains('alpha-pool@example.test') -or $dup.Raw.Contains('beta-pool@example.test')))
Check 'ordinary manager receives masked email' (($dupRows | Where-Object { $_.email -match 'x+@' }).Count -eq 2)
Check 'ordinary manager sees the contact name' (($dupRows | Where-Object { -not $_.name }).Count -eq 0)
Check 'safe contact DTO no longer carries a company name' (-not $dup.Raw.Contains('company_name'))
$poolExport = Api 'GET' "/api/pools/$poolID/contacts/export" $manager 1 $null
Check 'pool contact export is allowed with pools:export' ($poolExport.Status -eq 200)
Check 'pool contact export masks customer email' (-not ($poolExport.Raw.Contains('alpha-pool@example.test') -or $poolExport.Raw.Contains('beta-pool@example.test')))
$export = Api 'GET' "/api/customers/export?customer_list_id=$poolID" $manager 1 $null
Check 'pool export cannot expose customer email' (-not ($export.Raw.Contains('alpha-pool@example.test') -or $export.Raw.Contains('beta-pool@example.test')))

$allocations = Api 'GET' "/api/pools/$poolID/allocations" $manager 1 $null
$allocationRows = @($allocations.Json.data)
Check 'organization can inspect its pool allocation' ($allocations.Status -eq 200 -and $allocationRows.Count -eq 1)
Check 'pool allocations no longer expose a per-allocation reply mailbox' (-not $allocations.Raw.Contains('reply_mailbox'))

$otherRead = Api 'GET' "/api/pools/$poolID/contacts" $otherOrg 2 $null
Check 'organization without pool grant receives no contacts' ($otherRead.Status -eq 200 -and @($otherRead.Json.data.results).Count -eq 0)
$otherWrite = Api 'POST' '/api/pools/allocations/members' $otherOrg 2 ([pscustomobject]@{ allocation_id = $allocationID; contact_id = $removableContactID })
Check 'cross-organization allocation write is denied' ($otherWrite.Status -eq 403)

# Contact maintenance is governed by the configurable pools:manage permission
# (see docs/ARCHITECTURE.md, "一级公海与组织公海分配") and additionally scoped
# to the caller's own organization. This script grants the fixture manager role
# that permission for its own lifetime, so the assertions below lock the
# own-organization path for a permission holder; the cross-organization denial
# above covers the boundary.
$managerAssign = Api 'POST' '/api/pools/allocations/members' $manager 1 ([pscustomobject]@{ allocation_id = $allocationID; contact_id = $removableContactID })
Check 'organization manager with pools:manage can assign pool contacts' ($managerAssign.Status -eq 200)
$managerRemove = Api 'DELETE' '/api/org-pool-allocations/members' $manager 1 ([pscustomobject]@{ allocation_id = $allocationID; contact_id = $removableContactID; reason = 'e2e' })
Check 'organization manager with pools:manage can remove pool contacts' ($managerRemove.Status -eq 200)
$managerRestore = Api 'PUT' '/api/pools/allocations/members' $manager 1 ([pscustomobject]@{ allocation_id = $allocationID; contact_id = $removableContactID })
Check 'organization manager with pools:manage can restore pool contacts' ($managerRestore.Status -eq 200)

# The highest administrator performs the logical remove and restore. The primary
# pool row remains present and is annotated for the source organization; restore
# clears only that annotation. The manager keeps read access to its own view.
$restore = Api 'PUT' '/api/pools/allocations/members' $root 1 ([pscustomobject]@{ allocation_id = $allocationID; contact_id = $removableContactID })
Check 'fixture starts with restored member' ($restore.Status -eq 200)
$remove = Api 'DELETE' '/api/pools/allocations/members' $root 1 ([pscustomobject]@{ allocation_id = $allocationID; contact_id = $removableContactID; reason = 'e2e' })
Check 'highest administrator can logically remove member' ($remove.Status -eq 200)
$marked = Api 'GET' "/api/pools/$poolID/contacts" $manager 1 $null
$markedRow = @($marked.Json.data.results | Where-Object { $_.id -eq $removableContactID })[0]
Check 'primary pool marks organization removal' ($markedRow.excluded -eq $true -and $markedRow.exclusion_reason -eq 'e2e')
$restore = Api 'PUT' '/api/pools/allocations/members' $root 1 ([pscustomobject]@{ allocation_id = $allocationID; contact_id = $removableContactID })
Check 'highest administrator can restore member' ($restore.Status -eq 200)
$invalidRemove = Api 'DELETE' '/api/pools/allocations/members' $root 1 ([pscustomobject]@{ allocation_id = $allocationID; contact_id = 999999; reason = 'e2e' })
Check 'unassigned contact cannot be logically removed' ($invalidRemove.Status -eq 400)

# A first-level audience can be selected by an organization with delivery
# permission. Preview succeeds because the unique pool allocation resolves
# server-side and the organization's unified reply mailbox supplies the reply
# route; details remain unavailable to the manager UI.
$campaignBody = [pscustomobject]@{
  name = "wsqa-pool-campaign-$PID"; subject = 'pool QA'; body = '<p>pool QA</p>'
  content_type = 'html'; customer_list_ids = @($poolID); type = 'regular'; messenger = 'email'
  daily_send_limit = 300; daily_resume_time = '09:00'; tags = @(); headers = @(); attribs = @{}
  visibility = 'organization'
}
$created = Api 'POST' '/api/campaigns' $manager 1 $campaignBody
$campaignID = if ($null -ne $created.Json.data.id) { [int]$created.Json.data.id } else { 0 }
Check 'organization can create first-level pool campaign' ($created.Status -eq 200 -and $campaignID -gt 0)
if ($campaignID -gt 0) {
  try {
    $attached = Api 'POST' "/api/campaigns/$campaignID/pools" $manager 1 ([pscustomobject]@{ pool_id = $poolID; org_pool_allocation_id = $allocationID; organization_id = 1 })
    Check 'same campaign can add explicit pool allocation audience' ($attached.Status -eq 200)
    $poolRecipientCount = [int](DbScalar "SELECT count(*) FROM campaign_pool_recipients WHERE campaign_id=$campaignID")
    Check 'first-level and explicit pool allocation expansion deduplicate by pool contact' ($poolRecipientCount -eq 3)
    $campaignView = Api 'GET' "/api/campaigns/$campaignID" $root 1 $null
    # The raw API uses snake_case; the browser client camel-cases this field.
    $campaignPoolsValue = $campaignView.Json.data.customer_pools
    if ($null -eq $campaignPoolsValue) { $campaignPoolsValue = $campaignView.Json.data.customerPools }
    $campaignPools = @($campaignPoolsValue)
    Check 'campaign metadata identifies effective internal reply mailbox' ($campaignView.Status -eq 200 -and $campaignView.Raw.Contains('pool-replies@example.test'))
    Check 'campaign metadata retains first-level and explicit pool allocation sources' ($campaignPools.Count -eq 2)
    $updated = Api 'PUT' "/api/campaigns/$campaignID" $manager 1 $campaignBody
    Check 'draft campaign can replace explicit pool allocation audience' ($updated.Status -eq 200)
    $updatedView = Api 'GET' "/api/campaigns/$campaignID" $root 1 $null
    $updatedPoolsValue = $updatedView.Json.data.customer_pools
    if ($null -eq $updatedPoolsValue) { $updatedPoolsValue = $updatedView.Json.data.customerPools }
    Check 'draft audience replacement removes stale pool allocation metadata' (@($updatedPoolsValue).Count -eq 1)
    $updatedPoolRecipientCount = [int](DbScalar "SELECT count(*) FROM campaign_pool_recipients WHERE campaign_id=$campaignID")
    Check 'draft audience replacement rebuilds deduplicated pool snapshot' ($updatedPoolRecipientCount -eq 3)
    $preview = Api 'GET' "/api/campaigns/$campaignID/preview" $manager 1 $null
    Check 'resolved first-level pool campaign previews successfully' ($preview.Status -eq 200 -and $preview.Raw.Contains('pool QA'))
  } finally {
    $deleted = Api 'DELETE' "/api/campaigns/$campaignID" $manager 1 $null
    Check 'E2E campaign fixture is cleaned up' ($deleted.Status -in @(200, 204))
  }
}

# Missing unified reply mailbox is a draft-time condition only. Preview must
# fail closed and never fall back to a first-level, personal or system default
# address. The same draft previews once the organization's manager sets the
# organization-level mailbox through the organization endpoint.
$mailboxCleared = Api 'PUT' '/api/organizations/1/reply-mailbox' $manager 1 ([pscustomobject]@{ reply_mailbox_id = $null })
$blockedID = 0
try {
  Check 'organization unified reply mailbox can be cleared for validation' ($mailboxCleared.Status -eq 200)
  $blocked = Api 'POST' '/api/campaigns' $manager 1 $campaignBody
  $blockedID = if ($null -ne $blocked.Json.data.id) { [int]$blocked.Json.data.id } else { 0 }
  Check 'first-level pool draft is allowed without mailbox' ($blocked.Status -eq 200 -and $blockedID -gt 0)
  if ($blockedID -gt 0) {
    try {
      $blockedPreview = Api 'GET' "/api/campaigns/$blockedID/preview" $manager 1 $null
      Check 'preview is blocked when the organization unified reply mailbox is missing' ($blockedPreview.Status -eq 400)
      Check 'blocked preview names the pool list, the allocation and the organization' ($blockedPreview.Raw.Contains('pool list') -and $blockedPreview.Raw.Contains('organization allocation') -and $blockedPreview.Raw.Contains('has not configured its unified reply mailbox'))
      Check 'blocked preview explains the fix steps' ($blockedPreview.Raw.Contains('saves a verified mailbox in Manage organizations -> Organization reply mailboxes') -and $blockedPreview.Raw.Contains('then retry preview or send'))
      Check 'blocked preview points at the organization reply mailbox setting' ($blockedPreview.Raw.Contains('Manage organizations -> Organization reply mailboxes'))
      $mailboxSet = Api 'PUT' '/api/organizations/1/reply-mailbox' $manager 1 ([pscustomobject]@{ reply_mailbox_id = $replyMailboxID })
      Check 'organization unified reply mailbox is set through the organization endpoint' ($mailboxSet.Status -eq 200)
      $resumedPreview = Api 'GET' "/api/campaigns/$blockedID/preview" $manager 1 $null
      Check 'the same draft previews after the organization unified reply mailbox is set' ($resumedPreview.Status -eq 200 -and $resumedPreview.Raw.Contains('pool QA'))
    } finally {
      $blockedDelete = Api 'DELETE' "/api/campaigns/$blockedID" $manager 1 $null
      Check 'blocked-mailbox campaign fixture is cleaned up' ($blockedDelete.Status -in @(200, 204))
    }
  }
} finally {
  $mailboxRestored = Api 'PUT' '/api/organizations/1/reply-mailbox' $manager 1 ([pscustomobject]@{ reply_mailbox_id = $replyMailboxID })
  Check 'organization unified reply mailbox is restored after validation' ($mailboxRestored.Status -eq 200)
}

RestoreManagerRole
Write-Host 'Public-pool E2E verification complete.'
