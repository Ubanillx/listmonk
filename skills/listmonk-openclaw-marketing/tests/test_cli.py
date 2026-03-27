from __future__ import annotations

import argparse
import subprocess
import sys
import unittest
from unittest.mock import patch
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
SCRIPTS_DIR = ROOT / "scripts"
if str(SCRIPTS_DIR) not in sys.path:
    sys.path.insert(0, str(SCRIPTS_DIR))

from listmonk_marketing.cli import add_auth_arguments

SCRIPTS = [
    "run_marketing_flow.py",
    "ensure_list.py",
    "import_subscribers.py",
    "clone_template.py",
    "create_campaign.py",
    "update_campaign_status.py",
    "fetch_campaign_reports.py",
]


class CLITests(unittest.TestCase):
    def run_script(self, script_name: str, *args: str) -> subprocess.CompletedProcess[str]:
        script = ROOT / "scripts" / script_name
        return subprocess.run(
            [sys.executable, str(script), *args],
            capture_output=True,
            text=True,
            check=False,
        )

    def test_each_script_supports_help(self) -> None:
        for script in SCRIPTS:
            with self.subTest(script=script):
                result = self.run_script(script, "--help")
                self.assertEqual(result.returncode, 0)
                self.assertIn("usage:", result.stdout)

    def test_major_scripts_fail_without_required_args(self) -> None:
        for script in ["run_marketing_flow.py", "import_subscribers.py", "create_campaign.py"]:
            with self.subTest(script=script):
                result = self.run_script(script)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("usage:", result.stderr)

    def test_auth_arguments_can_read_environment_defaults(self) -> None:
        parser = argparse.ArgumentParser()
        with patch.dict(
            "os.environ",
            {
                "LISTMONK_BASE_URL": "https://listmonk.bid-gun.com",
                "LISTMONK_BEARER_TOKEN": "env-token",
            },
            clear=False,
        ):
            add_auth_arguments(parser)
            args = parser.parse_args([])

        self.assertEqual(args.base_url, "https://listmonk.bid-gun.com")
        self.assertEqual(args.bearer_token, "env-token")


if __name__ == "__main__":
    unittest.main()
