import unittest
import subprocess
import sys
import os
import json


class TestCLI(unittest.TestCase):
    def setUp(self):
        self.cli_path = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", "cli.py"))

    def run_cli(self, args):
        cmd = [sys.executable, self.cli_path] + args
        proc = subprocess.run(cmd, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        return proc

    def test_version(self):
        proc = self.run_cli(["--version"])
        self.assertEqual(proc.returncode, 0)
        self.assertIn("0.2.0", proc.stdout + proc.stderr)

    def test_stats(self):
        proc = self.run_cli(["stats", "--numbers", "1.0,2.0,3.0"])
        self.assertEqual(proc.returncode, 0)
        data = json.loads(proc.stdout)
        self.assertEqual(data["count"], 3)
        self.assertEqual(data["sum"], 6.0)

    def test_transform(self):
        proc = self.run_cli(["transform", "--text", "  Hello World  "])
        self.assertEqual(proc.returncode, 0)
        data = json.loads(proc.stdout)
        self.assertEqual(data["normalized"], "hello world")
        self.assertEqual(data["slug"], "hello-world")

    def test_process(self):
        proc = self.run_cli(["process", "--items", '[{"id": 1, "score": 100.0}]'])
        self.assertEqual(proc.returncode, 0)
        data = json.loads(proc.stdout)
        self.assertEqual(data["status"], "ok")
        self.assertEqual(data["valid_count"], 1)


if __name__ == "__main__":
    unittest.main()
