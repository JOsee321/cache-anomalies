"""Independent Python cross-verification for Stage 4 (SIEVE-TP parity, Exp F & G)."""

from __future__ import annotations

import json
from pathlib import Path
import random
import sys

ROOT = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(ROOT))

from sim.sieve import SieveTPCache


def verify_tp_extreme_zombie() -> None:
    data = json.loads((ROOT / "results" / "exp_fg_sieve_tp_go.json").read_text(encoding="utf-8"))
    for ext in data["extremes_tp"]:
        k, C, label = ext["k"], ext["C"], ext["label"]
        # Verify representative extreme cases including all (3, 1000) and (500, 1000)
        if C > 500 and k not in (3, 500):
            continue
        mult = float(label[:-1])
        pk = max(1, k // 2 if mult == 0.5 else int(round(k * mult)))
        pC = max(1, C // 2 if mult == 0.5 else int(round(C * mult)))

        zombies = list(range(C - 2))
        A, B, D = C - 2, C - 1, C
        prefix = zombies + [A, B] + zombies
        cycle = [D, A, B]

        ck = SieveTPCache(k, probe_interval=pk)
        cC = SieveTPCache(C, probe_interval=pC)
        for x in prefix:
            ck.access(x)
            cC.access(x)

        num_cycles = 3 * C + 500
        steady_start = num_cycles - 200
        first_zero_C = -1
        smk = smC = 0
        for ci in range(num_cycles):
            mk = mC = 0
            for x in cycle:
                if not ck.access(x)[0]:
                    mk += 1
                if not cC.access(x)[0]:
                    mC += 1
            if mC == 0 and first_zero_C == -1:
                first_zero_C = ci
            if ci >= steady_start:
                smk += mk
                smC += mC
        assert smk == 0 and smC == 0 and first_zero_C == ext["first_zero_C_cycle"], (
            f"TP extreme mismatch ({k},{C},{label}): py_zero={first_zero_C} vs go={ext['first_zero_C_cycle']}"
        )
    print("PASS: Python independently verified SIEVE-TP extreme Zombie-Pinning runs.")


def verify_tp_evo() -> None:
    data = json.loads((ROOT / "results" / "exp_fg_sieve_tp_go.json").read_text(encoding="utf-8"))
    for r in data["evo_tp"]:
        ck = SieveTPCache(r["k"], probe_interval=r["k"])
        cC = SieveTPCache(r["C"], probe_interval=r["C"])
        for x in r["best_prefix"]:
            ck.access(x)
            cC.access(x)
        smk = smC = 0
        for ci in range(5000):
            for x in r["best_cycle"]:
                hk = ck.access(x)[0]
                hC = cC.access(x)[0]
                if ci >= 1000:
                    if not hk:
                        smk += 1
                    if not hC:
                        smC += 1
        assert smk == r["best_steady_miss_k"] and smC == r["best_steady_miss_C"], (
            f"TP evo mismatch ({r['k']},{r['C']}): py=({smk},{smC}) vs go=({r['best_steady_miss_k']},{r['best_steady_miss_C']})"
        )
    print(f"PASS: Python independently re-verified all {len(data['evo_tp'])} SIEVE-TP evolutionary witnesses (5,000 cycles each).")


def verify_tp_mixed_exports() -> None:
    data = json.loads((ROOT / "results" / "exp_fg_sieve_tp_go.json").read_text(encoding="utf-8"))
    for ex in data["mixed_exports"]:
        C = ex["cap"]
        c = SieveTPCache(C, probe_interval=C)
        for x in ex["prefix"]:
            c.access(x)
        num_zombies = C - 2
        unwrap = -1
        misses = 0
        for step, x in enumerate(ex["trace"], start=1):
            h, ev = c.access(x)
            if not h:
                misses += 1
            if unwrap == -1 and ev is not None and 0 <= ev < num_zombies:
                unwrap = step
        assert unwrap == ex["unwrap"] and misses == ex["misses"], (
            f"TP mixed mismatch: py=({unwrap},{misses}) vs go=({ex['unwrap']},{ex['misses']})"
        )
    print(f"PASS: Python cross-verified {len(data['mixed_exports'])} full 50,000-req SIEVE-TP mixed traces.")


def verify_tp_parity_with_random_smoke() -> None:
    data = json.loads((ROOT / "results" / "exp_fg_sieve_tp_go.json").read_text(encoding="utf-8"))
    for p in data["parity_tp"]:
        c = SieveTPCache(p["cap"], probe_interval=p["k"])
        hits = 0
        dig = 0
        for i, x in enumerate(p["trace"]):
            h, _ = c.access(x)
            c.assert_invariants()
            if h:
                hits += 1
                dig ^= ((i + 1) * 2654435761) & 0xFFFFFFFFFFFFFFFF
        assert hits == p["hits"] and dig == p["digest"], (
            f"SIEVE-TP parity mismatch cap={p['cap']} K={p['k']}: py=({hits},{dig}) vs go=({p['hits']},{p['digest']})"
        )
    print(f"PASS: 100% Python vs Go bit-for-bit parity on {len(data['parity_tp'])} SIEVE-TP random traces (1,000 reqs each).")


def main() -> None:
    verify_tp_parity_with_random_smoke()
    verify_tp_extreme_zombie()
    verify_tp_evo()
    verify_tp_mixed_exports()


if __name__ == "__main__":
    main()
