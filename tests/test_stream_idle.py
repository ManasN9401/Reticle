from pathlib import Path
import sys
import threading
import time
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "cmd/forge/compiler/lib"))
import worker_sdk

pump = worker_sdk._stream_with_first_event_timeout


def blocked_after(chunks, release):
    """A provider stream that sends some chunks and then holds the connection open, silent."""
    for chunk in chunks:
        yield chunk
    release.wait(30)


class StreamSilence(unittest.TestCase):
    def setUp(self):
        self.release = threading.Event()
        self.addCleanup(self.release.set)

    def test_a_stream_that_goes_silent_after_it_started_is_cut_off(self):
        # Observed: a node held for 100 minutes by a request that never finished. Only the first
        # chunk used to be bounded, so a stream silent from the second chunk on waited forever.
        started = time.monotonic()
        with self.assertRaises(TimeoutError) as caught:
            list(pump(blocked_after(["a", "b"], self.release), 5, idle_seconds=0.2))
        self.assertLess(time.monotonic() - started, 3)
        # The dispatcher resumes on another route when the message reads as a timeout.
        self.assertIn("went silent", str(caught.exception))
        self.assertIn("timed out", str(caught.exception))

    def test_chunks_received_before_the_silence_are_still_delivered(self):
        seen = []
        with self.assertRaises(TimeoutError):
            for chunk in pump(blocked_after(["a", "b"], self.release), 5, idle_seconds=0.2):
                seen.append(chunk)
        self.assertEqual(seen, ["a", "b"])

    def test_a_stream_that_never_starts_keeps_its_own_message(self):
        with self.assertRaisesRegex(TimeoutError, "no stream event within"):
            list(pump(blocked_after([], self.release), 0.2, idle_seconds=60))

    def test_a_slow_but_steady_stream_is_not_cut_off(self):
        def steady():
            for index in range(4):
                time.sleep(0.1)
                yield index
        self.assertEqual(list(pump(steady(), 5, idle_seconds=0.5)), [0, 1, 2, 3])

    def test_a_stream_that_keeps_trickling_still_ends_at_the_time_budget(self):
        def trickle():
            while not self.release.is_set():
                time.sleep(0.05)
                yield "."
        started = time.monotonic()
        with self.assertRaisesRegex(TimeoutError, "time budget exhausted"):
            list(pump(trickle(), 5, idle_seconds=5, deadline=time.monotonic() + 0.4))
        self.assertLess(time.monotonic() - started, 3)

    def test_without_the_new_options_behaviour_is_unchanged(self):
        self.assertEqual(list(pump(iter([1, 2, 3]), 5)), [1, 2, 3])


if __name__ == "__main__":
    unittest.main()
