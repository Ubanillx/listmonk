from __future__ import annotations

from typing import Any

from listmonk_marketing.client import ListmonkClient


def clone_template(
    client: ListmonkClient,
    *,
    source_template_id: int,
    new_template_name: str,
    template_subject: str = "",
) -> dict[str, Any]:
    return client.clone_template(source_template_id, new_template_name, template_subject or None)

