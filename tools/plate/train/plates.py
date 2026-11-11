"""Synthetic Japanese licence-plate renderer and label space.

Used to train the plate reader (a multi-head classifier over the fixed plate
layout) and to paste plates into detector training images. Fonts are read
from the local system font directory and are never copied into outputs.
"""
from __future__ import annotations

import math
import os
import random
from dataclasses import dataclass
from functools import lru_cache
from pathlib import Path

import cv2
import numpy as np
from PIL import Image, ImageDraw, ImageFont

# 地名 shown on plates (transport branch offices and regional "ご当地" names).
# Names not listed here cannot be read; retrain after extending the list.
REGIONS = """
札幌 函館 旭川 室蘭 苫小牧 釧路 知床 帯広 北見
青森 八戸 弘前 岩手 盛岡 平泉 宮城 仙台 秋田 山形 庄内 福島 会津 郡山 白河 いわき
水戸 土浦 つくば 宇都宮 那須 とちぎ 群馬 前橋 高崎 大宮 川口 所沢 川越 熊谷 春日部 越谷
千葉 成田 習志野 市川 船橋 袖ヶ浦 市原 野田 柏 松戸
品川 世田谷 練馬 杉並 板橋 足立 江東 葛飾 八王子 多摩 横浜 川崎 湘南 相模 山梨 富士山
新潟 長岡 上越 富山 石川 金沢 福井 長野 松本 諏訪 岐阜 飛騨 静岡 浜松 沼津 伊豆
名古屋 尾張小牧 一宮 春日井 豊橋 三河 岡崎 豊田 鈴鹿 三重 伊勢志摩 四日市
滋賀 京都 大阪 なにわ 和泉 堺 神戸 姫路 奈良 飛鳥 和歌山
鳥取 島根 出雲 岡山 倉敷 広島 福山 山口 下関 徳島 香川 高松 愛媛 高知
福岡 北九州 久留米 筑豊 佐賀 長崎 佐世保 熊本 大分 宮崎 鹿児島 奄美 沖縄
""".split()

KANA_PRIVATE = list("さすせそたちつてとなにぬねのはひふほまみむめもやゆよらりるろ")
KANA_COMMERCIAL = list("あいうえかきくけこを")
KANA_RENTAL = list("われ")
KANA = KANA_COMMERCIAL + KANA_PRIVATE + KANA_RENTAL
CLASS_LETTERS = list("ACFHKLMPXY")
CLASS_CHARS = [""] + list("0123456789") + CLASS_LETTERS
NUMBER_CHARS = [""] + list("0123456789")

# Head layout shared by the model, the converter and the device engine.
HEADS = (
    [("region", [""] + REGIONS)]
    + [("class", CLASS_CHARS)] * 3
    + [("kana", [""] + KANA)]
    + [("number", NUMBER_CHARS)] * 4
)
HEAD_SIZES = [len(labels) for _, labels in HEADS]

STYLES = {  # background, text
    "private": ((244, 244, 238), (12, 92, 56)),
    "kei": ((240, 208, 30), (20, 20, 20)),
    "commercial": ((14, 104, 58), (245, 245, 240)),
    "kei_commercial": ((22, 22, 22), (240, 205, 40)),
}

FONT_DIR = Path(os.environ.get("WINDIR", "C:/Windows")) / "Fonts"
KANJI_FONTS = ["YuGothB.ttc", "meiryob.ttc", "NotoSansJP-Bold.ttf", "BIZ-UDGothicB.ttc", "FOT-NewRodinPro-DB.otf", "FOT-NewRodinPro-B.otf", "msgothic.ttc"]
DIGIT_FONTS = ["bahnschrift.ttf", "DejaVuSansCondensed-Bold.ttf", "LiberationSansNarrow-Bold.ttf", "ARIALNB.TTF", "ARIALN.TTF", "arialbd.ttf", "FOT-NewRodinPro-DB.otf", "YuGothB.ttc"]


@lru_cache(maxsize=None)
def _available(names: tuple[str, ...]) -> list[str]:
    found = [str(FONT_DIR / n) for n in names if (FONT_DIR / n).is_file()]
    if not found:
        raise SystemExit(f"no usable fonts in {FONT_DIR}")
    return found


