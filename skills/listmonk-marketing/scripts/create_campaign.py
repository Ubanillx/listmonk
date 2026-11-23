#!/usr/bin/env python3
from __future__ import annotations

import argparse
from pathlib import Path

from listmonk_marketing.campaigns import (
    build_campaign_payload,
    create_campaign,
    get_source_campaign,
    validate_campaign_body_vars,
    validate_campaign_template,
)
from listmonk_marketing.cli import add_auth_arguments, add_campaign_arguments, add_verbose_argument
from listmonk_marketing.client import APIError, ListmonkClient
from listmonk_marketing.common import emit_error, emit_json, log
from listmonk_marketing.media import (
    apply_media_tokens,
    merge_media_ids,
    upload_media_files,
    validate_media_tokens,
)


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Create a listmonk campaign with a Bearer personal API key.")
    add_auth_arguments(parser)
    add_campaign_arguments(parser)
    parser.add_argument(
        "--preview-check",
        action="store_true",
        help="After creating the draft, render it via /api/campaigns/{id}/preview and report render errors",
    )
    add_verbose_argument(parser)
    return parser.parse_args(argv)


def run_create_campaign(
    args: argparse.Namespace,
    client: ListmonkClient,
    *,
    progress: dict[str, object] | None = None,
) -> dict[str, object]:
    progress = progress if progress is not None else {}
    source_campaign = get_source_campaign(
        client,
        source_campaign_id=args.source_campaign_id,
        source_campaign_name=args.source_campaign_name or "",
    )

    effective_content_type = args.content_type or (source_campaign or {}).get("content_type") or "richtext"
    body = args.campaign_body
    if args.body_file:
        if effective_content_type == "visual":
            raise ValueError("--body-file is not supported with content_type=visual; use html or richtext")
        body = Path(args.body_file).read_text(encoding="utf-8-sig")
    if body is None and source_campaign:
        body = source_campaign.get("body")

    template_id = args.template_id
    if template_id is None and source_campaign:
        template_id = source_campaign.get("template_id")
    validate_campaign_template(client, template_id=template_id, content_type=effective_content_type)
    # Fail before creating anything if the body uses template fields the campaign
    # render root does not expose (for example the upstream .Subscriber naming).
    validate_campaign_body_vars(body)
    validate_media_tokens(body, [Path(path).name for path in args.media_file or []])
    # Validate explicit media IDs before uploading anything, so a bad argument
    # cannot leave orphaned uploads behind.
    merge_media_ids(args.media_id, [])

    # Validate scalar fields before uploading anything.
    build_campaign_payload(
        campaign_name=args.campaign_name,
        subject=args.subject,
        customer_list_id=args.customer_list_id,
        template_id=template_id,
        campaign_body=None,
        content_type=args.content_type,
        messenger=args.messenger,
        from_email=args.from_email,
        reply_mailbox_id=args.reply_mailbox_id,
        smtp_source=args.smtp_source,
        smtp_pool_id=args.smtp_pool_id,
        smtp_rate_limit=args.smtp_rate_limit,
        visibility=args.visibility,
        auto_track_links=args.auto_track_links,
        daily_send_limit=args.daily_send_limit,
        daily_resume_time=args.daily_resume_time,
        send_at=args.send_at,
        tags=args.tag,
        attribs_file=args.attribs_file or "",
        source_campaign=source_campaign,
    )

    uploaded = upload_media_files(
        client,
        args.media_file,
        folder_id=args.media_folder_id,
        visibility=args.media_visibility,
        verbose=args.verbose,
        progress=progress,
    )
    media_ids = merge_media_ids(args.media_id, uploaded)
    progress["media_ids"] = media_ids
    body = apply_media_tokens(body, uploaded)
    payload = build_campaign_payload(
        campaign_name=args.campaign_name,
        subject=args.subject,
        customer_list_id=args.customer_list_id,
        template_id=template_id,
        campaign_body=body,
        content_type=args.content_type,
        messenger=args.messenger,
        from_email=args.from_email,
        reply_mailbox_id=args.reply_mailbox_id,
        smtp_source=args.smtp_source,
        smtp_pool_id=args.smtp_pool_id,
        smtp_rate_limit=args.smtp_rate_limit,
        visibility=args.visibility,
        auto_track_links=args.auto_track_links,
        daily_send_limit=args.daily_send_limit,
        daily_resume_time=args.daily_resume_time,
        send_at=args.send_at,
        tags=args.tag,
        attribs_file=args.attribs_file or "",
        media_ids=media_ids,
        source_campaign=source_campaign,
    )
    log("Creating campaign", enabled=args.verbose)
    campaign = create_campaign(client, payload=payload)
    progress["campaign_id"] = int(campaign["id"])
    out: dict[str, object] = {
        "campaign_id": int(campaign["id"]),
        "campaign": campaign,
        "media": uploaded,
        "media_ids": media_ids,
    }
    if source_campaign:
        out["source_campaign_id"] = int(source_campaign["id"])
        out["source_campaign"] = source_campaign
    if getattr(args, "preview_check", False):
        # The draft is already persisted; a render failure is reported, not raised,
        # so the caller can fix the body on the same campaign id.
        try:
            client.preview_campaign(
                int(campaign["id"]),
                body=body or "",
                content_type=effective_content_type,
                template_id=template_id,
            )
            out["preview_ok"] = True
        except APIError as exc:
            out["preview_ok"] = False
            out["render_error"] = f"HTTP {exc.status}: {exc}"
    return out


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    client = ListmonkClient(args.base_url, args.bearer_token, organization_id=args.organization_id)
    progress: dict[str, object] = {}
    try:
        emit_json(run_create_campaign(args, client, progress=progress))
        return 0
    except Exception as exc:
        return emit_error(exc, progress=progress)


if __name__ == "__main__":
    raise SystemExit(main())
