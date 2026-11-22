"""
Validation and sanitization utilities.
Wave 0 Leaf Unit: core/validator
"""

from typing import Dict, Any, List


def validate_item(item: Dict[str, Any]) -> bool:
    """Validate item dictionary structure and fields."""
    if not isinstance(item, dict):
        return False
    if "id" not in item:
        return False
    score = item.get("score")
    if score is not None and not isinstance(score, (int, float)):
        return False
    return True


def sanitize_tags(tags: List[str]) -> List[str]:
    """Sanitize and deduplicate a list of tag strings."""
    if not tags:
        return []
    seen = set()
    result = []
    for tag in tags:
        if isinstance(tag, str):
            clean = tag.strip().lower()
            if clean and clean not in seen:
                seen.add(clean)
                result.append(clean)
    return result
