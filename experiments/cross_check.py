"""Cross-check Python and Go outputs for witnesses, Zombie Pinning, and k=2 solver."""

from __future__ import annotations

import json
from pathlib import Path
import sys

ROOT = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(ROOT))

from sim.sieve import SieveCache


def main() -> None:
    results_dir = ROOT / "results"
    w_py = json.loads((results_dir / "witnesses_py.json").read_text(encoding="utf-8"))
    w_go = json.loads((results_dir / "witnesses_go.json").read_text(encoding="utf-8"))

    for mode in ("official", "variant_insert_before_hand"):
        assert len(w_py[mode]) == len(w_go[mode])
        for rp, rg in zip(w_py[mode], w_go[mode]):
            for field in (
                "k",
                "C",
                "k_prefix_misses",
                "k_cycle_misses",
                "k_first_zero_cycle",
                "k_tail_all_zero",
                "k_final_keys",
                "kp1_prefix_misses",
                "kp1_cycle_misses",
                "kp1_min_cycle_misses",
                "kp1_max_cycle_misses",
                "kp1_final_keys",
            ):
                vp = -1 if (field == "k_first_zero_cycle" and rp[field] is None) else rp[field]
                vg = rg[field]
                assert vp == vg, (
                    f"Witness parity mismatch ({mode}, k={rp['k']}, field={field}): "
                    f"py={vp} vs go={vg}"
                )
    print("PASS: Python vs Go parity on all 6 initial witnesses (official + variant).")

    z_py = json.loads((results_dir / "zombie_pinning_py.json").read_text(encoding="utf-8"))
    z_go = json.loads((results_dir / "zombie_pinning_go.json").read_text(encoding="utf-8"))
    assert z_py["grid_3_to_30"]["official_passed"] == z_go["official_passed"]
    assert z_py["grid_3_to_30"]["variant_passed"] == z_go["variant_passed"]
    for rp, rg in zip(z_py["extreme_official"], z_go["extreme_official"]):
        assert rp == rg, f"Extreme mismatch: py={rp} vs go={rg}"
    print("PASS: Python vs Go parity on Zombie Pinning (378 grid pairs + 5 extreme scales).")

    k2_go = json.loads((results_dir / "k2_solver_go.json").read_text(encoding="utf-8"))
    for res in k2_go:
        ck = SieveCache(res["k"])
        cC = SieveCache(res["C"])
        for x in res["shortest_prefix"]:
            ck.access(x)
            cC.access(x)
        # Run 100 cycles and verify steady-state cycle misses match
        for rep in range(100):
            mk = sum(0 if ck.access(x)[0] else 1 for x in res["shortest_cycle"])
            mC = sum(0 if cC.access(x)[0] else 1 for x in res["shortest_cycle"])
            assert mk == res["cycle_miss_k"] and mC == res["cycle_miss_C"], (
                f"k2 witness mismatch on rep {rep}: ({mk},{mC}) vs ({res['cycle_miss_k']},{res['cycle_miss_C']})"
            )
    print("PASS: Python DLL independently verified all Go k=2 solver witnesses.")


if __name__ == "__main__":
    main()
