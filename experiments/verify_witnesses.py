"""Replicate the 6 initial Belady anomaly witnesses (k=3..8) in Python.

Compares the official NSDI '24 SIEVE specification against the edge-case
variant where `insert(head)` happens before `hand = o.prev`.
"""

from __future__ import annotations

import json
from pathlib import Path
import sys

ROOT = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(ROOT))

from sim.sieve import SieveCache, run_prefix_cycle

WITNESSES = [
    {
        "k": 3,
        "prefix": [0, 4, 1, 1, 3, 3, 3],
        "cycle": [2, 0, 4],
    },
    {
        "k": 4,
        "prefix": [4, 3, 1, 0, 3, 1, 5, 1, 1, 5, 0, 3, 3, 4, 2, 4, 2, 5, 3, 2, 0, 1, 5, 2, 0, 0, 0, 2, 5],
        "cycle": [2, 3, 4, 1],
    },
    {
        "k": 5,
        "prefix": [1, 5, 3, 3, 1, 3, 0, 5, 6, 1, 6, 5],
        "cycle": [3, 2, 4, 1, 0],
    },
    {
        "k": 6,
        "prefix": [1, 2, 5, 4, 5, 2, 0, 3, 4, 2, 1, 1, 7, 2, 6, 2, 3, 4, 3, 5, 7, 7, 1, 5, 4, 2, 1, 5, 3, 5, 4, 6, 5, 3, 6, 3, 6, 5, 5],
        "cycle": [1, 2, 0, 3, 6, 7],
    },
    {
        "k": 7,
        "prefix": [3, 1, 1, 4, 5, 6, 5, 5, 6, 8, 1, 3, 6, 5, 8, 7, 5, 1, 7, 5, 0, 3, 4, 6, 2, 3, 4, 8, 0, 2, 4, 0, 1, 4],
        "cycle": [1, 2, 5, 6, 4, 3, 7],
    },
    {
        "k": 8,
        "prefix": [9, 1, 4, 1, 7, 7, 7, 6, 3, 1, 7, 0, 6, 6, 9, 0],
        "cycle": [7, 4, 3, 8, 0, 2, 5],
    },
]


def eval_witness(w: dict, num_cycles: int, variant: bool) -> dict:
    k = w["k"]
    prefix = w["prefix"]
    cycle = w["cycle"]

    stats_k, misses_k = run_prefix_cycle(
        k, prefix, cycle, num_cycles, steady_tail_cycles=num_cycles - 10, insert_before_hand_update=variant
    )
    stats_kp1, misses_kp1 = run_prefix_cycle(
        k + 1, prefix, cycle, num_cycles, steady_tail_cycles=num_cycles - 10, insert_before_hand_update=variant
    )

    first_zero_cycle_k = next((i for i, m in enumerate(misses_k) if m == 0), None)
    # Verify whether misses_k remains 0 from first_zero_cycle_k onwards
    tail_all_zero_k = (
        first_zero_cycle_k is not None and all(m == 0 for m in misses_k[first_zero_cycle_k:])
    )

    return {
        "k": k,
        "C": k + 1,
        "variant_insert_before_hand": variant,
        "num_cycles": num_cycles,
        "prefix_len": len(prefix),
        "cycle_len": len(cycle),
        "k_prefix_misses": stats_k.prefix_misses,
        "k_cycle_misses": stats_k.cycle_misses,
        "k_first_zero_cycle": first_zero_cycle_k,
        "k_tail_all_zero": tail_all_zero_k,
        "k_last_cycle_misses": misses_k[-1],
        "k_final_keys": stats_k.final_keys,
        "kp1_prefix_misses": stats_kp1.prefix_misses,
        "kp1_cycle_misses": stats_kp1.cycle_misses,
        "kp1_min_cycle_misses": min(misses_kp1),
        "kp1_max_cycle_misses": max(misses_kp1),
        "kp1_last_cycle_misses": misses_kp1[-1],
        "kp1_steady_miss_rate": stats_kp1.steady_state_miss_rate,
        "kp1_final_keys": stats_kp1.final_keys,
    }


def main() -> None:
    num_cycles = 10000
    official_results = [eval_witness(w, num_cycles, variant=False) for w in WITNESSES]
    variant_results = [eval_witness(w, num_cycles, variant=True) for w in WITNESSES]

    # Also measure divergence rate between official and variant on random traces
    import random

    rng = random.Random(2024)
    div_trials = 0
    div_count = 0
    first_div_example = None
    for cap in range(2, 9):
        for trial in range(100):
            div_trials += 1
            trace = [rng.randint(0, cap + 3) for _ in range(200)]
            c1 = SieveCache(cap, insert_before_hand_update=False)
            c2 = SieveCache(cap, insert_before_hand_update=True)
            m1 = sum(0 if c1.access(x)[0] else 1 for x in trace)
            m2 = sum(0 if c2.access(x)[0] else 1 for x in trace)
            if m1 != m2 or c1.state_snapshot() != c2.state_snapshot():
                div_count += 1
                if first_div_example is None:
                    first_div_example = {"cap": cap, "m_official": m1, "m_variant": m2}

    out = {
        "num_cycles": num_cycles,
        "official": official_results,
        "variant_insert_before_hand": variant_results,
        "random_divergence": {
            "trials": div_trials,
            "divergent_trials": div_count,
            "example": first_div_example,
        },
    }

    out_path = ROOT / "results" / "witnesses_py.json"
    out_path.write_text(json.dumps(out, indent=2), encoding="utf-8")

    print("=== Python Witness Replication (10,000 cycles) ===")
    for r_off, r_var in zip(official_results, variant_results):
        print(
            f"k={r_off['k']} vs C={r_off['C']}: "
            f"k_cycle_misses={r_off['k_cycle_misses']} (zero_from_cycle={r_off['k_first_zero_cycle']}, tail_zero={r_off['k_tail_all_zero']}), "
            f"C_cycle_misses={r_off['kp1_cycle_misses']}/{num_cycles * r_off['cycle_len']} "
            f"(rate={r_off['kp1_steady_miss_rate']:.4f}, per_cycle={r_off['kp1_min_cycle_misses']}..{r_off['kp1_max_cycle_misses']})"
        )
        if r_off != r_var:
            diff_fields = [k for k in r_off if k != "variant_insert_before_hand" and r_off[k] != r_var[k]]
            print(f"  [VARIANT DIFF] k={r_off['k']} differs on: {diff_fields}")
            print(f"    variant: k_cycle_misses={r_var['k_cycle_misses']}, C_cycle_misses={r_var['kp1_cycle_misses']}, rate={r_var['kp1_steady_miss_rate']:.4f}")
    print(f"Random trace divergence (official vs insert-before-hand): {div_count}/{div_trials}")


if __name__ == "__main__":
    main()
