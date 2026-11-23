from __future__ import annotations

import re
from copy import deepcopy
from typing import Any

from listmonk_marketing.client import APIError, ListmonkClient
from listmonk_marketing.common import load_json_file


# Campaign content and campaign-type templates share one render root:
# *manager.CampaignMessage, which only exposes .Customer and .Campaign.
# Upstream listmonk templates use .Subscriber.* / .UnsubscribeURL / .Message.* / .Tx,
# none of which exist in this deployment and all of which fail at render time.
TEMPLATE_ROOT_FIELDS = frozenset({"Customer", "Campaign"})
LEGACY_TEMPLATE_FIELDS = {
    "Subscriber": "Customer",
    "UnsubscribeURL": "none in campaign content; use the {{ UnsubscribeURL }} helper the server appends to the shell template",
    "Message": "campaign shell fields are not exposed to content",
    "Tx": "tx data is only available to transactional templates",
    "OptinURL": "none in campaign content",
}
_FIELD_REF_RE = re.compile(r"(?<![\w.$])\.([A-Za-z_]\w*)")
_ACTION_RE = re.compile(r"{{.*?}}", re.S)
_SUPPORTED_SUMMARY = ".Customer(.Name / .Email / .Attribs.key) and .Campaign(.Name / .Subject)"


def find_unsupported_template_fields(body: str | None) -> list[dict[str, Any]]:
    """Locate root template fields that the campaign render context does not expose.

    Only `{{ ... }}` actions are scanned, so CSS class selectors and plain HTML
    text cannot trigger false positives.
    """
    if not body:
        return []
    findings: list[dict[str, Any]] = []
    seen: set[tuple[str, int]] = set()
    for action in _ACTION_RE.finditer(body):
        for match in _FIELD_REF_RE.finditer(action.group(0)):
            field = match.group(1)
            if field in TEMPLATE_ROOT_FIELDS:
                continue
            offset = action.start() + match.start()
            line = body.count("\n", 0, offset) + 1
            column = offset - body.rfind("\n", 0, offset)
            key = (field, line)
            if key in seen:
                continue
            seen.add(key)
            finding: dict[str, Any] = {"field": f".{field}", "line": line, "column": column}
            hint = LEGACY_TEMPLATE_FIELDS.get(field)
            if hint:
                finding["hint"] = hint
            findings.append(finding)
    return findings


def validate_campaign_body_vars(body: str | None) -> None:
    """Fail fast on template fields the campaign render root does not have."""
    findings = find_unsupported_template_fields(body)
    if not findings:
        return
    first = findings[0]
    detail = ", ".join(f"{item['field']} (line {item['line']}, column {item['column']})" for item in findings[:5])
    message = (
        f"campaign body references template fields that the render root does not expose: {detail}. "
        f"Root fields are {_SUPPORTED_SUMMARY}; the render root is *manager.CampaignMessage, "
        f"which has no .Subscriber (upstream listmonk naming) and no .UnsubscribeURL/.Message/.Tx. "
        f"Fix {first['field']}"
    )
    hint = first.get("hint")
    if hint:
        message += f" -> {hint}"
    message += ". Verify with POST /api/campaigns/{id}/preview or scripts/preview_campaign.py."
    raise ValueError(message)


INHERITED_CAMPAIGN_FIELDS = (
    "subject",
    "type",
    "body",
    "body_source",
    "altbody",
    "content_type",
    "template_id",
    "messenger",
    "from_email",
    "daily_send_limit",
    "daily_resume_time",
    "tags",
    "attribs",
    "headers",
    "auto_track_links",
    "name_fallback",
    "archive",
    "archive_template_id",
    "archive_meta",
)


def get_source_campaign(
    client: ListmonkClient,
    *,
    source_campaign_id: int | None = None,
    source_campaign_name: str = "",
) -> dict[str, Any] | None:
    if source_campaign_id is not None:
        return client.get_campaign(source_campaign_id)

    source_campaign_name = source_campaign_name.strip()
    if not source_campaign_name:
        return None

    matches = client.query_campaigns(source_campaign_name)
    for campaign in matches:
        name = str(campaign.get("name", "")).strip()
        if name.casefold() == source_campaign_name.casefold():
            campaign_id = campaign.get("id")
            if campaign_id is None:
                return campaign
            return client.get_campaign(int(campaign_id))

    raise ValueError(f"Could not find source campaign by exact name: {source_campaign_name}")


def inherit_campaign_fields(source_campaign: dict[str, Any] | None) -> dict[str, Any]:
    if not source_campaign:
        return {}

    payload: dict[str, Any] = {}
    for field in INHERITED_CAMPAIGN_FIELDS:
        if field in source_campaign and source_campaign[field] is not None:
            payload[field] = deepcopy(source_campaign[field])
    return payload


