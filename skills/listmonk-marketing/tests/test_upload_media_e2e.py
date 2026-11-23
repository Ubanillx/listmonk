from __future__ import annotations

import json
import os
import re
import subprocess
import sys
import tempfile
import threading
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import quote

ROOT = Path(__file__).resolve().parents[1]


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
        if not separator or not payload.endswith(b"\r\n"):
            raise AssertionError("malformed multipart part")
        name_match = re.search(rb'name="([^"]+)"', head)
        if not name_match:
            raise AssertionError("part has no name")
        fields[name_match.group(1).decode("utf-8")] = payload[:-2]
    return fields


class _UploadHandler(BaseHTTPRequestHandler):
    server: ThreadingHTTPServer

    def log_message(self, *args: object) -> None:
        pass

    def _json(self, payload: dict[str, object], status: int = 200) -> None:
        raw = json.dumps({"data": payload}).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(raw)))
        self.end_headers()
        self.wfile.write(raw)

    def do_POST(self) -> None:
        if self.path != "/api/media":
            self._json({"message": "not found"}, 404)
            return
        length = int(self.headers.get("Content-Length", "0"))
        body = self.rfile.read(length)
        requests = getattr(self.server, "requests", None)
        if requests is not None:
            requests.append(
                {
                    "authorization": self.headers.get("Authorization", ""),
                    "content_type": self.headers.get("Content-Type", ""),
                    "content_length": self.headers.get("Content-Length", ""),
                    "body": body,
                }
            )

        parse_multipart(body, self.headers.get("Content-Type", ""))
        filename_match = re.search(rb'filename="([^"]+)"', body)
        local_name = filename_match.group(1).decode("utf-8") if filename_match else "upload.bin"
        stored_name = local_name.replace(" ", "-")
        media_id = len(requests) + 6 if requests is not None else 7
        self._json(
            {
                "id": media_id,
                "filename": stored_name,
                "url": f"/api/media/file/{media_id}/{quote(stored_name)}",
                "content_type": "image/png",
            }
        )


class UploadMediaEndToEndTests(unittest.TestCase):
    def test_upload_media_script_uploads_multiple_files(self) -> None:
        server = ThreadingHTTPServer(("127.0.0.1", 0), _UploadHandler)
        server.requests = []  # type: ignore[attr-defined]
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        self.addCleanup(server.server_close)
        self.addCleanup(server.shutdown)

        with tempfile.TemporaryDirectory() as workdir:
            workdir_path = Path(workdir)
            image_path = workdir_path / "中文 图片.png"
            image_path.write_bytes(b"fake-image")
            pdf_path = workdir_path / "catalog.pdf"
            pdf_path.write_bytes(b"fake-pdf")

            result = subprocess.run(
                [
                    sys.executable,
                    str(ROOT / "scripts" / "upload_media.py"),
                    "--base-url",
                    f"http://127.0.0.1:{server.server_address[1]}",
                    "--bearer-token",
                    "test-token",
                    "--file",
                    str(image_path),
                    "--file",
                    str(pdf_path),
                    "--folder-id",
                    "9",
                    "--visibility",
                    "private",
                ],
                capture_output=True,
                text=True,
                encoding="utf-8",
                timeout=30,
                check=False,
                env={**os.environ, "PYTHONIOENCODING": "cp1252"},
            )

        self.assertEqual(result.returncode, 0, result.stderr)
        out = json.loads(result.stdout)
        self.assertEqual(out["media_ids"], [7, 8])
        self.assertEqual(len(out["media"]), 2)
        self.assertEqual(out["media"][0]["filename"], "中文-图片.png")
        self.assertEqual(out["media"][0]["local_filename"], "中文 图片.png")
        encoded = quote("中文-图片.png")
        self.assertEqual(out["media"][0]["url"], f"/api/media/file/7/{encoded}")
        self.assertEqual(out["media"][1]["filename"], "catalog.pdf")

        requests = server.requests  # type: ignore[attr-defined]
        self.assertEqual(len(requests), 2)
        for request, expected_name in zip(requests, ("中文 图片.png", "catalog.pdf")):
            self.assertEqual(request["authorization"], "Bearer test-token")
            self.assertTrue(request["content_type"].startswith("multipart/form-data; boundary="))
            self.assertEqual(request["content_length"], str(len(request["body"])))
            fields = parse_multipart(request["body"], request["content_type"])
            self.assertEqual(fields["folder_id"], b"9")
            self.assertEqual(fields["visibility"], b"private")
            self.assertIn(b'name="file"', request["body"])
            self.assertIn(expected_name.encode("utf-8"), request["body"])


if __name__ == "__main__":
    unittest.main()
