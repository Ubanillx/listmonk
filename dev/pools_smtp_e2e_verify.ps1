$ErrorActionPreference = 'Stop'
$base = if ($env:LISTMONK_QA_BASE_URL) { $env:LISTMONK_QA_BASE_URL } else { 'http://localhost:9173' }
$mailhog = if ($env:POOL_QA_MAILHOG_URL) { $env:POOL_QA_MAILHOG_URL } else { 'http://localhost:8265' }
$password = if ($env:POOL_QA_PASSWORD) { $env:POOL_QA_PASSWORD } else { 'Test@1234' }
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

# Resolve fixture IDs instead of relying on sequence values in a reused
# development database. Environment overrides remain available for a custom
# fixture.
$poolID = if ($env:POOL_QA_POOL_ID) { [int]$env:POOL_QA_POOL_ID } else { [int](DbScalar "SELECT id FROM customer_lists WHERE name='wsqa-pool-primary' AND type='pool'") }
$allocationID = if ($env:POOL_QA_SEGMENT_ID) { [int]$env:POOL_QA_SEGMENT_ID } else { [int](DbScalar "SELECT ps.id FROM org_pool_allocations ps JOIN customer_lists l ON l.id=ps.list_id WHERE l.name='wsqa-org-pool-allocation' AND ps.pool_id=$poolID") }

function LoginUser($username) {
  $jar = Join-Path $env:TEMP ("pool-smtp-e2e-{0}-{1}.jar" -f $username, $PID)
  Remove-Item $jar -ErrorAction SilentlyContinue
  curl.exe -s -c $jar "$base/admin/login" | Out-Null
  $html = [string](curl.exe -s -b $jar -c $jar "$base/admin/login")
  $nonce = [regex]::Match($html, 'name="nonce" value="([^"]+)"').Groups[1].Value
  Check "$username login nonce" ($nonce.Length -gt 0)
  curl.exe -s -o NUL -b $jar -c $jar -X POST -d "username=$username&password=$password&nonce=$nonce&next=/admin" "$base/admin/login" | Out-Null
  return $jar
}

function Api($method, $path, $jar, $organizationID, $body) {
  $out = Join-Path $env:TEMP ("pool-smtp-e2e-response-{0}.json" -f $PID)
  $args = @('-s', '-o', $out, '-w', '%{http_code}', '-b', $jar,
    '-H', "X-Listmonk-Organization-ID: $organizationID")
  if ($null -ne $body) {
    $bodyFile = Join-Path $env:TEMP ("pool-smtp-e2e-body-{0}.json" -f $PID)
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

$manager = LoginUser $managerUser
$managerID = [int](DbScalar "SELECT id FROM users WHERE username='$managerUser'")
$smtpName = "pool-e2e-mailhog-$PID"
$campaignID = 0
$smtpID = 0

# This isolated verifier owns no pre-existing SMTP rows for the fixture user.
# Do not replace a human/operator's profile configuration by accident.
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

  $recipientStats = DbScalar "SELECT count(*)::text || '|' || count(DISTINCT pool_contact_id)::text || '|' || count(*) FILTER (WHERE status='sent')::text || '|' || count(*) FILTER (WHERE reply_mailbox_id=(SELECT reply_mailbox_id FROM org_pool_allocations WHERE id=$allocationID))::text FROM campaign_pool_recipients WHERE campaign_id=$campaignID"
  Check 'send snapshot has one sent row per pool contact with allocation reply mailbox' ($recipientStats -eq '3|3|3|3')

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
Write-Host 'Public-pool SMTP E2E verification complete.'
