#!/usr/bin/env python3
from __future__ import annotations

import argparse

from listmonk_marketing.cli import add_auth_arguments, add_verbose_argument
from listmonk_marketing.client import ListmonkClient
from listmonk_marketing.common import emit_error, emit_json, log
from listmonk_marketing.reports import fetch_campaign_reports


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Fetch listmonk campaign reports with a Bearer token.")
    add_auth_arguments(parser)
    parser.add_argument("--campaign-id", type=int, required=True, help="Campaign ID to report on")
    parser.add_argument("--report-from", required=True, help="Start date/time for report fetch")
    parser.add_argument("--report-to", required=True, help="End date/time for report fetch")
    parser.add_argument("--recipient-per-page", type=int, default=100, help="Recipients page size when fetching recipient analytics")
    add_verbose_argument(parser)
    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    client = ListmonkClient(args.base_url, args.bearer_token)
    try:
        log("Fetching reports", enabled=args.verbose)
        reports = fetch_campaign_reports(
            client,
            campaign_id=args.campaign_id,
            report_from=args.report_from,
            report_to=args.report_to,
            recipient_per_page=args.recipient_per_page,
        )
        emit_json({"campaign_id": args.campaign_id, "reports": reports})
        return 0
    except Exception as exc:
        return emit_error(exc)


if __name__ == "__main__":
    raise SystemExit(main())
