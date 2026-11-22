"""
Business logic service processor.
Wave 1 Unit: service/processor
Depends on: core/transform, core/validator
"""

import sys
import os
from typing import List, Dict, Any

# Ensure core package can be imported regardless of execution context
sys.path.insert(0, os.path.abspath(os.path.join(os.path.dirname(__file__), "..")))

from core.transform import normalize_text, calculate_stats, slugify
from core.validator import validate_item, sanitize_tags


class DataProcessor:
    def __init__(self, prefix: str = ""):
        self.prefix = prefix

    def process_text(self, text: str) -> Dict[str, Any]:
        """Normalize text and generate slug and length."""
        norm = normalize_text(text)
        slug = slugify(text)
        return {
            "original": text,
            "normalized": norm,
            "slug": slug,
            "length": len(norm),
        }

    def process_items(self, items: List[Dict[str, Any]]) -> Dict[str, Any]:
        """Validate and aggregate items."""
        valid_items = []
        scores = []
        for item in items:
            if validate_item(item):
                valid_items.append(item)
                if "score" in item and item["score"] is not None:
                    scores.append(float(item["score"]))

        stats = calculate_stats(scores)
        return {
            "status": "ok",
            "total_received": len(items),
            "valid_count": len(valid_items),
            "stats": stats,
            "items": valid_items,
        }

    def summarize_numbers(self, numbers: List[float]) -> Dict[str, Any]:
        """Summarize numerical list."""
        return calculate_stats(numbers)
