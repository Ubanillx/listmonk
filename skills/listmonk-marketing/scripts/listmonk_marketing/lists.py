from __future__ import annotations

from typing import Any

from listmonk_marketing.client import ListmonkClient
from listmonk_marketing.common import normalize_tags

MARKETING_LIST_TYPES = {"private", "public"}


def validate_marketing_list(list_obj: dict[str, Any]) -> dict[str, Any]:
    list_type = str(list_obj.get("type", "")).strip()
    if list_type and list_type not in MARKETING_LIST_TYPES:
        raise ValueError(
            f"CustomerList type '{list_type}' is not supported by the marketing workflow; "
            "public pools must use the dedicated pool APIs"
        )
    return list_obj


def find_or_create_list(
    client: ListmonkClient,
    *,
    customer_list_id: int | None = None,
    customer_list_name: str = "",
    list_type: str = "private",
    list_optin: str = "single",
    list_status: str = "active",
    list_tags: str | list[str] = "",
    list_description: str = "",
) -> dict[str, Any]:
    if customer_list_id:
        return validate_marketing_list(client.get_list(customer_list_id))

    if not customer_list_name:
        raise ValueError("customer_list_name is required when customer_list_id is not provided")

    customer_lists = client.query_lists(customer_list_name)
    for item in customer_lists:
        if item.get("name") == customer_list_name:
            return validate_marketing_list(item)

    try:
        return client.create_list(
            name=customer_list_name,
            list_type=list_type,
            optin=list_optin,
            status=list_status,
            tags=normalize_tags(list_tags),
            description=list_description,
        )
    except Exception:
        customer_lists = client.query_lists(customer_list_name)
        for item in customer_lists:
            if item.get("name") == customer_list_name:
                return validate_marketing_list(item)
        raise
