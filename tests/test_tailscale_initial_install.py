import pathlib
import unittest


ROOT = pathlib.Path(__file__).resolve().parents[1]


class TailscaleInitialInstallContract(unittest.TestCase):
    def test_both_builds_verify_and_bundle_tailscale(self):
        for relative in ("tools/build.ps1", "tools/build.sh"):
            text = (ROOT / relative).read_text(encoding="utf8")
            self.assertIn("prepare-tailscale.py", text, relative)
            self.assertIn("artifacts/ncc-vpn/bin/tailscale", text, relative)
            self.assertIn("artifacts/ncc/vpn/bin", text, relative)
            self.assertIn("source.json", text, relative)

    def test_fresh_install_starts_vpn_without_restarting_it_on_update(self):
        text = (ROOT / "tools/device/install.sh").read_text(encoding="utf8")
        condition = 'if test ! -f "$BACKUP/ncc"; then sh tools/install-vpn.sh; fi'
        self.assertIn(condition, text)
        self.assertLess(text.index("systemctl restart np1-control.service"), text.index(condition))
        self.assertLess(text.index(condition), text.index("sh tools/verify.sh"))

    def test_owner_recovery_fresh_install_bundles_vpn_in_its_transaction(self):
        text = (ROOT / "tools/device/install-owner-recovery.sh").read_text(encoding="utf8")
        self.assertIn('test -f vpn/bin/tailscale && test -f vpn/bin/tailscaled', text)
        self.assertIn('workers=\'broker firewall modem ap apn led led-autostart plate android-auto tailscale\'', text)
        self.assertIn('for directory in led apn wifi ap nas ota-policy uplink tailscale', text)
        self.assertIn('touch "$BASE/vpn-enabled"', text)
        self.assertNotIn("tailscale up", text)

    def test_vpn_installer_preserves_auth_state_and_does_not_log_in(self):
        text = (ROOT / "tools/device/install-vpn.sh").read_text(encoding="utf8")
        self.assertIn('private/tailscale/tailscaled.state', text)
        self.assertNotIn("tailscale up", text)
        self.assertNotIn("--authkey", text)


if __name__ == "__main__":
    unittest.main()
