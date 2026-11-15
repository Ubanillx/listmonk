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


class _SafeRedirectHandler(urllib.request.HTTPRedirectHandler):
    """Keep the Bearer key on the configured origin only.

    urllib copies request headers, including Authorization, onto redirects. A
    malicious or misconfigured proxy could therefore receive the API key. The
    handler also refuses to turn a POST into a GET, which would silently change
    the operation.
    """

    _SENSITIVE_HEADERS = frozenset({"authorization", "x-listmonk-organization-id"})

    def redirect_request(self, req, fp, code, msg, headers, newurl):
        if req.get_method() not in ("GET", "HEAD"):
            raise urllib.error.HTTPError(
                req.full_url, code, "refusing to redirect a non-GET request", headers, fp
            )
        old = urllib.parse.urlsplit(req.full_url)
        new = urllib.parse.urlsplit(newurl)
        if old.scheme == "https" and new.scheme != "https":
            raise urllib.error.HTTPError(
                req.full_url, code, "refusing to redirect HTTPS to plain HTTP", headers, fp
            )
        redirected = super().redirect_request(req, fp, code, msg, headers, newurl)
        if redirected is None:
            return None
        if not self._trusted_origin(old, new):
            # Request normalizes header casing, so remove_header() is not
            # reliable here. Rebuild the redirect without the credentials.
            safe_headers = {
                key: value
                for key, value in redirected.headers.items()
                if key.lower() not in self._SENSITIVE_HEADERS
            }
            redirected = urllib.request.Request(
                redirected.full_url,
                headers=safe_headers,
                origin_req_host=req.origin_req_host,
                unverifiable=True,
            )
        return redirected

    @staticmethod
    def _effective_port(parts) -> int | None:
        if parts.port is not None:
            return parts.port
        return {"http": 80, "https": 443}.get(parts.scheme)

    def _trusted_origin(self, old, new) -> bool:
        if old.hostname != new.hostname:
            return False
        if old.scheme == new.scheme:
            return self._effective_port(old) == self._effective_port(new)
        # A same-host HTTP->HTTPS upgrade is the common proxy setup. Only keep
        # credentials for the standard HTTPS port to avoid crossing services.
        return old.scheme == "http" and new.scheme == "https" and new.port in (None, 443)


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
        self._opener = urllib.request.build_opener(_SafeRedirectHandler())

    def request(
        self,
        method: str,
        path: str,
        *,
        params: dict[str, Any] | None = None,
        payload: dict[str, Any] | list[Any] | None = None,
        data: bytes | None = None,
        headers: dict[str, str] | None = None,
        timeout: int | None = None,
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
            with self._opener.open(req, timeout=self.timeout if timeout is None else timeout) as resp:
                raw = resp.read()
                if not raw:
                    return None
                if "application/json" in resp.headers.get("Content-Type", ""):
                    parsed = json.loads(raw.decode("utf-8"))
                    return parsed.get("data", parsed)
                return raw.decode("utf-8")
        except urllib.error.HTTPError as exc:
            if exc.code in (301, 302, 303, 307, 308):
                raise APIError(
                    exc.code,
                    f"redirect refused: {exc.reason or f'HTTP {exc.code}'}",
                ) from exc
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

    def query_lists(self, query: str) -> list[dict[str, Any]]:
        page = self.request("GET", "/api/customer-lists", params={"query": query, "page": 1, "per_page": "all"})
        return page.get("results", [])

    def create_list(
        self,
        name: str,
        list_type: str,
        optin: str,
        status: str,
        tags: list[str],
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
        customer_lists = payload.get("customer_list_ids", [])
        if customer_lists in (None, ""):
            customer_lists = []
        if not isinstance(customer_lists, list):
            raise ValueError("customer_list_ids must be an array")
        if customer_list_id not in customer_lists:
            customer_lists = list(customer_lists) + [customer_list_id]
        payload["customer_list_ids"] = customer_lists
        payload.setdefault("status", "enabled")
        payload["preconfirm_subscriptions"] = preconfirm
        return self.request("POST", "/api/customers", payload=payload)

    def query_customers(self, search: str, per_page: int | str = "all") -> list[dict[str, Any]]:
        page = self.request("GET", "/api/customers", params={"search": search, "page": 1, "per_page": per_page})
        return page.get("results", [])

    def manage_customer_list_memberships(
        self,
        customer_ids: list[int],
        target_customer_list_ids: list[int],
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

    def upload_media(
        self,
        file_path: str,
        *,
        folder_id: int | None = None,
        visibility: str | None = None,
    ) -> dict[str, Any]:
        """Upload one image or attachment and return the media object (id, url, ...)."""
        boundary = f"----listmonk-{uuid.uuid4().hex}"
        source_path = Path(file_path)
        if not source_path.is_file():
            raise ValueError(f"Media file not found: {file_path}")
        if any(char in source_path.name for char in ('"', "\r", "\n")):
            raise ValueError("Media filename contains unsupported characters")
        if source_path.stat().st_size + 8192 > 32_000_000:
            raise ValueError(
                "Media file is too large for the 32 MB request limit; keep it below about 31.9 MB"
            )
        upload_name = source_path.name.replace("\\", "\\\\").replace('"', '\\"')
        content_type = mimetypes.guess_type(source_path.name)[0] or "application/octet-stream"

        body = bytearray()
        if folder_id is not None:
            body.extend(f"--{boundary}\r\n".encode("utf-8"))
            body.extend(b'Content-Disposition: form-data; name="folder_id"\r\n\r\n')
            body.extend(str(folder_id).encode("utf-8"))
            body.extend(b"\r\n")
        if visibility:
            body.extend(f"--{boundary}\r\n".encode("utf-8"))
            body.extend(b'Content-Disposition: form-data; name="visibility"\r\n\r\n')
            body.extend(visibility.encode("utf-8"))
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
            "/api/media",
            data=bytes(body),
            headers={
                "Content-Type": f"multipart/form-data; boundary={boundary}",
            },
            timeout=max(self.timeout, 120),
        )

    def get_template(self, template_id: int) -> dict[str, Any]:
        return self.request("GET", f"/api/templates/{template_id}", params={"no_body": "true"})

    def clone_template(self, template_id: int, name: str, subject: str | None) -> dict[str, Any]:
        payload: dict[str, Any] = {"name": name}
        if subject:
            payload["subject"] = subject
        return self.request("POST", f"/api/templates/{template_id}/clone", payload=payload)

    def get_campaign(self, campaign_id: int) -> dict[str, Any]:
        return self.request("GET", f"/api/campaigns/{campaign_id}")

    def query_campaigns(self, query: str) -> list[dict[str, Any]]:
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

    def get_report_geo(self, campaign_id: int, from_date: str, to_date: str) -> dict[str, Any]:
        return self.request("GET", f"/api/campaigns/{campaign_id}/report/geo", params={"from": from_date, "to": to_date})

    def get_campaigns_report(
        self,
        report: str,
        from_date: str,
        to_date: str,
        *,
        campaign_ids: list[int] | None = None,
        all_campaigns: bool = False,
        page: int = 1,
        per_page: int = 100,
    ) -> Any:
        if report not in {"summary", "timeseries", "links", "geo", "recipients"}:
            raise ValueError("Unsupported campaign report")
        params: dict[str, Any] = {"from": from_date, "to": to_date}
        if campaign_ids:
            params["id"] = campaign_ids
        if all_campaigns:
            params["all"] = "true"
        if report == "recipients":
            params.update({"page": page, "per_page": per_page})
        return self.request("GET", f"/api/campaigns/report/{report}", params=params)

    def get_report_recipients(
        self,
        campaign_id: int,
        from_date: str,
        to_date: str,
        per_page: int,
        page: int = 1,
    ) -> dict[str, Any]:
        params = {"from": from_date, "to": to_date, "page": page, "per_page": per_page}
        return self.request("GET", f"/api/campaigns/{campaign_id}/report/recipients", params=params)
    def get_delivery_overview(self, campaign_id: int) -> dict[str, Any]:
        """Campaign delivery progress: planned/sent/unsent/failed plus per-organization rows."""
        return self.request("GET", f"/api/campaigns/{campaign_id}/delivery-overview")

    def get_campaign_send_errors(
        self,
        campaign_id: int,
        *,
        search: str = "",
        category: str = "",
        organization_id: int | None = None,
        page: int = 1,
        per_page: int = 50,
    ) -> dict[str, Any]:
        """Historical delivery failures grouped by reason, not the current failed list."""
        params: dict[str, Any] = {}
        if search:
            params["search"] = search
        if category:
            params["category"] = category
        if organization_id is not None:
            params["organization_id"] = organization_id
        params.update({"page": page, "per_page": per_page})
        return self.request("GET", f"/api/campaigns/{campaign_id}/send-errors", params=params)

    def get_campaign_failed_recipients(
        self,
        campaign_id: int,
        *,
        search: str = "",
        category: str = "",
        organization_id: int | None = None,
        page: int = 1,
        per_page: int = 50,
    ) -> dict[str, Any]:
        """Recipients still unsent that failed at least once, plus can_export/can_retry."""
        params: dict[str, Any] = {}
        if search:
            params["search"] = search
        if category:
            params["category"] = category
        if organization_id is not None:
            params["organization_id"] = organization_id
        params.update({"page": page, "per_page": per_page})
        return self.request("GET", f"/api/campaigns/{campaign_id}/failed-recipients", params=params)

    def get_customer_activity(self, customer_id: int) -> dict[str, Any]:
        """Read-only activity: campaign views, link clicks and AI reply classification events."""
        return self.request("GET", f"/api/customers/{customer_id}/activity")
