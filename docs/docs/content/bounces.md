# Bounce processing

Enable bounce processing in Settings -> Bounces. POP3 bounce scanning and APIs only become available once the setting is enabled.

## AI classification of customer replies

When a campaign uses a dedicated reply mailbox (see Reply-To configuration in the campaign editor and the reply mailbox settings under your profile), you can optionally let listmonk classify inbound customer replies with an OpenAI-compatible model and act on the two explicit intents automatically:

- **Unsubscribe** — an explicit request to stop receiving marketing e-mail.
- **Complaint** — an explicit spam/abuse allegation or a threat to report the sender.

Negative tone, questions, ambiguous content, and every other reply is ignored and never changes customer data.

### Configuration

1. **Settings -> Reply AI classification**: fill in the API root of the OpenAI-compatible gateway (for example `https://api.openai.com/v1` or a self-hosted `new-api`/`one-api`/`LiteLLM` gateway such as `http://gateway.internal:3000/v1`; `/chat/completions` is appended automatically) together with the API key, then click **Load models** to read the gateway catalogue. Pick the model from that list (or type an id the gateway does not list), set the request timeout and the minimum confidence, and click **Test model**.
2. **Test model** pings the gateway, checks that the selected model is advertised, and sends one sample reply through the real classification prompt. Each of the four steps (configuration, gateway, model availability, model reply) reports its own status and reason, so a rejected key, a wrong root, an unreachable gateway, or a model that cannot return the bounded JSON is visible before automation is switched on. Leave the sample blank to send the built-in unsubscribe sample, or write your own body and pick the intent you expect. Nothing is saved by loading models or testing a model.
3. If the gateway answers on a different mount than the configured root (typically `/v1`), the result offers to use that address; classification calls always use the configured root, so save the suggested address before enabling.
4. Enable classification, then **Profile -> Reply mailboxes**: enable "AI automatic reply processing" on each verified mailbox individually. Only mailboxes that are explicitly enabled AND verified are scanned, and only when the global setting above is enabled.

Keys are masked after saving; leave the field untouched to keep the stored key. Loading models and testing a model reuse that stored key, and the key is only ever sent in the outbound `Authorization` header — it is never returned to the browser, stored elsewhere, or logged.

The mailbox is polled with POP3 without deleting messages; classification is queued per message (`Message-ID` plus content hash) so retries and restarts never double-process a reply.

### Actions

