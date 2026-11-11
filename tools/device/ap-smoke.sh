#!/bin/sh
# Isolated AP experiment: only our new interface/hostapd. Never configure stock interfaces/services.
set -eu
umask 077
IFACE=nccap0
PHY=phy0
HELPER=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)/ap-channel.py
for tool in iw ip hostapd hostapd_cli python3 systemctl mktemp cmp awk sort tr readlink grep head od cat basename rm sleep id; do
    command -v "$tool" >/dev/null || { echo "Required command unavailable: $tool" >&2; exit 1; }
done
test "$(id -u)" = 0 || { echo 'Run as root' >&2; exit 1; }
# An existing interface belongs to someone else. Do not even install a deletion trap for it.
test ! -e "/sys/class/net/$IFACE" || { echo 'Test interface already exists; refusing to touch it' >&2; exit 1; }
BASE=$(mktemp -d /run/np1-ap-smoke.XXXXXX)
CREATED=0
IFINDEX=
HOSTAPD_PID=
HOSTAPD_START=
STOCK_BEFORE=0
STA_BEFORE=0
ROUTES_BEFORE=0
INTERRUPTED=0
HOSTAPD_BIN=$(command -v hostapd)

capture_stock() {
    capture_rc=0
    for unit in app-@Communication app-@DriveRec app-@ImageRecog app-@Voice qmmf-server NEventWatcherCom; do
        printf 'Unit=%s.service\n' "$unit"
        systemctl show "$unit.service" -p MainPID -p ActiveState -p SubState || capture_rc=1
    done
    return "$capture_rc"
}
capture_sta() {
    # Keep diagnostic identity/status in our private runtime directory, never print it to the console.
    python3 "$HELPER" --snapshot
}
capture_routes() {
    # Route expiry timers may change naturally; retain the complete read-back and treat any change as inconclusive.
    ip -4 route show table all > "$BASE/routes4.tmp" || return 1
    ip -6 route show table all > "$BASE/routes6.tmp" || return 1
    ip -4 rule show > "$BASE/rules4.tmp" || return 1
    ip -6 rule show > "$BASE/rules6.tmp" || return 1
    printf '%s\n' '[IPv4]'; LC_ALL=C sort "$BASE/routes4.tmp"
    printf '%s\n' '[IPv6]'; LC_ALL=C sort "$BASE/routes6.tmp"
    printf '%s\n' '[IPv4 rules]'; LC_ALL=C sort "$BASE/rules4.tmp"
    printf '%s\n' '[IPv6 rules]'; LC_ALL=C sort "$BASE/rules6.tmp"
}
own_child() {
    test -n "$HOSTAPD_PID" && test -n "$HOSTAPD_START" || return 1
    test -r "/proc/$HOSTAPD_PID/stat" && test -r "/proc/$HOSTAPD_PID/cmdline" || return 1
    test "$(awk '{print $22}' "/proc/$HOSTAPD_PID/stat")" = "$HOSTAPD_START" || return 1
    test "$(awk '{print $4}' "/proc/$HOSTAPD_PID/stat")" = "$$" || return 1
    test "$(awk '{print $3}' "/proc/$HOSTAPD_PID/stat")" != Z || return 1
}
own_hostapd() {
    own_child || return 1
    # Match complete arguments, not a substring that could name another process/config.
    tr '\000' '\n' < "/proc/$HOSTAPD_PID/cmdline" > "$BASE/process-args.txt" || return 1
    grep -Fqx "$HOSTAPD_BIN" "$BASE/process-args.txt" && grep -Fqx "$BASE/hostapd.conf" "$BASE/process-args.txt"
}
cleanup() {
    cleanup_rc=0
    # The directly spawned child is ours even in the brief interval before exec(hostapd).
    # Parent PID + start time prevent a stale/reused PID from targeting another process.
    if own_child; then
        kill -TERM "$HOSTAPD_PID" || cleanup_rc=1
        count=0
        while own_child && test "$count" -lt 5; do sleep 1; count=$((count+1)); done
        if own_child; then kill -KILL "$HOSTAPD_PID" || cleanup_rc=1; fi
        count=0
        while own_child && test "$count" -lt 3; do sleep 1; count=$((count+1)); done
        if own_child; then echo 'Our hostapd did not exit; inspect private runtime logs' >&2; cleanup_rc=1; fi
    fi
    if test -n "$HOSTAPD_PID" && ! own_child; then wait "$HOSTAPD_PID" 2>/dev/null || true; fi
    if test "$CREATED" = 1 && test -e "/sys/class/net/$IFACE"; then
        if test -n "$IFINDEX" && test "$(cat "/sys/class/net/$IFACE/ifindex")" = "$IFINDEX" && test "$(basename "$(readlink "/sys/class/net/$IFACE/phy80211")")" = "$PHY"; then
            iw dev "$IFACE" del || cleanup_rc=1
        else
            echo 'Interface ownership no longer matches; refusing to delete an unowned interface' >&2
            cleanup_rc=1
        fi
    fi
    if test "$CREATED" = 1 && test -e "/sys/class/net/$IFACE"; then cleanup_rc=1; fi
    # Retain diagnostics, but remove the now-unused secret-bearing configuration.
    rm -f "$BASE/hostapd.conf" || cleanup_rc=1
    return "$cleanup_rc"
}
finish() {
    result=$?
    trap - 0
    trap '' 1 2 15
    set +e
    cleanup || result=1
    capture_stock > "$BASE/after.txt" || result=1
    capture_sta > "$BASE/sta-after.txt" 2> "$BASE/sta-after-error.txt" || result=1
    capture_routes > "$BASE/routes-after.txt" || result=1
    if test "$STOCK_BEFORE" = 1; then cmp -s "$BASE/before.txt" "$BASE/after.txt" || { echo 'Stock PID/state changed or could not be verified' >&2; result=1; }; fi
    if test "$STA_BEFORE" = 1; then cmp -s "$BASE/sta-before.txt" "$BASE/sta-after.txt" || { echo 'STA association/status changed or could not be verified' >&2; result=1; }; fi
    if test "$ROUTES_BEFORE" = 1; then cmp -s "$BASE/routes-before.txt" "$BASE/routes-after.txt" || { echo 'Routes/rules changed or could not be verified' >&2; result=1; }; fi
    printf 'Private AP experiment read-back: %s\n' "$BASE"
    if test "$result" = 0; then echo 'AP enabled, our AP/hostapd removed; stock PIDs/state, STA and routes preserved'; else echo 'AP experiment failed or verification inconclusive; inspect private read-back' >&2; fi
    exit "$result"
}
trap finish 0
trap 'exit 129' 1
trap 'exit 130' 2
trap 'exit 143' 15
capture_stock > "$BASE/before.txt"
STOCK_BEFORE=1
test "$(grep -c '^ActiveState=active$' "$BASE/before.txt")" = 6 && test "$(grep -c '^MainPID=[1-9][0-9]*$' "$BASE/before.txt")" = 6 || { echo 'Stock service baseline is not healthy; refusing AP experiment' >&2; exit 1; }
capture_sta > "$BASE/sta-before.txt"
STA_BEFORE=1
capture_routes > "$BASE/routes-before.txt"
ROUTES_BEFORE=1
iw dev wlan0 info > "$BASE/sta-iw-before.txt" 2>&1 || true
CHANNEL=$(python3 "$HELPER")
case "$CHANNEL" in 1|2|3|4|5|6|7|8|9|10|11|12|13) ;; *) echo 'Associated 2.4GHz STA channel unavailable' >&2; exit 1;; esac
PSK=$(head -c 16 /dev/urandom | od -An -tx1 | tr -d ' \n')
test "${#PSK}" = 32 || { echo 'Random PSK generation failed' >&2; exit 1; }
cat > "$BASE/hostapd.conf" <<EOF
interface=$IFACE
driver=nl80211
ssid=NP1-LAN-TEST
hw_mode=g
channel=$CHANNEL
beacon_int=100
wmm_enabled=1
auth_algs=1
wpa=2
wpa_passphrase=$PSK
wpa_key_mgmt=WPA-PSK
rsn_pairwise=CCMP
ctrl_interface=$BASE/ctrl
EOF
unset PSK
# Defer catchable signals across interface creation/read-back so successful creation is never untracked.
trap 'INTERRUPTED=1' 1 2 15
if (trap '' 1 2 15; exec iw phy "$PHY" interface add "$IFACE" type __ap); then
    CREATED=1
    IFINDEX=$(trap '' 1 2 15; cat "/sys/class/net/$IFACE/ifindex")
