#!/usr/bin/env python3
"""Read-only customer activity inspection, including AI reply classification.

The AI classifier itself is a platform setting that a personal API key cannot
configure. This script only reads the conclusions the server already recorded,
which never include the reply body.
"""
from __future__ import annotations

import argparse

from listmonk_marketing.cli import add_auth_arguments, add_verbose_argument
from listmonk_marketing.client import ListmonkClient
from listmonk_marketing.common import emit_error, emit_json, log

REPLY_INTENTS = ("unsubscribe", "complaint", "product_complaint", "other")


def summarize_replies(events: list[dict]) -> dict[str, object]:
    """Count AI replies by intent and action without guessing at missing fields."""
    by_intent: dict[str, int] = {}
    by_action: dict[str, int] = {}
    by_status: dict[str, int] = {}
    blocklisted = 0
    for event in events:
        intent = str(event.get("intent") or "unknown")
        action = str(event.get("action") or "unknown")
        status = str(event.get("status") or "unknown")
        by_intent[intent] = by_intent.get(intent, 0) + 1
        by_action[action] = by_action.get(action, 0) + 1
        by_status[status] = by_status.get(status, 0) + 1
        if action == "blocklisted":
            blocklisted += 1
    return {
        "total": len(events),
        "by_intent": by_intent,
        "by_action": by_action,
        "by_status": by_status,
        "blocklisted": blocklisted,
    }


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Inspect a listmonk customer activity and AI reply classification (read-only).")
    add_auth_arguments(parser)
    target = parser.add_mutually_exclusive_group(required=True)
    target.add_argument("--customer-id", type=int, help="Customer ID to inspect")
    target.add_argument("--search", help="Customer email, name or code; the exact match is inspected")
    parser.add_argument("--replies-only", action="store_true", help="Omit campaign views and link clicks")
    add_verbose_argument(parser)
    return parser.parse_args(argv)


def resolve_customer_id(client: ListmonkClient, customer_id: int | None, search: str | None) -> int:
    if customer_id is not None:
        return customer_id
    results = client.query_customers(search or "", per_page="all")
    if not results:
        raise ValueError(f"no customer matched {search!r}")
    if len(results) > 1:
        raise ValueError(f"{len(results)} customers matched {search!r}; pass --customer-id instead")
    return int(results[0]["id"])


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    client = ListmonkClient(args.base_url, args.bearer_token, organization_id=args.organization_id)
    try:
        customer_id = resolve_customer_id(client, args.customer_id, args.search)
        log(f"Fetching activity for customer {customer_id}", enabled=args.verbose)
        activity = client.get_customer_activity(customer_id)
        events = activity.get("reply_ai_events") or []
        out: dict[str, object] = {
            "customer_id": customer_id,
            "reply_ai_events": events,
            "reply_ai_summary": summarize_replies(events),
        }
        if not args.replies_only:
            out["campaign_views"] = activity.get("campaign_views") or []
            out["link_clicks"] = activity.get("link_clicks") or []
        emit_json(out)
        return 0
    except Exception as exc:
        return emit_error(exc)


if __name__ == "__main__":
    raise SystemExit(main())
