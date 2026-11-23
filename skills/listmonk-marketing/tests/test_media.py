from __future__ import annotations

import sys
import tempfile
import unittest
from pathlib import Path

SCRIPTS_DIR = Path(__file__).resolve().parents[1] / "scripts"
if str(SCRIPTS_DIR) not in sys.path:
    sys.path.insert(0, str(SCRIPTS_DIR))

from listmonk_marketing.media import (
    apply_media_tokens,
    merge_media_ids,
    upload_media_files,
    validate_media_tokens,
)


class MediaClient:
    def __init__(self) -> None:
        self.uploads: list[tuple[str, int | None, str | None]] = []

    def upload_media(self, file_path: str, *, folder_id: int | None = None, visibility: str | None = None) -> dict[str, object]:
        name = Path(file_path).name
        self.uploads.append((name, folder_id, visibility))
        return {"id": len(self.uploads) + 6, "filename": name, "url": f"/api/media/file/{len(self.uploads) + 6}/{name}"}


class FailingMediaClient(MediaClient):
    def __init__(self, fail_at: int) -> None:
        super().__init__()
        self.fail_at = fail_at

    def upload_media(self, file_path: str, *, folder_id: int | None = None, visibility: str | None = None) -> dict[str, object]:
        if len(self.uploads) + 1 == self.fail_at:
            raise RuntimeError("upload failed")
        return super().upload_media(file_path, folder_id=folder_id, visibility=visibility)


class MediaTests(unittest.TestCase):
    def make_file(self, name: str, content: bytes = b"data") -> str:
        handle = tempfile.NamedTemporaryFile(suffix=f"-{name}", delete=False)
        handle.write(content)
        handle.close()
        self.addCleanup(lambda: Path(handle.name).unlink(missing_ok=True))
        return handle.name

    def test_upload_media_files_passes_folder_and_visibility(self) -> None:
        client = MediaClient()
        path = self.make_file("hero.png")
        uploaded = upload_media_files(client, [path], folder_id=9, visibility="organization")
        self.assertEqual(len(uploaded), 1)
        self.assertEqual(uploaded[0]["filename"], Path(path).name)
        self.assertEqual(client.uploads[0][1:], (9, "organization"))

    def test_upload_media_files_exposes_partial_progress(self) -> None:
        client = FailingMediaClient(fail_at=2)
        first = self.make_file("one.png")
        second = self.make_file("two.png")
        progress: dict[str, object] = {}
        with self.assertRaisesRegex(RuntimeError, "upload failed"):
            upload_media_files(client, [first, second], progress=progress)
        self.assertEqual(progress["media_ids"], [7])
        media = progress["media"]
        self.assertEqual(len(media), 1)
        self.assertEqual(media[0]["local_filename"], Path(first).name)

    def test_upload_media_files_reports_empty_progress_on_first_failure(self) -> None:
        client = FailingMediaClient(fail_at=1)
        progress: dict[str, object] = {}
        with self.assertRaisesRegex(RuntimeError, "upload failed"):
            upload_media_files(client, [self.make_file("one.png")], progress=progress)
        self.assertEqual(progress, {"media": [], "media_ids": []})

    def test_apply_media_tokens_replaces_each_uploaded_filename(self) -> None:
        uploaded = [
            {"id": 7, "filename": "hero.png", "url": "/api/media/file/7/hero.png"},
            {"id": 8, "filename": "catalog.pdf", "url": "/api/media/file/8/catalog.pdf"},
        ]
        body = apply_media_tokens(
            '<img src="{{media:hero.png}}"><a href="{{ media:catalog.pdf }}">catalog</a>',
            uploaded,
        )
        self.assertEqual(
            body,
            '<img src="/api/media/file/7/hero.png"><a href="/api/media/file/8/catalog.pdf">catalog</a>',
        )

    def test_apply_media_tokens_accepts_local_filename_and_server_name(self) -> None:
        uploaded = [
            {
                "id": 7,
                "filename": "my-hero.png",
                "local_filename": "my hero.png",
                "url": "/api/media/file/7/my-hero.png",
            }
        ]
        self.assertEqual(
            apply_media_tokens("{{media:my hero.png}}", uploaded),
            "/api/media/file/7/my-hero.png",
        )
        self.assertEqual(
            apply_media_tokens("{{media:my-hero.png}}", uploaded),
            "/api/media/file/7/my-hero.png",
        )

    def test_apply_media_tokens_rejects_unknown_names(self) -> None:
        with self.assertRaisesRegex(ValueError, "missing.png"):
            apply_media_tokens('<img src="{{media:missing.png}}">', [])

    def test_apply_media_tokens_rejects_unsafe_urls(self) -> None:
        for unsafe in ("javascript:alert(1)", "data:text/html,x", "//evil.example/x.png", "/\\evil.example/x.png"):
            with self.assertRaisesRegex(ValueError, "unsafe URL"):
                apply_media_tokens(
                    "{{media:evil.png}}",
                    [{"id": 7, "filename": "evil.png", "url": unsafe}],
                )

    def test_apply_media_tokens_handles_empty_variants_and_malformed_tokens(self) -> None:
        uploaded = [{"id": 7, "filename": "hero.png", "url": "/api/media/file/7/hero.png"}]
        self.assertEqual(
            apply_media_tokens("{{ media : hero.png }}", uploaded),
            "/api/media/file/7/hero.png",
        )
        with self.assertRaisesRegex(ValueError, "Empty media token"):
            apply_media_tokens("{{media:}}", uploaded)
        with self.assertRaisesRegex(ValueError, "malformed media token"):
            apply_media_tokens("{{media:hero.png", uploaded)

    def test_validate_media_tokens_checks_before_upload(self) -> None:
        validate_media_tokens('<img src="{{media:hero.png}}">', ["hero.png"])
        with self.assertRaisesRegex(ValueError, "missing.png"):
            validate_media_tokens('<img src="{{media:missing.png}}">', ["hero.png"])
        with self.assertRaisesRegex(ValueError, "Empty media token"):
            validate_media_tokens("{{media:}}", ["hero.png"])
        with self.assertRaisesRegex(ValueError, "malformed media token"):
            validate_media_tokens("{{media:hero.png", ["hero.png"])

    def test_apply_media_tokens_preserves_missing_body(self) -> None:
        self.assertIsNone(apply_media_tokens(None, []))

    def test_merge_media_ids_dedupes_and_keeps_order(self) -> None:
        uploaded = [{"id": 7}, {"id": 8}]
        self.assertEqual(merge_media_ids([3, 7, 3], uploaded), [3, 7, 8])
        self.assertEqual(merge_media_ids(None, uploaded), [7, 8])
        with self.assertRaises(ValueError):
            merge_media_ids([0], [])
        with self.assertRaises(ValueError):
            merge_media_ids([True], [])
        with self.assertRaises(ValueError):
            merge_media_ids([1.5], [])


if __name__ == "__main__":
    unittest.main()