def validate_campaign_template(
    client: ListmonkClient,
    *,
    template_id: int | None,
    content_type: str | None,
) -> None:
    """Best-effort type check before create; the server remains authoritative."""
    if not template_id:
        return
    getter = getattr(client, "get_template", None)
    if getter is None:
        return
    try:
        template = getter(int(template_id))
    except APIError as exc:
        if exc.status in (401, 403):
            return
        raise
    template_type = str(template.get("type") or "")
    if template_type == "tx":
        raise ValueError("template_id must be a campaign template, not a transactional template")
    if content_type == "visual" and template_type != "campaign_visual":
        raise ValueError("content_type=visual requires a campaign_visual template")
    if content_type and content_type != "visual" and template_type == "campaign_visual":
        raise ValueError("campaign_visual templates require content_type=visual")


def build_campaign_payload(
    *,
    campaign_name: str,
    subject: str | None,
    customer_list_id: int,
    template_id: int | None = None,
    campaign_body: str | None = None,
    content_type: str | None = None,
    messenger: str | None = None,
    from_email: str | None = None,
    reply_mailbox_id: int | None = None,
    smtp_source: str | None = None,
    smtp_pool_id: int | None = None,
    smtp_rate_limit: int | None = None,
    visibility: str | None = None,
    auto_track_links: bool | None = None,
    daily_send_limit: int | None = None,
    daily_resume_time: str | None = None,
    send_at: str | None = None,
    tags: list[str] | None = None,
    attribs_file: str = "",
    media_ids: list[int] | None = None,
    source_campaign: dict[str, Any] | None = None,
) -> dict[str, Any]:
    payload = inherit_campaign_fields(source_campaign)
    payload["name"] = campaign_name
    payload["customer_list_ids"] = [customer_list_id]

    if subject is not None:
        payload["subject"] = subject
    if campaign_body is not None:
        payload["body"] = campaign_body
    if template_id is not None:
        payload["template_id"] = template_id
    if content_type is not None:
        payload["content_type"] = content_type
    if messenger is not None:
        payload["messenger"] = messenger
    if from_email is not None:
        payload["from_email"] = from_email
    if reply_mailbox_id is not None:
        payload["reply_mailbox_id"] = reply_mailbox_id
    if smtp_source is not None:
        payload["smtp_source"] = smtp_source
    if smtp_pool_id is not None:
        if smtp_source != "organization" or smtp_pool_id < 1:
            raise ValueError("--smtp-pool-id requires --smtp-source organization and a positive pool ID")
        payload["smtp_pool_id"] = smtp_pool_id
    if smtp_rate_limit is not None:
        if not 1 <= smtp_rate_limit <= 1000000:
            raise ValueError("--smtp-rate-limit must be between 1 and 1000000")
        payload["smtp_rate_limit"] = smtp_rate_limit
    if visibility is not None:
        payload["visibility"] = visibility
    if auto_track_links is not None:
        payload["auto_track_links"] = auto_track_links
    if daily_send_limit is not None:
        if isinstance(daily_send_limit, bool) or not isinstance(daily_send_limit, int) or daily_send_limit < 1:
            raise ValueError("--daily-send-limit must be a positive integer")
        payload["daily_send_limit"] = daily_send_limit
    if daily_resume_time is not None:
        payload["daily_resume_time"] = daily_resume_time
    if tags is not None:
        payload["tags"] = tags

    payload.setdefault("type", "regular")
    payload.setdefault("content_type", "richtext")
    payload.setdefault("body", "")
    payload.setdefault("messenger", "email")
    payload.setdefault("tags", [])

    subject_value = str(payload.get("subject", "")).strip()
    if not subject_value:
        raise ValueError("--subject is required unless inherited from --source-campaign-id or --source-campaign-name")
    payload["subject"] = subject_value

    if payload["type"] == "regular" and str(payload["messenger"]).startswith("email"):
        if not payload.get("daily_send_limit"):
            raise ValueError(
                "--daily-send-limit is required unless inherited from --source-campaign-id or --source-campaign-name"
            )
        if not payload.get("daily_resume_time"):
            payload["daily_resume_time"] = "09:00"

    if send_at:
        payload["send_at"] = send_at
    if attribs_file:
        payload["attribs"] = load_json_file(attribs_file, dict)
    if media_ids:
        cleaned: list[int] = []
        seen: set[int] = set()
        for raw in media_ids:
            if isinstance(raw, bool) or not isinstance(raw, int) or raw < 1:
                raise ValueError("media ids must be positive integers")
            if raw not in seen:
                seen.add(raw)
                cleaned.append(raw)
        payload["media"] = cleaned
    return payload


def create_campaign(client: ListmonkClient, *, payload: dict[str, Any]) -> dict[str, Any]:
    return client.create_campaign(payload)


def update_campaign_status(
    client: ListmonkClient,
    *,
    campaign_id: int,
    status: str = "running",
) -> dict[str, Any]:
    return client.update_campaign_status(campaign_id, status)
