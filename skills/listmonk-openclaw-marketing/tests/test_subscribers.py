from __future__ import annotations

import json
import sys
import tempfile
import unittest
from pathlib import Path

SCRIPTS_DIR = Path(__file__).resolve().parents[1] / "scripts"
if str(SCRIPTS_DIR) not in sys.path:
    sys.path.insert(0, str(SCRIPTS_DIR))

from listmonk_marketing.client import APIError
from listmonk_marketing.subscribers import create_subscribers_if_needed


class SubscriberClient:
    def __init__(self) -> None:
        self.manage_calls: list[tuple[list[int], list[int], str]] = []
        self.created: list[str] = []

    def create_subscriber(self, subscriber: dict[str, object], list_id: int, preconfirm: bool) -> dict[str, object]:
        email = str(subscriber.get("email"))
        if email == "exists@example.com":
            raise APIError(409, "exists")
        if email == "boom@example.com":
            raise APIError(500, "bad create")
        self.created.append(email)
        return {"id": len(self.created), "email": email, "list_id": list_id, "preconfirm": preconfirm}

    def query_subscribers(self, search: str, per_page: int | str = "all") -> list[dict[str, object]]:
        if search == "exists@example.com":
            return [
                {
                    "id": 41,
                    "email": "exists@example.com",
                    "lists": [{"id": 3, "subscription_status": "unconfirmed"}],
                }
            ]
        return []

    def manage_subscriber_lists(self, subscriber_ids: list[int], target_list_ids: list[int], status: str, action: str = "add") -> None:
        self.manage_calls.append((subscriber_ids, target_list_ids, status))


class SubscriberImportTests(unittest.TestCase):
    def make_json_file(self, payload: list[dict[str, object]]) -> str:
        handle = tempfile.NamedTemporaryFile(suffix=".json", delete=False)
        handle.write(json.dumps(payload).encode("utf-8"))
        handle.close()
        self.addCleanup(lambda: Path(handle.name).unlink(missing_ok=True))
        return handle.name

    def test_json_import_creates_subscribers(self) -> None:
        client = SubscriberClient()
        source = self.make_json_file([{"email": "new@example.com", "name": "New"}])

        result = create_subscribers_if_needed(
            client,
            list_id=3,
            subscribers_file=source,
            preconfirm_subscriptions=False,
        )

        self.assertEqual(result["import_source"], "json")
        self.assertEqual(result["imported_count"], 1)
        self.assertEqual(result["failed_rows"], [])

    def test_json_import_reuses_existing_subscriber_on_conflict(self) -> None:
        client = SubscriberClient()
        source = self.make_json_file([{"email": "exists@example.com"}])

        result = create_subscribers_if_needed(
            client,
            list_id=9,
            subscribers_file=source,
            preconfirm_subscriptions=True,
        )

        self.assertEqual(result["imported_count"], 1)
        self.assertEqual(client.manage_calls, [([41], [9], "confirmed")])

    def test_json_import_records_failed_rows(self) -> None:
        client = SubscriberClient()
        source = self.make_json_file([{"email": "boom@example.com"}])

        result = create_subscribers_if_needed(
            client,
            list_id=3,
            subscribers_file=source,
            preconfirm_subscriptions=False,
        )

        self.assertEqual(result["imported_count"], 0)
        self.assertEqual(result["failed_rows"][0]["email"], "boom@example.com")


class BatchImportClient:
    def __init__(self) -> None:
        self.started: list[dict[str, object]] = []
        self.status_calls = 0
        self.logs = "2026/03/27 10:00:00 importer.go:548: skipping line 2: email not found in row: [bad-row  ]"

    def start_subscriber_import(self, *, file_path: str, params: dict[str, object], filename: str = "") -> dict[str, object]:
        self.started.append(
            {
                "file_path": file_path,
                "filename": filename,
                "params": params,
                "content": Path(file_path).read_text(encoding="utf-8"),
            }
        )
        return {"status": "importing"}

    def get_subscriber_import_status(self) -> dict[str, object]:
        self.status_calls += 1
        return {"status": "finished", "total": 2, "imported": 1}

    def get_subscriber_import_logs(self) -> str:
        return self.logs


class BatchSubscriberImportTests(unittest.TestCase):
    def make_json_file(self, payload: list[dict[str, object]]) -> str:
        handle = tempfile.NamedTemporaryFile(suffix=".json", delete=False)
        handle.write(json.dumps(payload).encode("utf-8"))
        handle.close()
        self.addCleanup(lambda: Path(handle.name).unlink(missing_ok=True))
        return handle.name

    def test_json_import_uses_native_batch_import(self) -> None:
        client = BatchImportClient()
        source = self.make_json_file(
            [
                {"email": "good@example.com", "name": "Good", "attribs": {"city": "Shanghai"}},
                {"email": "", "name": "Bad"},
            ]
        )

        result = create_subscribers_if_needed(
            client,
            list_id=3,
            subscribers_file=source,
            preconfirm_subscriptions=True,
        )

        self.assertEqual(result["imported_count"], 1)
        self.assertEqual(result["created_subscribers"], [])
        self.assertEqual(result["failed_rows"], [{"row": 2, "reason": "email not found in row"}])
        self.assertEqual(client.started[0]["params"]["subscription_status"], "confirmed")
        self.assertEqual(client.started[0]["params"]["overwrite_userinfo"], False)
        self.assertEqual(client.started[0]["params"]["overwrite_subscription_status"], True)
        self.assertIn("good@example.com,Good,", client.started[0]["content"])
        self.assertIn('"{""city"":""Shanghai""}"', client.started[0]["content"])

    def test_batch_import_rejects_additional_lists(self) -> None:
        client = BatchImportClient()
        source = self.make_json_file([{"email": "good@example.com", "lists": [3, 9]}])

        with self.assertRaisesRegex(ValueError, "additional lists"):
            create_subscribers_if_needed(
                client,
                list_id=3,
                subscribers_file=source,
                preconfirm_subscriptions=False,
            )


if __name__ == "__main__":
    unittest.main()
