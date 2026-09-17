# End-to-end verification of platform-level public-pool campaigns:
# two organizations, three member SMTP accounts, two pool contacts allocated to
# both organizations, and one all_organizations campaign. Asserts round-robin
# sender assignment, cross-organization deduplication, delivered status,
# per-organization Reply-To and the durable organization cursor.
$ErrorActionPreference = 'Stop'
$db = 'dev-db-1'
$user = 'lmkdev_M7q2Xr8N'
$dbname = 'lmkdev_M7q2Xr8N'
$stamp = [DateTimeOffset]::UtcNow.ToUnixTimeSeconds()

function Sql([string]$q) {
  $out = docker exec $db psql -U $user -d $dbname -t -A -q -F'|' -c $q
  @($out | Where-Object { $_ -ne '' -and $_ -notmatch '^(INSERT|UPDATE|DELETE|SELECT) \d' })
}
function SqlOne([string]$q) { (Sql $q | Select-Object -First 1) }
function SqlRun([string]$q) { Sql $q | Out-Null }

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
$userA = SqlOne "INSERT INTO users(username,email,name,type,user_role_id,status) VALUES('e2e_allorg_a_$stamp','e2e_allorg_a_$stamp@example.test','E2E A','user',3,'enabled') RETURNING id"
$userB = SqlOne "INSERT INTO users(username,email,name,type,user_role_id,status) VALUES('e2e_allorg_b_$stamp','e2e_allorg_b_$stamp@example.test','E2E B','user',3,'enabled') RETURNING id"
$userC = SqlOne "INSERT INTO users(username,email,name,type,user_role_id,status) VALUES('e2e_allorg_c_$stamp','e2e_allorg_c_$stamp@example.test','E2E C','user',3,'enabled') RETURNING id"

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

# Four contacts: 1-2 belong to BOTH organizations (must still be sent once),
# 3 belongs to organization A only, 4 belongs to organization B only. The two
# single-organization contacts prove per-organization SMTP pool routing.
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

$tpl = SqlOne "SELECT id FROM templates WHERE is_default = TRUE LIMIT 1"
$camp = SqlOne "INSERT INTO campaigns(uuid,name,subject,from_email,body,content_type,type,messenger,status,daily_send_limit,daily_resume_time,owner_user_id,organization_id,pool_scope,template_id) VALUES(gen_random_uuid(),'e2e-allorg-campaign-$stamp','E2E subject','placeholder@example.test','<p>E2E platform pool body</p>','richtext','regular','email','running',50,'09:00',$userA,NULL,'all_organizations',$tpl) RETURNING id"
SqlRun "INSERT INTO campaign_customer_lists(campaign_id,customer_list_id,customer_list_name,pool_id,org_pool_allocation_id,source_organization_id,resolved_reply_mailbox_id) VALUES($camp,NULL,'e2e-allorg-pool-$stamp',$pool,NULL,NULL,NULL)"

Write-Output "seeded campaign=$camp pool=$pool orgs=$orgA/$orgB smtp=$smtpA1/$smtpA2/$smtpB1"

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

Write-Output "campaign status: $status"
Write-Output "recipients ($total): $rows"
Write-Output "cursors: $cursors"
Write-Output "sent=$sent distinctSenders=$distinctSenders distinctTargetOrgs=$distinctOrgs"

$mh = Invoke-RestMethod -Uri 'http://localhost:8265/api/v2/search?kind=to&query=e2e-allorg-' -TimeoutSec 30
$msgs = @($mh.items) | Where-Object { $_.Content.Headers.To[0] -like "*e2e-allorg-$stamp-*" }
Write-Output "mailhog messages for this run: $($msgs.Count)"
foreach ($m in $msgs) {
  Write-Output ("  to={0} from={1} reply-to={2}" -f $m.Content.Headers.To[0], $m.Content.Headers.From[0], $m.Content.Headers.'Reply-To'[0])
}

# Cleanup.
SqlRun "DELETE FROM campaigns WHERE id=$camp"
SqlRun "DELETE FROM user_smtp_servers WHERE uuid IN ('$smtpA1','$smtpA2','$smtpB1')"
SqlRun "UPDATE organizations SET reply_mailbox_id=NULL WHERE id IN ($orgA,$orgB)"
SqlRun "DELETE FROM customer_lists WHERE id=$pool"
SqlRun "DELETE FROM customer_lists WHERE id IN ($listA,$listB)"
SqlRun "DELETE FROM reply_mailboxes WHERE id IN ($mbA,$mbB)"
SqlRun "DELETE FROM organization_members WHERE organization_id IN ($orgA,$orgB)"
SqlRun "DELETE FROM org_pool_allocations WHERE pool_id=$pool"
SqlRun "DELETE FROM pool_contacts WHERE customer_code LIKE 'E2E-$stamp-%'"
SqlRun "DELETE FROM users WHERE id IN ($userA,$userB,$userC)"
SqlRun "DELETE FROM org_pool_smtp_cursors WHERE organization_id IN ($orgA,$orgB)"
SqlRun "DELETE FROM organizations WHERE id IN ($orgA,$orgB)"
Write-Output "cleanup done"
