"""Generate minimalist publication-grade vector SVG figures from JSON results."""

from __future__ import annotations

import json
import math
from pathlib import Path
from typing import Dict, List, Tuple

ROOT = Path(__file__).resolve().parent.parent
RESULTS = ROOT / "results"
FIGURES = ROOT / "figures"


def svg_header(width: int, height: int) -> List[str]:
    return [
        f'<svg xmlns="http://www.w3.org/2000/svg" width="{width}" height="{height}" '
        f'viewBox="0 0 {width} {height}" style="background:#ffffff;font-family:Arial,Helvetica,sans-serif;">',
        "<style>",
        "  .title { font-size: 14px; font-weight: bold; fill: #111111; }",
        "  .subtitle { font-size: 11px; fill: #444444; }",
        "  .axis-label { font-size: 11px; font-weight: bold; fill: #222222; }",
        "  .tick { font-size: 10px; fill: #333333; }",
        "  .legend { font-size: 10px; fill: #222222; }",
        "  .annot { font-size: 9.5px; fill: #222222; }",
        "  .grid { stroke: #e5e5e5; stroke-width: 1; }",
        "  .axis { stroke: #222222; stroke-width: 1.2; }",
        "</style>",
        '<rect width="100%" height="100%" fill="#ffffff"/>',
    ]


