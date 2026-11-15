from __future__ import annotations

import json
import re
import sys
import tempfile
import unittest
import urllib.error
import urllib.request
from pathlib import Path
from unittest.mock import patch
from urllib.parse import parse_qs, urlparse

SCRIPTS_DIR = Path(__file__).resolve().parents[1] / "scripts"
if str(SCRIPTS_DIR) not in sys.path:
    sys.path.insert(0, str(SCRIPTS_DIR))

from listmonk_marketing.campaigns import build_campaign_payload
from listmonk_marketing.client import APIError, ListmonkClient, _SafeRedirectHandler
from listmonk_marketing.reports import fetch_campaign_reports


def parse_multipart(body: bytes, content_type: str) -> dict[str, bytes]:
    boundary_match = re.search(r"boundary=([^;]+)", content_type)
    if not boundary_match:
        raise AssertionError(f"missing multipart boundary: {content_type}")
    boundary = boundary_match.group(1).strip().encode("utf-8")
    if not body.startswith(b"--" + boundary):
        raise AssertionError("body does not start with the boundary")
    if not body.endswith(b"--" + boundary + b"--\r\n"):
        raise AssertionError("body does not end with the closing boundary")
    fields: dict[str, bytes] = {}
    for part in body.split(b"--" + boundary)[1:-1]:
        head, separator, payload = part.partition(b"\r\n\r\n")
        if not separator:
            raise AssertionError("part has no header/body separator")
        if not payload.endswith(b"\r\n"):
            raise AssertionError("part payload has no trailing CRLF")
        name_match = re.search(rb'name="([^"]+)"', head)
        if not name_match:
            raise AssertionError("part has no name")
        fields[name_match.group(1).decode("utf-8")] = payload[:-2]
    return fields


class APIContractTests(unittest.TestCase):
    def test_multi_campaign_requests_encode_repeated_ids_and_timezone(self) -> None:
        client = ListmonkClient("https://listmonk.example", "test-token")
        with patch.object(client, "_opener") as transport:
            response = transport.open.return_value.__enter__.return_value
            response.headers = {"Content-Type": "application/json"}
            response.read.return_value = b'{"data":{"sent":10}}'
            result = client.get_campaigns_report(
                "summary", "2026-10-05T00:00:00+08:00", "2026-10-05T15:00:00+08:00",
                campaign_ids=[31, 41],
            )
        request = transport.open.call_args.args[0]
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
            media_ids=[7, 8, 7],
        )
        self.assertEqual(payload["smtp_source"], "organization")
        self.assertEqual(payload["smtp_pool_id"], 5)
        self.assertEqual(payload["smtp_rate_limit"], 100)
        self.assertEqual(payload["reply_mailbox_id"], 7)
        self.assertEqual(payload["media"], [7, 8])
        self.assertFalse(payload["auto_track_links"])
        with self.assertRaises(ValueError):
            build_campaign_payload(campaign_name="Launch", subject="Hello", customer_list_id=12, smtp_pool_id=5)
        with self.assertRaises(ValueError):
            build_campaign_payload(campaign_name="Launch", subject="Hello", customer_list_id=12, smtp_rate_limit=1000001)
        with self.assertRaises(ValueError):
            build_campaign_payload(campaign_name="Launch", subject="Hello", customer_list_id=12, media_ids=[0])

    def test_upload_media_builds_multipart_request(self) -> None:
        handle = tempfile.NamedTemporaryFile(suffix="-hero.png", delete=False)
        handle.write(b"image-bytes")
        handle.close()
        self.addCleanup(lambda: Path(handle.name).unlink(missing_ok=True))

        client = ListmonkClient("https://listmonk.example", "test-token")
        with patch.object(client, "_opener") as transport:
            response = transport.open.return_value.__enter__.return_value
            response.headers = {"Content-Type": "application/json"}
            response.read.return_value = (
                b'{"data":{"id":7,"filename":"hero.png","url":"/api/media/file/7/hero.png"}}'
            )
            result = client.upload_media(handle.name, folder_id=9, visibility="organization")

        request = transport.open.call_args.args[0]
        parsed = urlparse(request.full_url)
        body = request.data
        self.assertEqual(parsed.path, "/api/media")
        content_type = request.get_header("Content-type") or request.get_header("Content-Type") or ""
        self.assertTrue(content_type.startswith("multipart/form-data; boundary="))
        fields = parse_multipart(body, content_type)
        self.assertEqual(fields["folder_id"], b"9")
        self.assertEqual(fields["visibility"], b"organization")
        self.assertEqual(fields["file"], b"image-bytes")
        self.assertIn(b'filename="', body)
        self.assertEqual(result["id"], 7)


