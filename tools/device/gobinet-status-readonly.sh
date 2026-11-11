#!/bin/sh
# Only the firmware-pinned NAS 0x4f/0x24/0x31 read queries; no connection/profile changes.
set -eu
exec /bin/sh "$(dirname "$0")/gobinet-readonly.sh" status
