#!/usr/bin/env python3
"""Upload images or attachments to the workspace media library."""

from __future__ import annotations

import argparse

from listmonk_marketing.cli import add_auth_arguments, add_verbose_argument
from listmonk_marketing.client import ListmonkClient
from listmonk_marketing.common import emit_error, emit_json
from listmonk_marketing.media import upload_media_files


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Upload images or attachments and print their media IDs and URLs.")
    add_auth_arguments(parser)
    parser.add_argument(
        "--file",
        action="append",
        required=True,
        dest="files",
        help="Local image or attachment to upload; pass multiple times",
    )
    parser.add_argument("--folder-id", type=int, help="Optional media folder ID; omit or 0 for the root")
    parser.add_argument(
        "--visibility",
        choices=["private", "organization"],
        help="Media visibility; organization requires an organization workspace",
    )
    add_verbose_argument(parser)
    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    client = ListmonkClient(args.base_url, args.bearer_token, organization_id=args.organization_id)
    progress: dict[str, object] = {}
    try:
        uploaded = upload_media_files(
            client,
            args.files,
            folder_id=args.folder_id,
            visibility=args.visibility,
            verbose=args.verbose,
            progress=progress,
        )
        emit_json(
            {
                "media": uploaded,
                "media_ids": [int(item["id"]) for item in uploaded],
            }
        )
        return 0
    except Exception as exc:
        return emit_error(exc, progress=progress)


if __name__ == "__main__":
    raise SystemExit(main())
