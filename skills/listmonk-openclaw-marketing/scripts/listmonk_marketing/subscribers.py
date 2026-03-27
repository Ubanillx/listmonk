from __future__ import annotations

import csv
import json
import re
import tempfile
import time
from pathlib import Path
from typing import Any

from listmonk_marketing.client import APIError, ListmonkClient
from listmonk_marketing.common import load_json_file
from listmonk_marketing.excel import parse_excel_subscribers

IMPORT_TIMEOUT_SECONDS = 3600
IMPORT_POLL_INTERVAL_SECONDS = 0.25
IMPORT_DONE_STATUSES = {"finished", "failed", "stopped", "none"}
SKIP_LINE_COLON_RE = re.compile(r"skipping line (?P<line>\d+): (?P<reason>.+?)(?:: \[.*\])?$", re.IGNORECASE)
SKIP_LINE_DOT_RE = re.compile(r"skipping line (?P<line>\d+)\. (?P<reason>.+)$", re.IGNORECASE)


class BatchImportError(RuntimeError):
    def __init__(self, message: str, *, stats: dict[str, Any] | None = None, logs: str = "") -> None:
        super().__init__(message)
        self.data = {
            "import_status": stats or {},
            "import_logs": logs,
        }


def load_json_subscribers(subscribers_file: str) -> dict[str, Any]:
    subscribers = load_json_file(subscribers_file, list)
    rows = []
    for index, subscriber in enumerate(subscribers, start=1):
        if not isinstance(subscriber, dict):
            raise ValueError(f"Each subscriber entry must be a JSON object. Invalid item at index {index}")
        rows.append({"row": index, "subscriber": subscriber})

    return {
        "source": "json",
        "subscribers": rows,
        "skipped_rows": [],
        "failed_rows": [],
    }


def load_subscriber_source(
    *,
    subscribers_file: str = "",
    excel_file: str = "",
    excel_sheet: str = "",
    email_column: str = "",
    name_column: str = "",
    header_row: int = 1,
    start_row: int = 0,
    skip_empty_rows: bool = False,
    dedupe_by_email: bool = True,
) -> dict[str, Any]:
    if subscribers_file:
        return load_json_subscribers(subscribers_file)

    if not excel_file:
        raise ValueError("Either subscribers_file or excel_file is required")

    return parse_excel_subscribers(
        excel_file=excel_file,
        email_column=email_column,
        name_column=name_column,
        excel_sheet=excel_sheet,
        header_row=header_row,
        start_row=start_row,
        skip_empty_rows=skip_empty_rows,
        dedupe_by_email=dedupe_by_email,
    )


def reuse_existing_subscriber(
    client: ListmonkClient,
    *,
    subscriber: dict[str, Any],
    list_id: int,
    preconfirm_subscriptions: bool,
) -> dict[str, Any]:
    email = str(subscriber.get("email", "")).strip()
    matches = client.query_subscribers(email)
    match = next((item for item in matches if str(item.get("email", "")).casefold() == email.casefold()), None)
    if match is None:
        raise APIError(409, "subscriber already exists but could not be queried back")

    desired_status = "confirmed" if preconfirm_subscriptions else "unconfirmed"
    lists = match.get("lists", [])
    current = next((item for item in lists if item.get("id") == list_id), None)
    current_status = current.get("subscription_status") if current else None
    if current is None or current_status != desired_status:
        client.manage_subscriber_lists([int(match["id"])], [list_id], desired_status)
        refreshed = client.query_subscribers(email)
        updated = next((item for item in refreshed if str(item.get("email", "")).casefold() == email.casefold()), None)
        if updated is not None:
            match = updated

    return match


def ensure_batch_compatible_subscriber(subscriber: dict[str, Any], *, row: int, list_id: int) -> None:
    raw_lists = subscriber.get("lists")
    if raw_lists in (None, "", []):
        raw_lists = []
    if raw_lists and not isinstance(raw_lists, list):
        raise ValueError(f"Subscriber row {row} has an unsupported 'lists' value for batch import")

    extra_lists = [value for value in raw_lists if str(value).strip() and str(value).strip() != str(list_id)]
    if extra_lists:
        raise ValueError(
            f"Subscriber row {row} targets additional lists {extra_lists}, but batch import only supports the selected list {list_id}"
        )

    status = str(subscriber.get("status", "")).strip()
    if status and status != "enabled":
        raise ValueError(
            f"Subscriber row {row} has status '{status}', but batch import only supports the default enabled status"
        )


