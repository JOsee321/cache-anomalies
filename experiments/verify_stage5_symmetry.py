"""Independent Python cross-verification for Stage 5 symmetry check (SIEVE-TP/BW 8C & 16C)."""

from __future__ import annotations

import json
from pathlib import Path
import sys

ROOT = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(ROOT))

from sim.sieve import SieveBWCache, SieveTPCache


def main() -> None:
    data = json.loads((ROOT / "results" / "tahap5_symmetry.json").read_text(encoding="utf-8"))
    exports = data["mixed_exports"]
    assert len(exports) > 0
    for ex in exports:
        C = ex["cap"]
        var = ex["variant"]
        if var == "SIEVE-TP(8C)":
            c = SieveTPCache(C, probe_interval=8 * C)
        elif var == "SIEVE-TP(16C)":
            c = SieveTPCache(C, probe_interval=16 * C)
        elif var == "SIEVE-BW(16C)":
            c = SieveBWCache(C, wrap_limit=16 * C)
        else:
            raise ValueError(var)

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
            f"Symmetry mismatch {var} {ex['mode']}: py=({unwrap},{misses}) vs go=({ex['unwrap']},{ex['misses']})"
        )
    print(f"PASS: Python independently cross-verified all {len(exports)} Stage 5 symmetry 50,000-req traces.")


if __name__ == "__main__":
    main()
