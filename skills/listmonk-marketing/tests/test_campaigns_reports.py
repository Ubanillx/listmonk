from __future__ import annotations

import argparse
import json
import sys
import tempfile
import unittest
from pathlib import Path

SCRIPTS_DIR = Path(__file__).resolve().parents[1] / "scripts"
if str(SCRIPTS_DIR) not in sys.path:
    sys.path.insert(0, str(SCRIPTS_DIR))

from create_campaign import run_create_campaign
from listmonk_marketing.campaigns import (
    build_campaign_payload,
    get_source_campaign,
    validate_campaign_template,
)
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


class CreateCampaignClient:
    def __init__(self) -> None:
        self.payload: dict[str, object] = {}

    def upload_media(self, file_path: str, *, folder_id: int | None = None, visibility: str | None = None) -> dict[str, object]:
        name = Path(file_path).name
        return {"id": 7, "filename": name, "url": f"/api/media/file/7/{name}"}

    def create_campaign(self, payload: dict[str, object]) -> dict[str, object]:
        self.payload = payload
        return {"id": 41, "status": "draft", "name": payload["name"]}


class TemplateTypeClient:
    def __init__(self, template_type: str) -> None:
        self.template_type = template_type

    def get_template(self, template_id: int) -> dict[str, object]:
        return {"id": template_id, "type": self.template_type}


class FailingCreateCampaignClient(CreateCampaignClient):
    def create_campaign(self, payload: dict[str, object]) -> dict[str, object]:
        raise APIError(500, "campaign create failed")


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
            customer_list_id=3,
            template_id=4,
            campaign_body="Body",
            content_type="html",
            messenger="email",
            from_email="ops@example.com",
            reply_mailbox_id=6,
            daily_send_limit=500,
            daily_resume_time="10:30",
            send_at="2026-03-24T10:00:00Z",
            tags=["one", "two"],
            attribs_file=attribs_file,
        )

        self.assertEqual(payload["customer_list_ids"], [3])
        self.assertEqual(payload["template_id"], 4)
        self.assertEqual(payload["from_email"], "ops@example.com")
        self.assertEqual(payload["reply_mailbox_id"], 6)
        self.assertEqual(payload["send_at"], "2026-03-24T10:00:00Z")
        self.assertEqual(payload["attribs"], {"team": "growth"})

    def test_build_campaign_payload_inherits_source_campaign(self) -> None:
        payload = build_campaign_payload(
            campaign_name="Launch Copy",
            subject=None,
            customer_list_id=9,
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
        self.assertEqual(payload["customer_list_ids"], [9])
        self.assertEqual(payload["subject"], "测试主题")
        self.assertEqual(payload["template_id"], 1)
        self.assertEqual(payload["daily_send_limit"], 500)
        self.assertEqual(payload["tags"], ["test"])

    def test_get_source_campaign_by_exact_name(self) -> None:
        client = SourceCampaignClient()
        campaign = get_source_campaign(client, source_campaign_name="复制用模板")
        self.assertEqual(campaign["id"], 2)
        self.assertEqual(client.queries, ["复制用模板"])

    def test_validate_campaign_template_matches_content_type(self) -> None:
        validate_campaign_template(TemplateTypeClient("campaign"), template_id=4, content_type="html")
        validate_campaign_template(TemplateTypeClient("campaign_visual"), template_id=4, content_type="visual")
        with self.assertRaisesRegex(ValueError, "campaign_visual"):
            validate_campaign_template(TemplateTypeClient("campaign_visual"), template_id=4, content_type="html")
        with self.assertRaisesRegex(ValueError, "campaign_visual"):
            validate_campaign_template(TemplateTypeClient("campaign"), template_id=4, content_type="visual")
        with self.assertRaisesRegex(ValueError, "transactional"):
            validate_campaign_template(TemplateTypeClient("tx"), template_id=4, content_type="html")

    def test_build_campaign_payload_rejects_non_positive_daily_limit(self) -> None:
        with self.assertRaisesRegex(ValueError, "daily-send-limit"):
            build_campaign_payload(
                campaign_name="Launch",
                subject="Hello",
                customer_list_id=3,
                daily_send_limit=0,
            )

    def make_create_args(self, body_path: Path, media_path: Path) -> argparse.Namespace:
        return argparse.Namespace(
            base_url="https://listmonk.example",
            bearer_token="token",
            organization_id=None,
            source_campaign_id=None,
            source_campaign_name="",
            campaign_body=None,
            body_file=str(body_path),
            media_file=[str(media_path)],
            media_id=[3],
            media_folder_id=9,
            media_visibility="private",
            verbose=False,
            campaign_name="Media launch",
            subject="Hello",
            customer_list_id=12,
            template_id=9,
            content_type="html",
            messenger=None,
            from_email=None,
            reply_mailbox_id=None,
            smtp_source=None,
            smtp_pool_id=None,
            smtp_rate_limit=None,
            visibility=None,
            auto_track_links=None,
            daily_send_limit=300,
            daily_resume_time=None,
            send_at="",
            tag=None,
            attribs_file="",
        )

    def test_create_campaign_uploads_media_and_replaces_body_tokens(self) -> None:
        with tempfile.TemporaryDirectory() as workdir:
            media_path = Path(workdir) / "hero.png"
            media_path.write_bytes(b"image-bytes")
            body_path = Path(workdir) / "body.html"
            body_path.write_text('<p>Hello</p><img src="{{media:hero.png}}">', encoding="utf-8")

            client = CreateCampaignClient()
            args = self.make_create_args(body_path, media_path)
            result = run_create_campaign(args, client)

        self.assertEqual(result["campaign_id"], 41)
        self.assertEqual(result["media_ids"], [3, 7])
        self.assertEqual(client.payload["template_id"], 9)
        self.assertEqual(client.payload["media"], [3, 7])
        self.assertIn("/api/media/file/7/hero.png", client.payload["body"])

    def test_create_campaign_failure_reports_uploaded_media(self) -> None:
        with tempfile.TemporaryDirectory() as workdir:
            media_path = Path(workdir) / "hero.png"
            media_path.write_bytes(b"image-bytes")
            body_path = Path(workdir) / "body.html"
            body_path.write_text('<img src="{{media:hero.png}}">', encoding="utf-8")

            client = FailingCreateCampaignClient()
            args = self.make_create_args(body_path, media_path)
            progress: dict[str, object] = {}
            with self.assertRaisesRegex(APIError, "campaign create failed"):
                run_create_campaign(args, client, progress=progress)

        self.assertEqual(progress["media_ids"], [3, 7])
        self.assertEqual(progress["media"][0]["local_filename"], "hero.png")

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