- The sender address is matched **only inside the mailbox owner's workspace**, and only when it resolves to exactly one customer. Unmatched or ambiguous senders are ignored and audited.
- For both actionable intents the matched customer is globally **blocklisted** and unsubscribed from all of their lists.
- An explicit complaint additionally records a bounce entry with source `reply_ai` and metadata holding the queue event id, model, confidence, and reason, so provider feedback (`ses`, `postmark`, ...) and AI-derived complaints remain distinguishable.
- Only the normalized, trimmed latest reply text is sent to the model; attachments and quoted history are never sent. After classification the retained text is removed from the queue and only the hash plus the bounded decision fields are kept for the customer activity audit ("AI reply classifications" on the customer's Activity tab).


## POP3 bounce mailbox
Configure the bounce mailbox in Settings -> Bounces. Either the "From" e-mail that is set on a campaign (or in settings) should have a POP3 mailbox behind it to receive bounce e-mails, or you should configure a dedicated POP3 mailbox and add that address as the `Return-Path` (envelope sender) header in Settings -> SMTP -> Custom headers box. For example:

```
[
	{"Return-Path": "your-bounce-inbox@site.com"}
]

```

Some mail servers may also return the bounce to the `Reply-To` address, which can also be added to the header settings.

### Bounce classification
listmonk applies a series of heuristics looking for keywords in the bounced mail body to guess if it is a 'soft' bounce or a 'hard' bounce. For instance, 4.x.x and 5.x.x error status codes, common strings such as "mailbox not found" etc. If none of the heuristics match, then the bounce mail is considered to be 'soft' by default.

## Webhook API
The bounce webhook API can be used to record bounce events with custom scripting. This could be by reading a mailbox, a database, or mail server logs.

| Method | Endpoint         | Description            |
| ------ | ---------------- | ---------------------- |
| `POST` | /webhooks/bounce | Record a bounce event. |


| Name            | Type   | Required | Description                                                                          |
| --------------- | ------ | -------- | ------------------------------------------------------------------------------------ |
| customer_uuid | string |          | The UUID of the customer. Either this or `email` is required.                      |
| email           | string |          | The e-mail of the customer. Either this or `customer_uuid` is required.          |
| campaign_uuid   | string |          | UUID of the campaign for which the bounce happened.                                  |
| source          | string | Yes      | A string indicating the source, eg: `api`, `my_script` etc.                          |
| type            | string | Yes      | `hard` or `soft` bounce. Currently, this has no effect on how the bounce is treated. |
| meta            | string |          | An optional escaped JSON string with arbitrary metadata about the bounce event.      |
 

```shell
curl -u 'api_username:access_token' -X POST 'http://localhost:9000/webhooks/bounce' \
	-H "Content-Type: application/json" \
	--data '{"email": "user1@mail.com", "campaign_uuid": "9f86b50d-5711-41c8-ab03-bc91c43d711b", "source": "api", "type": "hard", "meta": "{\"additional\": \"info\"}}'

```

## External webhooks
listmonk supports receiving bounce webhook events from the following SMTP providers.

| Endpoint                                                      | Description                            | More info                                                                                                             |
| :------------------------------------------------------------ | :------------------------------------- | :-------------------------------------------------------------------------------------------------------------------- |
| `https://listmonk.yoursite.com/webhooks/service/ses`          | Amazon (AWS) SES                       | See below                                                                                                             |
| `https://listmonk.yoursite.com/webhooks/service/sendgrid`     | Sendgrid / Twilio Signed event webhook | [More info](https://docs.sendgrid.com/for-developers/tracking-events/getting-started-event-webhook-security-features) |
| `https://listmonk.yoursite.com/webhooks/service/postmark`     | Postmark webhook                       | [More info](https://postmarkapp.com/developer/webhooks/webhooks-overview)                                             |
| `https://listmonk.yoursite.com/webhooks/service/forwardemail` | Forward Email webhook                  | [More info](https://forwardemail.net/en/faq#do-you-support-bounce-webhooks)                                           |

## Amazon Simple Email Service (SES)

If using SES as your SMTP provider, automatic bounce processing is the recommended way to maintain your [sender reputation](https://docs.aws.amazon.com/ses/latest/dg/monitor-sender-reputation.html). The settings below are based on Amazon's [recommendations](https://docs.aws.amazon.com/ses/latest/dg/send-email-concepts-deliverability.html). Please note that your sending domain must be verified in SES before proceeding.

1. In listmonk settings, go to the "Bounces" tab and configure the following:
    - Enable bounce processing: `Enabled`
        - Soft:
            - Bounce count: `2`
            - Action: `None`
        - Hard:
            - Bounce count: `1`
            - Action: `Blocklist`
        - Complaint: 
            - Bounce count: `1`
            - Action: `Blocklist`
    - Enable bounce webhooks: `Enabled`
    - Enable SES: `Enabled`
2. In the AWS console, go to [Simple Notification Service](https://console.aws.amazon.com/sns/) and create a new topic with the following settings:
    - Type: `Standard`
    - Name: `ses-bounces` (or any other name)
3. Create a new subscription to that topic with the following settings:
    - Protocol: `HTTPS`
    - Endpoint: `https://listmonk.yoursite.com/webhooks/service/ses`
    - Enable raw message delivery: `Disabled` (unchecked)
4. SES will then make a request to your listmonk instance to confirm the subscription. After a page refresh, the subscription should have a status of "Confirmed". If not, your endpoint may be incorrect or not publicly accessible.
5. In the AWS console, go to [Simple Email Service](https://console.aws.amazon.com/ses/) and click "Identities" in the left sidebar.
6. Click your domain and go to the "Notifications" tab.
7. Next to "Feedback notifications", click "Edit".
8. For both "Bounce feedback" and "Complaint feedback", use the following settings:
    - SNS topic: `ses-bounces` (or whatever you named it)
    - Include original email headers: `Enabled` (checked)
9. Repeat steps 6-8 for any `Email address` identities you send from using listmonk
10. Bounce processing should now be working. You can test it with [SES simulator addresses](https://docs.aws.amazon.com/ses/latest/dg/send-an-email-from-console.html#send-email-simulator). Add them as customers, send them campaign previews, and ensure that the appropriate action was taken after the configured bounce count was reached.
    - Soft bounce: `ooto@simulator.amazonses.com`
    - Hard bounce: `bounce@simulator.amazonses.com`
    - Complaint: `complaint@simulator.amazonses.com`
11. You can optionally [disable email feedback forwarding](https://docs.aws.amazon.com/ses/latest/dg/monitor-sending-activity-using-notifications-email.html#monitor-sending-activity-using-notifications-email-disabling).

## Exporting bounces

Bounces can be exported via the JSON API:
```shell
curl -u 'username:passsword' 'http://localhost:9000/api/bounces'
```

Or by querying the database directly:
```sql
SELECT bounces.created_at,
    bounces.customer_id,
    customers.uuid AS customer_uuid,
    customers.email AS email
FROM bounces
LEFT JOIN customers ON (customers.id = bounces.customer_id)
ORDER BY bounces.created_at DESC LIMIT 1000;
```

## Test a POP bounce mailbox

In **Settings → Bounces**, select POP3 (plain), SSL/TLS (usually port 995), or STARTTLS (usually port 110), then click **Test mailbox**. The test uses the current form without saving it. An empty or masked password reuses the saved mailbox password by UUID; a new mailbox needs its password. Existing configurations keep their original TLS behavior. The optional `starttls` setting defaults to false and cannot be combined with `tls_enabled=true`.

The result lists connection (including TLS), login, retrieval and parsing outcomes, mailbox count, the outer notification sender/decoded subject, timestamp, failed recipients, SMTP status and diagnostic reason. Multiple DSN recipients retain their individual reasons. Standard recipient fields take priority over inferred body addresses; `body_inferred` identifies a heuristic result. Sender and ordinary To addresses are not treated as failed recipients.

Only the highest message number in the current POP session is retrieved. POP cannot sort by received time, so this is a last-message preview, not a search for the latest bounce. An ordinary message reports successful retrieval without an identified bounce. The timestamp uses Received when available, otherwise Date (labelled as a fallback), otherwise remains unknown.

Testing has a 30-second deadline and a 5 MiB message limit. It never deletes mail, saves settings or records bounce events. Existing background processing still runs independently and may consume mail; retry if the server reports that the mailbox is locked or a message is unavailable.

### Diagnostic API

`POST /api/settings/bounce/mailbox/test` requires the existing `settings:manage` permission. JSON fields: `uuid` (optional saved mailbox reference), `type` (`pop`), `host`, `port`, `auth_protocol` (`userpass` or `none`), `username`, `password`, `tls_enabled`, `starttls`, and `tls_skip_verify`. Extra persisted form fields such as `scan_interval` are ignored by the test.

Valid requests return the normal `{ "data": ... }` envelope, including `status` (`success`, `empty`, `not_bounce`, or `failed`), `count`, four `steps` (`name`, `status`, optional `detail`), and an optional `message` containing `from`, `subject`, `date`, `date_source`, `message_id`, `is_bounce`, `bounce_type`, `reason`, and `recipients` (`address`, `source`, `status`, `reason`). Operational failures stay in this result so completed steps remain visible; invalid configuration returns HTTP 400. No database migration is required.
