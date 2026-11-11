#!/bin/sh
set -eu
exec /bin/sh "$(dirname "$0")/native-apn-status-readonly.sh" network
