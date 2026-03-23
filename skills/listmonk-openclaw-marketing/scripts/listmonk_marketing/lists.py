from __future__ import annotations

from typing import Any

from listmonk_marketing.client import ListmonkClient
from listmonk_marketing.common import normalize_tags


def find_or_create_list(
    client: ListmonkClient,
    *,
    list_id: int | None = None,
    list_name: str = "",
    list_type: str = "private",
    list_optin: str = "single",
    list_status: str = "active",
    list_tags: str | list[str] = "",
    list_description: str = "",
) -> dict[str, Any]:
    if list_id:
        return client.get_list(list_id)

    if not list_name:
        raise ValueError("list_name is required when list_id is not provided")

    lists = client.query_lists(list_name)
    for item in lists:
        if item.get("name") == list_name:
            return item

    try:
        return client.create_list(
            name=list_name,
            list_type=list_type,
            optin=list_optin,
            status=list_status,
            tags=normalize_tags(list_tags),
            description=list_description,
        )
    except Exception:
        lists = client.query_lists(list_name)
        for item in lists:
            if item.get("name") == list_name:
                return item
        raise

