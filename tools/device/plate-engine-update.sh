#!/bin/sh
# Swap only the installed owner plate engine, runtime and unit. Run as root via ADB.
set -eu
umask 077

BASE=/overlay/owner-apps/np1-control
SERVICE=np1-control-plate.service
UNIT=/etc/systemd/system/$SERVICE
PLATE=$BASE/plate
ACTION=${1:-}
BACKUP=${2:-}
OWNER_UNIT=$BASE/units/$SERVICE
ENGINE=$PLATE/plate-engine
RUNTIME=$PLATE/libonnxruntime.so.1.30.0
WORKER=$PLATE/ncc-worker
MODELS=$PLATE/models
CACHE=$PLATE/opencl-cache
PREPARE=${3:-}

test "$(id -u)" = 0
name=${BACKUP##*/}
suffix=${name#plate-update-}
case "$suffix" in
    *[!a-zA-Z0-9]*) echo "Invalid plate recovery directory" >&2; exit 1 ;;
esac
test "${#suffix}" -eq 8 && test "$BACKUP" = "$BASE/recovery/plate-update-$suffix" || exit 1
test -d "$BACKUP" && test ! -L "$BACKUP" || exit 1
test -f "$UNIT" && test ! -L "$UNIT" && test "$UNIT" -ef "/overlay$UNIT" || exit 1
test ! -L "$OWNER_UNIT" && test ! -L "$PLATE" && test ! -L "$ENGINE" &&
    test ! -L "$RUNTIME" && test ! -L "$CACHE" && test ! -L "$WORKER" && test ! -L "$MODELS" || exit 1
case "$PREPARE" in ''|prepare-detector) ;; *) exit 1 ;; esac
exec 9>"$BASE/recovery/.plate-engine-update.lock"
flock -n 9

capture_stock() {
    systemctl show -p Id -p ActiveState -p MainPID -p InvocationID -p NRestarts \
        app-@Communication.service app-@DriveRec.service app-@ImageRecog.service \
        app-@Voice.service qmmf-server.service
}

put_file() {
    cp -p "$1" "$2.$suffix" && mv -f "$2.$suffix" "$2"
}

put_unit() {
    put_file "$1" "$UNIT" || return 1
    if test -f "$BACKUP/previous-owner-unit"; then
        put_file "$1" "$OWNER_UNIT" || return 1
    fi
}

restore() {
    test -f "$BACKUP/previous-engine" && test -f "$BACKUP/previous-unit" || return 1
    systemctl stop "$SERVICE" || return 1
    put_file "$BACKUP/previous-engine" "$ENGINE" || return 1
    if test -f "$BACKUP/previous-runtime"; then
        put_file "$BACKUP/previous-runtime" "$RUNTIME" || return 1
    else
        rm -f "$RUNTIME" || return 1
    fi
    if test -f "$BACKUP/plate/ncc-worker"; then
        if test -f "$BACKUP/previous-worker"; then
            put_file "$BACKUP/previous-worker" "$WORKER" || return 1
        else
            rm -f "$WORKER" || return 1
        fi
    fi
    if test -f "$BACKUP/models-update" && test ! -d "$BACKUP/next-models" && test -d "$MODELS"; then
        mv "$MODELS" "$BACKUP/next-models" || return 1
    fi
    if test -d "$BACKUP/previous-models"; then mv "$BACKUP/previous-models" "$MODELS" || return 1; fi
    if test -f "$BACKUP/cache-update" && test ! -d "$BACKUP/next-cache" && test -d "$CACHE"; then
        mv "$CACHE" "$BACKUP/next-cache" || return 1
    fi
    if test -d "$BACKUP/previous-cache"; then mv "$BACKUP/previous-cache" "$CACHE" || return 1; fi
    put_unit "$BACKUP/previous-unit" || return 1
    systemctl daemon-reload || return 1
    if test -f "$BACKUP/was-active"; then systemctl start "$SERVICE" || return 1; fi
    rm -f "$BACKUP/applied"
}

