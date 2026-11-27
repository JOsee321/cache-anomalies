"""Verify the general Zombie Pinning construction for all 3 <= k < C <= 30 and extreme scales."""

from __future__ import annotations

import json
from pathlib import Path
import sys

ROOT = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(ROOT))

from sim.sieve import SieveCache


def build_zombie_trace(C: int) -> tuple[list[int], list[int], set[int], list[int]]:
    """Construct prefix and cycle for Zombie Pinning at large cache size C.

    - C - 2 zombie keys: 0, 1, ..., C - 3
    - 3 active keys: A = C - 2, B = C - 1, D = C
    - prefix (length 2C - 2): [z_1..z_{C-2}, A, B, z_1..z_{C-2}]
    - cycle (length 3): [D, A, B]
    """
    zombies = list(range(C - 2))
    A = C - 2
    B = C - 1
    D = C
    prefix = zombies + [A, B] + zombies
    cycle = [D, A, B]
    return prefix, cycle, set(zombies), [D, A, B]


def test_pair(k: int, C: int, num_cycles: int = 1000, variant: bool = False) -> dict:
    prefix, cycle, zombie_set, active_keys = build_zombie_trace(C)

    ck = SieveCache(k, insert_before_hand_update=variant)
    cC = SieveCache(C, insert_before_hand_update=variant)

    for x in prefix:
        ck.access(x)
        cC.access(x)

    # Track steady state across all cycles (check from cycle 10 onwards, and also exact cycle when 0% / 100% locks in)
    first_zero_k = None
    steady_misses_k = 0
    steady_misses_C = 0
    steady_accesses = 0
    evicted_C_during_cycles: set[int] = set()
    steady_start = 10

    for c_idx in range(num_cycles):
        mk = 0
        mC = 0
        for x in cycle:
            hk, _ = ck.access(x)
            if not hk:
                mk += 1
            hC, evC = cC.access(x)
            if not hC:
                mC += 1
            if evC is not None:
                evicted_C_during_cycles.add(evC)
        if mk == 0 and first_zero_k is None:
            first_zero_k = c_idx
        if c_idx >= steady_start:
            steady_misses_k += mk
            steady_misses_C += mC
            steady_accesses += len(cycle)

    final_keys_C = cC.keys_head_to_tail()
    pinned_zombies = [x for x in final_keys_C if x in zombie_set]
    zombies_untouched_and_un_evicted = len(pinned_zombies) == (C - 2) and zombie_set.isdisjoint(evicted_C_during_cycles)

    ok = (
        steady_misses_k == 0
        and steady_misses_C == steady_accesses
        and zombies_untouched_and_un_evicted
    )

    return {
        "k": k,
        "C": C,
        "ok": ok,
        "first_zero_k_cycle": first_zero_k,
        "steady_misses_k": steady_misses_k,
        "steady_misses_C": steady_misses_C,
        "steady_accesses": steady_accesses,
        "pinned_zombie_count": len(pinned_zombies),
        "expected_zombie_count": C - 2,
        "zombies_never_evicted_in_cycles": zombie_set.isdisjoint(evicted_C_during_cycles),
    }


def main() -> None:
    num_cycles = 1000
    all_pairs_official = []
    all_pairs_variant = []
    failures_official = []
    failures_variant = []

    for k in range(3, 30):
        for C in range(k + 1, 31):
            r_off = test_pair(k, C, num_cycles=num_cycles, variant=False)
            all_pairs_official.append(r_off)
            if not r_off["ok"]:
                failures_official.append(r_off)

            r_var = test_pair(k, C, num_cycles=num_cycles, variant=True)
            all_pairs_variant.append(r_var)
            if not r_var["ok"]:
                failures_variant.append(r_var)

    extreme_pairs = [(3, 100), (3, 500), (3, 1000), (10, 1000), (500, 1000)]
    extreme_official = [test_pair(k, C, num_cycles=num_cycles, variant=False) for k, C in extreme_pairs]
    extreme_variant = [test_pair(k, C, num_cycles=num_cycles, variant=True) for k, C in extreme_pairs]

    out = {
        "num_cycles": num_cycles,
        "grid_3_to_30": {
            "total_pairs": len(all_pairs_official),
            "official_passed": len(all_pairs_official) - len(failures_official),
            "official_failures": failures_official[:10],
            "variant_passed": len(all_pairs_variant) - len(failures_variant),
            "variant_failures": failures_variant[:10],
            "max_first_zero_k_cycle_official": max(r["first_zero_k_cycle"] for r in all_pairs_official),
        },
        "extreme_official": extreme_official,
        "extreme_variant": extreme_variant,
    }

    out_path = ROOT / "results" / "zombie_pinning_py.json"
    out_path.write_text(json.dumps(out, indent=2), encoding="utf-8")

    print(
        f"Grid 3<=k<C<=30 ({len(all_pairs_official)} pairs, {num_cycles} cycles): "
        f"official={len(all_pairs_official)-len(failures_official)}/{len(all_pairs_official)}, "
        f"variant={len(all_pairs_variant)-len(failures_variant)}/{len(all_pairs_variant)}"
    )
    if failures_variant:
        print(f"  Variant first failure: {failures_variant[0]}")
    for r in extreme_official:
        print(
            f"Extreme ({r['k']}, {r['C']}): ok={r['ok']}, "
            f"k_steady_misses={r['steady_misses_k']}, "
            f"C_steady_misses={r['steady_misses_C']}/{r['steady_accesses']}, "
            f"pinned_zombies={r['pinned_zombie_count']}/{r['expected_zombie_count']}"
        )
    for r in extreme_variant:
        if not r["ok"]:
            print(f"  [VARIANT FAIL] Extreme ({r['k']}, {r['C']}): {r}")


if __name__ == "__main__":
    main()
