"""Unit tests for the 5 Stage-2 cache eviction policies."""

import random
import unittest

from sim.policies import (
    CLOCK2Cache,
    CLOCKCache,
    FIFOCache,
    S3FIFOCache,
    SecondChanceCache,
    make_cache,
)


class TestPolicies(unittest.TestCase):
    def test_fifo_belady_classic(self) -> None:
        trace = [1, 2, 3, 4, 1, 2, 5, 1, 2, 3, 4, 5]
        c3 = FIFOCache(3)
        c4 = FIFOCache(4)
        m3 = sum(0 if c3.access(x) else 1 for x in trace)
        m4 = sum(0 if c4.access(x) else 1 for x in trace)
        self.assertEqual(m3, 9)
        self.assertEqual(m4, 10)

    def test_clock_and_clock2_hand_step(self) -> None:
        c = CLOCKCache(2)
        self.assertFalse(c.access(0))
        self.assertFalse(c.access(1))
        self.assertTrue(c.access(0))  # bits=[1,0], hand=0
        self.assertFalse(c.access(2))  # clears slot 0 (0->0), evicts slot 1 (1), puts 2 at slot 1, hand=0
        self.assertEqual(c.slots, [0, 2])
        self.assertEqual(c.hand, 0)

        c2 = CLOCK2Cache(2)
        self.assertFalse(c2.access(0))
        self.assertFalse(c2.access(1))
        self.assertTrue(c2.access(0))
        self.assertTrue(c2.access(0))  # freq=[2,0]
        self.assertTrue(c2.access(0))  # capped at 2
        self.assertEqual(c2.freq[0], 2)
        self.assertFalse(c2.access(2))  # decrements 0->1, evicts 1, puts 2 at slot 1
        self.assertEqual(c2.slots, [0, 2])
        self.assertEqual(c2.freq, [1, 0])

    def test_second_chance_reinsertion(self) -> None:
        sc = SecondChanceCache(2)
        sc.access(0)
        sc.access(1)  # head=1 -> tail=0
        sc.access(0)  # 0.visited=True
        sc.access(2)  # 0 moved to head (visited=False), 1 evicted
        self.assertEqual(set(sc.table.keys()), {0, 2})

    def test_s3fifo_promotion_and_ghost(self) -> None:
        # C=10 -> S=1, M=9, G=9
        s3 = S3FIFOCache(10)
        self.assertEqual((s3.small_cap, s3.main_cap), (1, 9))
        s3.access(100)  # Small=[100]
        s3.access(100)  # freq=1 in Small
        s3.access(101)  # evicts 100 from Small -> promoted to Main! Small=[101], Main=[100]
        self.assertEqual(s3.small.size, 1)
        self.assertEqual(s3.main.size, 1)
        s3.access(102)  # 101 evicted from Small (freq=0) -> Ghost=[101]
        self.assertIn(101, s3.ghost_set)
        s3.access(101)  # Ghost hit -> bypasses Small directly into Main!
        self.assertNotIn(101, s3.ghost_set)
        self.assertEqual(s3.main.size, 2)
        self.assertEqual(s3.small.size, 1)

    def test_all_policies_capacity_bounds(self) -> None:
        rng = random.Random(123)
        for name in ("FIFO", "CLOCK", "CLOCK2", "SecondChance", "S3FIFO"):
            for cap in (2, 3, 5, 10, 15):
                cache = make_cache(name, cap)
                for _ in range(500):
                    cache.access(rng.randint(0, cap + 4))
                if hasattr(cache, "table"):
                    self.assertLessEqual(len(cache.table), cap)


if __name__ == "__main__":
    unittest.main()
