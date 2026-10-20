# Public-pool end-to-end verification.
#
# One entry point for the three public-pool flows that used to live in separate
# scripts. Fixtures are resolved by name (never by sequence value) and every
# mode cleans up after itself, including on failure.
#
#   -Mode Http    HTTP/API contract for the fixture pool: masked contact DTO,
#                 allocation scope, member maintenance, campaign preview, and
#                 the organization's unified reply mailbox. Uses
#                 dev/pools_e2e_seed.sql.
#   -Mode Smtp    Real MailHog delivery for the same fixture pool: one message
#                 per deduplicated contact, unified reply mailbox on Reply-To.
#                 Uses dev/pools_e2e_seed.sql and a temporary personal SMTP.
#   -Mode AllOrg  Platform-level (all_organizations) pool campaign. Seeds and
#                 removes its own two organizations, three member SMTP servers
#                 and four contacts; needs no fixture SQL.
#   -Mode All     Http, then Smtp, then AllOrg (default is Http).
#
# Usage:
#   pwsh -File dev/pools_e2e_verify.ps1
#   pwsh -File dev/pools_e2e_verify.ps1 -Mode Smtp
#   pwsh -File dev/pools_e2e_verify.ps1 -Mode All
#
# Prerequisites for Http/Smtp: the development stack is running and the fixture
# is loaded, e.g.
#   Get-Content dev/pools_e2e_seed.sql |
#     docker exec -i dev-db-1 psql -v ON_ERROR_STOP=1 -U <db-user> -d <db-name>
#
# Environment overrides: LISTMONK_QA_BASE_URL, POOL_QA_PASSWORD,
# POOL_QA_SUPER_PASSWORD, POOL_QA_DB_{CONTAINER,USER,NAME}, POOL_QA_POOL_ID,
# POOL_QA_SEGMENT_ID, POOL_QA_USER, POOL_QA_MAILHOG_URL.

param(
    [ValidateSet('Http', 'Smtp', 'AllOrg', 'All')]
    [string]$Mode = 'Http'
)

$ErrorActionPreference = 'Stop'

$base = if ($env:LISTMONK_QA_BASE_URL) { $env:LISTMONK_QA_BASE_URL } else { 'http://localhost:9173' }
$mailhog = if ($env:POOL_QA_MAILHOG_URL) { $env:POOL_QA_MAILHOG_URL } else { 'http://localhost:8265' }
$password = if ($env:POOL_QA_PASSWORD) { $env:POOL_QA_PASSWORD } else { 'possible1.' }
$superPassword = if ($env:POOL_QA_SUPER_PASSWORD) { $env:POOL_QA_SUPER_PASSWORD } else { $password }
$dbContainer = if ($env:POOL_QA_DB_CONTAINER) { $env:POOL_QA_DB_CONTAINER } else { 'dev-db-1' }
$dbUser = if ($env:POOL_QA_DB_USER) { $env:POOL_QA_DB_USER } else { 'lmkdev_M7q2Xr8N' }
$dbName = if ($env:POOL_QA_DB_NAME) { $env:POOL_QA_DB_NAME } else { 'lmkdev_M7q2Xr8N' }
$managerUser = if ($env:POOL_QA_USER) { $env:POOL_QA_USER } else { 'wsqa_pool_manager' }

function Check($label, $condition) {
    if ($condition) { Write-Host "PASS  $label" } else { throw "FAIL  $label" }
}

function DbScalar($sql) {
    $value = (& docker exec $dbContainer psql -X -A -t -v ON_ERROR_STOP=1 -U $dbUser -d $dbName -c $sql).Trim()
    if ($LASTEXITCODE -ne 0) { throw "database assertion failed" }
    return $value
}

function DbExec($sql) {
    & docker exec $dbContainer psql -X -v ON_ERROR_STOP=1 -U $dbUser -d $dbName -c $sql | Out-Null
    if ($LASTEXITCODE -ne 0) { throw "database execution failed" }
}

