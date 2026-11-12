"""Label-space checks for the synthetic plate generator (no fonts needed)."""
import random
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "tools" / "plate" / "train"))
import plates  # noqa: E402


def decode(targets):
    text = {"region": "", "class": "", "kana": "", "number": ""}
    for (field, labels), index in zip(plates.HEADS, targets):
        text[field] += labels[index]
    return text


class PlateLabels(unittest.TestCase):
    def test_targets_round_trip(self):
        rng = random.Random(5)
        for _ in range(2000):
            t = plates.random_text(rng)
            got = decode(t.targets())
            self.assertEqual(got, {"region": t.region, "class": t.klass, "kana": t.kana, "number": t.number})

    def test_label_space_is_consistent(self):
        self.assertEqual(len(plates.HEADS), 9)
        self.assertEqual(len(set(plates.REGIONS)), len(plates.REGIONS))
        self.assertEqual(plates.HEAD_SIZES, [len(labels) for _, labels in plates.HEADS])
        for field, labels in plates.HEADS:
            self.assertEqual(labels[0], "", field)  # index 0 is always "unread / blank"
        for kana in "おしへん":
            self.assertNotIn(kana, plates.KANA)  # never used on plates

    def test_serial_never_starts_with_zero(self):
        rng = random.Random(9)
        for _ in range(2000):
            n = plates.random_text(rng).number
            self.assertTrue(1 <= len(n) <= 4 and n[0] != "0", n)


if __name__ == "__main__":
    unittest.main()
