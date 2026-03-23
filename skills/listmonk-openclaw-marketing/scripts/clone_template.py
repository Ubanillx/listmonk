#!/usr/bin/env python3
from __future__ import annotations

import argparse

from listmonk_marketing.cli import add_auth_arguments, add_template_clone_arguments, add_verbose_argument
from listmonk_marketing.client import ListmonkClient
from listmonk_marketing.common import emit_error, emit_json, log
from listmonk_marketing.templates import clone_template


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Clone a listmonk template with a Bearer token.")
    add_auth_arguments(parser)
    add_template_clone_arguments(parser)
    add_verbose_argument(parser)
    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    client = ListmonkClient(args.base_url, args.bearer_token)
    try:
        log("Cloning template", enabled=args.verbose)
        template = clone_template(
            client,
            source_template_id=args.source_template_id,
            new_template_name=args.new_template_name,
            template_subject=args.template_subject,
        )
        emit_json({"template_id": int(template["id"]), "template": template})
        return 0
    except Exception as exc:
        return emit_error(exc)


if __name__ == "__main__":
    raise SystemExit(main())

