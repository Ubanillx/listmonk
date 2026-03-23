#!/usr/bin/env python3
from __future__ import annotations

import argparse

from listmonk_marketing.cli import (
    add_auth_arguments,
    add_list_create_arguments,
    add_list_target_arguments,
    add_subscriber_input_arguments,
    add_verbose_argument,
)
from listmonk_marketing.client import ListmonkClient
from listmonk_marketing.common import emit_error, emit_json, log
from listmonk_marketing.lists import find_or_create_list
from listmonk_marketing.subscribers import create_subscribers_if_needed


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Import subscribers into listmonk with a Bearer token.")
    add_auth_arguments(parser)
    add_list_target_arguments(parser, required=True)
    add_list_create_arguments(parser)
    add_subscriber_input_arguments(parser)
    add_verbose_argument(parser)
    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    client = ListmonkClient(args.base_url, args.bearer_token)
    progress: dict[str, object] = {}
    try:
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
        result = create_subscribers_if_needed(
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
        result["list_id"] = list_id
        result["list"] = list_obj
        emit_json(result)
        return 0
    except Exception as exc:
        return emit_error(exc, progress=progress)


if __name__ == "__main__":
    raise SystemExit(main())

