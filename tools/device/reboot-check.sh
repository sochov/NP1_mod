#!/bin/sh
# Only invoke via tools/reboot-test.py after verifying a physical recovery/ADB path.
set -eu
sh "$(dirname "$0")/verify.sh"
for unit in app-@Communication app-@DriveRec app-@ImageRecog app-@Voice qmmf-server NEventWatcherCom; do systemctl is-active "$unit.service"; done
sync
systemctl reboot
