#!/usr/bin/env python3
"""
Python CLI Entry Point.
Wave 2 Unit: cmd/cli (or cli.py)
Transports: Subprocess
"""

import sys
import os
import json
import argparse

sys.path.insert(0, os.path.abspath(os.path.dirname(__file__)))

from service.processor import DataProcessor


def main():
    parser = argparse.ArgumentParser(description="Python Dogfood CLI Service")
    parser.add_argument("--version", action="version", version="python-service 0.2.0")

    subparsers = parser.add_subparsers(dest="command", help="Available subcommands")

    # Command: stats
    stats_parser = subparsers.add_parser("stats", help="Compute numerical statistics")
    stats_parser.add_argument("--numbers", type=str, required=True, help="Comma-separated numbers")

    # Command: transform
    transform_parser = subparsers.add_parser("transform", help="Transform input text")
    transform_parser.add_argument("--text", type=str, required=True, help="Input string to transform")

    # Command: process
    process_parser = subparsers.add_parser("process", help="Process JSON batch items")
    process_parser.add_argument("--items", type=str, required=True, help="JSON encoded item list")

    args = parser.parse_args()

    processor = DataProcessor()

    if args.command == "stats":
        raw_items = [s.strip() for s in args.numbers.split(",") if s.strip()]
        try:
            numbers = [float(x) for x in raw_items]
        except ValueError as err:
            sys.stderr.write(f"Error parsing numbers: {err}\n")
            sys.exit(1)
        res = processor.summarize_numbers(numbers)
        print(json.dumps(res, sort_keys=True))

    elif args.command == "transform":
        res = processor.process_text(args.text)
        print(json.dumps(res, sort_keys=True))

    elif args.command == "process":
        try:
            items_data = json.loads(args.items)
            if not isinstance(items_data, list):
                if isinstance(items_data, dict) and "items" in items_data:
                    items_data = items_data["items"]
                else:
                    items_data = [items_data]
        except Exception as err:
            sys.stderr.write(f"Error parsing items JSON: {err}\n")
            sys.exit(1)
        res = processor.process_items(items_data)
        print(json.dumps(res, sort_keys=True))

    else:
        parser.print_help(sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
