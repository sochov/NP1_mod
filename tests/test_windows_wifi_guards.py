"""Host-only fail-closed checks; never authorize a Windows network change."""
import ast
import importlib.util
import pathlib
import subprocess
import sys
import unittest
import uuid


ROOT = pathlib.Path(__file__).resolve().parents[1]
PYTHON_ENTRIES = (
    "ap-sharing-trial.py", "ap-standalone-trial.py",
    "diagnose-ap-continuity.py", "verify-cellular-hotspot.py",
    "verify-hotspot-internet.py",
)
POWERSHELL_ENTRIES = (
    "verify-ap-client.ps1", "verify-ap-continuity.ps1", "verify-ap-internet.ps1",
)


class WindowsWiFiGuards(unittest.TestCase):
    def test_python_entrypoints_refuse_before_any_side_effect(self):
        for name in PYTHON_ENTRIES:
            with self.subTest(name=name):
                output = ROOT / "tmp" / ("guard-must-not-exist-" + uuid.uuid4().hex)
                command = [sys.executable, str(ROOT / "tools" / name),
                           "--serial", "fixture", "--out", str(output)]
                if name in ("ap-sharing-trial.py", "ap-standalone-trial.py"):
                    command += ["--binary", str(ROOT / "AGENTS.md")]
                result = subprocess.run(command, capture_output=True, timeout=10)
                self.assertEqual(result.returncode, 2, result.stderr)
                self.assertIn(b"Windows Wi-Fi changes are disabled", result.stderr)
                self.assertFalse(output.exists())

    def test_powershell_direct_entries_refuse_before_missing_input(self):
        for name in POWERSHELL_ENTRIES:
            with self.subTest(name=name):
                command = ["powershell", "-NoProfile", "-NonInteractive", "-File",
                           str(ROOT / "tools" / name)]
                if name == "verify-ap-internet.ps1":
                    command += ["-AccessFile", "missing-fixture.json", "-ResultFile", "missing-result.json"]
                result = subprocess.run(command, capture_output=True, timeout=15)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(b"Windows Wi-Fi changes are disabled", result.stderr)

    def test_direct_real_io_is_also_guarded(self):
        spec = importlib.util.spec_from_file_location("cellular_guard", ROOT / "tools/verify-cellular-hotspot.py")
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        with self.assertRaisesRegex(PermissionError, "explicit authorization"):
            module.RealIO(None, None, None, None, None)

    def test_python_wrappers_propagate_only_after_main_guard(self):
        for name in PYTHON_ENTRIES:
            with self.subTest(name=name):
                source = (ROOT / "tools" / name).read_text(encoding="utf-8")
                ast.parse(source)
                self.assertIn("if not args.allow_windows_wifi_changes:", source)
                self.assertIn("'-AllowWindowsWiFiChanges'", source)
                self.assertLess(source.index("if not args.allow_windows_wifi_changes:"), source.index("out.mkdir"))
        source = (ROOT / "tools/diagnose-ap-continuity.py").read_text(encoding="utf-8")
        self.assertIn("'--allow-windows-wifi-changes'", source)

    def test_all_netsh_mutators_have_an_early_guard(self):
        for path in (ROOT / "tools").rglob("*.ps1"):
            source = path.read_text(encoding="utf-8-sig")
            if "netsh wlan" not in source:
                continue
            with self.subTest(name=path.name):
                self.assertIn(path.name, POWERSHELL_ENTRIES)
                guard = source.index("if(-not $AllowWindowsWiFiChanges)")
                self.assertLess(guard, source.index("netsh wlan"))
                self.assertIn("[switch]$AllowWindowsWiFiChanges", source)


if __name__ == "__main__":
    unittest.main()
