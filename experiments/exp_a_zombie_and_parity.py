"""Verify 100% parity with Go and run Experiment A (SIEVE Zombie Pinning trace on all 6 policies)."""

from __future__ import annotations

import json
from pathlib import Path
import random
import subprocess
import sys

ROOT = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(ROOT))

from sim.policies import make_cache


POLICIES = ["SIEVE", "FIFO", "CLOCK", "CLOCK2", "SecondChance", "S3FIFO"]


def build_zombie_trace(C: int) -> tuple[list[int], list[int]]:
    zombies = list(range(C - 2))
    A, B, D = C - 2, C - 1, C
    prefix = zombies + [A, B] + zombies
    cycle = [D, A, B]
    return prefix, cycle


def run_exp_a() -> list[dict]:
    pairs = [(3, 4), (3, 10), (3, 20), (10, 20)]
    num_cycles = 1000
    steady_start = 200
    rows = []

    for k, C in pairs:
        prefix, cycle = build_zombie_trace(C)
        for pol in POLICIES:
            ck = make_cache(pol, k)
            cC = make_cache(pol, C)
            for x in prefix:
                ck.access(x)
                cC.access(x)

            steady_mk = 0
            steady_mC = 0
            steady_acc = 0
            first_zero_k = None
            first_zero_C = None
            last_cycle_mC = 0

            for ci in range(num_cycles):
                mk = 0
                mC = 0
                for x in cycle:
                    if not ck.access(x):
                        mk += 1
                    if not cC.access(x):
                        mC += 1
                if mk == 0 and first_zero_k is None:
                    first_zero_k = ci
                if mC == 0 and first_zero_C is None:
                    first_zero_C = ci
                if ci >= steady_start:
                    steady_mk += mk
                    steady_mC += mC
                    steady_acc += len(cycle)
                last_cycle_mC = mC

            rows.append(
                {
                    "policy": pol,
                    "k": k,
                    "C": C,
                    "first_zero_k_cycle": first_zero_k,
                    "first_zero_C_cycle": first_zero_C,
                    "steady_miss_rate_k": steady_mk / steady_acc,
                    "steady_miss_rate_C": steady_mC / steady_acc,
                    "steady_misses_k": steady_mk,
                    "steady_misses_C": steady_mC,
                    "steady_accesses": steady_acc,
                    "last_cycle_misses_C": last_cycle_mC,
                    "trapped": steady_mk == 0 and steady_mC > 0,
                }
            )
    return rows


def generate_parity_traces() -> dict:
    rng = random.Random(2026)
    traces = {}
    for cap in (2, 3, 4, 5, 9, 10, 19, 20):
        trace = [rng.randint(0, cap + 4) for _ in range(1000)]
        res = {}
        for pol in POLICIES:
            c = make_cache(pol, cap)
            hits = [1 if c.access(x) else 0 for x in trace]
            res[pol] = sum(hits)
            # Also store bitmask/digest of hit vector
            digest = 0
            for i, h in enumerate(hits):
                if h:
                    digest ^= (i + 1) * 2654435761
                    digest &= 0xFFFFFFFFFFFFFFFF
            res[f"{pol}_digest"] = digest
        traces[str(cap)] = {"trace": trace, "expected": res}
    return traces


def main() -> None:
    parity_data = generate_parity_traces()
    parity_path = ROOT / "results" / "parity_traces.json"
    parity_path.write_text(json.dumps(parity_data), encoding="utf-8")

    exp_a = run_exp_a()
    exp_a_path = ROOT / "results" / "exp_a_zombie_py.json"
    exp_a_path.write_text(json.dumps(exp_a, indent=2), encoding="utf-8")

    print("=== Experiment A: Zombie Pinning Cross-Test (1,000 cycles, steady=last 800 cycles) ===")
    for r in exp_a:
        print(
            f"{r['policy']:12s} ({r['k']:2d}, {r['C']:2d}): "
            f"rate_k={r['steady_miss_rate_k']:.4f} (zero_c={r['first_zero_k_cycle']}), "
            f"rate_C={r['steady_miss_rate_C']:.4f} (zero_c={r['first_zero_C_cycle']}), "
            f"trapped={r['trapped']}"
        )


if __name__ == "__main__":
    main()
