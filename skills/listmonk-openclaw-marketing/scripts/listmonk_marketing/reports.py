from __future__ import annotations

from typing import Any

from listmonk_marketing.client import APIError, ListmonkClient


def fetch_campaign_reports(
    client: ListmonkClient,
    *,
    campaign_id: int,
    report_from: str = "",
    report_to: str = "",
    recipient_per_page: int = 100,
) -> dict[str, Any]:
    if not report_from or not report_to:
        return {}

    reports = {
        "summary": client.get_report_summary(campaign_id, report_from, report_to),
        "timeseries": client.get_report_timeseries(campaign_id, report_from, report_to),
        "links": client.get_report_links(campaign_id, report_from, report_to),
    }

    try:
        reports["recipients"] = client.get_report_recipients(
            campaign_id,
            report_from,
            report_to,
            recipient_per_page,
        )
    except APIError as exc:
        if exc.status == 403:
            reports["recipients_unavailable"] = exc.message
        else:
            raise

    return reports

