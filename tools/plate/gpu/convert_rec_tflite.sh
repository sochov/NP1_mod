#!/bin/bash
# Convert a PP-OCR recognizer ONNX (dynamic width) into fixed-width float32 TFLite for the OpenCL GPU delegate.
#   tools/plate/gpu/convert_rec_tflite.sh MODEL.onnx WIDTH OUT.tflite [--attention]
# Needs Python with tensorflow==2.16.2 tf_keras==2.16.0 onnx==1.16.2 onnx2tf==1.22.3 onnx_graphsurgeon sng4onnx
# onnxsim==0.4.36 onnxruntime==1.18.1 (see docs/development.md). Runs offline.
# HardSigmoid is rewritten as Clip(0,6)/6 (GPU RELU6); --attention rewrites the SVTR qkv 5-D reshape/transpose/slice
# into Split + 4-D so the whole graph is delegated. Both rewrites are exact (max diff ~1e-5 vs ONNX).
set -euo pipefail
src=$(cd "$(dirname "$1")" && pwd)/$(basename "$1"); width=$2; out=$(cd "$(dirname "$3")" && pwd)/$(basename "$3"); attention=${4:-}
here=$(cd "$(dirname "$0")" && pwd); py=${PYTHON:-python3}
work=$(mktemp -d); trap 'rm -rf "$work"' EXIT
cd "$work"
onnxsim "$src" static.onnx --overwrite-input-shape "x:1,3,48,$width" >/dev/null
"$py" "$here/fix_hsig.py" static.onnx static.onnx
if [ "$attention" = --attention ]; then "$py" "$here/fix_attn.py" static.onnx static.onnx; fi
# onnx2tf checks outputs on a sample image file; give it a local synthetic one so it never downloads.
"$py" -c "import numpy as np; np.save('calibration_image_sample_data_20x128x128x3_float32.npy', np.random.RandomState(0).rand(20,128,128,3).astype(np.float32))"
onnx2tf -i static.onnx -o tf -n >/dev/null
cp tf/static_float32.tflite "$out"
echo "$out"
