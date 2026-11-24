import unittest
from worker import process_job

class TestWorker(unittest.TestCase):
    def test_calc_stats_happy(self):
        res = process_job("calc_stats", {"numbers": [10, 20, 30, 40, 50]})
        self.assertEqual(res["status"], "success")
        self.assertEqual(res["result"]["count"], 5)
        self.assertEqual(res["result"]["mean"], 30.0)
        self.assertEqual(res["result"]["min"], 10.0)
        self.assertEqual(res["result"]["max"], 50.0)

    def test_calc_stats_empty(self):
        res = process_job("calc_stats", {"numbers": []})
        self.assertEqual(res["status"], "error")
        self.assertIn("error", res)

    def test_hash_payload_sha256(self):
        res = process_job("hash_payload", {"data": "hello world", "algo": "sha256"})
        self.assertEqual(res["status"], "success")
        self.assertEqual(res["result"]["digest"], "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9")

    def test_hash_payload_invalid_algo(self):
        res = process_job("hash_payload", {"data": "test", "algo": "unknown"})
        self.assertEqual(res["status"], "error")

    def test_filter_records_happy(self):
        records = [
            {"id": "rec-1", "score": 95.5, "category": "A"},
            {"id": "rec-2", "score": 45.0, "category": "B"},
            {"id": "rec-3", "score": 88.0, "category": "A"}
        ]
        res = process_job("filter_records", {"records": records, "min_score": 50.0, "category": "A"})
        self.assertEqual(res["status"], "success")
        self.assertEqual(res["result"]["filtered_count"], 2)

    def test_unknown_task(self):
        res = process_job("non_existent", {})
        self.assertEqual(res["status"], "error")
        self.assertIn("unknown task", res["error"])

if __name__ == "__main__":
    unittest.main()
