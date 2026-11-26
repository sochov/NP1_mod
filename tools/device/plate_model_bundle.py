"""Resolve the checked model bundle used by builds and device updates."""
import argparse
import hashlib
import json
import re
import shutil
from pathlib import Path


DOCUMENTS = ("model-manifest.json", "README.md", "LICENSING.md", "ONNX_RUNTIME_LICENSE.txt", "PPOCR_MODEL_CARD.md")


def model_sources(directory: Path) -> dict[str, Path]:
    files = json.loads((directory / "model-manifest.json").read_text(encoding="utf-8"))["files"]
    if not isinstance(files, dict) or not {"detector.tflite", "ocr.tflite", "plate-model.json"} <= files.keys():
        raise ValueError("Model manifest must list the detector, reader and engine configuration")
    for name, digest in files.items():
        if not re.fullmatch(r"[A-Za-z0-9_][A-Za-z0-9_.-]*\.(?:tflite|onnx|mnn|json)", name):
            raise ValueError(f"Invalid model bundle filename: {name}")
        if not re.fullmatch(r"[a-f0-9]{64}", digest) or hashlib.sha256((directory / name).read_bytes()).hexdigest() != digest:
            raise ValueError(f"Model manifest SHA-256 mismatch: {name}")
    sources = {name: directory / name for name in (*files, *DOCUMENTS)}
    notices = directory / "notices"
    if notices.is_dir():
        sources.update((f"notices/{p.name}", p) for p in sorted(notices.iterdir()) if p.is_file())
    for source in sources.values():
        if not source.is_file():
            raise ValueError(f"Missing model bundle file: {source.name}")
    return sources


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("source", type=Path)
    parser.add_argument("destination", type=Path)
    args = parser.parse_args()
    try:
        for name, source in model_sources(args.source).items():
            destination = args.destination / name
            destination.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(source, destination)
    except (KeyError, OSError, TypeError, ValueError) as error:
        parser.error(str(error))


if __name__ == "__main__":
    main()
