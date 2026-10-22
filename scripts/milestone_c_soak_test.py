import copy
import unittest

from milestone_c_soak_check import summarize


class SoakReportTest(unittest.TestCase):
    def fixture(self):
        fields = ("RSS", "RSSHighWater", "Cgroup", "CgroupPeak", "Anon", "FileCache", "HeapAlloc", "HeapLive", "GoSys", "Goroutines", "Pending", "StagedParts", "DiskBytes", "StagedBytes", "CacheBytes", "OOMEvents", "OOMKills", "PublishedIncomplete", "MixedUploads", "FullDownloads", "CPUUsec", "TotalAlloc")
        sample = dict.fromkeys(fields, 0)
        return {"mixed_configured_seconds": 1800, "mixed_uploads": 4, "full_http_downloads": 4, "peak_remote_writers": 1, "samples": [dict(sample, seconds=i, phase="mixed_upload_copy_preview", MixedUploads=i // 60, FullDownloads=i // 30) for i in range(1800)]}

    def test_windows_use_observed_time(self):
        report = self.fixture()
        report["samples"][299]["CPUUsec"] = 299000000
        result = summarize(report)
        self.assertEqual(6, len(result["five_minute_windows"]))
        self.assertEqual(100, result["five_minute_windows"][0]["average_cpu_percent"])

    def test_rejects_unsafe_or_incomplete_evidence(self):
        valid = self.fixture()
        for field, value in (("OOMKills", 1), ("PublishedIncomplete", 1), ("Pending", 5), ("CacheBytes", (256 << 20) + 4097), ("DiskBytes", (4 << 30) + 1)):
            with self.subTest(field=field):
                report = copy.deepcopy(valid)
                report["samples"][900][field] = value
                with self.assertRaises(AssertionError):
                    summarize(report)
        with self.assertRaises(AssertionError):
            summarize(dict(valid, mixed_configured_seconds=65))
        with self.assertRaises(AssertionError):
            summarize(dict(valid, samples=valid["samples"][:20]))
        stalled = self.fixture()
        for sample in stalled["samples"]:
            sample["MixedUploads"] = 0
        with self.assertRaises(AssertionError):
            summarize(stalled)


if __name__ == "__main__":
    unittest.main()