case "$ACTION" in
    rollback)
        test -f "$BACKUP/applied"
        (cd "$BASE" && grep '  plate/' "$BACKUP/SHA256SUMS" | sha256sum -c - >/dev/null)
        cmp "$UNIT" "$BACKUP/next-unit"
        restore
        exit
        ;;
    apply) ;;
    *) echo "Usage: $0 {apply|rollback} PLATE_RECOVERY_DIRECTORY" >&2; exit 1 ;;
esac

test -f "$ENGINE" && test ! -e "$BACKUP/previous-engine" || exit 1
test -f "$BACKUP/np1-control-plate.service" && test -d "$BACKUP/plate" || exit 1
(cd "$BACKUP" && sha256sum -c SHA256SUMS >/dev/null)
chmod 0750 "$BACKUP/plate/plate-engine"
chmod 0644 "$BACKUP/plate/libonnxruntime.so.1.30.0"
cp -p "$ENGINE" "$BACKUP/previous-engine"
if test -f "$RUNTIME"; then cp -p "$RUNTIME" "$BACKUP/previous-runtime"; fi
if test -f "$BACKUP/plate/ncc-worker"; then
    chmod 0750 "$BACKUP/plate/ncc-worker"
    if test -f "$WORKER"; then cp -p "$WORKER" "$BACKUP/previous-worker"; fi
fi
cp -p "$UNIT" "$BACKUP/previous-unit"
if test -f "$OWNER_UNIT"; then
    cmp "$UNIT" "$OWNER_UNIT"
    cp -p "$OWNER_UNIT" "$BACKUP/previous-owner-unit"
fi
capture_stock > "$BACKUP/stock-before"
if systemctl is-active --quiet "$SERVICE"; then touch "$BACKUP/was-active"; fi

prepare_cache() (
    target_models=$1
    work=$(mktemp -d /run/np1-plate-prepare-XXXXXXXX)
    trap 'rm -f "$work/plate-engine" "$work/libonnxruntime.so.1"; rmdir "$work"' 0
    cp -p "$BACKUP/plate/plate-engine" "$work/plate-engine"
    cp -p "$BACKUP/plate/libonnxruntime.so.1.30.0" "$work/libonnxruntime.so.1"
    export LD_LIBRARY_PATH=$work
    mkdir -m 0700 "$BACKUP/next-cache"
    run_engine() {
        limit=$1
        shift
        if timeout -t 1 true 2>/dev/null; then
            set -- timeout -t "$limit" "$work/plate-engine" "$@"
        else
            set -- timeout "$limit" "$work/plate-engine" "$@"
        fi
        systemd-run --scope --quiet -p CPUQuota=150% -p MemoryLimit=384M \
            "$@" --detector "$target_models/detector.tflite" --ocr "$target_models/ocr.tflite" \
            --config "$target_models/plate-model.json"
    }
    run_engine 180 --prepare-gpu-cache "$BACKUP/next-cache" > "$BACKUP/cache-preparation.json"
    # Allow the same 45-second engine startup as the worker, plus time for the
    # detector and OCR read-back passes before reporting the cache ready.
    run_engine 60 --gpu-cache "$BACKUP/next-cache" --bench 1 > "$BACKUP/cache-readback.json"
    grep -Fq 'gpu detector' "$BACKUP/cache-readback.json"
    if grep -Fq '"gpuFile"' "$target_models/plate-model.json"; then
        grep -Fq 'gpu ppocr' "$BACKUP/cache-readback.json"
    fi
    grep -Fq '"bench":' "$BACKUP/cache-readback.json"
)
if test -d "$BACKUP/plate/models"; then
    chmod 0755 "$BACKUP/plate/models"
    if test -d "$BACKUP/plate/models/notices"; then chmod 0755 "$BACKUP/plate/models/notices"; fi
    find "$BACKUP/plate/models" -type f -exec chmod 0644 {} +
fi

