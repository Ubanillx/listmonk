from __future__ import annotations

import re
from pathlib import Path
from typing import Any
from urllib.parse import urlsplit

from listmonk_marketing.client import ListmonkClient
from listmonk_marketing.common import log

MEDIA_TOKEN_RE = re.compile(r"\{\{\s*media\s*:\s*([^{}]*?)\s*\}\}")
MEDIA_TOKEN_PREFIX_RE = re.compile(r"\{\{\s*media\s*:")


def upload_media_files(
    client: ListmonkClient,
    paths: list[str] | None,
    *,
    folder_id: int | None = None,
    visibility: str | None = None,
    verbose: bool = False,
    progress: dict[str, object] | None = None,
) -> list[dict[str, Any]]:
    """Upload files once each and keep the local name for token matching.

    When ``progress`` is supplied, successfully uploaded media is recorded
    there immediately so a later failure still exposes reusable IDs.
    """
    unique_paths: list[Path] = []
    seen_paths: set[Path] = set()
    for raw_path in paths or []:
        path = Path(raw_path)
        if not path.is_file():
            raise ValueError(f"Media file not found: {raw_path}")
        resolved = path.resolve()
        if resolved in seen_paths:
            continue
        seen_paths.add(resolved)
        unique_paths.append(path)

    names: set[str] = set()
    for path in unique_paths:
        if path.name in names:
            raise ValueError(f"Multiple uploaded files share the name {path.name}; tokens would be ambiguous")
        names.add(path.name)

    uploaded: list[dict[str, Any]] = []
    if progress is not None:
        progress["media"] = uploaded
        progress["media_ids"] = []
    for path in unique_paths:
        log(f"Uploading media {path.name}", enabled=verbose)
        item = client.upload_media(str(path), folder_id=folder_id, visibility=visibility)
        if not isinstance(item, dict) or not item.get("id"):
            raise ValueError(f"Media upload did not return an id for {path}")
        record = dict(item)
        record["local_filename"] = path.name
        uploaded.append(record)
        if progress is not None:
            progress["media"] = uploaded
            progress["media_ids"] = [int(entry["id"]) for entry in uploaded]
    return uploaded


def merge_media_ids(explicit: list[int] | None, uploaded: list[dict[str, Any]]) -> list[int]:
    """Merge explicit IDs and uploaded IDs, preserving order and dropping duplicates."""
    ids: list[int] = []
    seen: set[int] = set()
    for raw in explicit or []:
        if isinstance(raw, bool) or not isinstance(raw, int) or raw < 1:
            raise ValueError("--media-id must be a positive integer")
        if raw not in seen:
            seen.add(raw)
            ids.append(raw)
    for item in uploaded:
        media_id = int(item["id"])
        if media_id not in seen:
            seen.add(media_id)
            ids.append(media_id)
    return ids


def validate_media_tokens(body: str | None, local_names: list[str] | None) -> None:
    """Check tokens against the files about to be uploaded, before any upload."""
    if body is None:
        return
    names = {Path(name).name for name in local_names or []}
    missing: list[str] = []
    empty = False
    for match in MEDIA_TOKEN_RE.finditer(body):
        name = match.group(1).strip()
        if not name:
            empty = True
        elif name not in names:
            missing.append(name)
    if empty:
        raise ValueError("Empty media token; use {{media:filename}}")
    if missing:
        names_text = ", ".join(sorted(set(missing)))
        raise ValueError(
            f"Unknown media token(s): {names_text}. Pass each file with --media-file in this invocation."
        )
    if MEDIA_TOKEN_PREFIX_RE.search(MEDIA_TOKEN_RE.sub("", body)):
        raise ValueError("Unresolved or malformed media token remains in the campaign body")


def apply_media_tokens(body: str | None, uploaded: list[dict[str, Any]]) -> str | None:
    """Replace {{media:filename}} with the uploaded media URL.

    Server filenames (after whitespace sanitization/suffixes) and the original
    local filenames both resolve. Unknown or empty tokens fail the request.
    """
    if body is None:
        return None
    by_server: dict[str, dict[str, Any]] = {}
    by_local: dict[str, dict[str, Any]] = {}
    for item in uploaded:
        server_name = str(item.get("filename") or "")
        local_name = str(item.get("local_filename") or "")
        if server_name:
            if server_name in by_server:
                raise ValueError(f"Multiple uploaded files share the name {server_name}; tokens are ambiguous")
            by_server[server_name] = item
        if local_name:
            if local_name in by_local:
                raise ValueError(f"Multiple uploaded files share the name {local_name}; tokens are ambiguous")
            by_local[local_name] = item

    def repl(match: re.Match[str]) -> str:
        name = match.group(1).strip()
        if not name:
            raise ValueError("Empty media token; use {{media:filename}}")
        item = by_server.get(name) or by_local.get(name)
        if item is None:
            raise ValueError(f"Unknown media token: {name}")
        url = str(item.get("url") or "")
        if not url:
            raise ValueError(f"Uploaded media {name} has no URL")
        parts = urlsplit(url)
        safe = (
            parts.scheme in ("http", "https") and bool(parts.netloc)
        ) or (
            not parts.scheme
            and not parts.netloc
            and url.startswith("/")
            and not url.startswith("//")
            and "\\" not in url
            and not any(ord(char) < 0x20 for char in url)
        )
        if not safe:
            raise ValueError(f"Uploaded media {name} returned an unsafe URL")
        return url

    out = MEDIA_TOKEN_RE.sub(repl, body)
    if MEDIA_TOKEN_PREFIX_RE.search(out):
        raise ValueError("Unresolved or malformed media token remains in the campaign body")
    return out
