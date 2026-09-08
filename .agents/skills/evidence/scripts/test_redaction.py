from __future__ import annotations

import base64
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
VERIFY_RUN = ROOT / ".agents/skills/verify/scripts/verify_run.py"
VERIFY_AUDIT = ROOT / ".agents/skills/maintain-verification/scripts/verify_audit.py"
CAPTURE_SPEC = importlib.util.spec_from_file_location("evidence_capture", EVIDENCE_CAPTURE)
assert CAPTURE_SPEC is not None and CAPTURE_SPEC.loader is not None
CAPTURE_MODULE = importlib.util.module_from_spec(CAPTURE_SPEC)
CAPTURE_SPEC.loader.exec_module(CAPTURE_MODULE)
RUN_SPEC = importlib.util.spec_from_file_location("verify_run", VERIFY_RUN)
assert RUN_SPEC is not None and RUN_SPEC.loader is not None
VERIFY_RUN_MODULE = importlib.util.module_from_spec(RUN_SPEC)
RUN_SPEC.loader.exec_module(VERIFY_RUN_MODULE)


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
    def run_command(self, command, *, env=None, cwd=ROOT):
        return subprocess.run(command, cwd=cwd, env={**os.environ, **(env or {})}, text=True, capture_output=True)

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
            self.assertEqual(result.returncode, 3, result.stderr)
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

    def test_evidence_capture_redacts_oauth_oidc_labels_in_persisted_cli(self):
        with tempfile.TemporaryDirectory() as root:
            values = ["SYNTHETIC_OAUTH_TOKEN_ROUND6_7f3a", "SYNTHETIC_ID_TOKEN_ROUND6_7f3a", "SYNTHETIC_OAUTH_ACCESS_ROUND6_7f3a", "SYNTHETIC_OAUTH_REFRESH_ROUND6_7f3a"]
            text = "oauth_token=%s id_token=%s oauth_access_token=%s oauth_refresh_token=%s" % tuple(values)
            result = self.run_command([sys.executable, str(EVIDENCE_CAPTURE), "capture", "--scenario", "quota.oauth-label", "--role", "after", "--kind", "nonvisual", "--run", "oauth-evidence", "cli", "--expect-exit", "0", "--", sys.executable, "-c", "print(%r)" % text], env={"VERIFY_EVIDENCE_ROOT": root})
            self.assertEqual(result.returncode, 0, result.stderr)
            path, _ = self.read_capture(root)
            self.assert_redacted([path, *path.parent.glob("*.redacted.txt")], values)

    def test_verify_capture_redacts_oauth_oidc_labels_in_persisted_cli(self):
        with tempfile.TemporaryDirectory() as root:
            values = ["SYNTHETIC_OAUTH_TOKEN_ROUND6_8f3a", "SYNTHETIC_ID_TOKEN_ROUND6_8f3a", "SYNTHETIC_OAUTH_ACCESS_ROUND6_8f3a", "SYNTHETIC_OAUTH_REFRESH_ROUND6_8f3a"]
            text = "oauth_token=%s id_token=%s oauth_access_token=%s oauth_refresh_token=%s" % tuple(values)
            result = self.run_command([sys.executable, str(VERIFY_CAPTURE), "--feature", "quota", "--scenario", "quota.oauth-label", "--run-dir", root, "run", "--expect-exit", "0", "--", sys.executable, "-c", "print(%r)" % text])
            self.assertEqual(result.returncode, 0, result.stderr)
            path, _ = self.read_capture(Path(root) / "evidence")
            self.assert_redacted([path], values)

    def test_both_capture_entrypoints_redact_unicode_separator_suffixes(self):
        values = ["SYNTHETIC_OAUTH_UNICODE_2028", "SYNTHETIC_ID_UNICODE_2029", "SYNTHETIC_BEARER_UNICODE_2028", "SYNTHETIC_BEARER_UNICODE_2029"]
        payload = ("oauth_token=%s" % values[0]).encode() + b"\xe2\x80\xa8ROUND6_SUFFIX_2028 id_token=" + values[1].encode() + b"\xe2\x80\xa9ROUND6_SUFFIX_2029 Authorization: Bearer " + values[2].encode() + b"\xe2\x80\xa8ROUND6_BEARER_SUFFIX_2028 authorization: Bearer " + values[3].encode() + b"\xe2\x80\xa9ROUND6_BEARER_SUFFIX_2029"
        encoded = base64.b64encode(payload).decode()
        script = "import base64,sys; sys.stdout.write(base64.b64decode(%r).decode())" % encoded
        with tempfile.TemporaryDirectory() as root:
            evidence = self.run_command([sys.executable, str(EVIDENCE_CAPTURE), "capture", "--scenario", "quota.oauth-unicode", "--role", "after", "--kind", "nonvisual", "--run", "oauth-unicode-evidence", "cli", "--expect-exit", "0", "--", sys.executable, "-c", script], env={"VERIFY_EVIDENCE_ROOT": root})
            verify = self.run_command([sys.executable, str(VERIFY_CAPTURE), "--feature", "quota", "--scenario", "quota.oauth-unicode", "--run-dir", str(Path(root) / "verify"), "run", "--expect-exit", "0", "--", sys.executable, "-c", script])
            self.assertEqual(evidence.returncode, 0, evidence.stderr)
            self.assertEqual(verify.returncode, 0, verify.stderr)
            evidence_path, _ = self.read_capture(Path(root))
            verify_path, _ = self.read_capture(Path(root) / "verify" / "evidence")
            paths = [evidence_path, *evidence_path.parent.glob("*.redacted.txt"), verify_path]
            self.assert_redacted(paths, values)
            self.assert_redacted(paths, ["ROUND6_SUFFIX_2028", "ROUND6_SUFFIX_2029", "ROUND6_BEARER_SUFFIX_2028", "ROUND6_BEARER_SUFFIX_2029"])

    def test_both_capture_entrypoints_redact_oauth_labels_in_malformed_urls(self):
        with tempfile.TemporaryDirectory() as root:
            secret = "SYNTHETIC_OAUTH_MALFORMED_ROUND6_9f3a"
            url = f"not-http://example.invalid/?oauth_token={secret}&id_token={secret}"
            evidence = self.run_command([sys.executable, str(EVIDENCE_CAPTURE), "capture", "--scenario", "quota.oauth-label", "--role", "after", "--kind", "nonvisual", "--run", "oauth-malformed-evidence", "http", "GET", url], env={"VERIFY_EVIDENCE_ROOT": root})
            verify = self.run_command([sys.executable, str(VERIFY_CAPTURE), "--feature", "quota", "--scenario", "quota.oauth-label", "--run-dir", str(Path(root) / "verify"), "http", "GET", url])
            self.assertEqual(evidence.returncode, 3)
            self.assertEqual(verify.returncode, 2)
            self.assertNotIn(secret, evidence.stderr)
            self.assertNotIn(secret, verify.stderr)
            self.assertIn("[REDACTED]", evidence.stderr)
            self.assertIn("[REDACTED]", verify.stderr)

    def test_verify_run_rejects_parent_relative_freshness_paths(self):
        with tempfile.TemporaryDirectory() as root:
            contract = """# Fixture\n\n## Setup\n## Readiness\n## Teardown\n## Automated checks\n## Scenarios\n## Isolation\n## Artifacts\n\n```verify\nentrypoint = \"mise run verify\"\nfeature_maps = \"docs/features/README.md\"\nartifacts = \".artifacts/verification\"\n[freshness]\ninputs = [\"../outside\"]\noutputs = []\n```\n"""
            Path(root, "VERIFY.md").write_text(contract, encoding="utf-8")
            with self.assertRaises(VERIFY_RUN_MODULE.Blocked):
                VERIFY_RUN_MODULE.load_contract(Path(root))

    def test_verify_run_rejects_symlinked_configured_artifacts(self):
        with tempfile.TemporaryDirectory() as root, tempfile.TemporaryDirectory() as outside:
            root_path = Path(root)
            (root_path / "VERIFY.md").write_text("""# Fixture\n\n## Setup\n## Readiness\n## Teardown\n## Automated checks\n## Scenarios\n## Isolation\n## Artifacts\n\n```verify\nentrypoint = \"mise run verify\"\nfeature_maps = \"docs/features/README.md\"\nartifacts = \".artifacts/verification\"\n```\n""", encoding="utf-8")
            (root_path / ".artifacts").symlink_to(outside, target_is_directory=True)
            with self.assertRaises(VERIFY_RUN_MODULE.Blocked):
                VERIFY_RUN_MODULE.load_contract(root_path)

    def test_verify_capture_rejects_parent_relative_configured_artifacts(self):
        with tempfile.TemporaryDirectory() as root:
            outside = Path(root).parent / "round6-capture-outside"
            Path(root, "VERIFY.md").write_text("```verify\nartifacts = \"../round6-capture-outside\"\n```\n", encoding="utf-8")
            self.run_command(["git", "init", "-q"], cwd=Path(root))
            result = self.run_command([sys.executable, str(VERIFY_CAPTURE), "--feature", "quota", "run", "--", sys.executable, "-c", "print('ok')"], cwd=Path(root))
            self.assertEqual(result.returncode, 1, result.stderr)
            self.assertFalse(outside.exists())

    def test_verify_capture_rejects_symlinked_configured_artifacts(self):
        with tempfile.TemporaryDirectory() as root, tempfile.TemporaryDirectory() as outside:
            root_path = Path(root)
            (root_path / "VERIFY.md").write_text("```verify\nartifacts = \".artifacts/verification\"\n```\n", encoding="utf-8")
            (root_path / ".artifacts").symlink_to(outside, target_is_directory=True)
            self.run_command(["git", "init", "-q"], cwd=root_path)
            result = self.run_command([sys.executable, str(VERIFY_CAPTURE), "--feature", "quota", "run", "--", sys.executable, "-c", "print('ok')"], cwd=root_path)
            self.assertEqual(result.returncode, 1, result.stderr)
            self.assertEqual(list(Path(outside).rglob("*")), [])

    def test_verify_capture_rejects_symlinked_default_artifacts(self):
        with tempfile.TemporaryDirectory() as root, tempfile.TemporaryDirectory() as outside:
            root_path = Path(root)
            (root_path / ".artifacts").symlink_to(outside, target_is_directory=True)
            self.run_command(["git", "init", "-q"], cwd=root_path)
            result = self.run_command([sys.executable, str(VERIFY_CAPTURE), "--feature", "quota", "run", "--", sys.executable, "-c", "print('ok')"], cwd=root_path)
            self.assertEqual(result.returncode, 1, result.stderr)
            self.assertEqual(list(Path(outside).rglob("*")), [])

    def test_verify_capture_ignores_latest_run_dir_outside_artifacts(self):
        with tempfile.TemporaryDirectory() as root, tempfile.TemporaryDirectory() as outside:
            root_path = Path(root)
            artifacts = root_path / ".artifacts/verification"
            artifacts.mkdir(parents=True)
            outside_run = Path(outside) / "run"
            (outside_run / "evidence").mkdir(parents=True)
            (artifacts / "latest.json").write_text(json.dumps({"artifacts": {"run_dir": str(outside_run)}}), encoding="utf-8")
            self.run_command(["git", "init", "-q"], cwd=root_path)
            result = self.run_command([sys.executable, str(VERIFY_CAPTURE), "--feature", "quota", "run", "--", sys.executable, "-c", "print('ok')"], cwd=root_path)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(list(Path(outside).rglob("*.json")), [])
            self.assertEqual(len(list(artifacts.glob("manual-*/evidence/*.json"))), 1)

    def test_verify_capture_ignores_malformed_latest_run_dir(self):
        with tempfile.TemporaryDirectory() as root:
            root_path = Path(root)
            artifacts = root_path / ".artifacts/verification"
            artifacts.mkdir(parents=True)
            (artifacts / "latest.json").write_text(json.dumps({"artifacts": {"run_dir": 42}}), encoding="utf-8")
            self.run_command(["git", "init", "-q"], cwd=root_path)
            result = self.run_command([sys.executable, str(VERIFY_CAPTURE), "--feature", "quota", "run", "--", sys.executable, "-c", "print('ok')"], cwd=root_path)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(len(list(artifacts.glob("manual-*/evidence/*.json"))), 1)

    def test_evidence_capture_rejects_symlinked_configured_evidence(self):
        with tempfile.TemporaryDirectory() as root, tempfile.TemporaryDirectory() as outside:
            root_path = Path(root)
            (root_path / "VERIFY.md").write_text("```verify\nevidence = \".artifacts/evidence\"\n```\n", encoding="utf-8")
            (root_path / ".artifacts").symlink_to(outside, target_is_directory=True)
            self.run_command(["git", "init", "-q"], cwd=root_path)
            result = self.run_command([sys.executable, str(EVIDENCE_CAPTURE), "capture", "--scenario", "quota", "--role", "after", "--kind", "nonvisual", "--run", "symlinked-evidence", "cli", "--", sys.executable, "-c", "print('ok')"], cwd=root_path)
            self.assertEqual(result.returncode, 2, result.stderr)
            self.assertEqual(list(Path(outside).rglob("*")), [])

    def test_evidence_capture_rejects_parent_relative_configured_evidence(self):
        with tempfile.TemporaryDirectory() as root:
            root_path = Path(root)
            outside = root_path.parent / "round6-evidence-outside"
            (root_path / "VERIFY.md").write_text("```verify\nevidence = \"../round6-evidence-outside\"\n```\n", encoding="utf-8")
            self.run_command(["git", "init", "-q"], cwd=root_path)
            result = self.run_command([sys.executable, str(EVIDENCE_CAPTURE), "capture", "--scenario", "quota", "--role", "after", "--kind", "nonvisual", "--run", "parent-evidence", "cli", "--", sys.executable, "-c", "print('ok')"], cwd=root_path)
            self.assertEqual(result.returncode, 2, result.stderr)
            self.assertFalse(outside.exists())

    def test_verify_audit_rejects_parent_relative_configured_artifacts(self):
        with tempfile.TemporaryDirectory() as root:
            root_path = Path(root)
            (root_path / "docs/features").mkdir(parents=True)
            (root_path / "docs/features/README.md").write_text("[quota](quota.md)\n", encoding="utf-8")
            (root_path / "docs/features/quota.md").write_text("# quota\n", encoding="utf-8")
            (root_path / "VERIFY.md").write_text("```verify\nfeature_maps = \"docs/features/README.md\"\nartifacts = \"../outside\"\n```\n", encoding="utf-8")
            self.run_command(["git", "init", "-q"], cwd=Path(root))
            result = self.run_command([sys.executable, str(VERIFY_AUDIT), "--root", root, "--no-record"], cwd=ROOT)
            self.assertEqual(result.returncode, 2)

    def test_verify_audit_rejects_symlinked_configured_artifacts(self):
        with tempfile.TemporaryDirectory() as root, tempfile.TemporaryDirectory() as outside:
            root_path = Path(root)
            (root_path / "docs/features").mkdir(parents=True)
            (root_path / "docs/features/README.md").write_text("[quota](quota.md)\n", encoding="utf-8")
            (root_path / "docs/features/quota.md").write_text("# quota\n", encoding="utf-8")
            (root_path / "VERIFY.md").write_text("```verify\nfeature_maps = \"docs/features/README.md\"\nartifacts = \".artifacts/verification\"\n```\n", encoding="utf-8")
            (root_path / ".artifacts").symlink_to(outside, target_is_directory=True)
            self.run_command(["git", "init", "-q"], cwd=root_path)
            result = self.run_command([sys.executable, str(VERIFY_AUDIT), "--root", root, "--no-record"], cwd=ROOT)
            self.assertEqual(result.returncode, 2)

    def test_verify_audit_reports_maintenance_policy_changes(self):
        with tempfile.TemporaryDirectory() as root:
            root_path = Path(root)
            (root_path / "docs/features").mkdir(parents=True)
            (root_path / "docs/features/README.md").write_text("[quota](quota.md)\n", encoding="utf-8")
            (root_path / "docs/features/quota.md").write_text("# quota\n", encoding="utf-8")
            (root_path / "VERIFY.md").write_text("```verify\nfeature_maps = \"docs/features/README.md\"\nartifacts = \".artifacts/verification\"\n```\n", encoding="utf-8")
            (root_path / "mise.toml").write_text("[tasks.verify]\nrun = \"true\"\n", encoding="utf-8")
            self.run_command(["git", "init", "-q"], cwd=Path(root))
            self.run_command(["git", "add", "."], cwd=Path(root))
            self.run_command(["git", "-c", "user.email=test@example.invalid", "-c", "user.name=Test", "commit", "-qm", "base"], cwd=Path(root))
            policy_file = root_path / ".agents/skills/maintain-verification/changed.md"
            policy_file.parent.mkdir(parents=True)
            policy_file.write_text("synthetic\n", encoding="utf-8")
            result = self.run_command([sys.executable, str(VERIFY_AUDIT), "--root", root, "--base", "HEAD", "--no-record", "--json"], cwd=ROOT)
            self.assertEqual(result.returncode, 0, result.stderr)
            record = json.loads(result.stdout)
            self.assertIn(".agents/skills/maintain-verification/changed.md", record["changes"]["policy"])


if __name__ == "__main__":
    unittest.main()
