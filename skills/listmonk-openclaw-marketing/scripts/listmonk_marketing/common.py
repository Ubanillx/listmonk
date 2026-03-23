from __future__ import annotations

import json
import sys
from pathlib import Path
from typing import Any, TextIO


def load_json_file(path: str, expected: type) -> Any:
    data = json.loads(Path(path).read_text(encoding="utf-8"))
    if not isinstance(data, expected):
        raise ValueError(f"Expected {expected.__name__} in {path}")
    return data


def log(message: str, *, enabled: bool) -> None:
    if enabled:
        print(message, file=sys.stderr)


def normalize_tags(raw: str | list[str]) -> list[str]:
    if isinstance(raw, list):
        return [item.strip() for item in raw if str(item).strip()]
    return [item.strip() for item in raw.split(",") if item.strip()]


def emit_json(payload: Any, *, stream: TextIO | None = None) -> None:
    out = stream or sys.stdout
    print(json.dumps(payload, ensure_ascii=False, indent=2, default=str), file=out)


def emit_error(exc: Exception, *, progress: dict[str, Any] | None = None) -> int:
    error: dict[str, Any] = {"message": str(exc)}

    status = getattr(exc, "status", None)
    if status is not None:
        error["status"] = status

    data = getattr(exc, "data", None)
    if data is not None:
        error["data"] = data

    if progress:
        error["progress"] = progress

    emit_json({"error": error}, stream=sys.stderr)
    return 1

