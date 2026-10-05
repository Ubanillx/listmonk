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

from listmonk_marketing.campaigns import build_campaign_payload
from listmonk_marketing.client import APIError, ListmonkClient
from listmonk_marketing.reports import fetch_campaign_reports


class APIContractTests(unittest.TestCase):
    def test_multi_campaign_requests_encode_repeated_ids_and_timezone(self) -> None:
        client = ListmonkClient("https://listmonk.example", "test-token")
        with patch("urllib.request.urlopen") as transport:
            response = transport.return_value.__enter__.return_value
            response.headers = {"Content-Type": "application/json"}
            response.read.return_value = b'{"data":{"sent":10}}'
            result = client.get_campaigns_report(
                "summary", "2026-10-05T00:00:00+08:00", "2026-10-05T15:00:00+08:00",
                campaign_ids=[31, 41],
            )
        request = transport.call_args.args[0]
        parsed = urlparse(request.full_url)
        query = parse_qs(parsed.query)
        self.assertEqual(parsed.path, "/api/campaigns/report/summary")
        self.assertEqual(query["id"], ["31", "41"])
        self.assertEqual(query["from"], ["2026-10-05T00:00:00+08:00"])
        self.assertEqual(request.get_header("Authorization"), "Bearer test-token")
        self.assertEqual(result, {"sent": 10})

    def test_all_campaigns_recipient_request_preserves_pagination(self) -> None:
        client = ListmonkClient("https://listmonk.example", "test-token")
        with patch.object(client, "request", return_value={}) as request:
            client.get_campaigns_report(
                "recipients", "2026-10-01", "2026-10-05", all_campaigns=True, page=3, per_page=20,
            )
        self.assertEqual(request.call_args.args, ("GET", "/api/campaigns/report/recipients"))
        self.assertEqual(request.call_args.kwargs["params"], {
            "from": "2026-10-01", "to": "2026-10-05", "all": "true", "page": 3, "per_page": 20,
        })

    def test_single_campaign_geo_and_recipient_page_use_current_endpoints(self) -> None:
        client = ListmonkClient("https://listmonk.example", "test-token")
        with patch.object(client, "request", return_value={}) as request:
            reports = fetch_campaign_reports(
                client, campaign_id=31, report_from="2026-10-01", report_to="2026-10-05",
                include_geo=True, recipient_page=2,
            )
        self.assertIn("geo", reports)
        paths = [call.args[1] for call in request.call_args_list]
        self.assertIn("/api/campaigns/31/report/geo", paths)
        self.assertEqual(request.call_args.kwargs["params"]["page"], 2)

    def test_aggregate_reports_keep_geo_when_recipient_scope_is_missing(self) -> None:
        client = ListmonkClient("https://listmonk.example", "test-token")

        def reply(method, path, **kwargs):
            if path.endswith("/recipients"):
                raise APIError(403, "API key is missing scope: campaigns:recipients")
            return {"enabled": False} if path.endswith("/geo") else {}

        with patch.object(client, "request", side_effect=reply):
            reports = fetch_campaign_reports(
                client, all_campaigns=True, report_from="2026-10-01", report_to="2026-10-05", include_geo=True,
            )
        self.assertEqual(reports["geo"], {"enabled": False})
        self.assertIn("campaigns:recipients", reports["recipients_unavailable"])
        self.assertIn("summary", reports)

    def test_report_range_and_target_are_validated_before_http(self) -> None:
        client = ListmonkClient("https://listmonk.example", "test-token")
        with patch.object(client, "request") as request:
            with self.assertRaises(ValueError):
                fetch_campaign_reports(client, campaign_id=31, report_from="2026-10-01")
            with self.assertRaises(ValueError):
                fetch_campaign_reports(
                    client, campaign_id=31, all_campaigns=True, report_from="2026-10-01", report_to="2026-10-05",
                )
            request.assert_not_called()

    def test_campaign_blueprint_preserves_visual_content_without_sender_identity(self) -> None:
        source = {
            "subject": "Hello", "content_type": "visual", "body": "<p>Hello</p>",
            "body_source": '{"root":{"type":"EmailLayout"}}', "name_fallback": {"enabled": True, "value": "Customer"},
            "daily_send_limit": 300, "smtp_source": "organization", "smtp_pool_id": 99,
            "smtp_rate_limit": 500, "reply_mailbox_id": 88, "visibility": "global",
        }
        payload = build_campaign_payload(
            campaign_name="Copy", subject=None, customer_list_id=12, source_campaign=source,
        )
        self.assertEqual(payload["body_source"], source["body_source"])
        self.assertEqual(payload["name_fallback"], source["name_fallback"])
        for field in ("smtp_source", "smtp_pool_id", "smtp_rate_limit", "reply_mailbox_id", "visibility"):
            self.assertNotIn(field, payload)

    def test_campaign_delivery_overrides_match_request_fields(self) -> None:
        payload = build_campaign_payload(
            campaign_name="Launch", subject="Hello", customer_list_id=12, daily_send_limit=300,
            smtp_source="organization", smtp_pool_id=5, smtp_rate_limit=100,
            reply_mailbox_id=7, visibility="organization", auto_track_links=False,
        )
        self.assertEqual(payload["smtp_source"], "organization")
        self.assertEqual(payload["smtp_pool_id"], 5)
        self.assertEqual(payload["smtp_rate_limit"], 100)
        self.assertEqual(payload["reply_mailbox_id"], 7)
        self.assertFalse(payload["auto_track_links"])
        with self.assertRaises(ValueError):
            build_campaign_payload(campaign_name="Launch", subject="Hello", customer_list_id=12, smtp_pool_id=5)
        with self.assertRaises(ValueError):
            build_campaign_payload(campaign_name="Launch", subject="Hello", customer_list_id=12, smtp_rate_limit=1000001)
