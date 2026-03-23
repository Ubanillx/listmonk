from __future__ import annotations

from typing import Any

from listmonk_marketing.client import APIError, ListmonkClient
from listmonk_marketing.common import load_json_file
from listmonk_marketing.excel import parse_excel_subscribers


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
        return {
            "source": "json",
            "subscribers": load_json_file(subscribers_file, list),
            "skipped_rows": [],
            "failed_rows": [],
        }

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

    if source == "json":
        for index, subscriber in enumerate(source_rows, start=1):
            if not isinstance(subscriber, dict):
                raise ValueError(f"Each subscriber entry must be a JSON object. Invalid item at index {index}")
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

