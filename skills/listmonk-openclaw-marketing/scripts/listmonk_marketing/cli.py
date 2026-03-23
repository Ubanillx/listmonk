from __future__ import annotations

import argparse


def add_auth_arguments(parser: argparse.ArgumentParser) -> None:
    parser.add_argument("--base-url", required=True, help="Base listmonk URL, for example https://listmonk.example.com")
    parser.add_argument("--bearer-token", required=True, help="listmonk integration Bearer token")


def add_verbose_argument(parser: argparse.ArgumentParser) -> None:
    parser.add_argument("--verbose", action="store_true", help="Print progress messages to stderr")


def add_list_target_arguments(parser: argparse.ArgumentParser, *, required: bool = True) -> None:
    target = parser.add_mutually_exclusive_group(required=required)
    target.add_argument("--list-id", type=int, help="Existing list ID to reuse")
    target.add_argument("--list-name", help="List name to find or create")


def add_list_create_arguments(parser: argparse.ArgumentParser) -> None:
    parser.add_argument("--list-type", default="private", choices=["private", "public"], help="List type when creating a list")
    parser.add_argument("--list-optin", default="single", choices=["single", "double"], help="List opt-in mode when creating a list")
    parser.add_argument("--list-status", default="active", choices=["active", "archived"], help="List status when creating a list")
    parser.add_argument("--list-tags", default="", help="Comma-separated list tags used when creating a list")
    parser.add_argument("--list-description", default="", help="Optional description used when creating a list")


def add_subscriber_input_arguments(parser: argparse.ArgumentParser) -> None:
    subscriber_input = parser.add_mutually_exclusive_group(required=True)
    subscriber_input.add_argument("--subscribers-file", help="Path to a JSON file containing an array of subscribers")
    subscriber_input.add_argument("--excel-file", help="Path to a .xlsx file containing subscribers")

    parser.add_argument("--preconfirm-subscriptions", action="store_true", help="Preconfirm subscriber list memberships on create")
    parser.add_argument("--excel-sheet", default="", help="Excel sheet name or 1-based index; defaults to the first sheet")
    parser.add_argument("--email-column", default="", help="Excel e-mail column, by header name or column letter")
    parser.add_argument("--name-column", default="", help="Optional Excel name column, by header name or column letter")
    parser.add_argument("--header-row", type=int, default=1, help="Excel header row number, defaults to 1")
    parser.add_argument("--start-row", type=int, default=0, help="Excel data start row; defaults to header_row + 1")
    parser.add_argument("--skip-empty-rows", action="store_true", help="Skip fully empty Excel rows instead of reporting them")
    parser.add_argument(
        "--dedupe-by-email",
        action=argparse.BooleanOptionalAction,
        default=True,
        help="Deduplicate Excel rows by email before creating subscribers",
    )


def add_template_clone_arguments(parser: argparse.ArgumentParser) -> None:
    parser.add_argument("--source-template-id", type=int, help="Template ID to clone")
    parser.add_argument("--new-template-name", help="Name for the cloned template")
    parser.add_argument("--template-subject", default="", help="Optional replacement subject when cloning a tx template")


def add_source_campaign_arguments(parser: argparse.ArgumentParser) -> None:
    source = parser.add_mutually_exclusive_group(required=False)
    source.add_argument("--source-campaign-id", type=int, help="Existing campaign ID to reuse as a blueprint")
    source.add_argument("--source-campaign-name", help="Existing campaign name to reuse as a blueprint")


def add_campaign_arguments(parser: argparse.ArgumentParser) -> None:
    add_source_campaign_arguments(parser)
    parser.add_argument("--list-id", type=int, required=True, help="Existing list ID to use")
    parser.add_argument("--template-id", type=int, help="Template ID to use for the campaign, or inherit from a source campaign")
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
