from __future__ import annotations

import sys
import unittest
from pathlib import Path

SCRIPTS_DIR = Path(__file__).resolve().parents[1] / "scripts"
if str(SCRIPTS_DIR) not in sys.path:
    sys.path.insert(0, str(SCRIPTS_DIR))

from listmonk_marketing.client import APIError
from listmonk_marketing.lists import find_or_create_list


class CustomerListClient:
    def __init__(self) -> None:
        self.created_payloads: list[dict[str, object]] = []
        self.queries = 0

    def get_list(self, customer_list_id: int) -> dict[str, object]:
        if customer_list_id == 99:
            return {"id": customer_list_id, "name": "Pool", "type": "pool"}
        return {"id": customer_list_id, "name": "Existing", "type": "private"}

    def query_lists(self, query: str) -> list[dict[str, object]]:
        self.queries += 1
        if query == "Found":
            return [{"id": 7, "name": "Found"}]
        if query == "Retryable" and self.queries > 1:
            return [{"id": 8, "name": "Retryable"}]
        return []

    def create_list(
        self,
        *,
        name: str,
        list_type: str,
        optin: str,
        status: str,
        tags: list[str],
        description: str,
    ) -> dict[str, object]:
        payload = {
            "name": name,
            "type": list_type,
            "optin": optin,
            "status": status,
            "tags": tags,
            "description": description,
        }
        self.created_payloads.append(payload)
        if name == "Retryable":
            raise APIError(409, "exists")
        return {"id": 9, **payload}


class CustomerListResolutionTests(unittest.TestCase):
    def test_find_or_create_reuses_customer_list_id(self) -> None:
        client = CustomerListClient()
        result = find_or_create_list(client, customer_list_id=12)
        self.assertEqual(result["id"], 12)

    def test_find_or_create_rejects_public_pool_list(self) -> None:
        client = CustomerListClient()
        with self.assertRaisesRegex(ValueError, "pool"):
            find_or_create_list(client, customer_list_id=99)

    def test_find_or_create_reuses_exact_name_match(self) -> None:
        client = CustomerListClient()
        result = find_or_create_list(client, customer_list_name="Found")
        self.assertEqual(result["id"], 7)
        self.assertEqual(client.created_payloads, [])

    def test_find_or_create_creates_when_missing(self) -> None:
        client = CustomerListClient()
        result = find_or_create_list(
            client,
            customer_list_name="Created",
            list_type="private",
            list_optin="single",
            list_status="active",
            list_tags="foo, bar",
            list_description="desc",
        )
        self.assertEqual(result["id"], 9)
        self.assertEqual(client.created_payloads[0]["tags"], ["foo", "bar"])

    def test_find_or_create_requeries_after_create_failure(self) -> None:
        client = CustomerListClient()
        result = find_or_create_list(client, customer_list_name="Retryable")
        self.assertEqual(result["id"], 8)


if __name__ == "__main__":
    unittest.main()
