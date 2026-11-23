#!/usr/bin/env python3
"""Read-only template check for campaign content.

Two independent checks, no writes and no sending:

1. Local lint: scans `{{ ... }}` actions for root fields other than
   `.Customer` / `.Campaign`. Upstream listmonk examples use `.Subscriber.*`,
   which no longer exists in this deployment and fails at render time.
2. Server render: calls POST /api/campaigns/{id}/preview (draft-safe, sends nothing)
   and returns the exact template error with line and column when rendering fails.

Exit code 0 when both checks pass, 1 on a lint or render failure.
"""

from __future__ import annotations

import argparse
from pathlib import Path

from listmonk_marketing.campaigns import (
    find_unsupported_template_fields,
    validate_campaign_body_vars,
)
from listmonk_marketing.cli import add_auth_arguments, add_verbose_argument
from listmonk_marketing.client import APIError, ListmonkClient
from listmonk_marketing.common import emit_error, emit_json, log


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Lint and render-check a campaign body without sending anything."
    )
    add_auth_arguments(parser)
    parser.add_argument("--campaign-id", type=int, required=True,
                        help="Existing campaign whose template/content_type/context is reused")
    parser.add_argument("--body-file", default=None,
                        help="Check this body instead of the saved campaign body (UTF-8)")
    parser.add_argument("--body", default=None,
                        help="Inline body to check instead of the saved campaign body")
    parser.add_argument("--preview-name", default="Preview Customer",
                        help="Name used for the preview render")
    parser.add_argument("--skip-render", action="store_true",
                        help="Only run the local lint, skip the server-side preview")
    add_verbose_argument(parser)
    return parser.parse_args(argv)


def run_check(args: argparse.Namespace, client: ListmonkClient) -> dict[str, object]:
    campaign = client.get_campaign(args.campaign_id)

    body = args.body
    body_source = "argument"
    if args.body_file:
        body = Path(args.body_file).read_text(encoding="utf-8-sig")
        body_source = args.body_file
    if body is None:
        body = campaign.get("body") or ""
        body_source = "campaign"

    out: dict[str, object] = {
        "campaign_id": int(campaign["id"]),
        "campaign_name": campaign.get("name"),
        "campaign_status": campaign.get("status"),
        "body_source": body_source,
        "content_type": campaign.get("content_type"),
        "template_id": campaign.get("template_id"),
    }

    findings = find_unsupported_template_fields(body)
    out["lint"] = {"ok": not findings, "findings": findings}
    if findings:
        try:
            validate_campaign_body_vars(body)
        except ValueError as exc:
            out["lint"]["message"] = str(exc)

    if not args.skip_render:
        log("Rendering preview", enabled=args.verbose)
        try:
            client.preview_campaign(
                int(campaign["id"]),
                body=body,
                content_type=str(campaign.get("content_type") or "html"),
                template_id=campaign.get("template_id"),
                preview_name=args.preview_name,
            )
            out["render"] = {"ok": True}
        except APIError as exc:
            out["render"] = {"ok": False, "status": exc.status, "message": str(exc)}

    out["ok"] = bool(out["lint"]["ok"]) and bool(out.get("render", {}).get("ok", True))
    return out


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    client = ListmonkClient(args.base_url, args.bearer_token, organization_id=args.organization_id)
    try:
        result = run_check(args, client)
    except Exception as exc:  # noqa: BLE001 - surface API and IO errors as JSON
        return emit_error(exc)
    emit_json(result)
    return 0 if result.get("ok") else 1


if __name__ == "__main__":
    raise SystemExit(main())
