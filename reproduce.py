"""One-command reproducible verification pipeline for the Cache Anomalies workspace.

Usage:
  python reproduce.py --quick   # Unit tests, parity, 378 Zombie-Pinning + extremes, witness cross-checks, SVG figures (< 30s)
  python reproduce.py --full    # Full regeneration of all Go solvers, searches, benchmarks, and Python cross-checks
"""

from __future__ import annotations

import argparse
from pathlib import Path
import subprocess
import sys
import time

ROOT = Path(__file__).resolve().parent
VERIFIERS = ROOT / "verifiers"


def run_step(label: str, cmd: list[str], cwd: Path = ROOT) -> None:
    t0 = time.perf_counter()
    print(f"\n==> [{label}] {' '.join(cmd)}")
    subprocess.run(cmd, cwd=str(cwd), check=True)
    dt = time.perf_counter() - t0
    print(f"    OK ({dt:.2f}s)")


def run_quick() -> None:
    py = sys.executable
    run_step("Python unit tests", [py, "-m", "unittest", "discover", "-s", "sim", "-v"])
    run_step("Go unit tests", ["go", "test", "./...", "-v"], cwd=VERIFIERS)
    run_step("Stage 1 Python witnesses (k=3..8)", [py, "experiments/verify_witnesses.py"])
    run_step("Stage 1 Python Zombie Pinning (378 grid + extremes)", [py, "experiments/verify_zombie_pinning.py"])
    run_step("Stage 1 Go verifier (witnesses + Zombie Pinning)", ["go", "run", "./cmd/verify", "-out", "../results"], cwd=VERIFIERS)
    run_step("Stage 1 Python vs Go parity", [py, "experiments/cross_check.py"])
    run_step("Stage 2 Exp A + parity traces (Python)", [py, "experiments/exp_a_zombie_and_parity.py"])
    run_step("Stage 2 parity check (Go)", ["go", "run", "./cmd/parity", "-results", "../results"], cwd=VERIFIERS)
    run_step("Stage 2 Atlas witness re-verification (Python 5k cycles)", [py, "experiments/verify_atlas_witnesses.py"])
    run_step("Stage 3 cross-verification (Python)", [py, "experiments/verify_stage3_py.py"])
    run_step("Stage 4 SIEVE-TP cross-verification (Python)", [py, "experiments/verify_stage4_py.py"])
    run_step("Stage 5 symmetry cross-verification (Python)", [py, "experiments/verify_stage5_symmetry.py"])
    run_step("Generate publication SVG figures", [py, "experiments/generate_figures.py"])


def run_full() -> None:
    py = sys.executable
    run_step("Python unit tests", [py, "-m", "unittest", "discover", "-s", "sim", "-v"])
    run_step("Go unit tests", ["go", "test", "./...", "-v"], cwd=VERIFIERS)
    run_step("Stage 1 Python witnesses", [py, "experiments/verify_witnesses.py"])
    run_step("Stage 1 Python Zombie Pinning", [py, "experiments/verify_zombie_pinning.py"])
    run_step("Stage 1 Go verifier", ["go", "run", "./cmd/verify", "-out", "../results"], cwd=VERIFIERS)
    run_step("Stage 1 Go exact k=2 solver", ["go", "run", "./cmd/k2solver", "-out", "../results"], cwd=VERIFIERS)
    run_step("Stage 1 cross-check", [py, "experiments/cross_check.py"])
    run_step("Stage 2 Exp A + parity traces", [py, "experiments/exp_a_zombie_and_parity.py"])
    run_step("Stage 2 Go parity", ["go", "run", "./cmd/parity", "-results", "../results"], cwd=VERIFIERS)
    run_step("Stage 2 Go Atlas search", ["go", "run", "./cmd/atlas_search", "-out", "../results"], cwd=VERIFIERS)
    run_step("Stage 2 Python witness verification", [py, "experiments/verify_atlas_witnesses.py"])
    run_step("Stage 3 Go Exp C (mixed traffic)", ["go", "run", "./cmd/exp_c_mixed", "-out", "../results"], cwd=VERIFIERS)
    run_step("Stage 3 Go Exp D (S3-FIFO boundaries)", ["go", "run", "./cmd/exp_d_s3fifo", "-out", "../results"], cwd=VERIFIERS)
    run_step("Stage 3 Go Exp E (SIEVE-BW)", ["go", "run", "./cmd/exp_e_sievebw", "-out", "../results"], cwd=VERIFIERS)
    run_step("Stage 3 Python cross-check", [py, "experiments/verify_stage3_py.py"])
    run_step("Stage 4 Go Exp F & G (SIEVE-TP & Pareto)", ["go", "run", "./cmd/exp_fg_sieve_tp", "-out", "../results"], cwd=VERIFIERS)
    run_step("Stage 4 Python cross-check", [py, "experiments/verify_stage4_py.py"])
    run_step("Stage 5 Go symmetry benchmarks", ["go", "run", "./cmd/exp_h_symmetry", "-out", "../results"], cwd=VERIFIERS)
    run_step("Stage 5 Python symmetry cross-check", [py, "experiments/verify_stage5_symmetry.py"])
    run_step("Generate publication SVG figures", [py, "experiments/generate_figures.py"])


def main() -> None:
    parser = argparse.ArgumentParser(description="Reproduce Cache Anomalies research verification.")
    group = parser.add_mutually_exclusive_group()
    group.add_argument("--quick", action="store_true", help="Run fast verification suite (< 30s)")
    group.add_argument("--full", action="store_true", help="Re-run all heavy Go solvers/benchmarks and checks")
    args = parser.parse_args()

    t_start = time.perf_counter()
    if args.full:
        run_full()
    else:
        run_quick()
    total = time.perf_counter() - t_start
    print(f"\nALL CHECKS PASSED in {total:.2f}s.")


if __name__ == "__main__":
    main()
