#!/bin/sh
# Copy verified local profile1 recovery into an absent private editor config.
# Reads local recovery/index proof only. No modem query or APN apply.
set -eu
exec /bin/sh "$(dirname "$0")/gobinet-profiles-readonly.sh" seed
