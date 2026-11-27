"""Unit tests for the SIEVE doubly-linked list reference implementation."""

import unittest

from sim.sieve import SieveBWCache, SieveCache, SieveTPCache, run_prefix_cycle


class TestSieveCache(unittest.TestCase):
    def test_sieve_tp_invariants_and_cures_zombie_pinning(self) -> None:
        import random

        rng = random.Random(99)
        for cap in (1, 2, 3, 5, 8):
            for k_val in (1, max(1, cap // 2), cap, 2 * cap):
                c = SieveTPCache(cap, probe_interval=k_val)
                for _ in range(500):
                    c.access(rng.randint(0, cap + 3))
                    c.assert_invariants()

        for C in (4, 10, 20):
            zombies = list(range(C - 2))
            A, B, D = C - 2, C - 1, C
            prefix = zombies + [A, B] + zombies
            cycle = [D, A, B]
            for K in (max(1, C // 2), C, 2 * C, 4 * C):
                c = SieveTPCache(C, probe_interval=K)
                for x in prefix:
                    c.access(x)
                for _ in range(5 * C + 20):
                    for x in cycle:
                        c.access(x)
                steady_misses = sum(0 if c.access(x)[0] else 1 for _ in range(10) for x in cycle)
                self.assertEqual(steady_misses, 0)
                self.assertGreater(c.tail_probes, 0)

    def test_sieve_bw_cures_zombie_pinning(self) -> None:
        for C in (4, 10, 20):
            zombies = list(range(C - 2))
            A, B, D = C - 2, C - 1, C
            prefix = zombies + [A, B] + zombies
            cycle = [D, A, B]
            for mult in (1, 2):
                c = SieveBWCache(C, wrap_limit=C * mult)
                for x in prefix:
                    c.access(x)
                for _ in range(50):
                    for x in cycle:
                        c.access(x)
                # Steady state must be 0 misses per cycle
                steady_misses = sum(0 if c.access(x)[0] else 1 for _ in range(10) for x in cycle)
                self.assertEqual(steady_misses, 0)
                self.assertGreater(c.forced_wraps, 0)

    def test_invariants_random_traces(self) -> None:
        import random

        rng = random.Random(42)
        for cap in (1, 2, 3, 5, 8):
            for variant in (False, True):
                cache = SieveCache(cap, insert_before_hand_update=variant)
                for _ in range(500):
                    key = rng.randint(0, cap + 3)
                    cache.access(key)
                    cache.assert_invariants()

    def test_official_vs_variant_head_eviction_divergence(self) -> None:
        # Minimal sequence where `o == head` during eviction causes divergence:
        # cap=2: insert 0 (head=0,tail=0), insert 1 (head=1,tail=0),
        # hit 0 (0.visited=True), miss 2 -> hand wraps/scans:
        # o=0 (visited->False), o=1 (head, visited=False) -> evict 1 (o == head!).
        # Official: hand becomes None, insert 2 -> state [2(F), 0(F)], hand=None.
        #   Next miss 3 -> hand is None -> starts at tail (0), evicts 0!
        # Variant (insert before hand=o.prev): insert 2 at head -> 1.prev=2,
        #   hand becomes 2, remove 1 -> state [2(F), 0(F)], hand=2.
        #   Next miss 3 -> hand is 2 (visited=False) -> evicts 2 instead of 0!
        trace = [0, 1, 0, 2, 3]
        c_off = SieveCache(2, insert_before_hand_update=False)
        c_var = SieveCache(2, insert_before_hand_update=True)

        ev_off = [c_off.access(x)[1] for x in trace]
        ev_var = [c_var.access(x)[1] for x in trace]

        self.assertEqual(ev_off, [None, None, None, 1, 0])
        self.assertEqual(ev_var, [None, None, None, 1, 2])
        self.assertEqual(c_off.keys_head_to_tail(), [3, 2])
        self.assertEqual(c_var.keys_head_to_tail(), [3, 0])

    def test_k3_witness_smoke(self) -> None:
        prefix = [0, 4, 1, 1, 3, 3, 3]
        cycle = [2, 0, 4]
        s3, m3 = run_prefix_cycle(3, prefix, cycle, 100)
        s4, m4 = run_prefix_cycle(4, prefix, cycle, 100)
        self.assertEqual(sum(m3[10:]), 0)
        self.assertEqual(m4[-1], 3)


if __name__ == "__main__":
    unittest.main()
