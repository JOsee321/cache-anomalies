"""Independent Python cross-verification for all Stage 3 experiments (C, D, E)."""

from __future__ import annotations

import json
from pathlib import Path
import sys

ROOT = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(ROOT))

from sim.policies import make_cache
from sim.sieve import SieveBWCache, SieveCache


def verify_exp_c() -> None:
    data = json.loads((ROOT / "results" / "exp_c_mixed_go.json").read_text(encoding="utf-8"))
    exports = data["sample_exports"]
    assert len(exports) >= 3
    for item in exports:
        row = item["row"]
        C = row["cap"]
        prefix = item["prefix"]
        trace = item["trace"]
        num_zombies = C - 2

        sv = SieveCache(C)
        bw = SieveBWCache(C, wrap_limit=C)
        clk = make_cache("CLOCK", C)
        s3 = make_cache("S3FIFO", C)
        lru = make_cache("LRU", C)

        for x in prefix:
            sv.access(x)
            bw.access(x)
            clk.access(x)
            s3.access(x)
            lru.access(x)

        still_pinned = [True] * num_zombies
        pinned_cnt = num_zombies
        unwrap = -1
        m_sv = m_bw = m_clk = m_s3 = m_lru = 0

        for step, x in enumerate(trace, start=1):
            h_sv, ev = sv.access(x)
            if not h_sv:
                m_sv += 1
            if ev is not None and 0 <= ev < num_zombies and still_pinned[ev]:
                still_pinned[ev] = False
                pinned_cnt -= 1
                if unwrap == -1:
                    unwrap = step
            if not bw.access(x)[0]:
                m_bw += 1
            if not clk.access(x):
                m_clk += 1
            if not s3.access(x):
                m_s3 += 1
            if not lru.access(x):
                m_lru += 1

        assert unwrap == row["time_to_unwrap"], f"unwrap mismatch: {unwrap} vs {row['time_to_unwrap']}"
        assert abs(m_sv / len(trace) - row["miss_rate_sieve_50k"]) < 1e-12
        assert abs(m_bw / len(trace) - row["miss_rate_sieve_bw_50k"]) < 1e-12
        assert abs(m_clk / len(trace) - row["miss_rate_clock_50k"]) < 1e-12
        assert abs(m_s3 / len(trace) - row["miss_rate_s3fifo_50k"]) < 1e-12
        assert abs(m_lru / len(trace) - row["miss_rate_lru_50k"]) < 1e-12
    print(f"PASS: Python cross-verified {len(exports)} full 50,000-req Experiment C traces (SIEVE, SIEVE-BW, CLOCK, S3FIFO, LRU).")


def verify_exp_d() -> None:
    data = json.loads((ROOT / "results" / "exp_d_s3fifo_go.json").read_text(encoding="utf-8"))
    checked = 0
    for group, total_cycles, steady_start in (("analytical", 2000, 500), ("evo", 5000, 1000)):
        for r in data[group]:
            if not r.get("cycle"):
                continue
            pk = make_cache("S3FIFO", r["k"])
            pC = make_cache("S3FIFO", r["C"])
            for x in r["prefix"]:
                pk.access(x)
                pC.access(x)
            smk = smC = sacc = 0
            for ci in range(total_cycles):
                for x in r["cycle"]:
                    hk = pk.access(x)
                    hC = pC.access(x)
                    if ci >= steady_start:
                        if not hk:
                            smk += 1
                        if not hC:
                            smC += 1
                        sacc += 1
            assert smk == r["steady_miss_k"] and smC == r["steady_miss_C"], (
                f"Exp D mismatch {r['name']}: py=({smk},{smC}) vs go=({r['steady_miss_k']},{r['steady_miss_C']})"
            )
            checked += 1
    print(f"PASS: Python independently re-verified all {checked} Experiment D S3-FIFO witnesses.")


def verify_exp_e() -> None:
    data = json.loads((ROOT / "results" / "exp_e_sievebw_go.json").read_text(encoding="utf-8"))
    # Verify extreme Zombie Pinning on SIEVE-BW(C) and SIEVE-BW(2C) in Python
    for ext in data["extremes"]:
        k, C, mult = ext["k"], ext["C"], ext["wrap_mult"]
        if C > 500:
            # Verify (3, 1000) for mult=1 too; skip nothing!
            pass
        zombies = list(range(C - 2))
        A, B, D = C - 2, C - 1, C
        prefix = zombies + [A, B] + zombies
        cycle = [D, A, B]
        ck = SieveBWCache(k, wrap_limit=k * mult)
        cC = SieveBWCache(C, wrap_limit=C * mult)
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
            f"Exp E extreme mismatch ({k},{C},{mult}): py_zero_C={first_zero_C} vs go={ext['first_zero_C_cycle']}"
        )

    # Verify evo_bw witnesses
    for r in data["evo_bw"]:
        ck = SieveBWCache(r["k"], wrap_limit=r["k"])
        cC = SieveBWCache(r["C"], wrap_limit=r["C"])
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
            f"Exp E evo_bw mismatch ({r['k']},{r['C']}): py=({smk},{smC}) vs go=({r['best_steady_miss_k']},{r['best_steady_miss_C']})"
        )

    print(f"PASS: Python independently verified all {len(data['extremes'])} extreme SIEVE-BW Zombie-Pinning runs and {len(data['evo_bw'])} SIEVE-BW evolutionary runs.")


def main() -> None:
    verify_exp_c()
    verify_exp_d()
    verify_exp_e()


if __name__ == "__main__":
    main()
