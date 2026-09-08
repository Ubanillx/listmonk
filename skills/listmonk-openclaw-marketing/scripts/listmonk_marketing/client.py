from __future__ import annotations

import json
import mimetypes
import urllib.error
import urllib.parse
import urllib.request
import uuid
from pathlib import Path
from typing import Any


class APIError(RuntimeError):
    def __init__(self, status: int, message: str, data: Any | None = None) -> None:
        super().__init__(message)
        self.status = status
        self.message = message
        self.data = data


class ListmonkClient:
    def __init__(
        self,
        base_url: str,
        bearer_token: str,
        timeout: int = 30,
        organization_id: int | None = None,
    ) -> None:
        self.base_url = base_url.rstrip("/")
        self.timeout = timeout
        self.headers = {
            "Authorization": f"Bearer {bearer_token}",
            "Content-Type": "application/json",
            "Accept": "application/json",
        }
        if organization_id is not None:
            self.headers["X-Listmonk-Organization-ID"] = str(organization_id)

    def request(
        self,
        method: str,
        path: str,
        *,
        params: dict[str, Any] | None = None,
        payload: dict[str, Any] | customer_list[Any] | None = None,
        data: bytes | None = None,
        headers: dict[str, str] | None = None,
    ) -> Any:
        url = f"{self.base_url}{path}"
        if params:
            query = urllib.parse.urlencode(params, doseq=True)
            url = f"{url}?{query}"

        body = data
        if payload is not None:
            body = json.dumps(payload).encode("utf-8")

        request_headers = dict(self.headers)
        if headers:
            request_headers.update(headers)

        req = urllib.request.Request(url, data=body, method=method, headers=request_headers)
        try:
            with urllib.request.urlopen(req, timeout=self.timeout) as resp:
                raw = resp.read()
                if not raw:
                    return None
                if "application/json" in resp.headers.get("Content-Type", ""):
                    parsed = json.loads(raw.decode("utf-8"))
                    return parsed.get("data", parsed)
                return raw.decode("utf-8")
        except urllib.error.HTTPError as exc:
            body = exc.read().decode("utf-8", errors="replace")
            message = body
            data_out = None
            try:
                parsed = json.loads(body)
                message = parsed.get("message", body)
                data_out = parsed.get("data")
            except json.JSONDecodeError:
                pass
            raise APIError(exc.code, message, data_out) from exc

    def validate_token(self) -> Any:
        return self.request("GET", "/api/customer-lists", params={"minimal": "true", "per_page": "all"})

    def get_list(self, customer_list_id: int) -> dict[str, Any]:
        return self.request("GET", f"/api/customer-lists/{customer_list_id}")

    def query_lists(self, query: str) -> customer_list[dict[str, Any]]:
        page = self.request("GET", "/api/customer-lists", params={"query": query, "page": 1, "per_page": "all"})
        return page.get("results", [])

    def create_list(
        self,
        name: str,
        list_type: str,
        optin: str,
        status: str,
        tags: customer_list[str],
        description: str,
    ) -> dict[str, Any]:
        payload = {
            "name": name,
            "type": list_type,
            "optin": optin,
            "status": status,
            "tags": tags,
            "description": description,
        }
        return self.request("POST", "/api/customer-lists", payload=payload)

    def create_customer(self, customer: dict[str, Any], customer_list_id: int, preconfirm: bool) -> dict[str, Any]:
        payload = dict(customer)
        customer_lists = payload.get("customerLists", [])
        if customer_list_id not in customer_lists:
            customer_lists = customer_list(customer_lists) + [customer_list_id]
        payload["customerLists"] = customer_lists
        payload.setdefault("status", "enabled")
        payload["preconfirm_subscriptions"] = preconfirm
        return self.request("POST", "/api/customers", payload=payload)

    def query_customers(self, search: str, per_page: int | str = "all") -> customer_list[dict[str, Any]]:
        page = self.request("GET", "/api/customers", params={"search": search, "page": 1, "per_page": per_page})
        return page.get("results", [])

    def manage_customer_list_memberships(
        self,
        customer_ids: customer_list[int],
        target_customer_list_ids: customer_list[int],
        status: str,
        action: str = "add",
    ) -> Any:
        payload = {
            "ids": customer_ids,
            "action": action,
            "target_customer_list_ids": target_customer_list_ids,
            "status": status,
        }
        return self.request("PUT", "/api/customers/customer-lists", payload=payload)

    def start_customer_import(
        self,
        *,
        file_path: str,
        params: dict[str, Any],
        filename: str = "",
    ) -> dict[str, Any]:
        boundary = f"----listmonk-{uuid.uuid4().hex}"
        source_path = Path(file_path)
        upload_name = filename or source_path.name
        content_type = mimetypes.guess_type(upload_name)[0] or "application/octet-stream"

        body = bytearray()
        body.extend(f"--{boundary}\r\n".encode("utf-8"))
        body.extend(b'Content-Disposition: form-data; name="params"\r\n\r\n')
        body.extend(json.dumps(params, ensure_ascii=False).encode("utf-8"))
        body.extend(b"\r\n")

        body.extend(f"--{boundary}\r\n".encode("utf-8"))
        disposition = f'Content-Disposition: form-data; name="file"; filename="{upload_name}"\r\n'
        body.extend(disposition.encode("utf-8"))
        body.extend(f"Content-Type: {content_type}\r\n\r\n".encode("utf-8"))
        body.extend(source_path.read_bytes())
        body.extend(b"\r\n")
        body.extend(f"--{boundary}--\r\n".encode("utf-8"))

        return self.request(
            "POST",
            "/api/import/customers",
            data=bytes(body),
            headers={
                "Content-Type": f"multipart/form-data; boundary={boundary}",
            },
        )

    def get_customer_import_status(self) -> dict[str, Any]:
        return self.request("GET", "/api/import/customers")

    def get_customer_import_logs(self) -> str:
        return self.request("GET", "/api/import/customers/logs")

    def clone_template(self, template_id: int, name: str, subject: str | None) -> dict[str, Any]:
        payload: dict[str, Any] = {"name": name}
        if subject:
            payload["subject"] = subject
        return self.request("POST", f"/api/templates/{template_id}/clone", payload=payload)

    def get_campaign(self, campaign_id: int) -> dict[str, Any]:
        return self.request("GET", f"/api/campaigns/{campaign_id}")

    def query_campaigns(self, query: str) -> customer_list[dict[str, Any]]:
        page = self.request("GET", "/api/campaigns", params={"query": query, "page": 1, "per_page": "all"})
        return page.get("results", [])

    def create_campaign(self, payload: dict[str, Any]) -> dict[str, Any]:
        return self.request("POST", "/api/campaigns", payload=payload)

    def update_campaign_status(self, campaign_id: int, status: str) -> dict[str, Any]:
        return self.request("PUT", f"/api/campaigns/{campaign_id}/status", payload={"status": status})

    def get_report_summary(self, campaign_id: int, from_date: str, to_date: str) -> dict[str, Any]:
        return self.request(
            "GET",
            f"/api/campaigns/{campaign_id}/report/summary",
            params={"from": from_date, "to": to_date},
        )

    def get_report_timeseries(self, campaign_id: int, from_date: str, to_date: str) -> dict[str, Any]:
        return self.request(
            "GET",
            f"/api/campaigns/{campaign_id}/report/timeseries",
            params={"from": from_date, "to": to_date},
        )

    def get_report_links(self, campaign_id: int, from_date: str, to_date: str) -> Any:
        return self.request(
            "GET",
            f"/api/campaigns/{campaign_id}/report/links",
            params={"from": from_date, "to": to_date},
        )

    def get_report_recipients(
        self,
        campaign_id: int,
        from_date: str,
        to_date: str,
        per_page: int,
    ) -> dict[str, Any]:
        params = {"from": from_date, "to": to_date, "page": 1, "per_page": per_page}
        return self.request("GET", f"/api/campaigns/{campaign_id}/report/recipients", params=params)
