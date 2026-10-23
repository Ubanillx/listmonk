from __future__ import annotations

import json
import sys
import unittest
from pathlib import Path
from unittest.mock import patch
from urllib.parse import parse_qs, urlparse

SCRIPTS_DIR = Path(__file__).resolve().parents[1] / "scripts"
if str(SCRIPTS_DIR) not in sys.path:
    sys.path.insert(0, str(SCRIPTS_DIR))

from listmonk_marketing.client import ListmonkClient
from inspect_customer_replies import summarize_replies


class DeliveryInspectionTests(unittest.TestCase):
    def setUp(self) -> None:
        self.client = ListmonkClient("https://listmonk.example", "test-token")

    def test_delivery_overview_uses_analytics_endpoint(self) -> None:
        with patch.object(self.client, "request", return_value={}) as request:
            self.client.get_delivery_overview(41)
        self.assertEqual(request.call_args.args, ("GET", "/api/campaigns/41/delivery-overview"))

    def test_send_errors_omit_empty_filters_but_keep_pagination(self) -> None:
        with patch.object(self.client, "request", return_value={}) as request:
            self.client.get_campaign_send_errors(41, page=2, per_page=25)
        self.assertEqual(request.call_args.args, ("GET", "/api/campaigns/41/send-errors"))
        self.assertEqual(request.call_args.kwargs["params"], {"page": 2, "per_page": 25})

    def test_send_errors_forward_search_category_and_organization(self) -> None:
        with patch.object(self.client, "request", return_value={}) as request:
            self.client.get_campaign_send_errors(41, search="张", category="smtp_rejected", organization_id=9)
        params = request.call_args.kwargs["params"]
        self.assertEqual(params["search"], "张")
        self.assertEqual(params["category"], "smtp_rejected")
        self.assertEqual(params["organization_id"], 9)

    def test_failed_recipients_use_the_retry_candidate_endpoint(self) -> None:
        with patch.object(self.client, "request", return_value={}) as request:
            self.client.get_campaign_failed_recipients(41, category="media")
        self.assertEqual(request.call_args.args, ("GET", "/api/campaigns/41/failed-recipients"))
        self.assertEqual(request.call_args.kwargs["params"]["category"], "media")

    def test_delivery_endpoints_serialize_ids_into_the_path(self) -> None:
        with patch("urllib.request.urlopen") as transport:
            response = transport.return_value.__enter__.return_value
            response.headers = {"Content-Type": "application/json"}
            response.read.return_value = b'{"data":{"sent":3}}'
            self.client.get_delivery_overview(77)
        request = transport.call_args.args[0]
        self.assertEqual(urlparse(request.full_url).path, "/api/campaigns/77/delivery-overview")


class ReplyAITests(unittest.TestCase):
    def setUp(self) -> None:
        self.client = ListmonkClient("https://listmonk.example", "test-token")

    def test_customer_activity_is_read_with_the_business_prefix(self) -> None:
        with patch.object(self.client, "request", return_value={}) as request:
            self.client.get_customer_activity(1234)
        self.assertEqual(request.call_args.args, ("GET", "/api/customers/1234/activity"))

    def test_summary_counts_intent_action_and_status(self) -> None:
        events = [
            {"intent": "unsubscribe", "action": "blocklisted", "status": "processed"},
            {"intent": "unsubscribe", "action": "blocklisted", "status": "processed"},
            {"intent": "complaint", "action": "ignored", "status": "processed"},
            {"intent": "other", "action": "ignored", "status": "failed"},
        ]
        summary = summarize_replies(events)
        self.assertEqual(summary["total"], 4)
        self.assertEqual(summary["by_intent"]["unsubscribe"], 2)
        self.assertEqual(summary["by_action"]["blocklisted"], 2)
        self.assertEqual(summary["by_status"]["failed"], 1)
        self.assertEqual(summary["blocklisted"], 2)

    def test_summary_does_not_invent_missing_fields(self) -> None:
        summary = summarize_replies([{}])
        self.assertEqual(summary["by_intent"], {"unknown": 1})
        self.assertEqual(summary["blocklisted"], 0)

    def test_summary_handles_an_empty_event_list(self) -> None:
        summary = summarize_replies([])
        self.assertEqual(summary, {"total": 0, "by_intent": {}, "by_action": {}, "by_status": {}, "blocklisted": 0})


if __name__ == "__main__":
    unittest.main()
