from __future__ import annotations

import json
import sys
import tempfile
import unittest
from pathlib import Path

SCRIPTS_DIR = Path(__file__).resolve().parents[1] / "scripts"
if str(SCRIPTS_DIR) not in sys.path:
    sys.path.insert(0, str(SCRIPTS_DIR))

from listmonk_marketing.campaigns import build_campaign_payload, get_source_campaign
from listmonk_marketing.client import APIError
from listmonk_marketing.reports import fetch_campaign_reports


class ReportClient:
    def get_report_summary(self, campaign_id: int, report_from: str, report_to: str) -> dict[str, object]:
        return {"campaign_id": campaign_id, "sent": 10}

    def get_report_timeseries(self, campaign_id: int, report_from: str, report_to: str) -> dict[str, object]:
        return {"views": [], "clicks": [], "bounces": []}

    def get_report_links(self, campaign_id: int, report_from: str, report_to: str) -> list[dict[str, object]]:
        return [{"url": "https://example.com", "total_clicks": 1}]

    def get_report_recipients(self, campaign_id: int, report_from: str, report_to: str, per_page: int) -> dict[str, object]:
        raise APIError(403, "recipient detail unavailable")


class SourceCampaignClient:
    def __init__(self) -> None:
        self.queries: list[str] = []

    def get_campaign(self, campaign_id: int) -> dict[str, object]:
        return {"id": campaign_id, "name": "复制用模板", "subject": "测试主题", "daily_send_limit": 500}

    def query_campaigns(self, query: str) -> list[dict[str, object]]:
        self.queries.append(query)
        return [
            {"id": 2, "name": "复制用模板"},
            {"id": 7, "name": "别的活动"},
        ]


class CampaignAndReportTests(unittest.TestCase):
    def make_attribs_file(self) -> str:
        handle = tempfile.NamedTemporaryFile(suffix=".json", delete=False)
        handle.write(json.dumps({"team": "growth"}).encode("utf-8"))
        handle.close()
        self.addCleanup(lambda: Path(handle.name).unlink(missing_ok=True))
        return handle.name

    def test_build_campaign_payload_includes_optional_fields(self) -> None:
        attribs_file = self.make_attribs_file()
        payload = build_campaign_payload(
            campaign_name="Launch",
            subject="Hello",
            list_id=3,
            template_id=4,
            campaign_body="Body",
            content_type="html",
            messenger="email",
            from_email="ops@example.com",
            daily_send_limit=500,
            daily_resume_time="10:30",
            send_at="2026-03-24T10:00:00Z",
            tags=["one", "two"],
            attribs_file=attribs_file,
        )

        self.assertEqual(payload["lists"], [3])
        self.assertEqual(payload["template_id"], 4)
        self.assertEqual(payload["from_email"], "ops@example.com")
        self.assertEqual(payload["send_at"], "2026-03-24T10:00:00Z")
        self.assertEqual(payload["attribs"], {"team": "growth"})

    def test_build_campaign_payload_inherits_source_campaign(self) -> None:
        payload = build_campaign_payload(
            campaign_name="Launch Copy",
            subject=None,
            list_id=9,
            template_id=None,
            campaign_body=None,
            content_type=None,
            messenger=None,
            from_email=None,
            daily_send_limit=None,
            daily_resume_time=None,
            send_at="",
            tags=None,
            source_campaign={
                "id": 2,
                "name": "复制用模板",
                "subject": "测试主题",
                "body": "# 我是一篇文章",
                "content_type": "html",
                "template_id": 1,
                "messenger": "email",
                "from_email": "listmonk <noreply@example.com>",
                "daily_send_limit": 500,
                "daily_resume_time": "09:00",
                "tags": ["test"],
            },
        )

        self.assertEqual(payload["name"], "Launch Copy")
        self.assertEqual(payload["lists"], [9])
        self.assertEqual(payload["subject"], "测试主题")
        self.assertEqual(payload["template_id"], 1)
        self.assertEqual(payload["daily_send_limit"], 500)
        self.assertEqual(payload["tags"], ["test"])

    def test_get_source_campaign_by_exact_name(self) -> None:
        client = SourceCampaignClient()
        campaign = get_source_campaign(client, source_campaign_name="复制用模板")
        self.assertEqual(campaign["id"], 2)
        self.assertEqual(client.queries, ["复制用模板"])

    def test_fetch_campaign_reports_degrades_on_recipients_403(self) -> None:
        client = ReportClient()
        reports = fetch_campaign_reports(
            client,
            campaign_id=12,
            report_from="2026-03-01",
            report_to="2026-03-31",
            recipient_per_page=100,
        )

        self.assertIn("summary", reports)
        self.assertIn("timeseries", reports)
        self.assertIn("links", reports)
        self.assertEqual(reports["recipients_unavailable"], "recipient detail unavailable")


if __name__ == "__main__":
    unittest.main()
