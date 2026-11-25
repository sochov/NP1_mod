"""Subset BIZ UDPGothic for the Android Auto dashboard renderer.

The renderer only shows its own UI strings, plate regions, kana and ASCII, so
the 4.6 MB fonts are cut down to those characters.

  pip install fonttools
  python tools/androidauto-font-subset.py --source resources/fonts

--source must contain BIZUDPGothic-Regular.ttf, BIZUDPGothic-Bold.ttf and
OFL.txt from https://github.com/googlefonts/morisawa-biz-ud-gothic (SIL OFL 1.1).
"""
import argparse
import pathlib
import shutil

from fontTools import subset

ROOT = pathlib.Path(__file__).resolve().parents[1]
OUT = ROOT / 'backend/internal/androidauto/render/fonts'
TEXT_SOURCES = [
    ROOT / 'backend/internal/platespeech/regions.go',
    *sorted((ROOT / 'backend/internal/androidauto').rglob('*.go')),
]


def jis_level1_kanji():
    # JIS X 0208 level 1 (rows 16-47): the kanji owners are likely to use in
    # rule names; level 2 would double the size for rare characters.
    out = set()
    for row in range(16, 48):
        for cell in range(1, 95):
            try:
                out.add(bytes([0xA0 + row, 0xA0 + cell]).decode('euc_jp'))
            except UnicodeDecodeError:
                pass
    return out


def characters():
    chars = set(chr(c) for c in range(0x20, 0x7F))
    chars |= jis_level1_kanji()
    chars |= set(chr(c) for c in range(0x3041, 0x3097))  # hiragana
    chars |= set(chr(c) for c in range(0x30A0, 0x3100))  # katakana
    chars |= set('・？…©°％：（）「」、。ー')
    for path in TEXT_SOURCES:
        if path.name.endswith('_test.go'):
            continue
        for ch in path.read_text(encoding='utf-8'):
            if ord(ch) > 0x7F and not ch.isspace():
                chars.add(ch)
    return chars


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--source', required=True, type=pathlib.Path)
    args = parser.parse_args()
    chars = characters()
    OUT.mkdir(parents=True, exist_ok=True)
    for name in ('Regular', 'Bold'):
        src = args.source / f'BIZUDPGothic-{name}.ttf'
        dst = OUT / f'BIZUDPGothic-{name}.subset.ttf'
        opts = subset.Options()
        opts.layout_features = ['kern', 'palt', 'liga']
        opts.name_IDs = ['*']
        opts.notdef_outline = True
        opts.hinting = False
        font = subset.load_font(str(src), opts)
        sub = subset.Subsetter(opts)
        sub.populate(text=''.join(sorted(chars)))
        sub.subset(font)
        subset.save_font(font, str(dst), opts)
        print(dst.relative_to(ROOT), dst.stat().st_size, 'bytes,', len(chars), 'characters')
    shutil.copyfile(args.source / 'OFL.txt', OUT / 'OFL.txt')


if __name__ == '__main__':
    main()
