from __future__ import annotations

import sys
import tempfile
import unittest
from pathlib import Path

from openpyxl import Workbook

SCRIPTS_DIR = Path(__file__).resolve().parents[1] / "scripts"
if str(SCRIPTS_DIR) not in sys.path:
    sys.path.insert(0, str(SCRIPTS_DIR))

from listmonk_marketing.excel import extract_emails, parse_excel_customers


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

    def test_parse_excel_customers_by_header_name(self) -> None:
        path = self.make_workbook(
            [
                ["邮箱", "客户编码", "姓名", "城市", "预算"],
                ["alice@example.com", "C001", "Alice", "Shanghai", 100],
                ["bob@example.com", "C002", "Bob", "Shenzhen", 200],
            ]
        )

        parsed = parse_excel_customers(
            excel_file=path,
            email_column="邮箱",
            customer_code_column="客户编码",
            name_column="姓名",
        )

        self.assertEqual(parsed["source"], "excel")
        self.assertEqual(len(parsed["customers"]), 2)
        first = parsed["customers"][0]["customer"]
        self.assertEqual(first["email"], "alice@example.com")
        self.assertEqual(first["customer_code"], "C001")
        self.assertEqual(first["name"], "Alice")
        self.assertEqual(first["attribs"], {"城市": "Shanghai", "预算": 100})

    def test_parse_excel_customers_by_column_letter(self) -> None:
        path = self.make_workbook(
            [
                ["Ignore", "Email", "Code", "Name"],
                ["n/a", "charlie@example.com", "C003", "Charlie"],
            ]
        )

        parsed = parse_excel_customers(
            excel_file=path,
            email_column="B",
            customer_code_column="C",
            name_column="D",
        )

        self.assertEqual(parsed["customers"][0]["customer"]["email"], "charlie@example.com")
        self.assertEqual(parsed["customers"][0]["customer"]["name"], "Charlie")

    def test_parse_excel_respects_header_and_start_row(self) -> None:
        path = self.make_workbook(
            [
                ["meta", "meta", "meta"],
                ["Email", "Code", "Name"],
                ["skip@example.com", "C004", "Skip"],
                ["keep@example.com", "C005", "Keep"],
            ]
        )

        parsed = parse_excel_customers(
            excel_file=path,
            email_column="Email",
            customer_code_column="Code",
            name_column="Name",
            header_row=2,
            start_row=4,
        )

        self.assertEqual(len(parsed["customers"]), 1)
        self.assertEqual(parsed["customers"][0]["customer"]["email"], "keep@example.com")

    def test_parse_excel_reports_empty_and_missing_and_invalid_rows(self) -> None:
        path = self.make_workbook(
            [
                ["Email", "Code", "Name"],
                [None, None, None],
                ["", "C006", "No Email"],
                ["invalid", "C007", "Bad Email"],
            ]
        )

        parsed = parse_excel_customers(
            excel_file=path,
            email_column="Email",
            customer_code_column="Code",
            name_column="Name",
        )

        self.assertEqual(parsed["failed_rows"][0]["reason"], "empty_row")
        self.assertEqual(parsed["skipped_rows"][0]["reason"], "missing_email")
        self.assertEqual(parsed["failed_rows"][1]["reason"], "invalid_email")

    def test_parse_excel_extracts_multiple_emails_and_dedupes(self) -> None:
        path = self.make_workbook(
            [
                ["Email", "Code", "Name"],
                ["alpha@example.com; beta@example.com", "C008", "Mix"],
                ["beta@example.com", "C009", "Dup"],
            ]
        )

        parsed = parse_excel_customers(
            excel_file=path,
            email_column="Email",
            customer_code_column="Code",
            name_column="Name",
            dedupe_by_email=True,
        )

        emails = [item["customer"]["email"] for item in parsed["customers"]]
        self.assertEqual(emails, ["alpha@example.com", "beta@example.com"])
        self.assertEqual(parsed["skipped_rows"][0]["reason"], "duplicate_email")


if __name__ == "__main__":
    unittest.main()