# Fixture lookups must fail loudly and early: a missing row otherwise surfaces
# as an unhelpful "cannot convert """ to Int32" from the [int] cast.
function DbInt($label, $sql) {
    $value = DbScalar $sql
    if (-not $value) {
        throw "fixture lookup '$label' returned no row; load dev/pools_e2e_seed.sql into the development database (query: $sql)"
    }
    return [int]$value
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
        Set-Content -Path $bodyFile -Value ($body | ConvertTo-Json -Compress -Depth 8) -NoNewline -Encoding Ascii
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

function GetMailhogMessages($subject) {
    # MailHog returns complete MIME payloads. Keep this bounded so historic
    # development-fixture attachments cannot make the verification timeout.
    $raw = & curl.exe -fsS --max-time 5 "$mailhog/api/v2/messages?limit=50"
    if ($LASTEXITCODE -ne 0) { throw 'MailHog message query failed' }
    $messages = $raw | ConvertFrom-Json
    return @($messages.items | Where-Object {
            $headers = $_.Content.Headers
            @($headers.Subject) -contains $subject
        })
}

# v6.45.0: public-pool contacts are governed by the configurable permissions
# pools:get / pools:manage / pools:export. The fixture manager role does not
# hold them by default (the migration only backfills the Super Admin role), so
# the fixture modes grant them for their own lifetime and restore the exact
# previous grant no matter how they exit.
$script:managerRoleBackup = ''

function GrantManagerPoolPermissions {
    $script:managerRoleBackup = DbScalar 'SELECT array_to_string(permissions,'','') FROM roles WHERE id=4'
    DbExec "UPDATE roles SET permissions = ARRAY(SELECT DISTINCT p FROM unnest(permissions || ARRAY['pools:get','pools:manage','pools:export']) AS p) WHERE id=4"
}

function RestoreManagerRole {
    if (-not $script:managerRoleBackup) { return }
    $quoted = ($script:managerRoleBackup -split ',' | ForEach-Object { "'" + $_.Trim() + "'" }) -join ','
    DbExec "UPDATE roles SET permissions = ARRAY[$quoted]::TEXT[] WHERE id=4"
    $script:managerRoleBackup = ''
}

function Invoke-PoolHttpChecks {
    $poolID = if ($env:POOL_QA_POOL_ID) { [int]$env:POOL_QA_POOL_ID } else { DbInt 'first-level pool list' "SELECT id FROM customer_lists WHERE name='wsqa-pool-primary' AND type='pool'" }
    $allocationID = if ($env:POOL_QA_SEGMENT_ID) { [int]$env:POOL_QA_SEGMENT_ID } else { DbInt 'pool allocation' "SELECT ps.id FROM org_pool_allocations ps JOIN customer_lists l ON l.id=ps.list_id WHERE l.name='wsqa-org-pool-allocation' AND ps.pool_id=$poolID" }
    # Every pool audience of the organization replies through the organization's
    # single unified reply mailbox; the allocation has no mailbox of its own.
    $replyMailboxID = DbInt 'organization unified reply mailbox' 'SELECT reply_mailbox_id FROM organizations WHERE id=1'
    $removableContactID = DbInt 'removable pool contact' "SELECT id FROM pool_contacts WHERE email='beta-pool@example.test'"

    GrantManagerPoolPermissions
    $root = LoginUser 'root' $superPassword
    $manager = LoginUser $managerUser
    $otherOrg = LoginUser 'wsqa_multi'

    # A failed login leaves a session-less cookie jar, which would otherwise
    # surface as a confusing 403 in the assertions below. Probe both sessions
    # up front.
    $managerProbe = Api 'GET' "/api/pools/$poolID/allocations" $manager 1 $null
    if ($managerProbe.Status -ne 200) { throw "$managerUser session is not authenticated (HTTP $($managerProbe.Status)); load dev/pools_e2e_seed.sql into the development database." }
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
    # models.MaskPoolEmail keeps the first three local-part characters and stars
    # the rest, e.g. alpha-pool@example.test -> alp***@example.test.
    Check 'ordinary manager receives masked email' (($dupRows | Where-Object { $_.email -match '^[^*]{0,3}\*+@example\.test$' }).Count -eq 2)
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
    # v6.43.0 removed the mailbox from the allocation itself; the effective
    # address is the organization's unified mailbox. The listing still reports
    # it (read-side resolution for the management UI), so the invariant is
    # asserted on both sides: the column must be gone, and the reported mailbox
    # must be the organization's.
    Check 'org_pool_allocations no longer owns a reply mailbox column' ([int](DbScalar "SELECT count(*) FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='org_pool_allocations' AND column_name='reply_mailbox_id'") -eq 0)
    Check 'allocation listing resolves the organization unified reply mailbox' ($allocations.Raw.Contains("`"reply_mailbox_id`":$replyMailboxID") -and $allocations.Raw.Contains('pool-replies@example.test'))

    $otherRead = Api 'GET' "/api/pools/$poolID/contacts" $otherOrg 2 $null
    # v6.45.0 turned the pool read into a permission plus a per-organization
    # delivery grant (`requirePoolPermission` + the allocation scope), so a
    # workspace without the grant is denied outright instead of receiving an
    # empty list.
    Check 'organization without pool grant is denied the contact listing' ($otherRead.Status -eq 403)
    Check 'denied listing leaks no contact data' (-not ($otherRead.Raw.Contains('alpha-pool@example.test') -or $otherRead.Raw.Contains('DUP-001')))
    $otherWrite = Api 'POST' '/api/pools/allocations/members' $otherOrg 2 ([pscustomobject]@{ allocation_id = $allocationID; contact_id = $removableContactID })
    Check 'cross-organization allocation write is denied' ($otherWrite.Status -eq 403)

    # Contact maintenance is governed by the configurable pools:manage
    # permission (see docs/ARCHITECTURE.md, "一级公海与组织公海分配") and
    # additionally scoped to the caller's own organization. For a non-highest
    # caller ChangeAllocationMembers also requires the contact's
    # allocation_department to equal the organization name, so the probe contact
    # gets that department for this run and is restored afterwards. The previous
    # value is stashed inside PostgreSQL rather than round-tripped through the
    # shell, so a non-ASCII organization name cannot be mangled by encoding.
    DbExec "DROP TABLE IF EXISTS pool_qa_department_backup;
        CREATE TABLE pool_qa_department_backup AS
        SELECT id, allocation_department FROM pool_contacts WHERE id=$removableContactID;
        UPDATE pool_contacts SET allocation_department=(SELECT name FROM organizations WHERE id=1) WHERE id=$removableContactID"
    try {
        $managerAssign = Api 'POST' '/api/pools/allocations/members' $manager 1 ([pscustomobject]@{ allocation_id = $allocationID; contact_id = $removableContactID })
        Check 'organization manager with pools:manage can assign pool contacts' ($managerAssign.Status -eq 200)
        $managerRemove = Api 'DELETE' '/api/org-pool-allocations/members' $manager 1 ([pscustomobject]@{ allocation_id = $allocationID; contact_id = $removableContactID; reason = 'e2e' })
        Check 'organization manager with pools:manage can remove pool contacts' ($managerRemove.Status -eq 200)
        $managerRestore = Api 'PUT' '/api/pools/allocations/members' $manager 1 ([pscustomobject]@{ allocation_id = $allocationID; contact_id = $removableContactID })
        Check 'organization manager with pools:manage can restore pool contacts' ($managerRestore.Status -eq 200)
    } finally {
        DbExec "UPDATE pool_contacts c SET allocation_department=b.allocation_department
            FROM pool_qa_department_backup b WHERE c.id=b.id;
            DROP TABLE IF EXISTS pool_qa_department_backup"
    }

    # The highest administrator performs the logical remove and restore. The
    # primary pool row remains present and is annotated for the source
    # organization; restore clears only that annotation. The manager keeps read
    # access to its own view.
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
    # server-side and the organization's unified reply mailbox supplies the
    # reply route; details remain unavailable to the manager UI.
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
    # fail closed and never fall back to a first-level, personal or system
    # default address. The same draft previews once the organization's manager
    # sets the organization-level mailbox through the organization endpoint.
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
                # Assertions run against the decoded message: the raw JSON body
                # escapes ">" as \u003e, which would make an arrow-containing
                # substring miss.
                $blockedMessage = [string]$blockedPreview.Json.message
                Check 'preview is blocked when the organization unified reply mailbox is missing' ($blockedPreview.Status -eq 400)
                Check 'blocked preview names the pool list, the allocation and the organization' ($blockedMessage.Contains('pool list') -and $blockedMessage.Contains('organization allocation') -and $blockedMessage.Contains('has not configured its unified reply mailbox'))
                Check 'blocked preview explains the fix steps' ($blockedMessage.Contains('Fix:') -and $blockedMessage.Contains('saves an available reply address in Manage organizations') -and $blockedMessage.Contains('then retry preview or send'))
                Check 'blocked preview points at the organization reply mailbox setting' ($blockedMessage.Contains('Manage organizations -> Organization reply mailboxes'))
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

    Write-Host 'Public-pool HTTP verification complete.'
}

function Invoke-PoolSmtpChecks {
    $poolID = if ($env:POOL_QA_POOL_ID) { [int]$env:POOL_QA_POOL_ID } else { DbInt 'first-level pool list' "SELECT id FROM customer_lists WHERE name='wsqa-pool-primary' AND type='pool'" }
    $allocationID = if ($env:POOL_QA_SEGMENT_ID) { [int]$env:POOL_QA_SEGMENT_ID } else { DbInt 'pool allocation' "SELECT ps.id FROM org_pool_allocations ps JOIN customer_lists l ON l.id=ps.list_id WHERE l.name='wsqa-org-pool-allocation' AND ps.pool_id=$poolID" }

    $manager = LoginUser $managerUser
    $managerID = [int](DbScalar "SELECT id FROM users WHERE username='$managerUser'")
    $smtpName = "pool-e2e-mailhog-$PID"
    $campaignID = 0
    $smtpID = 0

    # This verifier owns no pre-existing SMTP rows for the fixture user. Do not
    # replace a human/operator's profile configuration by accident.
    Check 'fixture manager has no pre-existing personal SMTP configuration' ([int](DbScalar "SELECT count(*) FROM user_smtp_servers WHERE user_id=$managerID") -eq 0)

    try {
        $smtpBody = [pscustomobject]@{
            smtp = @([pscustomobject]@{
                    name = $smtpName; enabled = $true; from_email = 'Pool QA Sender <pool-sender@example.test>'
                    daily_limit = 0; host = 'mailhog'; hello_hostname = ''; port = 1025; auth_protocol = 'none'
                    username = ''; password = ''; email_headers = @(); max_conns = 3; max_msg_retries = 2
                    idle_timeout = '15s'; wait_timeout = '5s'; tls_type = 'none'; tls_skip_verify = $false
                })
        }
        $smtp = Api 'PUT' '/api/profile/smtp' $manager 1 $smtpBody
        Check 'fixture manager personal MailHog SMTP is configured' ($smtp.Status -eq 200)
        $smtpID = [int](DbScalar "SELECT id FROM user_smtp_servers WHERE user_id=$managerID AND name='$smtpName'")
        Check 'fixture manager SMTP row is persisted' ($smtpID -gt 0)

        $subject = "pool SMTP E2E $PID"
        $campaignBody = [pscustomobject]@{
            name = "wsqa-pool-smtp-campaign-$PID"; subject = $subject; body = '<p>pool SMTP E2E</p>'
            content_type = 'html'; customer_list_ids = @($poolID); type = 'regular'; messenger = 'email'
            daily_send_limit = 300; daily_resume_time = '09:00'; tags = @(); headers = @(); attribs = @{}
            visibility = 'organization'
        }
        $created = Api 'POST' '/api/campaigns' $manager 1 $campaignBody
        $campaignID = if ($null -ne $created.Json.data.id) { [int]$created.Json.data.id } else { 0 }
        Check 'organization can create a first-level pool delivery campaign' ($created.Status -eq 200 -and $campaignID -gt 0)

        $started = Api 'PUT' "/api/campaigns/$campaignID/status" $manager 1 ([pscustomobject]@{ status = 'running' })
        Check 'pool campaign starts with personal SMTP' ($started.Status -eq 200)

        $deadline = (Get-Date).AddSeconds(45)
        $state = ''
        do {
            $state = DbScalar "SELECT status::text || '|' || to_send::text || '|' || sent::text FROM campaigns WHERE id=$campaignID"
            if ($state -eq 'finished|3|3') { break }
            Start-Sleep -Seconds 1
        } while ((Get-Date) -lt $deadline)
        Check 'campaign sends the three deduplicated pool contacts' ($state -eq 'finished|3|3')

        $recipientStats = DbScalar "SELECT count(*)::text || '|' || count(DISTINCT pool_contact_id)::text || '|' || count(*) FILTER (WHERE status='sent')::text || '|' || count(*) FILTER (WHERE reply_mailbox_id=(SELECT o.reply_mailbox_id FROM org_pool_allocations a JOIN organizations o ON o.id=a.organization_id WHERE a.id=$allocationID))::text FROM campaign_pool_recipients WHERE campaign_id=$campaignID"
        Check 'send snapshot has one sent row per pool contact with the organization unified reply mailbox' ($recipientStats -eq '3|3|3|3')

        $deadline = (Get-Date).AddSeconds(20)
        $messages = @()
        do {
            $messages = GetMailhogMessages $subject
            if ($messages.Count -eq 3) { break }
            Start-Sleep -Seconds 1
        } while ((Get-Date) -lt $deadline)
        Check 'MailHog received one message per deduplicated pool contact' ($messages.Count -eq 3)
        $recipients = @($messages | ForEach-Object { $_.To | ForEach-Object { "$($_.Mailbox)@$($_.Domain)" } })
        Check 'MailHog recipient addresses are complete and unique' (($recipients | Sort-Object -Unique).Count -eq 3 -and @(@('alpha-pool@example.test', 'beta-pool@example.test', 'unique-pool@example.test') | Where-Object { $_ -notin $recipients }).Count -eq 0)
        $replyTos = @($messages | ForEach-Object { @($_.Content.Headers.'Reply-To')[0] })
        Check 'every first-level pool message uses the pool-allocation internal reply mailbox' (($replyTos | Where-Object { $_ -ne 'pool-replies@example.test' }).Count -eq 0)
    } finally {
        if ($campaignID -gt 0) {
            $deleted = Api 'DELETE' "/api/campaigns/$campaignID" $manager 1 $null
            Check 'SMTP E2E campaign fixture is cleaned up' ($deleted.Status -in @(200, 204))
        }
        if ($smtpID -gt 0) {
            $deletedSMTP = Api 'DELETE' "/api/profile/smtp/$smtpID" $manager 1 $null
            Check 'fixture manager MailHog SMTP is cleaned up' ($deletedSMTP.Status -in @(200, 204))
        }
    }

    Check 'fixture manager personal SMTP configuration is restored' ([int](DbScalar "SELECT count(*) FROM user_smtp_servers WHERE user_id=$managerID") -eq 0)
    Write-Host 'Public-pool SMTP verification complete.'
}

# Platform-level public-pool campaigns: two organizations, three member SMTP
# accounts, two contacts allocated to both organizations, and one
# all_organizations campaign. Asserts round-robin sender assignment,
# cross-organization deduplication, delivered status, per-organization Reply-To
# and the durable organization cursor. Seeds everything itself; needs no fixture
# SQL and no HTTP session.
function Invoke-AllOrgSmtpChecks {
    $stamp = [DateTimeOffset]::UtcNow.ToUnixTimeSeconds()

    function Sql([string]$q) {
        $out = & docker exec $dbContainer psql -X -t -A -q -v ON_ERROR_STOP=1 -F'|' -U $dbUser -d $dbName -c $q
        if ($LASTEXITCODE -ne 0) { throw "database query failed: $q" }
        return @($out | Where-Object { $_ -ne '' -and $_ -notmatch '^(INSERT|UPDATE|DELETE|SELECT) \d' })
    }
    function SqlOne([string]$q) { (Sql $q | Select-Object -First 1) }
    function SqlRun([string]$q) { Sql $q | Out-Null }

    $orgA = 0; $orgB = 0
    try {
        # Remove leftovers from any earlier failed run.
        $oldCampaigns = Sql "SELECT id FROM campaigns WHERE name LIKE 'e2e-allorg-campaign-%'"
        foreach ($c in $oldCampaigns) { SqlRun "DELETE FROM campaigns WHERE id=$c" }
        $oldPools = Sql "SELECT id FROM customer_lists WHERE name LIKE 'e2e-allorg-pool-%'"
        foreach ($p in $oldPools) { SqlRun "DELETE FROM customer_lists WHERE id=$p" }
        $oldContacts = Sql "SELECT id FROM pool_contacts WHERE customer_code LIKE 'E2E-%'"
        foreach ($c in $oldContacts) { SqlRun "DELETE FROM pool_contacts WHERE id=$c" }
        $oldUsers = Sql "SELECT id FROM users WHERE username LIKE 'e2e_allorg_%'"
        foreach ($u in $oldUsers) { SqlRun "DELETE FROM users WHERE id=$u" }
        $oldOrgs = Sql "SELECT id FROM organizations WHERE name LIKE 'e2e-allorg-%'"
        foreach ($o in $oldOrgs) { SqlRun "DELETE FROM organizations WHERE id=$o" }

        $orgA = SqlOne "INSERT INTO organizations(name,created_by_user_id) VALUES('e2e-allorg-a-$stamp',1) RETURNING id"
        $orgB = SqlOne "INSERT INTO organizations(name,created_by_user_id) VALUES('e2e-allorg-b-$stamp',1) RETURNING id"
        # Role ids are install-specific (a fresh install only seeds Super Admin
        # and "user"), so the plain user role is resolved by name.
        $userRole = SqlOne "SELECT id FROM roles WHERE type='user' AND name='user' ORDER BY id LIMIT 1"
        if (-not $userRole) { throw "no plain 'user' role found; cannot seed the all-organization fixture users" }
        $userA = SqlOne "INSERT INTO users(username,email,name,type,user_role_id,status) VALUES('e2e_allorg_a_$stamp','e2e_allorg_a_$stamp@example.test','E2E A','user',$userRole,'enabled') RETURNING id"
        $userB = SqlOne "INSERT INTO users(username,email,name,type,user_role_id,status) VALUES('e2e_allorg_b_$stamp','e2e_allorg_b_$stamp@example.test','E2E B','user',$userRole,'enabled') RETURNING id"
        $userC = SqlOne "INSERT INTO users(username,email,name,type,user_role_id,status) VALUES('e2e_allorg_c_$stamp','e2e_allorg_c_$stamp@example.test','E2E C','user',$userRole,'enabled') RETURNING id"

        SqlRun "INSERT INTO organization_members(organization_id,user_id,role) VALUES($orgA,$userA,'manager'),($orgA,$userC,'member'),($orgB,$userB,'manager')"

        $mbA = SqlOne "INSERT INTO reply_mailboxes(user_id,organization_id,email,status,verified_at) VALUES($userA,$orgA,'e2e-allorg-a-$stamp@example.test','active',NOW()) RETURNING id"
        $mbB = SqlOne "INSERT INTO reply_mailboxes(user_id,organization_id,email,status,verified_at) VALUES($userB,$orgB,'e2e-allorg-b-$stamp@example.test','active',NOW()) RETURNING id"
        SqlRun "UPDATE organizations SET reply_mailbox_id=$mbA WHERE id=$orgA"
        SqlRun "UPDATE organizations SET reply_mailbox_id=$mbB WHERE id=$orgB"

        # Three member SMTP accounts: two in organization A, one in organization B.
        $smtpA1 = SqlOne "INSERT INTO user_smtp_servers(uuid,user_id,name,enabled,from_email,daily_limit,host,port,auth_protocol,username,password,tls_type,max_conns,max_msg_retries,idle_timeout,wait_timeout) VALUES(gen_random_uuid(),$userA,'mailhog-a1',TRUE,'e2e-a1-$stamp@example.test',0,'mailhog',1025,'none','','','none',4,1,'15s','5s') RETURNING uuid"
        $smtpA2 = SqlOne "INSERT INTO user_smtp_servers(uuid,user_id,name,enabled,from_email,daily_limit,host,port,auth_protocol,username,password,tls_type,max_conns,max_msg_retries,idle_timeout,wait_timeout) VALUES(gen_random_uuid(),$userC,'mailhog-a2',TRUE,'e2e-a2-$stamp@example.test',0,'mailhog',1025,'none','','','none',4,1,'15s','5s') RETURNING uuid"
        $smtpB1 = SqlOne "INSERT INTO user_smtp_servers(uuid,user_id,name,enabled,from_email,daily_limit,host,port,auth_protocol,username,password,tls_type,max_conns,max_msg_retries,idle_timeout,wait_timeout) VALUES(gen_random_uuid(),$userB,'mailhog-b1',TRUE,'e2e-b1-$stamp@example.test',0,'mailhog',1025,'none','','','none',4,1,'15s','5s') RETURNING uuid"

        $pool = SqlOne "INSERT INTO customer_lists(uuid,name,type,status,visibility) VALUES(gen_random_uuid(),'e2e-allorg-pool-$stamp','pool','active','global') RETURNING id"
        $listA = SqlOne "INSERT INTO customer_lists(uuid,name,type,status,visibility) VALUES(gen_random_uuid(),'e2e-allorg-alloc-a-$stamp','org_pool_allocation','active','private') RETURNING id"
        $listB = SqlOne "INSERT INTO customer_lists(uuid,name,type,status,visibility) VALUES(gen_random_uuid(),'e2e-allorg-alloc-b-$stamp','org_pool_allocation','active','private') RETURNING id"
        $allocA = SqlOne "INSERT INTO org_pool_allocations(list_id,pool_id,organization_id,created_by_user_id) VALUES($listA,$pool,$orgA,1) RETURNING id"
        $allocB = SqlOne "INSERT INTO org_pool_allocations(list_id,pool_id,organization_id,created_by_user_id) VALUES($listB,$pool,$orgB,1) RETURNING id"

        # Four contacts: 1-2 belong to BOTH organizations (must still be sent
        # once), 3 belongs to organization A only, 4 belongs to organization B
        # only. The two single-organization contacts prove per-organization SMTP
        # pool routing.
        foreach ($i in 1..4) {
            $cid = SqlOne "INSERT INTO pool_contacts(uuid,customer_code,email,name,status) VALUES(gen_random_uuid(),'E2E-$stamp-$i','e2e-allorg-$stamp-$i@example.test','Contact $i','active') RETURNING id"
            SqlRun "INSERT INTO pool_members(pool_id,contact_id) VALUES($pool,$cid)"
            if ($i -le 3) {
                SqlRun "INSERT INTO org_pool_allocation_members(allocation_id,contact_id,status) VALUES($allocA,$cid,'active')"
            }
            if ($i -le 2 -or $i -eq 4) {
                SqlRun "INSERT INTO org_pool_allocation_members(allocation_id,contact_id,status) VALUES($allocB,$cid,'active')"
            }
        }

        $tpl = SqlOne 'SELECT id FROM templates WHERE is_default = TRUE LIMIT 1'
        $camp = SqlOne "INSERT INTO campaigns(uuid,name,subject,from_email,body,content_type,type,messenger,status,daily_send_limit,daily_resume_time,owner_user_id,organization_id,pool_scope,template_id) VALUES(gen_random_uuid(),'e2e-allorg-campaign-$stamp','E2E subject','placeholder@example.test','<p>E2E platform pool body</p>','richtext','regular','email','running',50,'09:00',$userA,NULL,'all_organizations',$tpl) RETURNING id"
        SqlRun "INSERT INTO campaign_customer_lists(campaign_id,customer_list_id,customer_list_name,pool_id,org_pool_allocation_id,source_organization_id,resolved_reply_mailbox_id) VALUES($camp,NULL,'e2e-allorg-pool-$stamp',$pool,NULL,NULL,NULL)"

        Write-Host "seeded campaign=$camp pool=$pool orgs=$orgA/$orgB smtp=$smtpA1/$smtpA2/$smtpB1"

        # Wait for the scheduler (5s scan) to claim and drain the campaign.
        $deadline = (Get-Date).AddSeconds(90)
        $status = ''
        do {
            Start-Sleep -Seconds 5
            $status = SqlOne "SELECT status FROM campaigns WHERE id=$camp"
        } while ($status -ne 'finished' -and (Get-Date) -lt $deadline)

        $rows = (Sql "SELECT 'contact '||pool_contact_id||' status='||status||' sender='||COALESCE(sender_smtp_uuid::text,'-')||' org='||organization_id||' mailbox='||COALESCE(reply_mailbox_id,0) FROM campaign_pool_recipients WHERE campaign_id=$camp ORDER BY pool_contact_id") -join ' | '
        $cursors = (Sql "SELECT 'org '||organization_id||' cursor='||COALESCE(next_smtp_uuid::text,'-') FROM org_pool_smtp_cursors WHERE organization_id IN ($orgA,$orgB) ORDER BY organization_id") -join ' | '
        $sent = SqlOne "SELECT COUNT(*) FROM campaign_pool_recipients WHERE campaign_id=$camp AND status='sent'"
        $total = SqlOne "SELECT COUNT(*) FROM campaign_pool_recipients WHERE campaign_id=$camp"
        $distinctSenders = SqlOne "SELECT COUNT(DISTINCT sender_smtp_uuid) FROM campaign_pool_recipients WHERE campaign_id=$camp"
        $distinctOrgs = SqlOne "SELECT COUNT(DISTINCT organization_id) FROM campaign_pool_recipients WHERE campaign_id=$camp"
        # Read-only invariant: each recipient must reply through its own
        # organization's unified mailbox. The sender rotation depends on the
        # organization order the scheduler persisted, so it is reported rather
        # than asserted.
        $wrongMailbox = SqlOne "SELECT COUNT(*) FROM campaign_pool_recipients r JOIN organizations o ON o.id=r.organization_id WHERE r.campaign_id=$camp AND r.reply_mailbox_id IS DISTINCT FROM o.reply_mailbox_id"

        Write-Host "campaign status: $status"
        Write-Host "recipients ($total): $rows"
        Write-Host "cursors: $cursors"
        Write-Host "sent=$sent distinctSenders=$distinctSenders distinctTargetOrgs=$distinctOrgs wrongReplyMailbox=$wrongMailbox"

        Check 'platform-level pool campaign finishes' ($status -eq 'finished')
        Check 'four unique contacts are deduplicated across two organizations' ($total -eq '4')
        Check 'every unique contact is delivered once' ($sent -eq '4')
        Check 'both organizations receive traffic' ($distinctOrgs -eq '2')
        Check 'senders rotate across the member SMTP accounts' ([int]$distinctSenders -ge 2)
        Check 'every recipient uses its own organization reply mailbox' ($wrongMailbox -eq '0')

        $mh = Invoke-RestMethod -Uri "$mailhog/api/v2/search?kind=to&query=e2e-allorg-" -TimeoutSec 30
        $msgs = @($mh.items) | Where-Object { $_.Content.Headers.To[0] -like "*e2e-allorg-$stamp-*" }
        Write-Host "mailhog messages for this run: $($msgs.Count)"
        foreach ($m in $msgs) {
            Write-Host ("  to={0} from={1} reply-to={2}" -f $m.Content.Headers.To[0], $m.Content.Headers.From[0], $m.Content.Headers.'Reply-To'[0])
        }
        Check 'MailHog received one message per unique contact' (@($msgs).Count -eq 4)
    } finally {
        # Cleanup is best-effort and must not hide the original assertion error.
        try {
            SqlRun "DELETE FROM campaigns WHERE name LIKE 'e2e-allorg-campaign-%'"
            SqlRun "DELETE FROM org_pool_smtp_cursors WHERE organization_id IN ($orgA,$orgB)"
            SqlRun "DELETE FROM org_pool_allocations WHERE pool_id IN (SELECT id FROM customer_lists WHERE name LIKE 'e2e-allorg-pool-%')"
            SqlRun "DELETE FROM customer_lists WHERE name LIKE 'e2e-allorg-alloc-%'"
            SqlRun "DELETE FROM customer_lists WHERE name LIKE 'e2e-allorg-pool-%'"
            SqlRun "DELETE FROM pool_members WHERE contact_id IN (SELECT id FROM pool_contacts WHERE customer_code LIKE 'E2E-%')"
            SqlRun "DELETE FROM pool_contacts WHERE customer_code LIKE 'E2E-%'"
            SqlRun "DELETE FROM user_smtp_servers WHERE user_id IN (SELECT id FROM users WHERE username LIKE 'e2e_allorg_%')"
            SqlRun "UPDATE organizations SET reply_mailbox_id=NULL WHERE name LIKE 'e2e-allorg-%'"
            SqlRun "DELETE FROM reply_mailboxes WHERE email LIKE 'e2e-allorg-%'"
            SqlRun "DELETE FROM organization_members WHERE organization_id IN (SELECT id FROM organizations WHERE name LIKE 'e2e-allorg-%')"
            SqlRun "DELETE FROM users WHERE username LIKE 'e2e_allorg_%'"
            SqlRun "DELETE FROM organizations WHERE name LIKE 'e2e-allorg-%'"
            Write-Host 'cleanup done'
        } catch {
            Write-Warning "all-org fixture cleanup failed: $($_.Exception.Message)"
        }
    }

    Write-Host 'Platform-level pool SMTP verification complete.'
}

    $modes = if ($Mode -eq 'All') { @('Http', 'Smtp', 'AllOrg') } else { @($Mode) }

    trap {
        # Traps cover the whole script scope, including statements that run
        # before the helpers above are defined. Capture the original failure
        # first, restore the manager role best-effort, and rethrow the captured
        # record so the real cause (for example a missing session) is what the
        # operator sees.
        $failure = $_
        try {
            if (Get-Command RestoreManagerRole -ErrorAction SilentlyContinue) { RestoreManagerRole }
        } catch {
            Write-Warning "restoring the manager role failed: $($_.Exception.Message)"
        }
        throw $failure
    }

    try {
        foreach ($name in $modes) {
            Write-Host "=== pool E2E mode: $name ==="
            switch ($name) {
                'Http' { Invoke-PoolHttpChecks }
                'Smtp' { Invoke-PoolSmtpChecks }
                'AllOrg' { Invoke-AllOrgSmtpChecks }
            }
        }
    } finally {
        RestoreManagerRole
    }
