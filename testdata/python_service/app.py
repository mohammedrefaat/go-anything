#!/usr/bin/env python3
"""
Python FastAPI / HTTP Service Entry Point.
Wave 2 Unit: cmd/api (or app.py)
Transports: HTTP
"""

import sys
import os
import json
import urllib.parse
from http.server import HTTPServer, BaseHTTPRequestHandler

sys.path.insert(0, os.path.abspath(os.path.dirname(__file__)))

from service.processor import DataProcessor

processor = DataProcessor()

# -----------------------------------------------------------------------------
# Optional FastAPI Application Definition
# -----------------------------------------------------------------------------
try:
    from fastapi import FastAPI, Response
    from pydantic import BaseModel
    from typing import Optional, List, Dict, Any

    app = FastAPI(title="Python Dogfood Service", version="0.2.0")

    class TransformRequest(BaseModel):
        text: str

    class ProcessRequest(BaseModel):
        items: List[Dict[str, Any]]

    @app.get("/health")
    def health():
        return {"status": "healthy", "service": "python_service", "version": "0.2.0"}

    @app.get("/stats")
    def get_stats(numbers: str):
        nums = [float(x.strip()) for x in numbers.split(",") if x.strip()]
        return processor.summarize_numbers(nums)

    @app.post("/transform")
    def post_transform(req: TransformRequest):
        return processor.process_text(req.text)

    @app.post("/process")
    def post_process(req: ProcessRequest):
        return processor.process_items(req.items)

except ImportError:
    app = None


# -----------------------------------------------------------------------------
# Standalone Pure-Python HTTP Handler (Zero external dependencies for testing)
# -----------------------------------------------------------------------------
class PythonServiceHTTPHandler(BaseHTTPRequestHandler):
    def _send_json(self, status: int, data: dict):
        body = json.dumps(data, sort_keys=True).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        parsed = urllib.parse.urlparse(self.path)
        path = parsed.path
        query = urllib.parse.parse_qs(parsed.query)

        if path == "/health":
            self._send_json(200, {"status": "healthy", "service": "python_service", "version": "0.2.0"})
        elif path == "/stats":
            numbers_param = query.get("numbers", [""])[0]
            if not numbers_param:
                self._send_json(400, {"error": "missing numbers parameter"})
                return
            try:
                nums = [float(x.strip()) for x in numbers_param.split(",") if x.strip()]
                res = processor.summarize_numbers(nums)
                self._send_json(200, res)
            except ValueError as e:
                self._send_json(400, {"error": str(e)})
        else:
            self._send_json(404, {"error": "not found"})

    def do_POST(self):
        parsed = urllib.parse.urlparse(self.path)
        path = parsed.path
        length = int(self.headers.get("Content-Length", 0))
        body = self.rfile.read(length) if length > 0 else b"{}"

        try:
            payload = json.loads(body.decode("utf-8")) if body else {}
        except Exception as e:
            self._send_json(400, {"error": f"invalid json: {e}"})
            return

        if path == "/transform":
            text = payload.get("text", "")
            res = processor.process_text(text)
            self._send_json(200, res)
        elif path == "/process":
            items = payload.get("items", [])
            if not isinstance(items, list):
                items = [items]
            res = processor.process_items(items)
            self._send_json(200, res)
        else:
            self._send_json(404, {"error": "not found"})

    def log_message(self, format, *args):
        # Suppress noisy request logs in automated test runs
        pass


def run_standalone(host="127.0.0.1", port=8080):
    server = HTTPServer((host, port), PythonServiceHTTPHandler)
    server.serve_forever()


if __name__ == "__main__":
    port = 8080
    if len(sys.argv) > 1:
        try:
            port = int(sys.argv[1])
        except ValueError:
            pass
    run_standalone(port=port)