else
    exit 1
fi
trap 'exit 129' 1
trap 'exit 130' 2
trap 'exit 143' 15
test "$INTERRUPTED" = 0 || exit 2
# Foreground child: $! identifies exactly our process even when hostapd startup fails before a pidfile exists.
trap 'INTERRUPTED=1' 1 2 15
"$HOSTAPD_BIN" -f "$BASE/hostapd.log" "$BASE/hostapd.conf" &
HOSTAPD_PID=$!
HOSTAPD_START=$(awk '{print $22}' "/proc/$HOSTAPD_PID/stat")
trap 'exit 129' 1
trap 'exit 130' 2
trap 'exit 143' 15
test "$INTERRUPTED" = 0 || exit 2
count=0
while test "$count" -lt 10; do
    own_child || { echo 'Our hostapd exited before AP became enabled' >&2; exit 1; }
    if hostapd_cli -p "$BASE/ctrl" -i "$IFACE" status > "$BASE/ap-last.txt" 2>&1 && grep -Fqx 'state=ENABLED' "$BASE/ap-last.txt" && grep -Fqx "channel=$CHANNEL" "$BASE/ap-last.txt"; then break; fi
    sleep 1
    count=$((count+1))
done
test "$count" -lt 10 || { echo 'AP did not become enabled on the STA channel' >&2; exit 1; }
echo 'AP enabled for 20 seconds (isolated interface; no DHCP/NAT/route changes)'
sleep 20
own_hostapd
hostapd_cli -p "$BASE/ctrl" -i "$IFACE" status > "$BASE/ap-last.txt"
grep -Fqx 'state=ENABLED' "$BASE/ap-last.txt"
grep -Fqx "channel=$CHANNEL" "$BASE/ap-last.txt"
# EXIT always performs ownership-checked cleanup and all three after-comparisons, including failures/signals.