def write_batch_import_csv(source_rows: list[dict[str, Any]], *, list_id: int) -> tuple[str, dict[int, int]]:
    handle = tempfile.NamedTemporaryFile("w", suffix=".csv", delete=False, encoding="utf-8", newline="")
    line_map: dict[int, int] = {}
    try:
        writer = csv.writer(handle)
        for line_number, item in enumerate(source_rows, start=1):
            row = int(item["row"])
            subscriber = item["subscriber"]
            if not isinstance(subscriber, dict):
                raise ValueError(f"Subscriber row {row} must be an object")

            ensure_batch_compatible_subscriber(subscriber, row=row, list_id=list_id)

            email = "" if subscriber.get("email") is None else str(subscriber.get("email")).strip()
            name = "" if subscriber.get("name") is None else str(subscriber.get("name")).strip()
            attribs = subscriber.get("attribs")
            attribs_json = ""
            if attribs not in (None, "", {}):
                attribs_json = json.dumps(attribs, ensure_ascii=False, separators=(",", ":"))

            writer.writerow([email, name, attribs_json])
            line_map[line_number] = row
    finally:
        handle.close()

    return handle.name, line_map


def build_batch_import_params(*, list_id: int, preconfirm_subscriptions: bool) -> dict[str, Any]:
    return {
        "mode": "subscribe",
        "subscription_status": "confirmed" if preconfirm_subscriptions else "unconfirmed",
        "delim": ",",
        "lists": [list_id],
        "overwrite_userinfo": False,
        "overwrite_subscription_status": True,
        "field_map": {
            "email": "A",
            "name": "B",
            "attributes": "C",
        },
    }


def wait_for_batch_import(
    client: ListmonkClient,
    *,
    timeout_seconds: float = IMPORT_TIMEOUT_SECONDS,
    poll_interval_seconds: float = IMPORT_POLL_INTERVAL_SECONDS,
) -> tuple[dict[str, Any], str]:
    deadline = time.monotonic() + timeout_seconds
    last_stats: dict[str, Any] = {}

    while time.monotonic() <= deadline:
        stats = client.get_subscriber_import_status()
        last_stats = stats
        status = str(stats.get("status", "")).lower()
        if status in IMPORT_DONE_STATUSES:
            logs = client.get_subscriber_import_logs()
            if status != "finished":
                raise BatchImportError(f"Subscriber import ended with status '{status}'", stats=stats, logs=logs)
            return stats, logs
        time.sleep(poll_interval_seconds)

    logs = client.get_subscriber_import_logs()
    raise BatchImportError(
        f"Subscriber import did not finish within {int(timeout_seconds)} seconds",
        stats=last_stats,
        logs=logs,
    )


def parse_import_failed_rows(logs: str, *, line_map: dict[int, int]) -> list[dict[str, Any]]:
    failed_rows: list[dict[str, Any]] = []
    seen: set[tuple[int, str]] = set()

    for line in logs.splitlines():
        match = SKIP_LINE_COLON_RE.search(line) or SKIP_LINE_DOT_RE.search(line)
        if match is None:
            continue

        import_row = int(match.group("line"))
        source_row = line_map.get(import_row, import_row)
        reason = match.group("reason").strip()
        key = (source_row, reason)
        if key in seen:
            continue
        seen.add(key)
        failed_rows.append({"row": source_row, "reason": reason})

    return failed_rows