def generate_fig1() -> None:
    """Fig 1: Mixed Traffic Miss Rates (C=100) across policies."""
    exp_c = json.loads((RESULTS / "exp_c_mixed_go.json").read_text(encoding="utf-8"))
    exp_fg = json.loads((RESULTS / "exp_fg_sieve_tp_go.json").read_text(encoding="utf-8"))
    sym = json.loads((RESULTS / "tahap5_symmetry.json").read_text(encoding="utf-8"))

    c100_c: Dict[Tuple[str, float], Dict[str, float]] = {}
    counts: Dict[Tuple[str, float], int] = {}
    for r in exp_c["rows"]:
        if r["cap"] != 100:
            continue
        key = (r["mode"], round(r["p"], 2))
        if key not in c100_c:
            c100_c[key] = {"SIEVE": 0.0, "CLOCK": 0.0, "S3FIFO": 0.0, "LRU": 0.0}
            counts[key] = 0
        c100_c[key]["SIEVE"] += r["miss_rate_sieve_50k"]
        c100_c[key]["CLOCK"] += r["miss_rate_clock_50k"]
        c100_c[key]["S3FIFO"] += r["miss_rate_s3fifo_50k"]
        c100_c[key]["LRU"] += r["miss_rate_lru_50k"]
        counts[key] += 1
    for k, d in c100_c.items():
        for pol in d:
            d[pol] /= counts[k]

    tp_map: Dict[Tuple[str, float, str], float] = {}
    for r in exp_fg["mixed_tp"]:
        if r["cap"] == 100:
            tp_map[(r["mode"], round(r["p"], 2), r["variant"])] = r["avg_miss_rate_50k"]

    sym_map: Dict[Tuple[str, float, str], float] = {}
    for r in sym["mixed_8c_16c"]:
        if r["cap"] == 100:
            sym_map[(r["mode"], round(r["p"], 2), r["variant"])] = r["avg_miss_rate_50k"]

    groups = [
        ("zipf_disjoint", 0.01, "Zipf Disjoint (p=1%)"),
        ("zipf_disjoint", 0.05, "Zipf Disjoint (p=5%)"),
        ("zombie_hits_only", 0.25, "Zombie Hits Only (p=25%)"),
    ]
    policies = [
        ("SIEVE", "#111111", "SIEVE"),
        ("SIEVE-TP(C)", "#4b5563", "SIEVE-TP(C)"),
        ("SIEVE-BW(16C)", "#9ca3af", "SIEVE-BW(16C)"),
        ("CLOCK", "#d1d5db", "CLOCK"),
        ("S3FIFO", "#6b7280", "S3-FIFO"),
        ("LRU", "#e5e7eb", "LRU"),
    ]

    W, H = 760, 430
    ml, mr, mt, mb = 68, 25, 76, 85
    pw, ph = W - ml - mr, H - mt - mb

    lines = svg_header(W, H)
    lines.append(
        f'<text x="{ml}" y="26" class="title">Fig 1. Zombie Pinning Survival under Mixed Traffic (C = 100, 50k Requests)</text>'
    )
    lines.append(
        f'<text x="{ml}" y="44" class="subtitle">Mean 50k-request miss rate across 3 seeds. SIEVE-BW(16C) shown on p=1% and Zombie Hits p=25%.</text>'
    )

    for tick in [0.0, 0.2, 0.4, 0.6, 0.8, 1.0]:
        y = mt + ph * (1.0 - tick)
        lines.append(f'<line x1="{ml}" y1="{y:.1f}" x2="{ml + pw}" y2="{y:.1f}" class="grid"/>')
        lines.append(
            f'<text x="{ml - 8}" y="{y + 3.5:.1f}" text-anchor="end" class="tick">{tick:.1f}</text>'
        )

    lines.append(f'<line x1="{ml}" y1="{mt}" x2="{ml}" y2="{mt + ph}" class="axis"/>')
    lines.append(f'<line x1="{ml}" y1="{mt + ph}" x2="{ml + pw}" y2="{mt + ph}" class="axis"/>')
    lines.append(
        f'<text x="20" y="{mt + ph / 2:.1f}" transform="rotate(-90 20 {mt + ph / 2:.1f})" '
        f'text-anchor="middle" class="axis-label">Total Miss Rate (50,000 requests)</text>'
    )

    group_w = pw / len(groups)
    bar_w = 24
    gap = 6

    for gi, (mode, p_val, label) in enumerate(groups):
        gx = ml + gi * group_w + group_w / 2
        lines.append(
            f'<text x="{gx:.1f}" y="{mt + ph + 20}" text-anchor="middle" class="axis-label">{label}</text>'
        )
        vals: List[Tuple[str, str, float]] = []
        for pol_key, color, disp in policies:
            if pol_key in ("SIEVE", "CLOCK", "S3FIFO", "LRU"):
                v = c100_c[(mode, p_val)][pol_key]
            elif pol_key == "SIEVE-TP(C)":
                v = tp_map[(mode, p_val, "SIEVE-TP(C)")]
            else:
                if (mode, p_val, "SIEVE-BW(16C)") not in sym_map:
                    continue
                v = sym_map[(mode, p_val, "SIEVE-BW(16C)")]
            vals.append((disp, color, v))

        total_bars_w = len(vals) * bar_w + (len(vals) - 1) * gap
        start_x = gx - total_bars_w / 2
        for bi, (disp, color, v) in enumerate(vals):
            bx = start_x + bi * (bar_w + gap)
            bh = max(1.5, ph * v)
            by = mt + ph - bh
            lines.append(
                f'<rect x="{bx:.1f}" y="{by:.1f}" width="{bar_w}" height="{bh:.1f}" '
                f'fill="{color}" stroke="#111111" stroke-width="0.8"/>'
            )
            val_y = by - 5
            lines.append(
                f'<text x="{bx + bar_w / 2:.1f}" y="{val_y:.1f}" text-anchor="middle" '
                f'style="font-size:8.5px;fill:#111111;">{v:.3f}</text>'
            )

    lx = ml
    ly = H - 30
    for i, (_, color, disp) in enumerate(policies):
        x = lx + i * 112
        lines.append(
            f'<rect x="{x}" y="{ly - 9}" width="12" height="10" fill="{color}" stroke="#111111" stroke-width="0.8"/>'
        )
        lines.append(f'<text x="{x + 17}" y="{ly}" class="legend">{disp}</text>')

    lines.append("</svg>")
    (FIGURES / "fig1_zombie_pinning_mixed.svg").write_text("\n".join(lines), encoding="utf-8")


