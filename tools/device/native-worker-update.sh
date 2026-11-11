#!/bin/sh
# Update ONLY our native worker, persistent binary/helpers and owned native unit.
# Never restart Web/broker/AP/modem or any stock service. Keep exact originals.
set -eu
umask 077
BASE=/overlay/owner-apps/np1-control
RUN=/run/np1-control-native
UNIT=/etc/systemd/system/np1-control-native.service
UPDATE_LOCK=/run/np1-native-control-update.lock
STAGE=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
test "$(id -u)" = 0
for path in "$BASE" "$BASE/bin" "$BASE/experimental" "$BASE/recovery" "$RUN"; do test -d "$path" && test ! -L "$path"; done
for path in "$BASE/bin/ncc" "$UNIT"; do test -f "$path" && test ! -L "$path"; done
test ! -L "$UPDATE_LOCK"
if test -e "$UPDATE_LOCK"; then test -f "$UPDATE_LOCK"; test "$(stat -c '%u:%a:%h' "$UPDATE_LOCK")" = 0:600:1; fi
exec 8>"$UPDATE_LOCK"
chmod 0600 "$UPDATE_LOCK"
flock -n 8 || { echo 'Another native update is active' >&2; exit 75; }
# Quiesce the old worker too, including versions without the outside update lock.
test ! -L "$RUN/control.lock"
if test -e "$RUN/control.lock"; then test -f "$RUN/control.lock"; fi
exec 9>"$RUN/control.lock"
flock -n 9 || { echo 'A native operation is active; update refused' >&2; exit 75; }
test "$(systemctl show np1-control-native.service -p ActiveState --value)" = active
cd "$STAGE"
sha256sum -c SHA256SUMS >/dev/null
for path in bin/ncc systemd/np1-control-native.service experimental/ncc-native-setting-helper; do test -f "$path" && test ! -L "$path"; done
chmod 0755 bin/ncc
bin/ncc --mode self-test
BACKUP=$(mktemp -d "$BASE/recovery/native-worker-XXXXXXXX")
chmod 0700 "$BACKUP"
printf 'NCC_NATIVE_WORKER_UPDATE_1\n' > "$BACKUP/owner"
UNITS='app-@DriveRec.service app-@ImageRecog.service app-@Voice.service app-@Navigation.service app-@Communication.service app-@Setting.service app-@Update.service app-@RemoteMonitoring.service app-@VirtualPassenger.service app-@InfoMtgEngine.service app-@SitAwrEng.service app-@Opening.service app-@Misc.service app-@System.service app-AppManager.service qmmf-server.service NEventWatcherAV.service NEventWatcherCom.service NEventWatcherITS.service NEventWatcherSys.service QCMAP_ConnectionManagerd.service loc_launcher.service location_hal_daemon.service pulseaudio.service NPSystemMonitor.service socketserver.service np1-control.service np1-control-broker.service np1-control-ap.service np1-control-modem.service np1-control-apn.service np1-control-led.service'
capture(){ cat /proc/sys/kernel/random/boot_id; systemctl show $UNITS --no-pager -p Id -p MainPID -p InvocationID -p ActiveState -p SubState; }
native_identity(){ systemctl show np1-control-native.service -p MainPID -p InvocationID; }
capture > "$BACKUP/preserved-before.txt"
native_identity > "$BACKUP/native-before.txt"
FILES='bin/ncc'
for name in ncc-native-setting-helper ncc-native-setting-extended-helper ncc-native-setting-choice-helper ncc-native-setting-notification-helper ncc-native-alexa-helper ncc-native-audio-helper; do
 if test -e "experimental/$name"; then test -f "experimental/$name" && test ! -L "experimental/$name"; FILES="$FILES experimental/$name"; fi
done
printf '%s\n' "$FILES" > "$BACKUP/managed-files.txt"
for relative in $FILES; do
 target=$BASE/$relative
 test ! -L "$target"
 mkdir -p "$BACKUP/original/$(dirname "$relative")"
 if test -e "$target"; then test -f "$target"; cp -p "$target" "$BACKUP/original/$relative"; else printf '%s\n' "$relative" >> "$BACKUP/originally-absent.txt"; fi
