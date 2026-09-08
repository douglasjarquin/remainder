from __future__ import annotations

import http.server
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import threading
from types import SimpleNamespace
import unittest
from unittest.mock import patch


ROOT = Path(__file__).resolve().parents[4]
EVIDENCE_CAPTURE = ROOT / ".agents/skills/evidence/scripts/evidence_capture.py"
VERIFY_CAPTURE = ROOT / ".agents/skills/verify/scripts/verify_capture.py"
CAPTURE_SPEC = importlib.util.spec_from_file_location("evidence_capture", EVIDENCE_CAPTURE)
assert CAPTURE_SPEC is not None and CAPTURE_SPEC.loader is not None
CAPTURE_MODULE = importlib.util.module_from_spec(CAPTURE_SPEC)
CAPTURE_SPEC.loader.exec_module(CAPTURE_MODULE)


class TokenHandler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        body = b"access_token=SYNTHETIC_HTTP_RESPONSE_SECRET_53b2"
        self.send_response(200)
        self.send_header("Content-Type", "text/plain")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, format, *args):
        return


class CaptureRedactionTests(unittest.TestCase):
    def run_command(self, command, *, env=None):
        return subprocess.run(command, cwd=ROOT, env={**os.environ, **(env or {})}, text=True, capture_output=True)

    def read_capture(self, directory):
        captures = sorted(Path(directory).rglob("capture.json"))
        if not captures:
            captures = sorted(Path(directory).rglob("*.json"))
        self.assertEqual(len(captures), 1)
        return captures[0], json.loads(captures[0].read_text(encoding="utf-8"))

    def assert_redacted(self, paths, secrets):
        contents = "\n".join(Path(path).read_text(encoding="utf-8") for path in paths)
        self.assert_redacted_text(contents, secrets)

    def assert_redacted_text(self, contents, secrets):
        for secret in secrets:
            self.assertNotIn(secret, contents)
        self.assertIn("[REDACTED]", contents)

    def test_evidence_unavailable_redacts_reason_and_limitations(self):
        with tempfile.TemporaryDirectory() as root:
            reason = "baseline unavailable token=SYNTHETIC_UNAVAILABLE_SECRET_53b2 accessToken=SYNTHETIC_ACCESS_SECRET_53b2"
            result = self.run_command([sys.executable, str(EVIDENCE_CAPTURE), "unavailable", "--scenario", "quota.red-unavailable", "--run", "unavailable", "--reason", reason], env={"VERIFY_EVIDENCE_ROOT": root})
            self.assertEqual(result.returncode, 0, result.stderr)
            path, record = self.read_capture(root)
            self.assertEqual(record["outcome"], "unavailable")
            self.assertEqual(record["redaction"]["labelled"], True)
            self.assert_redacted([path, Path(result.stdout.strip().split("evidence: ", 1)[-1].split(" run=", 1)[0])], ["SYNTHETIC_UNAVAILABLE_SECRET_53b2", "SYNTHETIC_ACCESS_SECRET_53b2"])

    def test_evidence_cli_redacts_persisted_outputs(self):
        with tempfile.TemporaryDirectory() as root:
            cli_root = Path(root) / "cli"
            result = self.run_command([sys.executable, str(EVIDENCE_CAPTURE), "capture", "--scenario", "quota.red-cli", "--role", "after", "--kind", "nonvisual", "--run", "cli", "cli", "--expect-exit", "0", "--", sys.executable, "-c", "print('token=SYNTHETIC_CLI_SECRET_53b2 access_token=SYNTHETIC_CLI_ACCESS_SECRET_53b2')"], env={"VERIFY_EVIDENCE_ROOT": str(cli_root)})
            self.assertEqual(result.returncode, 0, result.stderr)
            path, _ = self.read_capture(cli_root)
            self.assert_redacted([path, *path.parent.glob("*.redacted.txt")], ["SYNTHETIC_CLI_SECRET_53b2", "SYNTHETIC_CLI_ACCESS_SECRET_53b2"])

    def test_evidence_http_redacts_persisted_outputs(self):
        with tempfile.TemporaryDirectory() as root:
            http_root = Path(root) / "http"
            server = http.server.HTTPServer(("127.0.0.1", 0), TokenHandler)
            thread = threading.Thread(target=server.serve_forever)
            thread.start()
            try:
                url = f"http://127.0.0.1:{server.server_port}/?access_token=SYNTHETIC_EVIDENCE_HTTP_URL_SECRET_53b2"
                result = self.run_command([sys.executable, str(EVIDENCE_CAPTURE), "capture", "--scenario", "quota.red-http", "--role", "after", "--kind", "nonvisual", "--run", "http", "http", "--data", '{"access_token":"SYNTHETIC_EVIDENCE_HTTP_DATA_SECRET_53b2"}', "--expect-status", "200", "GET", url], env={"VERIFY_EVIDENCE_ROOT": str(http_root)})
                self.assertEqual(result.returncode, 0, result.stderr)
                path, _ = self.read_capture(http_root)
                self.assert_redacted([path, *path.parent.glob("*.redacted.txt")], ["SYNTHETIC_HTTP_RESPONSE_SECRET_53b2", "SYNTHETIC_EVIDENCE_HTTP_URL_SECRET_53b2", "SYNTHETIC_EVIDENCE_HTTP_DATA_SECRET_53b2"])
            finally:
                server.shutdown()
                server.server_close()
                thread.join()

    def test_evidence_browser_redacts_textual_persistence(self):
        with tempfile.TemporaryDirectory() as root:
            capture_dir = Path(root)
            args = SimpleNamespace(url="http://example.test/?access_token=SYNTHETIC_BROWSER_URL_SECRET_53b2", viewport="1280x800", theme="light", locale="en-US", timezone="UTC", max_seconds=1, step=[], no_video=True, max_frames=1, step_timeout=1, observe="#value", expect_text=[], expect_selector=[], side_effect=None, side_effect_text=[])
            result = {"ok": True, "diagnostics": ["access_token=SYNTHETIC_BROWSER_DIAGNOSTIC_SECRET_53b2"], "steps": [], "console": [{"text": "accessToken=SYNTHETIC_BROWSER_CONSOLE_SECRET_53b2"}], "observations": {"browser_version": "synthetic", "protocol_version": "1", "url": args.url, "title": "synthetic", "text": "access_token=SYNTHETIC_BROWSER_TEXT_SECRET_53b2", "selector_text": "access_token=SYNTHETIC_BROWSER_SELECTOR_SECRET_53b2", "viewport": {"width": 1280, "height": 800}}}
            record = {"environment": {}, "limitations": [], "assertions": []}
            fake_process = SimpleNamespace(stdout=json.dumps(result), stderr="", returncode=0)
            with patch.object(CAPTURE_MODULE, "capabilities", return_value={"browser": {"supported": True, "binary": "synthetic"}}), patch.object(CAPTURE_MODULE.subprocess, "run", return_value=fake_process):
                CAPTURE_MODULE.capture_browser(args, capture_dir, record, CAPTURE_MODULE.Redactor([]))
            diagnostics = capture_dir / "browser-diagnostics.log"
            self.assert_redacted([diagnostics], ["SYNTHETIC_BROWSER_DIAGNOSTIC_SECRET_53b2"])
            self.assert_redacted_text(json.dumps(record), ["SYNTHETIC_BROWSER_CONSOLE_SECRET_53b2", "SYNTHETIC_BROWSER_TEXT_SECRET_53b2", "SYNTHETIC_BROWSER_SELECTOR_SECRET_53b2"])

    def test_verify_capture_run_redacts_command_and_output(self):
        with tempfile.TemporaryDirectory() as root:
            result = self.run_command([sys.executable, str(VERIFY_CAPTURE), "--feature", "quota", "--scenario", "quota.red-run", "--run-dir", root, "run", "--", sys.executable, "-c", "print('accessToken=SYNTHETIC_VERIFY_RUN_SECRET_53b2')"])
            self.assertEqual(result.returncode, 0, result.stderr)
            path, record = self.read_capture(Path(root) / "evidence")
            self.assertEqual(record["redaction"]["labelled"], True)
            self.assert_redacted([path], ["SYNTHETIC_VERIFY_RUN_SECRET_53b2"])

    def test_verify_capture_http_redacts_request_and_response(self):
        server = http.server.HTTPServer(("127.0.0.1", 0), TokenHandler)
        thread = threading.Thread(target=server.serve_forever)
        thread.start()
        try:
            with tempfile.TemporaryDirectory() as root:
                url = f"http://127.0.0.1:{server.server_port}/?accessToken=SYNTHETIC_VERIFY_HTTP_URL_SECRET_53b2"
                result = self.run_command([sys.executable, str(VERIFY_CAPTURE), "--feature", "quota", "--scenario", "quota.red-http", "--run-dir", root, "http", "--data", '{"accessToken":"SYNTHETIC_VERIFY_HTTP_DATA_SECRET_53b2"}', "--expect-status", "200", "GET", url])
                self.assertEqual(result.returncode, 0, result.stderr)
                path, _ = self.read_capture(Path(root) / "evidence")
                self.assert_redacted([path], ["SYNTHETIC_VERIFY_HTTP_URL_SECRET_53b2", "SYNTHETIC_VERIFY_HTTP_DATA_SECRET_53b2", "SYNTHETIC_HTTP_RESPONSE_SECRET_53b2"])
        finally:
            server.shutdown()
            server.server_close()
            thread.join()

    def test_evidence_capture_malformed_url_redacts_error(self):
        with tempfile.TemporaryDirectory() as root:
            secret = "SYNTHETIC_MALFORMED_URL_SECRET_5a7c"
            url = f"not-http://example.invalid/?access_token={secret}"
            result = self.run_command([sys.executable, str(EVIDENCE_CAPTURE), "capture", "--scenario", "quota.malformed-url", "--role", "after", "--kind", "nonvisual", "--run", "malformed", "http", "GET", url], env={"VERIFY_EVIDENCE_ROOT": root})
            self.assertEqual(result.returncode, 3)
            self.assertNotIn(secret, result.stderr)
            self.assertIn("[REDACTED]", result.stderr)

    def test_verify_capture_malformed_url_redacts_error(self):
        with tempfile.TemporaryDirectory() as root:
            secret = "SYNTHETIC_MALFORMED_VERIFY_URL_SECRET_5a7c"
            url = f"not-http://example.invalid/?access_token={secret}"
            result = self.run_command([sys.executable, str(VERIFY_CAPTURE), "--feature", "quota", "--scenario", "quota.malformed-url", "--run-dir", root, "http", "GET", url])
            self.assertEqual(result.returncode, 2)
            self.assertNotIn(secret, result.stderr)
            self.assertIn("[REDACTED]", result.stderr)

    def test_redactor_covers_labeled_oauth_shapes(self):
        redact = CAPTURE_MODULE.Redactor([])
        text = "access_token=SYNTHETIC_OAUTH_ACCESS_5a7c refresh_token=SYNTHETIC_OAUTH_REFRESH_5a7c Authorization: Bearer SYNTHETIC_OAUTH_BEARER_5a7c"
        redacted = redact(text)
        self.assert_redacted_text(redacted, ["SYNTHETIC_OAUTH_ACCESS_5a7c", "SYNTHETIC_OAUTH_REFRESH_5a7c", "SYNTHETIC_OAUTH_BEARER_5a7c"])


if __name__ == "__main__":
    unittest.main()
