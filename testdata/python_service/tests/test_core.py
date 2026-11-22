import unittest
import sys
import os

sys.path.insert(0, os.path.abspath(os.path.join(os.path.dirname(__file__), "..")))

from core.transform import normalize_text, slugify, calculate_stats
from core.validator import validate_item, sanitize_tags


class TestCore(unittest.TestCase):
    def test_normalize_text(self):
        self.assertEqual(normalize_text("  Hello World  "), "hello world")
        self.assertEqual(normalize_text(""), "")
        self.assertEqual(normalize_text(None), "")

    def test_slugify(self):
        self.assertEqual(slugify("Hello World! 2026"), "hello-world-2026")
        self.assertEqual(slugify(""), "")

    def test_calculate_stats(self):
        res = calculate_stats([1.0, 2.0, 3.0])
        self.assertEqual(res["count"], 3)
        self.assertEqual(res["sum"], 6.0)
        self.assertEqual(res["avg"], 2.0)
        self.assertEqual(res["min"], 1.0)
        self.assertEqual(res["max"], 3.0)

        empty = calculate_stats([])
        self.assertEqual(empty["count"], 0)
        self.assertEqual(empty["sum"], 0.0)

    def test_validate_item(self):
        self.assertTrue(validate_item({"id": 1, "score": 10.5}))
        self.assertTrue(validate_item({"id": "item-1"}))
        self.assertFalse(validate_item("not a dict"))
        self.assertFalse(validate_item({"name": "missing id"}))
        self.assertFalse(validate_item({"id": 1, "score": "invalid score"}))

    def test_sanitize_tags(self):
        self.assertEqual(sanitize_tags([" python ", "GO", "python", ""]), ["python", "go"])
        self.assertEqual(sanitize_tags([]), [])


if __name__ == "__main__":
    unittest.main()
