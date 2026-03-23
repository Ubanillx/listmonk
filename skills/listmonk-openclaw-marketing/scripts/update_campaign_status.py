#!/usr/bin/env python3
from __future__ import annotations

import argparse

from listmonk_marketing.campaigns import update_campaign_status
from listmonk_marketing.cli import add_auth_arguments, add_verbose_argument
from listmonk_marketing.client import ListmonkClient
from listmonk_marketing.common import emit_error, emit_json, log


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Update a listmonk campaign status with a Bearer token.")
    add_auth_arguments(parser)
    parser.add_argument("--campaign-id", type=int, required=True, help="Campaign ID to update")
    parser.add_argument("--status", default="running", help="Target status, defaults to running")
    add_verbose_argument(parser)
    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    client = ListmonkClient(args.base_url, args.bearer_token)
    try:
        log("Updating campaign status", enabled=args.verbose)
        campaign = update_campaign_status(client, campaign_id=args.campaign_id, status=args.status)
        emit_json({"campaign_id": int(campaign["id"]), "status": campaign.get("status"), "campaign": campaign})
        return 0
    except Exception as exc:
        return emit_error(exc)


if __name__ == "__main__":
    raise SystemExit(main())

