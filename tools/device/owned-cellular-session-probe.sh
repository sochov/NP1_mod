#!/bin/sh
# Explicit finite own-WDS session only. Does not change APN, STA, IP, routes,
# resolver, stock services or launch/kill quectel-CM. Run only in a root-
# coordinated network test window. Linux/AP Internet readiness remains false.
set -eu
umask 077
test "$#" = 0
test "$(id -u)" = 0
STAGE=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
cd "$STAGE"
sha256sum -c SHA256SUMS >/dev/null
test -f bin/ncc && test ! -L bin/ncc
BASE=$(mktemp -d /run/np1-owned-cellular-session.XXXXXX)
chmod 0700 "$BASE"
cp bin/ncc "$BASE/ncc"
chmod 0700 "$BASE/ncc"
OBSERVER_WAS_ACTIVE=0
KEEPER=''
CHILD=''
WATCHDOG=''
finish() {
 result=$?
 trap - 0
 trap '' HUP INT TERM
 # EOF asks the helper to stop only its own handle. Wait/reap before allowing
 # the ordinary observer to open any new FD. Never kill a stock process.
 if test -n "$KEEPER"; then kill "$KEEPER" 2>/dev/null || true; wait "$KEEPER" 2>/dev/null || true; KEEPER=''; fi
 if test -n "$CHILD"; then kill -TERM "$CHILD" 2>/dev/null || true; wait "$CHILD" 2>/dev/null || true; CHILD=''; fi
 if test -n "$WATCHDOG"; then kill -TERM "$WATCHDOG" 2>/dev/null || true; wait "$WATCHDOG" 2>/dev/null || true; WATCHDOG=''; fi
 if test "$OBSERVER_WAS_ACTIVE" = 1; then
  systemctl start np1-control-modem.service || result=1
  systemctl is-active --quiet np1-control-modem.service || result=1
 fi
 rm -f "$BASE/ncc" "$BASE/parent-pipe"
 printf 'Private owned-cellular session records: %s\n' "$BASE"
 printf 'NCC_EXIT_STATUS=%s\n' "$result"
 exit "$result"
}
trap finish 0
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM
if systemctl is-active --quiet np1-control-modem.service; then
 OBSERVER_WAS_ACTIVE=1
 systemctl stop np1-control-modem.service
fi
test "$(systemctl show np1-control-modem.service -p MainPID --value)" = 0
mkfifo -m 0600 "$BASE/parent-pipe"
sleep 90 >"$BASE/parent-pipe" &
KEEPER=$!
NCC_OWNED_DATA_SESSION_TRIAL=1 "$BASE/ncc" --mode owned-cellular-session-probe <"$BASE/parent-pipe" >"$BASE/result.json" 2>"$BASE/error-private.txt" &
CHILD=$!
# The executable path is unique to this one invocation. The child remains
# unreaped while the watchdog runs, so its PID cannot be reused during wait.
# Kernel D-state cannot be forcefully repaired; the persistent journal then
# blocks another session and the observer is not resumed before reap.
watch() {
 SLEEPER=''
 trap 'if test -n "$SLEEPER"; then kill "$SLEEPER" 2>/dev/null || true; wait "$SLEEPER" 2>/dev/null || true; fi; exit 0' HUP INT TERM
 sleep 100 & SLEEPER=$!; wait "$SLEEPER" || exit 0; SLEEPER=''
 test "$(readlink "/proc/$CHILD/exe" 2>/dev/null || true)" = "$BASE/ncc" || exit 0
 kill -TERM "$CHILD" 2>/dev/null || exit 0
 sleep 15 & SLEEPER=$!; wait "$SLEEPER" || exit 0; SLEEPER=''
 test "$(readlink "/proc/$CHILD/exe" 2>/dev/null || true)" = "$BASE/ncc" || exit 0
 kill -KILL "$CHILD" 2>/dev/null || true
}
watch & WATCHDOG=$!
set +e
wait "$CHILD"
result=$?
set -e
CHILD=''
cat "$BASE/result.json"
exit "$result"
