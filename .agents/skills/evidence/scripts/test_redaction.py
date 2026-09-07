from __future__ import annotations

import http.server
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import threading
import unittest


ROOT = Path(__file__).resolve().parents[4]
EVIDENCE_CAPTURE = ROOT / ".agents/skills/evidence/scripts/evidence_capture.py"
VERIFY_CAPTURE = ROOT / ".agents/skills/verify/scripts/verify_capture.py"


class TokenHandler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        body = b"token=SYNTHETIC_HTTP_RESPONSE_SECRET_53b2"
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
        for secret in secrets:
            self.assertNotIn(secret, contents)
        self.assertIn("[REDACTED]", contents)

    def test_evidence_unavailable_redacts_reason_and_limitations(self):
        with tempfile.TemporaryDirectory() as root:
            reason = "baseline unavailable token=SYNTHETIC_UNAVAILABLE_SECRET_53b2"
            result = self.run_command([sys.executable, str(EVIDENCE_CAPTURE), "unavailable", "--scenario", "quota.red-unavailable", "--run", "unavailable", "--reason", reason], env={"VERIFY_EVIDENCE_ROOT": root})
            self.assertEqual(result.returncode, 0, result.stderr)
            path, record = self.read_capture(root)
            self.assertEqual(record["outcome"], "unavailable")
            self.assertEqual(record["redaction"]["labelled"], True)
            self.assert_redacted([path, Path(result.stdout.strip().split("evidence: ", 1)[-1].split(" run=", 1)[0])], ["SYNTHETIC_UNAVAILABLE_SECRET_53b2"])

    def test_evidence_cli_and_http_redact_persisted_outputs(self):
        with tempfile.TemporaryDirectory() as root:
            cli_root = Path(root) / "cli"
            result = self.run_command([sys.executable, str(EVIDENCE_CAPTURE), "capture", "--scenario", "quota.red-cli", "--role", "after", "--kind", "nonvisual", "--run", "cli", "cli", "--expect-exit", "0", "--", sys.executable, "-c", "print('token=SYNTHETIC_CLI_SECRET_53b2')"], env={"VERIFY_EVIDENCE_ROOT": str(cli_root)})
            self.assertEqual(result.returncode, 0, result.stderr)
            path, _ = self.read_capture(cli_root)
            self.assert_redacted([path, *path.parent.glob("*.redacted.txt")], ["SYNTHETIC_CLI_SECRET_53b2"])

            http_root = Path(root) / "http"
            server = http.server.HTTPServer(("127.0.0.1", 0), TokenHandler)
            thread = threading.Thread(target=server.serve_forever)
            thread.start()
            try:
                url = f"http://127.0.0.1:{server.server_port}/?token=SYNTHETIC_EVIDENCE_HTTP_URL_SECRET_53b2"
                result = self.run_command([sys.executable, str(EVIDENCE_CAPTURE), "capture", "--scenario", "quota.red-http", "--role", "after", "--kind", "nonvisual", "--run", "http", "http", "--data", '{"token":"SYNTHETIC_EVIDENCE_HTTP_DATA_SECRET_53b2"}', "--expect-status", "200", "GET", url], env={"VERIFY_EVIDENCE_ROOT": str(http_root)})
                self.assertEqual(result.returncode, 0, result.stderr)
                path, _ = self.read_capture(http_root)
                self.assert_redacted([path, *path.parent.glob("*.redacted.txt")], ["SYNTHETIC_HTTP_RESPONSE_SECRET_53b2", "SYNTHETIC_EVIDENCE_HTTP_URL_SECRET_53b2", "SYNTHETIC_EVIDENCE_HTTP_DATA_SECRET_53b2"])
            finally:
                server.shutdown()
                server.server_close()
                thread.join()

    def test_verify_capture_run_redacts_command_and_output(self):
        with tempfile.TemporaryDirectory() as root:
            result = self.run_command([sys.executable, str(VERIFY_CAPTURE), "--feature", "quota", "--scenario", "quota.red-run", "--run-dir", root, "run", "--", sys.executable, "-c", "print('token=SYNTHETIC_VERIFY_RUN_SECRET_53b2')"])
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
                url = f"http://127.0.0.1:{server.server_port}/?token=SYNTHETIC_VERIFY_HTTP_URL_SECRET_53b2"
                result = self.run_command([sys.executable, str(VERIFY_CAPTURE), "--feature", "quota", "--scenario", "quota.red-http", "--run-dir", root, "http", "--data", '{"token":"SYNTHETIC_VERIFY_HTTP_DATA_SECRET_53b2"}', "--expect-status", "200", "GET", url])
                self.assertEqual(result.returncode, 0, result.stderr)
                path, _ = self.read_capture(Path(root) / "evidence")
                self.assert_redacted([path], ["SYNTHETIC_VERIFY_HTTP_URL_SECRET_53b2", "SYNTHETIC_VERIFY_HTTP_DATA_SECRET_53b2", "SYNTHETIC_HTTP_RESPONSE_SECRET_53b2"])
        finally:
            server.shutdown()
            server.server_close()
            thread.join()


if __name__ == "__main__":
    unittest.main()
