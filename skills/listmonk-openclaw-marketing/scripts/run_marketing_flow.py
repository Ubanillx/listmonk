#!/usr/bin/env python3
"""Drive a listmonk marketing workflow with a Bearer integration token."""

from __future__ import annotations

import argparse
from typing import Any
from listmonk_marketing.campaigns import (
    build_campaign_payload,
    create_campaign,
    get_source_campaign,
    update_campaign_status,
)
from listmonk_marketing.cli import (
    add_auth_arguments,
    add_list_create_arguments,
    add_list_target_arguments,
    add_source_campaign_arguments,
    add_subscriber_input_arguments,
    add_template_clone_arguments,
    add_verbose_argument,
)
from listmonk_marketing.client import ListmonkClient
from listmonk_marketing.common import emit_error, emit_json, log
from listmonk_marketing.lists import find_or_create_list
from listmonk_marketing.reports import fetch_campaign_reports
from listmonk_marketing.subscribers import create_subscribers_if_needed
from listmonk_marketing.templates import clone_template


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Run a listmonk marketing flow with a Bearer token.")
    add_auth_arguments(parser)
    add_list_target_arguments(parser, required=True)
    add_list_create_arguments(parser)
    add_subscriber_input_arguments(parser)
    add_source_campaign_arguments(parser)
    add_template_clone_arguments(parser)

    parser.add_argument("--campaign-name", required=True, help="Campaign name")
    parser.add_argument("--subject", help="Campaign subject, or inherit from a source campaign")
    parser.add_argument("--campaign-body", help="Campaign body, or inherit from a source campaign")
    parser.add_argument("--content-type", choices=["richtext", "html", "markdown", "plain", "visual"])
    parser.add_argument("--messenger", help="Messenger name, defaults to email unless inherited")
    parser.add_argument("--from-email", help="Optional campaign from_email, or inherit from a source campaign")
    parser.add_argument("--daily-send-limit", type=int, help="Daily send limit, or inherit from a source campaign")
    parser.add_argument("--daily-resume-time", help="Daily resume time in HH:MM local server time, or inherit")
    parser.add_argument("--send-at", default="", help="Optional RFC3339 send time for scheduling")
    parser.add_argument("--tag", action="append", default=None, help="Campaign tag; pass multiple times for multiple tags")
    parser.add_argument("--attribs-file", help="Path to a JSON object for campaign attribs")

    parser.add_argument("--auto-start", action="store_true", help="Start or schedule the campaign immediately after creation")
    parser.add_argument("--report-from", default="", help="Start date/time for report fetch")
    parser.add_argument("--report-to", default="", help="End date/time for report fetch")
    parser.add_argument("--recipient-per-page", type=int, default=100, help="Recipients page size when fetching recipient analytics")
    add_verbose_argument(parser)
    return parser.parse_args(argv)


def run_workflow(args: argparse.Namespace, *, client: ListmonkClient | None = None) -> dict[str, Any]:
    client = client or ListmonkClient(args.base_url, args.bearer_token)
    progress: dict[str, Any] = {}
    log("Validating Bearer token", enabled=args.verbose)
    client.validate_token()

    log("Resolving target list", enabled=args.verbose)
    list_obj = find_or_create_list(
        client,
        list_id=args.list_id,
        list_name=args.list_name or "",
        list_type=args.list_type,
        list_optin=args.list_optin,
        list_status=args.list_status,
        list_tags=args.list_tags,
        list_description=args.list_description,
    )
    list_id = int(list_obj["id"])
    progress["list_id"] = list_id
    progress["list"] = list_obj

    log("Importing subscribers", enabled=args.verbose)
    import_result = create_subscribers_if_needed(
        client,
        list_id=list_id,
        subscribers_file=args.subscribers_file or "",
        excel_file=args.excel_file or "",
        preconfirm_subscriptions=args.preconfirm_subscriptions,
        excel_sheet=args.excel_sheet,
        email_column=args.email_column,
        name_column=args.name_column,
        header_row=args.header_row,
        start_row=args.start_row,
        skip_empty_rows=args.skip_empty_rows,
        dedupe_by_email=args.dedupe_by_email,
    )
    progress.update(import_result)

    source_campaign = get_source_campaign(
        client,
        source_campaign_id=args.source_campaign_id,
        source_campaign_name=args.source_campaign_name or "",
    )
    if source_campaign:
        log("Resolving source campaign blueprint", enabled=args.verbose)
        progress["source_campaign_id"] = int(source_campaign["id"])
        progress["source_campaign"] = source_campaign
        source_template_id = source_campaign.get("template_id")
        if source_template_id is not None:
            progress["template_id"] = int(source_template_id)
    else:
        if args.source_template_id is None:
            raise ValueError(
                "One of --source-template-id or --source-campaign-id/--source-campaign-name is required"
            )
        if not args.new_template_name:
            raise ValueError("--new-template-name is required when using --source-template-id")

        log("Cloning template", enabled=args.verbose)
        template = clone_template(
            client,
            source_template_id=args.source_template_id,
            new_template_name=args.new_template_name,
            template_subject=args.template_subject,
        )
        progress["template_id"] = int(template["id"])
        progress["template"] = template

    log("Creating campaign", enabled=args.verbose)
    campaign = create_campaign(
        client,
        payload=build_campaign_payload(
            campaign_name=args.campaign_name,
            subject=args.subject,
            list_id=list_id,
            template_id=progress.get("template_id"),
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
        ),
    )
    progress["campaign_id"] = int(campaign["id"])
    progress["campaign"] = campaign

    status_result = None
    if args.auto_start:
        log("Starting or scheduling campaign", enabled=args.verbose)
        status_result = update_campaign_status(client, campaign_id=int(campaign["id"]), status="running")

    log("Fetching reports", enabled=args.verbose)
    reports = fetch_campaign_reports(
        client,
        campaign_id=int(campaign["id"]),
        report_from=args.report_from,
        report_to=args.report_to,
        recipient_per_page=args.recipient_per_page,
    )

    result = {
        "import_source": progress.get("import_source"),
        "list_id": list_id,
        "template_id": progress.get("template_id"),
        "campaign_id": int(campaign["id"]),
        "status": (status_result or campaign).get("status", "draft"),
        "list": list_obj,
        "campaign": campaign,
        "created_subscribers": progress.get("created_subscribers", []),
        "imported_count": progress.get("imported_count", 0),
        "skipped_rows": progress.get("skipped_rows", []),
        "failed_rows": progress.get("failed_rows", []),
    }
    if "template" in progress:
        result["template"] = progress["template"]
    if "source_campaign" in progress:
        result["source_campaign_id"] = progress["source_campaign_id"]
        result["source_campaign"] = progress["source_campaign"]
    if reports:
        result["reports"] = reports
        if "summary" in reports:
            result["report_summary"] = reports["summary"]

    return result


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    try:
        emit_json(run_workflow(args))
        return 0
    except Exception as exc:
        return emit_error(exc)


if __name__ == "__main__":
    raise SystemExit(main())
