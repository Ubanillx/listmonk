#!/usr/bin/env python3
"""Read-only delivery inspection for one campaign.

Never sends, retries or mutates anything: it only reads the delivery overview,
the recorded failure history and the current failed-recipient list, so an
operator can decide what to do next.
"""
from __future__ import annotations

import argparse

from listmonk_marketing.cli import add_auth_arguments, add_verbose_argument
from listmonk_marketing.client import ListmonkClient
from listmonk_marketing.common import emit_error, emit_json, log


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Inspect listmonk campaign delivery, failures and failed recipients (read-only).")
    add_auth_arguments(parser)
    parser.add_argument("--campaign-id", type=int, required=True, help="Campaign ID to inspect")
    parser.add_argument("--error-category", default="", help="Filter failures by category, e.g. smtp_rejected")
    parser.add_argument("--search", default="", help="Filter by customer name or customer code")
    parser.add_argument("--filter-organization-id", type=int, default=None, help="Filter a public-pool campaign by target organization")
    parser.add_argument("--page", type=int, default=1, help="Page number for failure and recipient lists")
    parser.add_argument("--per-page", type=int, default=50, help="Page size (the server caps it at 100)")
    parser.add_argument("--skip-errors", action="store_true", help="Do not fetch the recorded failure history")
    parser.add_argument("--skip-recipients", action="store_true", help="Do not fetch the current failed-recipient list")
    add_verbose_argument(parser)
    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    client = ListmonkClient(args.base_url, args.bearer_token, organization_id=args.organization_id)
    try:
        log("Fetching delivery overview", enabled=args.verbose)
        out: dict[str, object] = {
            "campaign_id": args.campaign_id,
            "overview": client.get_delivery_overview(args.campaign_id),
        }

        filters = {
            "search": args.search,
            "category": args.error_category,
            "organization_id": args.filter_organization_id,
            "page": args.page,
            "per_page": args.per_page,
        }

        if not args.skip_errors:
            log("Fetching failure history", enabled=args.verbose)
            try:
                out["send_errors"] = client.get_campaign_send_errors(args.campaign_id, **filters)
            except Exception as exc:  # aggregated overview stays usable without detail scope
                out["send_errors_unavailable"] = getattr(exc, "message", str(exc))

        if not args.skip_recipients:
            log("Fetching failed recipients", enabled=args.verbose)
            try:
                out["failed_recipients"] = client.get_campaign_failed_recipients(args.campaign_id, **filters)
            except Exception as exc:
                out["failed_recipients_unavailable"] = getattr(exc, "message", str(exc))

        emit_json(out)
        return 0
    except Exception as exc:
        return emit_error(exc)


if __name__ == "__main__":
    raise SystemExit(main())