@lru_cache(maxsize=256)
def font(path: str, size: int) -> ImageFont.FreeTypeFont:
    f = ImageFont.truetype(path, size)
    if path.endswith("bahnschrift.ttf"):
        try:
            f.set_variation_by_name(random.choice([b"Bold SemiCondensed", b"SemiBold Condensed", b"Bold Condensed", b"Bold"]))
        except Exception:
            pass
    return f


@dataclass
class PlateText:
    region: str
    klass: str
    kana: str
    number: str  # 1-4 digits, no dots
    style: str

    def targets(self) -> list[int]:
        out = [HEADS[0][1].index(self.region)]
        out += [CLASS_CHARS.index(c) for c in list(self.klass) + [""] * (3 - len(self.klass))]
        out += [HEADS[4][1].index(self.kana)]
        digits = [""] * (4 - len(self.number)) + list(self.number)
        out += [NUMBER_CHARS.index(d) for d in digits]
        return out


def random_text(rng: random.Random) -> PlateText:
    style = rng.choices(list(STYLES), weights=[62, 25, 9, 4])[0]
    if style in ("commercial", "kei_commercial"):
        kana = rng.choice(KANA_COMMERCIAL)
    else:
        kana = rng.choice(KANA_RENTAL) if rng.random() < 0.08 else rng.choice(KANA_PRIVATE)
    n = rng.choices([3, 2, 1], weights=[85, 13, 2])[0]
    first = rng.choice("35" if style in ("private", "kei") and rng.random() < 0.8 else "123456789")
    rest = []
    for i in range(n - 1):
        if n == 3 and rng.random() < 0.12:
            rest.append(rng.choice(CLASS_LETTERS))
        else:
            rest.append(rng.choice("0123456789"))
    klass = first + "".join(rest)
    length = rng.choices([4, 3, 2, 1], weights=[70, 15, 10, 5])[0]
    number = str(rng.randint(1, 9)) + "".join(rng.choice("0123456789") for _ in range(length - 1))
    return PlateText(rng.choice(REGIONS), klass, kana, number, style)