def create_subscribers_via_batch_import(
    client: ListmonkClient,
    *,
    list_id: int,
    parsed: dict[str, Any],
    preconfirm_subscriptions: bool,
) -> dict[str, Any]:
    source_rows = list(parsed["subscribers"])
    skipped_rows = list(parsed["skipped_rows"])
    failed_rows = list(parsed["failed_rows"])

    if not source_rows:
        return {
            "import_source": parsed["source"],
            "created_subscribers": [],
            "imported_count": 0,
            "skipped_rows": skipped_rows,
            "failed_rows": failed_rows,
            "import_status": {"status": "finished", "total": 0, "imported": 0},
            "import_logs": "",
        }

    csv_path, line_map = write_batch_import_csv(source_rows, list_id=list_id)
    try:
        client.start_subscriber_import(
            file_path=csv_path,
            filename=Path(csv_path).name,
            params=build_batch_import_params(
                list_id=list_id,
                preconfirm_subscriptions=preconfirm_subscriptions,
            ),
        )
        stats, logs = wait_for_batch_import(client)
    finally:
        Path(csv_path).unlink(missing_ok=True)

    failed_rows.extend(parse_import_failed_rows(logs, line_map=line_map))
    return {
        "import_source": parsed["source"],
        "created_subscribers": [],
        "imported_count": int(stats.get("imported", 0)),
        "skipped_rows": skipped_rows,
        "failed_rows": failed_rows,
        "import_status": stats,
        "import_logs": logs,
    }


def create_subscribers_if_needed(
    client: ListmonkClient,
    *,
    list_id: int,
    subscribers_file: str = "",
    excel_file: str = "",
    preconfirm_subscriptions: bool = False,
    excel_sheet: str = "",
    email_column: str = "",
    name_column: str = "",
    header_row: int = 1,
    start_row: int = 0,
    skip_empty_rows: bool = False,
    dedupe_by_email: bool = True,
) -> dict[str, Any]:
    parsed = load_subscriber_source(
        subscribers_file=subscribers_file,
        excel_file=excel_file,
        excel_sheet=excel_sheet,
        email_column=email_column,
        name_column=name_column,
        header_row=header_row,
        start_row=start_row,
        skip_empty_rows=skip_empty_rows,
        dedupe_by_email=dedupe_by_email,
    )
    source = parsed["source"]
    source_rows = parsed["subscribers"]
    skipped_rows = list(parsed["skipped_rows"])
    failed_rows = list(parsed["failed_rows"])
    created = []

    if hasattr(client, "start_subscriber_import") and hasattr(client, "get_subscriber_import_status"):
        return create_subscribers_via_batch_import(
            client,
            list_id=list_id,
            parsed=parsed,
            preconfirm_subscriptions=preconfirm_subscriptions,
        )

    if source == "json":
        for item in source_rows:
            index = int(item["row"])
            subscriber = item["subscriber"]
            try:
                created.append(client.create_subscriber(subscriber, list_id, preconfirm_subscriptions))
            except APIError as exc:
                if exc.status == 409 and subscriber.get("email"):
                    try:
                        created.append(
                            reuse_existing_subscriber(
                                client,
                                subscriber=subscriber,
                                list_id=list_id,
                                preconfirm_subscriptions=preconfirm_subscriptions,
                            )
                        )
                        continue
                    except APIError as lookup_exc:
                        exc = lookup_exc
                failed_rows.append(
                    {
                        "row": index,
                        "email": subscriber.get("email"),
                        "reason": exc.message,
                        "status": exc.status,
                    }
                )

        return {
            "import_source": source,
            "created_subscribers": created,
            "imported_count": len(created),
            "skipped_rows": skipped_rows,
            "failed_rows": failed_rows,
        }

    for item in source_rows:
        row = int(item["row"])
        subscriber = item["subscriber"]
        try:
            created.append(client.create_subscriber(subscriber, list_id, preconfirm_subscriptions))
        except APIError as exc:
            if exc.status == 409 and subscriber.get("email"):
                try:
                    created.append(
                        reuse_existing_subscriber(
                            client,
                            subscriber=subscriber,
                            list_id=list_id,
                            preconfirm_subscriptions=preconfirm_subscriptions,
                        )
                    )
                    continue
                except APIError as lookup_exc:
                    exc = lookup_exc
            failed_rows.append(
                {
                    "row": row,
                    "email": subscriber.get("email"),
                    "reason": exc.message,
                    "status": exc.status,
                }
            )

    return {
        "import_source": source,
        "created_subscribers": created,
        "imported_count": len(created),
        "skipped_rows": skipped_rows,
        "failed_rows": failed_rows,
    }
