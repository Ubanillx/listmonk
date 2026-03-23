from __future__ import annotations

import sys
import tempfile
import unittest
from pathlib import Path

from openpyxl import Workbook

SCRIPTS_DIR = Path(__file__).resolve().parents[1] / "scripts"
if str(SCRIPTS_DIR) not in sys.path:
    sys.path.insert(0, str(SCRIPTS_DIR))

from listmonk_marketing.excel import extract_emails, parse_excel_subscribers


class ExcelParsingTests(unittest.TestCase):
    def make_workbook(self, rows: list[list[object]]) -> str:
        workbook = Workbook()
        sheet = workbook.active
        for row in rows:
            sheet.append(row)

        handle = tempfile.NamedTemporaryFile(suffix=".xlsx", delete=False)
        handle.close()
        workbook.save(handle.name)
        workbook.close()
        self.addCleanup(lambda: Path(handle.name).unlink(missing_ok=True))
        return handle.name

    def test_extract_emails_deduplicates_matches(self) -> None:
        emails = extract_emails("a@example.com; b@example.com; a@example.com")
        self.assertEqual(emails, ["a@example.com", "b@example.com"])

    def test_parse_excel_subscribers_by_header_name(self) -> None:
        path = self.make_workbook(
            [
                ["邮箱", "姓名", "城市", "预算"],
                ["alice@example.com", "Alice", "Shanghai", 100],
                ["bob@example.com", "Bob", "Shenzhen", 200],
            ]
        )

        parsed = parse_excel_subscribers(
            excel_file=path,
            email_column="邮箱",
            name_column="姓名",
        )

        self.assertEqual(parsed["source"], "excel")
        self.assertEqual(len(parsed["subscribers"]), 2)
        first = parsed["subscribers"][0]["subscriber"]
        self.assertEqual(first["email"], "alice@example.com")
        self.assertEqual(first["name"], "Alice")
        self.assertEqual(first["attribs"], {"城市": "Shanghai", "预算": 100})

    def test_parse_excel_subscribers_by_column_letter(self) -> None:
        path = self.make_workbook(
            [
                ["Ignore", "Email", "Name"],
                ["n/a", "charlie@example.com", "Charlie"],
            ]
        )

        parsed = parse_excel_subscribers(
            excel_file=path,
            email_column="B",
            name_column="C",
        )

        self.assertEqual(parsed["subscribers"][0]["subscriber"]["email"], "charlie@example.com")
        self.assertEqual(parsed["subscribers"][0]["subscriber"]["name"], "Charlie")

    def test_parse_excel_respects_header_and_start_row(self) -> None:
        path = self.make_workbook(
            [
                ["meta", "meta"],
                ["Email", "Name"],
                ["skip@example.com", "Skip"],
                ["keep@example.com", "Keep"],
            ]
        )

        parsed = parse_excel_subscribers(
            excel_file=path,
            email_column="Email",
            name_column="Name",
            header_row=2,
            start_row=4,
        )

        self.assertEqual(len(parsed["subscribers"]), 1)
        self.assertEqual(parsed["subscribers"][0]["subscriber"]["email"], "keep@example.com")

    def test_parse_excel_reports_empty_and_missing_and_invalid_rows(self) -> None:
        path = self.make_workbook(
            [
                ["Email", "Name"],
                [None, None],
                ["", "No Email"],
                ["invalid", "Bad Email"],
            ]
        )

        parsed = parse_excel_subscribers(
            excel_file=path,
            email_column="Email",
            name_column="Name",
        )

        self.assertEqual(parsed["failed_rows"][0]["reason"], "empty_row")
        self.assertEqual(parsed["skipped_rows"][0]["reason"], "missing_email")
        self.assertEqual(parsed["failed_rows"][1]["reason"], "invalid_email")

    def test_parse_excel_extracts_multiple_emails_and_dedupes(self) -> None:
        path = self.make_workbook(
            [
                ["Email", "Name"],
                ["alpha@example.com; beta@example.com", "Mix"],
                ["beta@example.com", "Dup"],
            ]
        )

        parsed = parse_excel_subscribers(
            excel_file=path,
            email_column="Email",
            name_column="Name",
            dedupe_by_email=True,
        )

        emails = [item["subscriber"]["email"] for item in parsed["subscribers"]]
        self.assertEqual(emails, ["alpha@example.com", "beta@example.com"])
        self.assertEqual(parsed["skipped_rows"][0]["reason"], "duplicate_email")


if __name__ == "__main__":
    unittest.main()