def generate_fig2() -> None:
    """Fig 2: S3-FIFO Periodic Small-Queue Boundary Belady Spikes."""
    data = json.loads((RESULTS / "exp_d_s3fifo_go.json").read_text(encoding="utf-8"))
    evo = sorted(data["evo"], key=lambda r: r["C"])

    W, H = 760, 395
    ml, mr, mt, mb = 65, 30, 68, 78
    pw, ph = W - ml - mr, H - mt - mb

    lines = svg_header(W, H)
    lines.append(
        f'<text x="{ml}" y="26" class="title">Fig 2. S3-FIFO Periodic Belady Spikes at Multiple-of-10 Capacity Boundaries</text>'
    )
    lines.append(
        f'<text x="{ml}" y="44" class="subtitle">Dark bars: k=10m-1 to C=10m (Small grows m-1 to m for m&gt;=2, Main fixed at 9m). Light bars: non-boundary neighbors.</text>'
    )

    y_min, y_max = 1.0, 2.4
    for tick in [1.0, 1.2, 1.4, 1.6, 1.8, 2.0, 2.2, 2.4]:
        y = mt + ph * (1.0 - (tick - y_min) / (y_max - y_min))
        lines.append(f'<line x1="{ml}" y1="{y:.1f}" x2="{ml + pw}" y2="{y:.1f}" class="grid"/>')
        lines.append(
            f'<text x="{ml - 8}" y="{y + 3.5:.1f}" text-anchor="end" class="tick">{tick:.1f}x</text>'
        )

    lines.append(f'<line x1="{ml}" y1="{mt}" x2="{ml}" y2="{mt + ph}" class="axis"/>')
    lines.append(f'<line x1="{ml}" y1="{mt + ph}" x2="{ml + pw}" y2="{mt + ph}" class="axis"/>')
    lines.append(
        f'<text x="19" y="{mt + ph / 2:.1f}" transform="rotate(-90 19 {mt + ph / 2:.1f})" '
        f'text-anchor="middle" class="axis-label">Steady-State Belady Ratio miss(C) / miss(k)</text>'
    )

    slot_w = pw / len(evo)
    bar_w = 38
    for i, r in enumerate(evo):
        cx = ml + i * slot_w + slot_w / 2
        ratio = r["best_ratio"]
        bh = ph * (ratio - y_min) / (y_max - y_min)
        by = mt + ph - bh
        is_bound = r["kind"] == "boundary"
        fill = "#1f2937" if is_bound else "#d1d5db"
        lines.append(
            f'<rect x="{cx - bar_w / 2:.1f}" y="{by:.1f}" width="{bar_w}" height="{bh:.1f}" '
            f'fill="{fill}" stroke="#111111" stroke-width="0.9"/>'
        )
        lines.append(
            f'<text x="{cx:.1f}" y="{by - 5:.1f}" text-anchor="middle" '
            f'style="font-size:9.5px;font-weight:{"bold" if is_bound else "normal"};fill:#111111;">{ratio:.2f}x</text>'
        )
        lines.append(
            f'<text x="{cx:.1f}" y="{mt + ph + 16}" text-anchor="middle" class="tick">({r["k"]},{r["C"]})</text>'
        )
        lines.append(
            f'<text x="{cx:.1f}" y="{mt + ph + 29}" text-anchor="middle" style="font-size:8.5px;fill:#555555;">'
            f'S:{r["small_k"]}&#8594;{r["small_C"]}</text>'
        )

    lines.append(f'<rect x="{ml}" y="{H - 25}" width="12" height="10" fill="#1f2937" stroke="#111111"/>')
    lines.append(f'<text x="{ml + 18}" y="{H - 16}" class="legend">Boundary pair (10m-1, 10m)</text>')
    lines.append(f'<rect x="{ml + 195}" y="{H - 25}" width="12" height="10" fill="#d1d5db" stroke="#111111"/>')
    lines.append(f'<text x="{ml + 213}" y="{H - 16}" class="legend">Neighbor non-boundary pair</text>')

    lines.append("</svg>")
    (FIGURES / "fig2_s3fifo_periodic_boundaries.svg").write_text("\n".join(lines), encoding="utf-8")


