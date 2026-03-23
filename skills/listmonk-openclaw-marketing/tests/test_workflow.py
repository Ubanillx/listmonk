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

from run_marketing_flow import run_workflow


class WorkflowClient:
    def __init__(self) -> None:
        self.clone_template_called = False

    def validate_token(self) -> list[dict[str, object]]:
        return []

    def query_lists(self, query: str) -> list[dict[str, object]]:
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
        return {"id": 11, "name": name}

    def create_subscriber(self, subscriber: dict[str, object], list_id: int, preconfirm: bool) -> dict[str, object]:
        return {"id": 21, "email": subscriber["email"]}

    def clone_template(self, template_id: int, name: str, subject: str | None) -> dict[str, object]:
        self.clone_template_called = True
        return {"id": 31, "name": name}

    def get_campaign(self, campaign_id: int) -> dict[str, object]:
        return {
            "id": campaign_id,
            "name": "复制用模板",
            "subject": "测试主题",
            "body": "# 我是一篇文章",
            "content_type": "html",
            "template_id": 8,
            "messenger": "email",
            "daily_send_limit": 500,
            "daily_resume_time": "09:00",
            "tags": ["test"],
        }

    def query_campaigns(self, query: str) -> list[dict[str, object]]:
        return [{"id": 2, "name": "复制用模板"}]

    def create_campaign(self, payload: dict[str, object]) -> dict[str, object]:
        return {
            "id": 41,
            "status": "draft",
            "name": payload["name"],
            "subject": payload["subject"],
            "template_id": payload.get("template_id"),
        }


class WorkflowTests(unittest.TestCase):
    def make_json_file(self, payload: list[dict[str, object]]) -> str:
        handle = tempfile.NamedTemporaryFile(suffix=".json", delete=False)
        handle.write(json.dumps(payload).encode("utf-8"))
        handle.close()
        self.addCleanup(lambda: Path(handle.name).unlink(missing_ok=True))
        return handle.name

    def test_run_workflow_aggregates_subsystems(self) -> None:
        source = self.make_json_file([{"email": "flow@example.com", "name": "Flow"}])
        client = WorkflowClient()
        args = argparse.Namespace(
            base_url="http://localhost:9000",
            bearer_token="token",
            list_id=None,
            list_name="Flow List",
            list_type="private",
            list_optin="single",
            list_status="active",
            list_tags="growth",
            list_description="",
            subscribers_file=source,
            excel_file="",
            preconfirm_subscriptions=False,
            excel_sheet="",
            email_column="",
            name_column="",
            header_row=1,
            start_row=0,
            skip_empty_rows=False,
            dedupe_by_email=True,
            source_campaign_id=None,
            source_campaign_name="",
            source_template_id=5,
            new_template_name="Cloned Template",
            template_subject="",
            campaign_name="Launch",
            subject="Hello",
            campaign_body=None,
            content_type=None,
            messenger=None,
            from_email=None,
            daily_send_limit=100,
            daily_resume_time=None,
            send_at="",
            tag=["growth"],
            attribs_file="",
            auto_start=False,
            report_from="",
            report_to="",
            recipient_per_page=100,
            verbose=False,
        )
        result = run_workflow(args, client=client)
        self.assertEqual(result["list_id"], 11)
        self.assertEqual(result["template_id"], 31)
        self.assertEqual(result["campaign_id"], 41)
        self.assertEqual(result["imported_count"], 1)
        self.assertEqual(result["status"], "draft")
        self.assertTrue(client.clone_template_called)

    def test_run_workflow_can_inherit_from_source_campaign(self) -> None:
        source = self.make_json_file([{"email": "flow@example.com", "name": "Flow"}])
        client = WorkflowClient()
        args = argparse.Namespace(
            base_url="http://localhost:9000",
            bearer_token="token",
            list_id=None,
            list_name="Flow List",
            list_type="private",
            list_optin="single",
            list_status="active",
            list_tags="growth",
            list_description="",
            subscribers_file=source,
            excel_file="",
            preconfirm_subscriptions=False,
            excel_sheet="",
            email_column="",
            name_column="",
            header_row=1,
            start_row=0,
            skip_empty_rows=False,
            dedupe_by_email=True,
            source_campaign_id=None,
            source_campaign_name="复制用模板",
            source_template_id=None,
            new_template_name=None,
            template_subject="",
            campaign_name="Launch Copy",
            subject=None,
            campaign_body=None,
            content_type=None,
            messenger=None,
            from_email=None,
            daily_send_limit=None,
            daily_resume_time=None,
            send_at="",
            tag=None,
            attribs_file="",
            auto_start=False,
            report_from="",
            report_to="",
            recipient_per_page=100,
            verbose=False,
        )
        result = run_workflow(args, client=client)
        self.assertEqual(result["list_id"], 11)
        self.assertEqual(result["template_id"], 8)
        self.assertEqual(result["campaign_id"], 41)
        self.assertEqual(result["source_campaign_id"], 2)
        self.assertFalse(client.clone_template_called)
        self.assertEqual(result["campaign"]["subject"], "测试主题")


if __name__ == "__main__":
    unittest.main()
