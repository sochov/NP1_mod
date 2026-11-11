#!/bin/sh
# Fixed 3GPP WDS GetProfileList. Names are skipped; only index set is emitted.
set -eu
exec /bin/sh "$(dirname "$0")/gobinet-profiles-readonly.sh" inventory
