import unittest
import threading
import time
import urllib.request
import urllib.parse
import json
import sys
import os

sys.path.insert(0, os.path.abspath(os.path.join(os.path.dirname(__file__), "..")))

from app import run_standalone, HTTPServer, PythonServiceHTTPHandler


class TestApp(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.server = HTTPServer(("127.0.0.1", 0), PythonServiceHTTPHandler)
        cls.port = cls.server.server_port
        cls.thread = threading.Thread(target=cls.server.serve_forever)
        cls.thread.daemon = True
        cls.thread.start()
        time.sleep(0.1)

    @classmethod
    def tearDownClass(cls):
        cls.server.shutdown()
        cls.server.server_close()

    def test_health(self):
        url = f"http://127.0.0.1:{self.port}/health"
        with urllib.request.urlopen(url) as resp:
            self.assertEqual(resp.status, 200)
            data = json.loads(resp.read().decode())
            self.assertEqual(data["status"], "healthy")
            self.assertEqual(data["version"], "0.2.0")

    def test_stats_endpoint(self):
        url = f"http://127.0.0.1:{self.port}/stats?numbers=10,20,30"
        with urllib.request.urlopen(url) as resp:
            self.assertEqual(resp.status, 200)
            data = json.loads(resp.read().decode())
            self.assertEqual(data["count"], 3)
            self.assertEqual(data["sum"], 60.0)

    def test_transform_endpoint(self):
        url = f"http://127.0.0.1:{self.port}/transform"
        payload = json.dumps({"text": " Test Python Service "}).encode()
        req = urllib.request.Request(url, data=payload, headers={"Content-Type": "application/json"})
        with urllib.request.urlopen(req) as resp:
            self.assertEqual(resp.status, 200)
            data = json.loads(resp.read().decode())
            self.assertEqual(data["normalized"], "test python service")


if __name__ == "__main__":
    unittest.main()