# Preserve the installed unit's ownership and sandbox, including owner recovery
# overrides. Add the new runtime dependency using the current package's lines.
cp -p "$UNIT" "$BACKUP/next-unit"
sed '/^ExecStartPre=.*libonnxruntime\.so/d' "$UNIT" > "$BACKUP/next-unit.content"
grep '^ExecStartPre=.*libonnxruntime\.so' "$BACKUP/np1-control-plate.service" > "$BACKUP/runtime-lines"
sed "/^ExecStartPre=.*chmod .*\/plate-engine$/r $BACKUP/runtime-lines" "$BACKUP/next-unit.content" > "$BACKUP/next-unit"
test "$(grep -c '^ExecStartPre=.*libonnxruntime\.so' "$BACKUP/next-unit")" -eq 3
if test -f "$BACKUP/plate/ncc-worker"; then
    sed -i "s|^ExecStartPre=/bin/cp $BASE/bin/ncc /run/np1-control-plate/ncc$|ExecStartPre=/bin/cp $WORKER /run/np1-control-plate/ncc|" "$BACKUP/next-unit"
    grep -Fxq "ExecStartPre=/bin/cp $WORKER /run/np1-control-plate/ncc" "$BACKUP/next-unit"
fi

# Move the verified upload into its staging slot before entering the transaction.
# Both directories are on the owner overlay, so this needs no second model copy.
if test -d "$BACKUP/plate/models"; then
    test ! -e "$BACKUP/next-models"
    mv "$BACKUP/plate/models" "$BACKUP/next-models"
fi

completed=0
finish() {
    result=$?
    trap - 0
    trap '' HUP INT TERM
    if test "$completed" = 0; then
        restore || result=1
    fi
    exit "$result"
}
trap finish 0
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM
systemctl stop "$SERVICE"
if test "$PREPARE" = prepare-detector; then
    # Avoid loading two GPU engines concurrently. The exit trap has already
    # been installed so a failed compile/read-back restarts the old worker.
    TARGET_MODELS=$MODELS
    if test -d "$BACKUP/next-models"; then TARGET_MODELS=$BACKUP/next-models; fi
    echo "Preparing the Adreno OpenCL caches with the installed worker paused"
    prepare_cache "$TARGET_MODELS"
fi
if test -d "$BACKUP/next-models"; then
    touch "$BACKUP/models-update"
    if test -d "$MODELS"; then mv "$MODELS" "$BACKUP/previous-models"; fi
    mv "$BACKUP/next-models" "$MODELS"
fi
if test "$PREPARE" = prepare-detector; then
    touch "$BACKUP/cache-update"
    if test -d "$CACHE"; then mv "$CACHE" "$BACKUP/previous-cache"; fi
    mv "$BACKUP/next-cache" "$CACHE"
fi
put_file "$BACKUP/plate/plate-engine" "$ENGINE"
put_file "$BACKUP/plate/libonnxruntime.so.1.30.0" "$RUNTIME"
if test -f "$BACKUP/plate/ncc-worker"; then put_file "$BACKUP/plate/ncc-worker" "$WORKER"; fi
put_unit "$BACKUP/next-unit"
systemctl daemon-reload
if test -f "$BACKUP/was-active"; then
    systemctl start "$SERVICE"
    systemctl is-active --quiet "$SERVICE"
    cmp "$ENGINE" /run/np1-control-plate/plate-engine
    cmp "$RUNTIME" /run/np1-control-plate/libonnxruntime.so.1.30.0
    if test -f "$BACKUP/plate/ncc-worker"; then cmp "$WORKER" /run/np1-control-plate/ncc; fi
fi
(cd "$BASE" && grep '  plate/' "$BACKUP/SHA256SUMS" | sha256sum -c - >/dev/null)
capture_stock > "$BACKUP/stock-after"
cmp "$BACKUP/stock-before" "$BACKUP/stock-after"
touch "$BACKUP/applied"
completed=1
echo "Plate engine updated; previous engine, runtime and unit retained"
