#!/usr/bin/env python3
from __future__ import annotations

import argparse

from listmonk_marketing.cli import (
    add_auth_arguments,
    add_list_create_arguments,
    add_list_target_arguments,
    add_verbose_argument,
)
from listmonk_marketing.client import ListmonkClient
from listmonk_marketing.common import emit_error, emit_json, log
from listmonk_marketing.lists import find_or_create_list


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Find or create a listmonk list with a Bearer token.")
    add_auth_arguments(parser)
    add_list_target_arguments(parser, required=True)
    add_list_create_arguments(parser)
    add_verbose_argument(parser)
    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    client = ListmonkClient(args.base_url, args.bearer_token)
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
        emit_json({"list_id": int(list_obj["id"]), "list": list_obj})
        return 0
    except Exception as exc:
        return emit_error(exc)


if __name__ == "__main__":
    raise SystemExit(main())

