"""
Text and numerical transformation utilities.
Wave 0 Leaf Unit: core/transform
"""

import re
from typing import List, Dict, Any


def normalize_text(text: str) -> str:
    """Strip whitespace and lowercase text."""
    if text is None:
        return ""
    return text.strip().lower()


def slugify(text: str) -> str:
    """Generate a clean URL slug from input text."""
    if text is None:
        return ""
    cleaned = re.sub(r"[^\w\s-]", "", text.lower()).strip()
    return re.sub(r"[-\s]+", "-", cleaned)


def calculate_stats(numbers: List[float]) -> Dict[str, Any]:
    """Calculate count, sum, average, min, and max for a list of numbers."""
    if not numbers:
        return {
            "count": 0,
            "sum": 0.0,
            "avg": 0.0,
            "min": 0.0,
            "max": 0.0,
        }

    total = float(sum(numbers))
    count = len(numbers)
    return {
        "count": count,
        "sum": total,
        "avg": total / count,
        "min": float(min(numbers)),
        "max": float(max(numbers)),
    }
