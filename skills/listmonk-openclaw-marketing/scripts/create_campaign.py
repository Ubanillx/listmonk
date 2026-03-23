#!/usr/bin/env python3
from __future__ import annotations

import argparse

from listmonk_marketing.campaigns import build_campaign_payload, create_campaign, get_source_campaign
from listmonk_marketing.cli import add_auth_arguments, add_campaign_arguments, add_verbose_argument
from listmonk_marketing.client import ListmonkClient
from listmonk_marketing.common import emit_error, emit_json, log


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Create a listmonk campaign with a Bearer token.")
    add_auth_arguments(parser)
    add_campaign_arguments(parser)
    add_verbose_argument(parser)
    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    client = ListmonkClient(args.base_url, args.bearer_token)
    try:
        source_campaign = get_source_campaign(
            client,
            source_campaign_id=args.source_campaign_id,
            source_campaign_name=args.source_campaign_name or "",
        )
        payload = build_campaign_payload(
            campaign_name=args.campaign_name,
            subject=args.subject,
            list_id=args.list_id,
            template_id=args.template_id,
            campaign_body=args.campaign_body,
            content_type=args.content_type,
            messenger=args.messenger,
            from_email=args.from_email,
            daily_send_limit=args.daily_send_limit,
            daily_resume_time=args.daily_resume_time,
            send_at=args.send_at,
            tags=args.tag,
            attribs_file=args.attribs_file or "",
            source_campaign=source_campaign,
        )
        log("Creating campaign", enabled=args.verbose)
        campaign = create_campaign(client, payload=payload)
        out = {"campaign_id": int(campaign["id"]), "campaign": campaign}
        if source_campaign:
            out["source_campaign_id"] = int(source_campaign["id"])
            out["source_campaign"] = source_campaign
        emit_json(out)
        return 0
    except Exception as exc:
        return emit_error(exc)


if __name__ == "__main__":
    raise SystemExit(main())