def generate_fig3() -> None:
    """Fig 3: Pareto Frontier (Worst-Case Recovery Time vs Normal Miss-Ratio Regression)."""
    sym = json.loads((RESULTS / "tahap5_symmetry.json").read_text(encoding="utf-8"))
    rows = {r["policy"]: r for r in sym["pareto_symmetric"]}

    bw_keys = ["SIEVE-BW(C)", "SIEVE-BW(2C)", "SIEVE-BW(4C)", "SIEVE-BW(8C)", "SIEVE-BW(16C)"]
    tp_keys = ["SIEVE-TP(C)", "SIEVE-TP(2C)", "SIEVE-TP(4C)", "SIEVE-TP(8C)", "SIEVE-TP(16C)"]
    labels = ["C", "2C", "4C", "8C", "16C"]

    W, H = 780, 435
    ml, mr, mt, mb = 74, 52, 66, 74
    pw, ph = W - ml - mr, H - mt - mb

    x_min, x_max = math.log10(700), math.log10(26000)
    y_min, y_max = -0.016, 0.082

    def map_x(reqs: float) -> float:
        return ml + pw * (math.log10(reqs) - x_min) / (x_max - x_min)

    def map_y(delta: float) -> float:
        return mt + ph * (1.0 - (delta - y_min) / (y_max - y_min))

    lines = svg_header(W, H)
    lines.append(
        f'<text x="{ml}" y="26" class="title">Fig 3. Pareto Frontier: Recovery Cost vs. Normal Workload Regression</text>'
    )
    lines.append(
        f'<text x="{ml}" y="44" class="subtitle">Pure Zombie Pinning recovery at C=1000 (log scale) vs. mean Zipf/Scan miss-ratio delta across 15 benchmarks.</text>'
    )

    for xv, xl in [(1000, "1,000"), (2000, "2,000"), (4000, "4,000"), (8000, "8,000"), (16000, "16,000")]:
        x = map_x(xv)
        lines.append(f'<line x1="{x:.1f}" y1="{mt}" x2="{x:.1f}" y2="{mt + ph}" class="grid"/>')
        lines.append(f'<text x="{x:.1f}" y="{mt + ph + 16}" text-anchor="middle" class="tick">{xl}</text>')

    for yv in [-0.01, 0.0, 0.01, 0.02, 0.03, 0.04, 0.05, 0.06, 0.07, 0.08]:
        y = map_y(yv)
        if abs(yv) < 1e-9:
            lines.append(
                f'<line x1="{ml}" y1="{y:.1f}" x2="{ml + pw}" y2="{y:.1f}" '
                f'stroke="#111111" stroke-width="1.2" stroke-dasharray="5,4"/>'
            )
            lines.append(
                f'<text x="{ml + 8}" y="{y + 13:.1f}" text-anchor="start" '
                f'style="font-size:9.5px;font-weight:bold;fill:#111111;">SIEVE baseline (&#916; = 0.0000, recovery = &#8734;)</text>'
            )
        else:
            lines.append(f'<line x1="{ml}" y1="{y:.1f}" x2="{ml + pw}" y2="{y:.1f}" class="grid"/>')
        lines.append(
            f'<text x="{ml - 8}" y="{y + 3.5:.1f}" text-anchor="end" class="tick">{yv:+.2f}</text>'
        )

    lines.append(f'<line x1="{ml}" y1="{mt}" x2="{ml}" y2="{mt + ph}" class="axis"/>')
    lines.append(f'<line x1="{ml}" y1="{mt + ph}" x2="{ml + pw}" y2="{mt + ph}" class="axis"/>')
    lines.append(
        f'<text x="{ml + pw / 2:.1f}" y="{H - 37}" text-anchor="middle" class="axis-label">'
        f'Worst-Case Zombie Pinning Recovery at C = 1000 (requests, log scale)</text>'
    )
    lines.append(
        f'<text x="20" y="{mt + ph / 2:.1f}" transform="rotate(-90 20 {mt + ph / 2:.1f})" '
        f'text-anchor="middle" class="axis-label">Mean Miss-Ratio Delta vs. SIEVE (&#916;)</text>'
    )

    # SIEVE-BW polyline (dashed gray) + squares
    bw_pts = [(map_x(rows[k]["recov_reqs_pure_c1000"]), map_y(rows[k]["avg_delta_vs_sieve"])) for k in bw_keys]
    lines.append(
        f'<polyline fill="none" stroke="#6b7280" stroke-width="2" stroke-dasharray="4,3" '
        f'points="{" ".join(f"{x:.1f},{y:.1f}" for x, y in bw_pts)}"/>'
    )
    for (x, y), lbl, k in zip(bw_pts, labels, bw_keys):
        d = rows[k]["avg_delta_vs_sieve"]
        lines.append(
            f'<rect x="{x - 4.5:.1f}" y="{y - 4.5:.1f}" width="9" height="9" fill="#ffffff" stroke="#374151" stroke-width="1.8"/>'
        )
        if lbl in ("C", "2C", "4C"):
            lines.append(f'<text x="{x + 7:.1f}" y="{y - 7:.1f}" text-anchor="start" class="annot">BW({lbl}) {d:+.4f}</text>')
        elif lbl == "8C":
            # Strictly below the point and below the Δ=0 dashed line
            lines.append(f'<text x="{x:.1f}" y="{y + 24:.1f}" text-anchor="middle" class="annot">BW(8C) {d:+.4f}</text>')
        else:
            # Strictly below BW(16C)
            lines.append(f'<text x="{x:.1f}" y="{y + 16:.1f}" text-anchor="middle" class="annot">BW(16C) {d:+.4f}</text>')

    # SIEVE-TP polyline (solid black) + circles
    tp_pts = [(map_x(rows[k]["recov_reqs_pure_c1000"]), map_y(rows[k]["avg_delta_vs_sieve"])) for k in tp_keys]
    lines.append(
        f'<polyline fill="none" stroke="#111111" stroke-width="2.2" '
        f'points="{" ".join(f"{x:.1f},{y:.1f}" for x, y in tp_pts)}"/>'
    )
    for (x, y), lbl, k in zip(tp_pts, labels, tp_keys):
        d = rows[k]["avg_delta_vs_sieve"]
        lines.append(f'<circle cx="{x:.1f}" cy="{y:.1f}" r="4.8" fill="#111111"/>')
        if lbl in ("C", "2C", "4C"):
            # Strictly below the solid black line in clear space above Δ=0
            lines.append(
                f'<text x="{x + 5:.1f}" y="{y + 15:.1f}" text-anchor="start" class="annot" style="font-weight:bold;">'
                f'TP({lbl}) {d:+.4f}</text>'
            )
        else:
            # Strictly above the points at 8C and 16C
            lines.append(
                f'<text x="{x:.1f}" y="{y - 11:.1f}" text-anchor="middle" class="annot" style="font-weight:bold;">'
                f'TP({lbl}) {d:+.4f}</text>'
            )

    # Legend
    ly = H - 14
    lines.append(f'<line x1="{ml}" y1="{ly - 3}" x2="{ml + 24}" y2="{ly - 3}" stroke="#111111" stroke-width="2.2"/>')
    lines.append(f'<circle cx="{ml + 12}" cy="{ly - 3}" r="4" fill="#111111"/>')
    lines.append(f'<text x="{ml + 30}" y="{ly}" class="legend">SIEVE-TP (Periodic Tail-Probe)</text>')

    lines.append(f'<line x1="{ml + 235}" y1="{ly - 3}" x2="{ml + 259}" y2="{ly - 3}" stroke="#6b7280" stroke-width="2" stroke-dasharray="4,3"/>')
    lines.append(f'<rect x="{ml + 243}" y="{ly - 7}" width="8" height="8" fill="#ffffff" stroke="#374151" stroke-width="1.6"/>')
    lines.append(f'<text x="{ml + 266}" y="{ly}" class="legend">SIEVE-BW (Bounded-Wrap)</text>')

    lines.append("</svg>")
    (FIGURES / "fig3_pareto_frontier.svg").write_text("\n".join(lines), encoding="utf-8")


def main() -> None:
    FIGURES.mkdir(parents=True, exist_ok=True)
    generate_fig1()
    generate_fig2()
    generate_fig3()
    print("Generated 3 SVG figures in figures/:")
    for name in ("fig1_zombie_pinning_mixed.svg", "fig2_s3fifo_periodic_boundaries.svg", "fig3_pareto_frontier.svg"):
        p = FIGURES / name
        print(f"  {p} ({p.stat().st_size} bytes)")


if __name__ == "__main__":
    main()
