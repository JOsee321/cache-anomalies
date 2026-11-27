"""Independently re-verify all Experiment B witnesses from Go in Python (5,000+ cycles)."""

from __future__ import annotations

import json
from pathlib import Path
import sys

ROOT = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(ROOT))

from sim.policies import make_cache


def eval_python(pol: str, cap_k: int, cap_C: int, prefix: list[int], cycle: list[int], cycles: int = 5000, steady_start: int = 1000) -> tuple[int, int, int]:
    ck = make_cache(pol, cap_k)
    cC = make_cache(pol, cap_C)
    for x in prefix:
        ck.access(x)
        cC.access(x)
    smk = 0
    smc = 0
    sacc = 0
    for ci in range(cycles):
        for x in cycle:
            hk = ck.access(x)
            hC = cC.access(x)
            if ci >= steady_start:
                if not hk:
                    smk += 1
                if not hC:
                    smc += 1
                sacc += 1
    return smk, smc, sacc


def main() -> None:
    atlas_path = ROOT / "results" / "atlas_search_go.json"
    data = json.loads(atlas_path.read_text(encoding="utf-8"))

    verified_exact = 0
    for r in data["exact"]:
        if not r["complete"] or not r["cycle"]:
            continue
        smk, smc, sacc = eval_python(r["policy"], r["k"], r["C"], r["prefix"] or [], r["cycle"], cycles=5000, steady_start=1000)
        ratio = smc / smk if smk > 0 else float("inf")
        assert abs(ratio - r["max_ratio"]) < 1e-9, (
            f"Exact witness mismatch {r['policy']} ({r['k']},{r['C']}): py={ratio} vs go={r['max_ratio']}"
        )
        verified_exact += 1

    verified_evo = 0
    for r in data["evo"]:
        if r["found_zero_miss_k_pos_C"]:
            smk, smc, sacc = eval_python(
                r["policy"], r["k"], r["C"],
                r["zero_witness_prefix"], r["zero_witness_cycle"],
                cycles=5000, steady_start=1000,
            )
            assert smk == 0 and smc > 0, (
                f"Evo zero-miss witness failed in Python: {r['policy']} ({r['k']},{r['C']}) smk={smk} smc={smc}"
            )
            verified_evo += 1
        elif r.get("best_cycle"):
            smk, smc, sacc = eval_python(
                r["policy"], r["k"], r["C"],
                r.get("best_prefix") or [], r["best_cycle"],
                cycles=5000, steady_start=1000,
            )
            assert smk == r["best_steady_miss_k"] and smc == r["best_steady_miss_C"], (
                f"Evo witness count mismatch {r['policy']} ({r['k']},{r['C']}): "
                f"py=({smk},{smc}) vs go=({r['best_steady_miss_k']},{r['best_steady_miss_C']})"
            )
            verified_evo += 1

    print(f"PASS: Independently re-verified {verified_exact} exact witnesses and {verified_evo} evolutionary witnesses in Python (5,000 cycles each).")


if __name__ == "__main__":
    main()
