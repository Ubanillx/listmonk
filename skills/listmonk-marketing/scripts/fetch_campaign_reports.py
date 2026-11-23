#!/usr/bin/env python3
from __future__ import annotations

import argparse

from listmonk_marketing.cli import add_auth_arguments, add_verbose_argument
from listmonk_marketing.client import ListmonkClient
from listmonk_marketing.common import emit_error, emit_json, log
from listmonk_marketing.reports import fetch_campaign_reports


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Fetch listmonk campaign reports with a Bearer personal API key.")
    add_auth_arguments(parser)
    target = parser.add_mutually_exclusive_group(required=True)
    target.add_argument("--campaign-id", type=int, help="Single campaign ID to report on")
    target.add_argument("--campaign-ids", type=int, nargs="+", help="Campaign IDs to aggregate")
    target.add_argument("--all-campaigns", action="store_true", help="Aggregate all campaigns authorized in the bound workspace")
    parser.add_argument("--report-from", required=True, help="Start date/time for report fetch")
    parser.add_argument("--report-to", required=True, help="End date/time for report fetch")
    parser.add_argument("--recipient-per-page", type=int, default=100, help="Recipients page size when fetching recipient analytics")
    parser.add_argument("--recipient-page", type=int, default=1, help="Recipient page to fetch; one page is returned per run")
    parser.add_argument("--include-geo", action="store_true", help="Also fetch approximate open locations")
    add_verbose_argument(parser)
    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    client = ListmonkClient(args.base_url, args.bearer_token, organization_id=args.organization_id)
    try:
        log("Fetching reports", enabled=args.verbose)
        reports = fetch_campaign_reports(
            client,
            campaign_id=args.campaign_id,
            campaign_ids=args.campaign_ids,
            all_campaigns=args.all_campaigns,
            report_from=args.report_from,
            report_to=args.report_to,
            recipient_per_page=args.recipient_per_page,
            recipient_page=args.recipient_page,
            include_geo=args.include_geo,
        )
        out = {"reports": reports}
        if args.campaign_id is not None:
            out["campaign_id"] = args.campaign_id
        elif args.campaign_ids:
            out["campaign_ids"] = args.campaign_ids
        else:
            out["all_campaigns"] = True
        emit_json(out)
        return 0
    except Exception as exc:
        return emit_error(exc)


if __name__ == "__main__":
    raise SystemExit(main())