def _fit_text(draw, text, path, box, rng, condensed=1.0):
    """Draw text scaled to fill box=(x0,y0,x1,y1); returns nothing."""
    x0, y0, x1, y1 = box
    w, h = x1 - x0, y1 - y0
    size = max(8, int(h * 1.25) // 2 * 2)  # quantized so the font cache hits
    f = font(path, size)
    l, t, r, b = draw.textbbox((0, 0), text, font=f)
    tw, th = max(1, r - l), max(1, b - t)
    img = Image.new("L", (tw + 4, th + 4), 0)
    ImageDraw.Draw(img).text((2 - l, 2 - t), text, font=f, fill=255)
    target_w = min(w, int(tw * h / th * condensed))
    img = img.resize((max(1, target_w), h), Image.BILINEAR)
    return img, x0 + (w - target_w) // 2, y0


def render(text: PlateText, rng: random.Random, scale: float = 2.0) -> tuple[np.ndarray, np.ndarray]:
    """Render a 330x165 mm plate at `scale` px/mm. Returns RGB image and a
    full-plate text mask (for debugging)."""
    W, H = int(330 * scale), int(165 * scale)
    bg, fg = STYLES[text.style]
    jitter = lambda c: tuple(int(np.clip(v + rng.gauss(0, 10), 0, 255)) for v in c)
    bg, fg = jitter(bg), jitter(fg)
    plate = Image.new("RGB", (W, H), bg)
    draw = ImageDraw.Draw(plate)
    if text.style == "private" and rng.random() < 0.15:  # graphic-design plates
        for _ in range(rng.randint(3, 8)):
            c = tuple(rng.randint(120, 255) for _ in range(3))
            x, y, r = rng.randint(0, W), rng.randint(int(H * 0.55), H), rng.randint(20, 120)
            draw.ellipse((x - r, y - r, x + r, y + r), fill=tuple((a + b) // 2 for a, b in zip(c, bg)))
    s = scale
    kanji = rng.choice(_available(tuple(KANJI_FONTS)))
    digit = rng.choice(_available(tuple(DIGIT_FONTS)))
    mask = Image.new("L", (W, H), 0)

    def paste(img, x, y):
        plate.paste(Image.new("RGB", img.size, fg), (x, y), img)
        mask.paste(img, (x, y), img)

    top = (int(14 * s), int(54 * s))
    reg_w = int((22 + 22 * len(text.region)) * s)
    reg_x0 = int(max(40, 120 - reg_w / s / 2) * s)
    img, x, y = _fit_text(draw, text.region, kanji, (reg_x0, top[0], min(reg_x0 + reg_w, int(175 * s)), top[1]), rng, condensed=0.95)
    paste(img, x, y)
    img, x, y = _fit_text(draw, text.klass, digit, (int(180 * s), top[0], int(285 * s), top[1]), rng, condensed=0.9)
    paste(img, x, y)
    img, x, y = _fit_text(draw, text.kana, kanji, (int(16 * s), int(88 * s), int(60 * s), int(132 * s)), rng)
    paste(img, x, y)
    # Serial: 4 slots, hyphen between slot 2 and 3 for 4 digits, dots for blanks.
    slots = [""] * (4 - len(text.number)) + list(text.number)
    slot_w, gap = 52, 8
    xs = [70, 70 + slot_w + gap, 70 + 2 * (slot_w + gap) + 14, 70 + 3 * (slot_w + gap) + 14]
    for sx, ch in zip(xs, slots):
        if ch:
            img, x, y = _fit_text(draw, ch, digit, (int(sx * s), int(64 * s), int((sx + slot_w) * s), int(156 * s)), rng, condensed=1.0)
            paste(img, x, y)
        else:
            r = int(6 * s)
            cx, cy = int((sx + slot_w / 2) * s), int(110 * s)
            draw.ellipse((cx - r, cy - r, cx + r, cy + r), fill=fg)
    if len(text.number) == 4:
        y0 = int(106 * s)
        draw.rectangle((int(185 * s), y0, int(203 * s), y0 + int(7 * s)), fill=fg)
    for bx in (int(70 * s), int(260 * s)):  # mounting bolts
        r = int(rng.uniform(4, 7) * s)
        draw.ellipse((bx - r, int(8 * s) - r // 2, bx + r, int(8 * s) + r + r // 2), fill=tuple(int(v * 0.75) for v in bg))
    arr = np.array(plate)
    if rng.random() < 0.6:  # frame / border
        t = int(rng.uniform(2, 7) * s)
        color = rng.choice([(200, 200, 205), (30, 30, 30), (120, 120, 125)])
        cv2.rectangle(arr, (0, 0), (W - 1, H - 1), color, t)
    return arr, np.array(mask)


def degrade(img: np.ndarray, rng: random.Random, out_w: int, out_h: int, plate_px: int) -> np.ndarray:
    """Simulate a dash-camera view: downscale to plate_px wide, blur, noise,
    compression, lighting, then resample to the reader input size (gray)."""
    h, w = img.shape[:2]
    small_h = max(8, int(round(h * plate_px / w)))
    a = cv2.resize(img, (plate_px, small_h), interpolation=cv2.INTER_AREA)
    if rng.random() < 0.35:
        k = 3 if plate_px < 70 else rng.choice([3, 5])
        kernel = np.zeros((k, k), np.float32)
        if rng.random() < 0.5:
            kernel[k // 2, :] = 1
        else:
            kernel[:, k // 2] = 1
        a = cv2.filter2D(a, -1, kernel / k)
    if rng.random() < 0.5:
        a = cv2.GaussianBlur(a, (0, 0), rng.uniform(0.3, 0.8))
    a = a.astype(np.float32)
    gain = rng.uniform(0.55, 1.35)
    if rng.random() < 0.15:
        gain *= rng.uniform(0.4, 0.7)  # night
    a = a * gain + rng.uniform(-30, 30)
    gamma = rng.uniform(0.7, 1.4)
    a = 255 * np.clip(a / 255, 0, 1) ** gamma
    if rng.random() < 0.3:  # glare
        gx = rng.uniform(0, a.shape[1])
        xs = np.arange(a.shape[1])[None, :, None]
        a = a + rng.uniform(40, 140) * np.exp(-((xs - gx) ** 2) / (2 * (a.shape[1] * 0.15) ** 2))
    a = a + rng.gauss(0, 1) * rng.uniform(0, 12) * np.random.default_rng(rng.randrange(1 << 30)).standard_normal(a.shape)
    a = np.clip(a, 0, 255).astype(np.uint8)
    q = rng.randint(15, 90)
    ok, buf = cv2.imencode(".jpg", a, [cv2.IMWRITE_JPEG_QUALITY, q])
    a = cv2.imdecode(buf, cv2.IMREAD_COLOR)
    gray = cv2.cvtColor(a, cv2.COLOR_RGB2GRAY)
    return cv2.resize(gray, (out_w, out_h), interpolation=cv2.INTER_LINEAR)


def place_with_context(plate: np.ndarray, rng: random.Random, margin: float = 0.06) -> np.ndarray:
    """Warp the plate into a background and crop it as the detector + engine
    would (jittered box expanded by `margin`)."""
    h, w = plate.shape[:2]
    canvas_w, canvas_h = int(w * 2.2), int(h * 2.6)
    bg = np.full((canvas_h, canvas_w, 3), [rng.randint(0, 255) for _ in range(3)], np.uint8)
    noise = np.random.default_rng(rng.randrange(1 << 30)).integers(0, 40, bg.shape, dtype=np.uint8)
    bg = cv2.add(bg, noise)
    cx, cy = canvas_w / 2, canvas_h / 2
    angle = math.radians(rng.uniform(-7, 7))
    persp = rng.uniform(-0.12, 0.12)
    sx = rng.uniform(0.85, 1.1)
    corners = np.array([[-w / 2, -h / 2], [w / 2, -h / 2], [w / 2, h / 2], [-w / 2, h / 2]], np.float32)
    corners[:, 0] *= sx
    corners[[0, 3], 1] *= 1 + persp
    corners[[1, 2], 1] *= 1 - persp
    rot = np.array([[math.cos(angle), -math.sin(angle)], [math.sin(angle), math.cos(angle)]], np.float32)
    dst = corners @ rot.T + np.array([cx, cy], np.float32)
    src = np.array([[0, 0], [w, 0], [w, h], [0, h]], np.float32)
    M = cv2.getPerspectiveTransform(src, dst)
    warped = cv2.warpPerspective(plate, M, (canvas_w, canvas_h))
    m = cv2.warpPerspective(np.full((h, w), 255, np.uint8), M, (canvas_w, canvas_h))
    out = np.where(m[..., None] > 127, warped, bg)
    x0, y0 = dst[:, 0].min(), dst[:, 1].min()
    bw, bh = dst[:, 0].max() - x0, dst[:, 1].max() - y0
    # Detector box jitter, then the engine's margin.
    jx, jy = rng.gauss(0, 0.04) * bw, rng.gauss(0, 0.06) * bh
    js = rng.uniform(0.9, 1.12)
    bw2, bh2 = bw * js, bh * rng.uniform(0.9, 1.12)
    x0 = x0 + jx - (bw2 - bw) / 2 - bw2 * margin
    y0 = y0 + jy - (bh2 - bh) / 2 - bh2 * margin
    bw2, bh2 = bw2 * (1 + 2 * margin), bh2 * (1 + 2 * margin)
    x0, y0 = int(max(0, x0)), int(max(0, y0))
    x1, y1 = int(min(canvas_w, x0 + bw2)), int(min(canvas_h, y0 + bh2))
    return out[y0:y1, x0:x1]


def sample(rng: random.Random, out_w: int = 192, out_h: int = 96) -> tuple[np.ndarray, list[int], PlateText]:
    text = random_text(rng)
    plate_px = int(math.exp(rng.uniform(math.log(26), math.log(150))))
    # Render only as large as needed: ~3x the final width, but never tiny glyphs.
    scale = min(2.0, max(0.7, plate_px * 3 / 330))
    img, _ = render(text, rng, scale=scale)
    crop = place_with_context(img, rng)
    return degrade(crop, rng, out_w, out_h, plate_px), text.targets(), text


if __name__ == "__main__":
    import sys
    rng = random.Random(int(sys.argv[1]) if len(sys.argv) > 1 else 0)
    out = Path(sys.argv[2] if len(sys.argv) > 2 else "plate-samples")
    out.mkdir(parents=True, exist_ok=True)
    tiles = []
    for i in range(24):
        g, t, text = sample(rng)
        tiles.append(cv2.copyMakeBorder(g, 2, 2, 2, 2, cv2.BORDER_CONSTANT, value=0))
        clean, _ = render(text, rng)
        cv2.imwrite(str(out / f"clean-{i:02d}.png"), cv2.cvtColor(clean, cv2.COLOR_RGB2BGR))
    rows = [np.hstack(tiles[i:i + 6]) for i in range(0, 24, 6)]
    cv2.imwrite(str(out / "degraded-grid.png"), np.vstack(rows))
    print("wrote", out)
