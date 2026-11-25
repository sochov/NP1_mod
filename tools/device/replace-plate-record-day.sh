#!/bin/sh
# Install a verified replacement of one NCC plate-history day.
set -eu
umask 077

base=/overlay/owner-apps/np1-control
service=np1-control.service
day=${2:?Specify the JST recording day}
case "$day" in [0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]) ;; *) exit 64 ;; esac
stage=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
case "$stage" in /run/np1-plate-day-*) ;; *) exit 64 ;; esac
case "${1:-}" in apply|restore) action=$1 ;; *) exit 64 ;; esac

dir=$base/state/plate/records
record=$dir/$day.jsonl
backup=$base/recovery/plate-day-$day-$(basename -- "$stage")
next=$dir/.$day.reviewed-$$.next
images=/mnt/sdcard/NP1CC/plate/images
new_images=$stage/installed-images.sha256
test "$(id -u)" = 0
test -d "$dir" && test ! -L "$dir"
test -f "$record" && test ! -L "$record"
test ! -e "$next" && test ! -L "$next"
exec 9>"$base/recovery/.plate-drive-web.lock"
flock -n 9

checksum() { sha256sum "$1" | cut -d ' ' -f 1; }
stock() {
    systemctl show -p Id -p MainPID -p NRestarts -p ActiveState \
        app-@Communication.service app-@DriveRec.service app-@ImageRecog.service \
        app-@Voice.service qmmf-server.service
}
check_images() {
    manifest=$stage/images.sha256
    test -s "$manifest" && test ! -L "$manifest"
    test -d "$stage/images" && test ! -L "$stage/images"
    test -d "$images" && test ! -L "$images"
    grep -q ' /mnt/sdcard ' /proc/mounts
    if grep -Eqv '^[0-9a-f]{64}  pt-[a-z0-9-]{5,45}\.jpg$' "$manifest"; then return 1; fi
    (cd "$stage/images" && sha256sum -c "$manifest" >/dev/null)
    while read -r sha name; do
        test ! -L "$stage/images/$name" && test -f "$stage/images/$name" || return 1
        test ! -e "$images/$name" && test ! -L "$images/$name" || return 1
    done < "$manifest"
}
install_images() {
    : > "$new_images"
    while read -r sha name; do
        # This ID belongs to the replay. Refuse to overwrite a live image.
        temp_image=$(mktemp "$images/.plate-day-XXXXXXXX")
        cat "$stage/images/$name" > "$temp_image"
        test "$(checksum "$temp_image")" = "$sha"
        # The FAT volume does not support hard links. mv -n must move the
        # temporary file for this invocation to own the destination.
        mv -n -- "$temp_image" "$images/$name"
        test ! -e "$temp_image"
        printf '%s  %s\n' "$sha" "$name" >> "$new_images"
        temp_image=
    done < "$stage/images.sha256"
    (cd "$images" && sha256sum -c "$new_images" >/dev/null)
}
remove_images() {
    while read -r sha name; do
        if test -f "$images/$name" && test ! -L "$images/$name" &&
            test "$(checksum "$images/$name")" = "$sha"; then
            rm -f -- "$images/$name"
        fi
    done < "$1"
}
install_day() {
    cp -p -- "$record" "$next" || return 1
    cat -- "$1" > "$next" || return 1
    cmp "$1" "$next" || return 1
    mv -f -- "$next" "$record"
}
readback() {
    systemctl is-active --quiet "$service" &&
        wget -qO "$stage/readback.json" -T 5 "http://127.0.0.1/api/v1/plate/records?from=$day&to=$day" &&
        grep -q '"records":' "$stage/readback.json"
}

if test "$action" = apply; then
    test ! -e "$backup" && test ! -L "$backup"
    test -s "$stage/replacement.jsonl" && test ! -L "$stage/replacement.jsonl"
    from=$(cat -- "$stage/source.sha256")
    to=$(cat -- "$stage/replacement.sha256")
    input=$stage/replacement.jsonl
    if test -e "$stage/images.sha256"; then check_images; fi
else
    test -d "$backup" && test ! -L "$backup"
    test -s "$backup/original.jsonl" && test ! -L "$backup/original.jsonl"
    from=$(cat -- "$backup/replacement.sha256")
    to=$(cat -- "$backup/source.sha256")
    input=$backup/original.jsonl
fi
test "$(checksum "$record")" = "$from"
test "$(checksum "$input")" = "$to"
test "$from" != "$to"
readback
stock > "$stage/stock-before"
installed=0
done=0
temp_image=
finish() {
    result=$?
    trap - 0
    if test "$done" = 0; then
        if test "$action" = apply && test "$installed" = 1; then
            systemctl stop "$service" || result=1
            if ! systemctl is-active --quiet "$service" && test "$(checksum "$record")" = "$to"; then
                install_day "$backup/original.jsonl" || result=1
            else
                echo 'History changed after replacement; original retained in backup' >&2
                result=1
            fi
        fi
        systemctl start "$service" || result=1
        if test "$action" = apply && test -f "$new_images" &&
            test "$(checksum "$record")" = "$from"; then
            remove_images "$new_images" || result=1
        fi
    fi
    if test -n "$temp_image"; then rm -f -- "$temp_image"; fi
    rm -f -- "$next"
    exit "$result"
}
trap finish 0
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

if test "$action" = apply && test -e "$stage/images.sha256"; then install_images; fi
systemctl stop "$service"
if systemctl is-active --quiet "$service"; then exit 1; fi
# The stopped writer may have completed another append. Require the exact
# snapshot reviewed on the PC before replacing any bytes.
test "$(checksum "$record")" = "$from"
if test "$action" = apply; then
    mkdir -m 0700 -- "$backup"
    cp -p -- "$record" "$backup/original.jsonl"
    cmp "$record" "$backup/original.jsonl"
    printf '%s\n' "$from" > "$backup/source.sha256"
    printf '%s\n' "$to" > "$backup/replacement.sha256"
    if test -f "$new_images"; then cp -- "$new_images" "$backup/images.sha256"; fi
    echo "Backup: $backup"
fi
install_day "$input"
installed=1
test "$(checksum "$record")" = "$to"
systemctl start "$service"
for attempt in 1 2 3 4 5; do
    if readback; then break; fi
    test "$attempt" -lt 5
    sleep 1
done
stock > "$stage/stock-after"
cmp "$stage/stock-before" "$stage/stock-after"
done=1
if test "$action" = restore && test -f "$backup/images.sha256"; then remove_images "$backup/images.sha256"; fi
echo 'Plate history and Web read-back verified'
