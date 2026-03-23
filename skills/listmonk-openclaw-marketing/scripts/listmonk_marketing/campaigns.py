from __future__ import annotations

from copy import deepcopy
from typing import Any

from listmonk_marketing.client import ListmonkClient
from listmonk_marketing.common import load_json_file


INHERITED_CAMPAIGN_FIELDS = (
    "subject",
    "type",
    "body",
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


def build_campaign_payload(
    *,
    campaign_name: str,
    subject: str | None,
    list_id: int,
    template_id: int | None = None,
    campaign_body: str | None = None,
    content_type: str | None = None,
    messenger: str | None = None,
    from_email: str | None = None,
    daily_send_limit: int | None = None,
    daily_resume_time: str | None = None,
    send_at: str | None = None,
    tags: list[str] | None = None,
    attribs_file: str = "",
    source_campaign: dict[str, Any] | None = None,
) -> dict[str, Any]:
    payload = inherit_campaign_fields(source_campaign)
    payload["name"] = campaign_name
    payload["lists"] = [list_id]

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
    if daily_send_limit is not None:
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
