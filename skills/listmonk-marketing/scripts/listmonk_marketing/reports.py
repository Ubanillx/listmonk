from __future__ import annotations

from typing import Any

from listmonk_marketing.client import APIError, ListmonkClient


def fetch_campaign_reports(
    client: ListmonkClient,
    *,
    campaign_id: int | None = None,
    campaign_ids: list[int] | None = None,
    all_campaigns: bool = False,
    report_from: str = "",
    report_to: str = "",
    recipient_per_page: int = 100,
    recipient_page: int = 1,
    include_geo: bool = False,
) -> dict[str, Any]:
    if not report_from and not report_to:
        return {}
    if not report_from or not report_to:
        raise ValueError("Both --report-from and --report-to are required")
    if sum((campaign_id is not None, bool(campaign_ids), all_campaigns)) != 1:
        raise ValueError("Select one campaign ID, a list of campaign IDs, or all campaigns")
    if recipient_page < 1 or recipient_per_page < 1:
        raise ValueError("Recipient page and page size must be positive")

    if campaign_id is not None:
        reports = {
            "summary": client.get_report_summary(campaign_id, report_from, report_to),
            "timeseries": client.get_report_timeseries(campaign_id, report_from, report_to),
            "links": client.get_report_links(campaign_id, report_from, report_to),
        }
        if include_geo:
            reports["geo"] = client.get_report_geo(campaign_id, report_from, report_to)
    else:
        reports = {
            report: client.get_campaigns_report(report, report_from, report_to, campaign_ids=campaign_ids, all_campaigns=all_campaigns)
            for report in (["summary", "timeseries", "links", "geo"] if include_geo else ["summary", "timeseries", "links"])
        }

    try:
        if campaign_id is not None:
            page_args = {"page": recipient_page} if recipient_page != 1 else {}
            reports["recipients"] = client.get_report_recipients(campaign_id, report_from, report_to, recipient_per_page, **page_args)
        else:
            reports["recipients"] = client.get_campaigns_report(
                "recipients", report_from, report_to, campaign_ids=campaign_ids, all_campaigns=all_campaigns,
                page=recipient_page, per_page=recipient_per_page,
            )
    except APIError as exc:
        if exc.status == 403:
            reports["recipients_unavailable"] = exc.message
        else:
            raise

    return reports