class RedirectSafetyTests(unittest.TestCase):
    def _request(self, url: str = "https://listmonk.example/api/campaigns", method: str = "GET",
                 data: bytes | None = None) -> urllib.request.Request:
        return urllib.request.Request(
            url,
            data=data,
            method=method,
            headers={
                "Authorization": "Bearer secret-token",
                "X-Listmonk-Organization-ID": "7",
            },
        )

    def _header_names(self, request: urllib.request.Request) -> set[str]:
        return {name.lower() for name in request.headers}

    def test_cross_origin_redirect_drops_credentials(self) -> None:
        redirected = _SafeRedirectHandler().redirect_request(
            self._request(), None, 302, "Found", {}, "https://other.example/api/campaigns"
        )
        self.assertIsNotNone(redirected)
        self.assertNotIn("authorization", self._header_names(redirected))
        self.assertNotIn("x-listmonk-organization-id", self._header_names(redirected))

    def test_same_origin_redirect_keeps_credentials(self) -> None:
        redirected = _SafeRedirectHandler().redirect_request(
            self._request(), None, 302, "Found", {}, "https://listmonk.example/api/campaigns/2"
        )
        self.assertIsNotNone(redirected)
        self.assertEqual(redirected.get_header("Authorization"), "Bearer secret-token")
        self.assertIn("x-listmonk-organization-id", self._header_names(redirected))

    def test_default_port_and_https_upgrade_keep_credentials(self) -> None:
        handler = _SafeRedirectHandler()
        explicit_default = handler.redirect_request(
            self._request("https://listmonk.example/api/campaigns"), None, 302, "Found", {},
            "https://listmonk.example:443/api/campaigns/2",
        )
        self.assertEqual(explicit_default.get_header("Authorization"), "Bearer secret-token")

        upgraded = handler.redirect_request(
            self._request("http://listmonk.example/api/campaigns"), None, 302, "Found", {},
            "https://listmonk.example/api/campaigns/2",
        )
        self.assertEqual(upgraded.get_header("Authorization"), "Bearer secret-token")

    def test_post_redirect_is_refused(self) -> None:
        with self.assertRaises(urllib.error.HTTPError):
            _SafeRedirectHandler().redirect_request(
                self._request(method="POST", data=b"{}"), None, 307, "Temporary Redirect", {},
                "https://listmonk.example/api/campaigns/2",
            )

    def test_https_downgrade_is_refused(self) -> None:
        with self.assertRaises(urllib.error.HTTPError):
            _SafeRedirectHandler().redirect_request(
                self._request(), None, 302, "Found", {}, "http://listmonk.example/api/campaigns"
            )

    def test_rejected_redirect_surfaces_readable_api_error(self) -> None:
        client = ListmonkClient("https://listmonk.example", "test-token")
        with patch.object(client, "_opener") as opener:
            opener.open.side_effect = urllib.error.HTTPError(
                "https://listmonk.example/api/campaigns", 307,
                "refusing to redirect a non-GET request", {}, None,
            )
            with self.assertRaises(APIError) as ctx:
                client.request("POST", "/api/campaigns", payload={})
        self.assertEqual(ctx.exception.status, 307)
        self.assertIn("redirect refused", str(ctx.exception))