done
cp -p "$UNIT" "$BACKUP/native.service"
COMPLETED=0
CHANGED=0
atomic_copy(){
 source=$1
 target=$2
 mode=$3
 test -f "$source" && test ! -L "$source" && test ! -L "$target"
 temporary=$(mktemp "$target.ncc-new.XXXXXXXX")
 if ! cp -p "$source" "$temporary"; then rm -f "$temporary"; return 1; fi
 if test "$mode" != preserve; then if ! chmod "$mode" "$temporary"; then rm -f "$temporary"; return 1; fi; fi
 if ! mv -f "$temporary" "$target"; then rm -f "$temporary"; return 1; fi
}
wait_native(){
 stable=0; count=0; last_pid=0; last_inv=
 while test "$count" -lt 80; do
  state=$(systemctl show np1-control-native.service -p ActiveState --value)
  pid=$(systemctl show np1-control-native.service -p MainPID --value)
  invocation=$(systemctl show np1-control-native.service -p InvocationID --value)
  if test "$state" = active && test "$pid" -gt 0 && test -n "$invocation"; then
   if test "$pid:$invocation" = "$last_pid:$last_inv"; then stable=$((stable+1)); else stable=0; fi
   last_pid=$pid;last_inv=$invocation
   if test "$stable" -ge 12; then test -S "$RUN/control.sock"; return; fi
  else stable=0; if test "$state" = failed; then return 1; fi; fi
  count=$((count+1));sleep 0.25
 done
 return 1
}
rollback(){
 # The newly started worker honors fd8's outside lock. Reacquire the current
 # runtime lock before stopping it; never kill an active vendor child.
 if test -d "$RUN" && test ! -L "$RUN" && test ! -L "$RUN/control.lock"; then
  exec 9>"$RUN/control.lock"
  flock -w 30 9 || { echo 'Native operation did not quiesce; rollback restart deferred' >&2; return 1; }
 else return 1; fi
 test "$(cat "$BACKUP/owner")" = NCC_NATIVE_WORKER_UPDATE_1 || return 1
 rollback_ok=1
 for relative in $FILES; do
  target=$BASE/$relative
  if test -f "$BACKUP/original/$relative"; then
   atomic_copy "$BACKUP/original/$relative" "$target" preserve || rollback_ok=0
  elif test -e "$target"; then
   # Remove only an originally absent file still equal to this update's input.
   if test -f "$BACKUP/originally-absent.txt" && grep -Fxq "$relative" "$BACKUP/originally-absent.txt" && test -f "$target" && test ! -L "$target" && cmp -s "$STAGE/$relative" "$target"; then rm -f "$target" || rollback_ok=0; else rollback_ok=0; fi
  fi
 done
 atomic_copy "$BACKUP/native.service" "$UNIT" preserve || rollback_ok=0
 test "$rollback_ok" = 1 || return 1
 systemctl daemon-reload || return 1
 systemctl --no-block restart np1-control-native.service || return 1
 wait_native || return 1
 cmp -s "$BACKUP/original/bin/ncc" "$RUN/ncc" || return 1
 capture > "$BACKUP/preserved-restored.txt"
 cmp -s "$BACKUP/preserved-before.txt" "$BACKUP/preserved-restored.txt"
}
finish(){
 result=$?
 trap - 0
 trap '' 1 2 15
 set +e
 if test "$COMPLETED" != 1 && test "$CHANGED" = 1; then
  if rollback > "$BACKUP/rollback.txt" 2>&1; then echo 'Native update failed; original native files restored and only native worker restarted' >&2; else echo 'Native rollback incomplete; inspect private recovery evidence before another action' >&2; fi
  result=1
 fi
 printf 'Private native update backup: %s\n' "$BACKUP"
 exit "$result"
}
trap finish 0
trap 'exit 129' 1
trap 'exit 130' 2
trap 'exit 143' 15
CHANGED=1
for relative in $FILES; do
 mode=0600
 if test "$relative" = bin/ncc; then mode=0755; fi
 atomic_copy "$STAGE/$relative" "$BASE/$relative" "$mode"
done
atomic_copy "$STAGE/systemd/np1-control-native.service" "$UNIT" 0644
systemctl daemon-reload
systemctl --no-block restart np1-control-native.service
wait_native
native_identity > "$BACKUP/native-after.txt"
if cmp -s "$BACKUP/native-before.txt" "$BACKUP/native-after.txt"; then echo 'Native worker identity did not change' >&2; exit 1; fi
cmp -s bin/ncc "$RUN/ncc"
for pair in 'ncc-native-setting-helper:helper' 'ncc-native-setting-extended-helper:extended-helper' 'ncc-native-setting-choice-helper:choice-helper' 'ncc-native-setting-notification-helper:notification-helper' 'ncc-native-alexa-helper:alexa-helper' 'ncc-native-audio-helper:audio-helper'; do
 source=${pair%:*};target=${pair#*:}
 if test -f "experimental/$source"; then cmp -s "experimental/$source" "$RUN/$target"; fi
done
capture > "$BACKUP/preserved-after.txt"
cmp -s "$BACKUP/preserved-before.txt" "$BACKUP/preserved-after.txt"
COMPLETED=1
echo 'PASS: native worker updated; Web/broker/AP/modem and stock identities/boot preserved'
