# Hand Starvation and Zombie Pinning in SIEVE: A Comparative Atlas of Unbounded Belady Anomalies in Modern Cache Policies

**Author:** Rafi Wildan (Independent Researcher)  
**Date:** November 27, 2026

## Abstract

SIEVE (Zhang et al., NSDI '24) achieves strong miss-ratio reductions over LRU and CLOCK using a single FIFO queue, one `visited` bit per object, and a roving eviction `hand` that preserves spatial locality near `head`. We identify and prove a structural pathology unique to SIEVE's decoupled insertion/eviction geometry: **hand starvation (zombie pinning)**. Whenever an eviction scan clears a block of older objects and stops ahead of them, subsequent misses at `head` can trap `hand` in a tight oscillation over just two active slots, permanently pinning $C - 2$ unvisited *zombie* objects in a cache of size $C$ indefinitely—even under background Zipf noise ($p = 1\%$) or continuous hits to the pinned zombies ($p = 25\%$).

Using an explicit construction, we prove that for **every** capacity pair $3 \le k < C$, SIEVE suffers an **unbounded Belady anomaly**: `SIEVE(k)` converges to a **0% steady-state miss rate** while `SIEVE(C)` locks at a **100% steady-state miss rate**. We prove a sharp phase transition at $k = 2$ (where `0% vs >0%` is structurally impossible, yet exact state-graph solving yields finite Belady ratios of $3/2$ at $(2,3)$ and $4/3$ at $(2,4)$) and a structural separation theorem showing that **FIFO, CLOCK, 2-bit CLOCK, SecondChance, and S3-FIFO** are all immune to `0% vs >0%` starvation—even though all exhibit finite Belady anomalies (with S3-FIFO spiking up to $2.2727\times$ at multiple-of-10 capacity boundaries where `Small` grows while `Main` stagnates). Finally, we design and evaluate two $O(1)$, zero-per-node-overhead mitigations—**Bounded-Wrap SIEVE (`SIEVE-BW`)** and **Periodic Tail-Probe SIEVE (`SIEVE-TP`)**—mapping their Pareto frontier across 15 Zipf and Zipf+Scan benchmarks (`200,000` requests $\times$ 3 seeds) and adversarial recovery regimes. At matched recovery budgets ($K \in \{C, 2C, 4C\}$), `SIEVE-TP` cuts average normal-workload miss-ratio regression by $2.6\times$–$3.9\times$ compared to `SIEVE-BW`, while `SIEVE-BW(16C)` reveals an unexpected win-win regime that beats vanilla SIEVE on average ($\Delta = -0.0040$) while bounding worst-case starvation.

---

## 1. Introduction

FIFO-derived eviction algorithms have experienced a renaissance in modern systems due to their lock-free hit paths and low metadata overhead. Two prominent designs—**S3-FIFO** (Yang et al., SOSP '23) and **SIEVE** (Zhang et al., NSDI '24)—achieve lower miss ratios than LRU on skewed web workloads while avoiding list relinking on cache hits. Yet relaxing LRU's stack property reintroduces **Belady's anomaly**, where increasing cache capacity worsens miss ratio. While Fornai and Iványi (2010) proved that classic FIFO admits unbounded Belady ratios when both caches continue missing, and recent work (*SR-Gadgets*, 2026) addresses repeated-scan cliffs in multi-queue policies, the worst-case pointer dynamics of SIEVE's stationary `hand` have not been formally characterized.

**Contributions.**
1. **Unbounded Anomaly via Zombie Pinning (Theorem 1):** We construct an explicit trace family of prefix length $2C - 2$ and 3-key cycle for all $3 \le k < C$ yielding `0%` steady-state misses in `SIEVE(k)` vs. `100%` in `SIEVE(C)`, cross-verified in Python and Go across all $378$ pairs $3 \le k < C \le 30$ and scales up to $(500, 1000)$. We also document a subtle implementation divergence when `insert(head)` precedes `hand = o.prev`.
2. **Exact Phase Transition at $k = 2$ & Policy Atlas (Theorems 2–3):** We prove `0% vs >0%` anomalies are impossible at $k = 2$ for SIEVE and impossible for all $k < C$ in `FIFO`, `CLOCK`, `CLOCK2`, `SecondChance`, and `S3-FIFO`, while computing exact maximum finite Belady ratios via state-graph search and identifying periodic spikes in `S3-FIFO` at $C = 10m$.
3. **Mixed-Traffic Survival & Pareto-Optimal Mitigations (Proposition 4):** We show SIEVE remains 100% pinned under `1%` disjoint Zipf noise and `25%` zombie-hit traffic for 50,000 requests, and evaluate `SIEVE-BW` and `SIEVE-TP` across $\{C, 2C, 4C, 8C, 16C\}$.

---

## 2. SIEVE Pointer Semantics and Implementation Sensitivity

Following Zhang et al. (NSDI '24) and directly verified against upstream `1a1a11a/libCacheSim` commit `4b66d977` (`libCacheSim/cache/eviction/Sieve.c`, `Sieve_find` L126–134, `Sieve_insert` L147–154, `Sieve_evict` L219–245; `libCacheSim/cache/cache.c`, `cache_get_base` L321–326), SIEVE maintains a doubly-linked list from `head` (newest) to `tail` (oldest) and a pointer `hand` (initially `None`):
- **Hit:** Set `node.visited = True` without moving the node.
- **Miss (full cache):** Set `o = hand` (or `tail` if `hand == None`). While `o.visited == True`, set `o.visited = False` and step `o = o.prev` (wrapping to `tail` if `o.prev == None`). Then set `hand = o.prev` (which is `None` if `o == head`), remove `o`, and only then insert the incoming key at `head` with `visited = False`.

**Conformance Note on `S3-FIFO`.** Our comparative Stage 2–4 S3-FIFO baseline uses strict per-queue capacities ($S = \max(1, \lfloor 0.1 C \rfloor)$, $M = G = C - S$) with `move-to-main-threshold = 1` (matching SOSP '23 prose and `S3FIFOd.c:46`). Direct audit against `libCacheSim` commit `4b66d97` (`S3FIFO.c` and `S3FIFOv0.c`) documents default `move-to-main-threshold = 2`, `(int64_t)(C * 0.90)` ghost sizing (matching $G = M$ at multiples of 10), and total-capacity eviction (`S3FIFO.c:422-435`); a standalone conformance binary (`verifiers/cmd/conformance_check`) confirms that both Zombie Pinning immunity (`0/1200` steady misses) and multiple-of-10 boundary Belady spikes (`1.2833x` at `(19,20)`, `1.4999x` at `(29,30)`, `1.7044x` at `(39,40)`) persist under exact `S3FIFO.c` semantics (see [`results/libcachesim_conformance.md`](../results/libcachesim_conformance.md)).

**Order-of-Operations Sensitivity.** If an implementation inserts the new node at `head` *before* executing `hand = o.prev`, then whenever `o == head`, `o.prev` becomes the newly inserted node rather than `None`, causing `hand` to target the newest item on the next eviction. This edge-case variant diverges from the official specification on `700/700` random traces (200 steps each, capacities $2..8$) and breaks all 6 initial empirical witnesses (`k = 3..8`, causing `SIEVE(k)` to suffer `20,000`–`30,000` cycle misses instead of stabilizing at `0`). However, it does *not* prevent Zombie Pinning (`378/378` pairs still fail at `SIEVE(C)`), because during steady-state pinning the victim is always at index 1 (`o != head`).

---

## 3. Formal Analysis

### Theorem 1 (Unbounded Belady Anomaly / Zombie Pinning in SIEVE)
*For any integers $3 \le k < C$, let $z_1, \dots, z_{C-2}, A, B, D$ be $C + 1$ distinct keys. Define:*
$$\text{prefix} = (z_1, \dots, z_{C-2}, A, B, z_1, \dots, z_{C-2}), \qquad \text{cycle} = (D, A, B).$$
*Under repeated execution of `cycle`, `SIEVE(C)` locks into a steady state with **100% miss rate** where all $C - 2$ zombie keys $z_1, \dots, z_{C-2}$ remain unaccessed and unevicted forever, while `SIEVE(k)` achieves a **0% steady-state miss rate**.*

**Proof.**
1. *State of `SIEVE(C)` after `prefix`:* Because `prefix` contains exactly $C$ distinct keys $\{z_1, \dots, z_{C-2}, A, B\}$, no eviction occurs during `prefix`. After the first $C$ insertions, the list (from `head` to `tail`) is $[B(F), A(F), z_{C-2}(F), \dots, z_1(F)]$ with `hand = None`. The remaining $C - 2$ requests $(z_1, \dots, z_{C-2})$ are hits that set each $z_i.\text{visited} = \text{True}$ without changing positions.
2. *Cycle 0 of `SIEVE(C)`:*
   - Request $D$ misses. Since `hand = None`, scanning begins at `tail` ($z_1$), clears $z_1, \dots, z_{C-2}$ to `visited = False`, and stops at $o = A(F)$ (index 1, so $o \ne \text{head}$). Thus `hand` becomes $o.\text{prev} = B$, $A$ is evicted, and $D$ is inserted at `head`, yielding $[D(F), B(F), z_{C-2}(F), \dots, z_1(F)]$ with `hand = B` (index 1).
   - Request $A$ misses. `hand` points to $B(F)$ (index 1 $\ne \text{head}$), evicts $B$, sets `hand = D`, and inserts $A$ at `head`, yielding $[A(F), D(F), z_{C-2}(F), \dots, z_1(F)]$ with `hand = D`.
   - Request $B$ misses. `hand` points to $D(F)$ (index 1), evicts $D$, sets `hand = A`, and inserts $B$ at `head`, yielding $[B(F), A(F), z_{C-2}(F), \dots, z_1(F)]$ with `hand = A`.
3. *Induction for cycles $t \ge 1$:* The list and `hand` at the start of every cycle $t \ge 1$ are identical to the state before Cycle 0 except that all zombies now have `visited = False` (which `hand` never reaches). Hence every access in `(D, A, B)` misses forever (`100%` miss rate), while $z_1, \dots, z_{C-2}$ remain permanently pinned.
4. *Convergence of `SIEVE(k)` ($k \ge 3$):* In `SIEVE(k)`, the cycle accesses only 3 distinct keys $\{D, A, B\}$ and $k \ge 3$. Whenever a miss inserts one of $\{D, A, B\}$ at `head`, `hand` is strictly behind the newly inserted node (or `None`, wrapping to `tail`), and no sequence of misses on 3 keys can perpetually evict one of $\{D, A, B\}$ in a cache of size $k \ge 3$: once all 3 keys reside simultaneously in `SIEVE(k)`, all subsequent cycle requests are hits (`0%` steady-state miss rate). Cross-verification across all $378$ pairs $3 \le k < C \le 30$ and $(3,100), (3,500), (3,1000), (10,1000), (500,1000)$ confirms `SIEVE(k)` reaches `0` misses per cycle at cycle index 1 (`first_zero_k_cycle = 1`) in every case. $\square$

---

### Theorem 2 (Sharp Phase Transition at $k = 2$)
*For SIEVE with $k = 2$ and any $C \ge 3$, no reachable steady-state cycle can achieve **0% miss rate** in `SIEVE(2)` alongside **$>0\%$ miss rate** in `SIEVE(C)`.*

**Proof.** Any steady-state cycle $W$ achieving 0% miss rate in `SIEVE(2)` cannot trigger an eviction in `SIEVE(2)`, so every key in $W$ must already reside in `SIEVE(2)` at the start of $W$. Hence $W$ accesses a set $D(W)$ of at most $|D(W)| \le 2$ distinct keys. In `SIEVE(C)` ($C \ge 3$), if $|D(W)| \le 1$, after at most one miss the key remains at `head` forever (since $C \ge 3 > 1$, `tail != head`, so a subsequent hit never occurs alongside a miss). If $D(W) = \{A, B\}$, once either key (say $A$) is in `SIEVE(C)`, a miss on $B$ inserts $B$ at `head`. It can only evict $A$ if `hand` reaches $A$ at `head`, which sets `hand = None`; on the very next miss ($A$), `hand` starts at `tail` ($\ne B$ since $B$ is at `head` and $C \ge 3$), evicting a non-$B$ item and placing both $A$ and $B$ inside `SIEVE(C)` simultaneously. Once $\{A, B\} \subseteq \text{SIEVE}(C)$, no further misses occur in `SIEVE(C)` on $W$. $\square$

Exact Go state-graph analysis confirms `zeroK_posC = false` for `(2,3)` ($m \in \{3,4,5\}$) and `(2,4)` ($m \in \{3,4,5\}$), while revealing finite Belady ratios of **$3/2$** (`8` vs `12` misses over a 20-step cycle, `11,361` reachable states at $m=4$) for `(2,3)` and **$4/3$** (`18` vs `24` misses over a 48-step cycle, `267,091` states at $m=5$) for `(2,4)`. For $m \le C$, `SIEVE(C)` fits the entire universe after warm-up, giving exact maximum ratio `0/1`.

---

### Theorem 3 (Structural Separation Across Modern Policies)
*For every $1 \le k < C$, none of `FIFO`, `CLOCK`, `CLOCK2`, `SecondChance`, or `S3-FIFO` can exhibit a reachable steady-state cycle with 0% misses at capacity $k$ and $>0\%$ misses at capacity $C$.*

**Proof.** Let $W$ be a cycle with 0% misses at capacity $k$, so its distinct key set satisfies $|D(W)| \le k < C$. Under `FIFO(C)`, every miss evicts the oldest entry; non-$D(W)$ keys are never re-inserted and drain within $\le C$ misses. Under `CLOCK(C)` and `CLOCK2(C)`, every miss advances the circular hand `(hand + 1) % C` and non-$D(W)$ keys never increment their counters, so all non-$D(W)$ slots are swept within $\le 2C$ (`CLOCK`) or $\le 3C$ (`CLOCK2`) misses. Under `SecondChance(C)`, any non-$D(W)$ key reaching `tail` with `visited = True` is moved to `head` with `visited = False` once and evicted on its next arrival at `tail`. Finally, in `S3-FIFO(C)` ($S = \max(1, \lfloor 0.1 C \rfloor)$, $M = C - S$, Ghost capacity $G = M$), suppose $D(W)$ has $r$ keys not yet in `Main`. Any key of $D(W)$ evicted from `Small` with `freq = 0` enters `Ghost`. Because `Small` holds at most $S$ keys and $S + M = C > k \ge |D(W)|$, at most $|D(W)| - 1 < S + M$ other keys from $D(W)$ can enter `Small` or `Ghost` without promoting to `Main`; once at most $M$ stale prefix keys drain from `Ghost`, no key of $D(W)$ can be evicted from `Ghost` before returning, ensuring every key in $D(W)$ enters `Small` $\cup$ `Main` and remains hit-resident forever. $\square$

---

### Proposition 4 (Bounded Recovery of `SIEVE-BW` and `SIEVE-TP`)
*Let `SIEVE-BW(C, K)` force `hand = None` after $K$ evictions without a natural wrap, and let `SIEVE-TP(C, K)` probe `tail` after $K$ evictions without a natural wrap (evicting `tail` directly if `!tail.visited`, or clearing `tail.visited` and moving `tail` to `head` before normal eviction).*
1. *On the pure Zombie Pinning construction (`prefix` + repeated `[D, A, B]`), both `SIEVE-BW(C, K)` and `SIEVE-TP(C, K)` evict a zombie at eviction $K + 1$ and reach **0% steady-state miss rate** within $\lceil (K+1)/3 \rceil$ cycles.*
2. *Even under adversarial `visited`-bit refreshes on pinned items (`zombie_hits_only`), `SIEVE-TP(C, K)` strictly rotates any `visited = True` item at `tail` to `head` with `visited = False`, preventing permanent starvation of unaccessed slots.*

---

## 4. Comparative Atlas & Periodic S3-FIFO Spikes

Table 1 summarizes exact state-graph and 5,000-cycle evolutionary results across all 6 policies (all witnesses independently verified in Python).

| Policy | Zombie Pinning `rate_k / rate_C` | Unbounded `0% vs >0%` | Smallest `(k, C)` Belady Ratio $>1$ | Exact `(2,3,m=4)` Max Ratio | Max Observed Finite Ratio |
| :--- | :---: | :---: | :---: | :---: | :---: |
| **`SIEVE`** | `0.00% / 100.00%` | **Yes** ($\forall 3 \le k < C$) | `(2, 3)` | `3/2` (`1.5000`) | **$\infty$** (`0%` vs `100%`) |
| **`FIFO`** | `0.00% / 0.00%` | No | `(3, 4)` | `1/1` (none) | `2.0000` at `(3,4,m=5)` |
| **`CLOCK`** | `0.00% / 0.00%` | No | `(2, 3)` | `3/2` (`1.5000`) | `1.5000` at `(2,3,m=4)` |
| **`CLOCK2`** | `0.00% / 0.00%` | No | `(2, 3)` | `2/1` (`2.0000`) | `2.0000` at `(2,3)` & `(3,4)` |
| **`SecondChance`** | `0.00% / 0.00%` | No | `(2, 3)` | `3/2` (`1.5000`) | `1.5000` at `(2,3,m=4)` |
| **`S3-FIFO`** | `0.00% / 0.00%` | No | `(2, 3)` | `15/8` (`1.8750`, 582,113 states) | **`2.2727`** at `(39, 40)` |

**Periodic S3-FIFO Capacity-Boundary Spikes.** Because `S3-FIFO` sets $S = \max(1, \lfloor 0.1 C \rfloor)$ and $M = C - S$, crossing $k = 10m - 1 \to C = 10m$ ($m \ge 2$) expands `Small` ($m-1 \to m$) while `Main` stagnates at $9m$. Evolutionary search (5,000 cycles) reveals elevated Belady spikes at every boundary relative to adjacent non-boundary pairs (Figure 2): `(9,10) = 1.3334x`, `(19,20) = 1.6000x` (vs. `(18,19) = 1.2245x`, `(20,21) = 1.4482x`), `(29,30) = 2.0738x` (vs. `(28,29) = 1.5555x`, `(30,31) = 1.8181x`), **`(39,40) = 2.2727x`** (`4.04%` vs. `9.18%`), and `(49,50) = 1.6531x`. An explicit deterministic contention family (`A3_contention`) reproduces `1.2583x` at `(29,30)`.

---

## 5. Mixed-Traffic Resilience and Mitigation Pareto Frontier

### 5.1 Mixed-Traffic Survival (Figure 1)
When `SIEVE(100)` and `SIEVE(200)` enter the pinned state and then receive background traffic with probability $p$ over 50,000 requests (3 seeds):
- **`zipf_disjoint (p = 1%)`:** `SIEVE` remains **100.0% pinned** for all 50,000 requests (`miss = 1.0000` at both `C=100` and `C=200`). `SIEVE-TP(C)` unwraps at request `86` (`C=100`, `miss = 0.0066`) and `186` (`C=200`, `miss = 0.0079`).
- **`zipf_disjoint (p = 5%)`:** `SIEVE(100)` takes `14,154` requests to first unwrap (`miss = 0.2969`), vs. `0.0207` for `SIEVE-TP(C)`.
- **`zombie_hits_only (p = 25%)`:** Random hits strictly on zombie keys (`0..C-3`) flip `visited = True` without triggering evictions, so `SIEVE` never unwraps (`unwrap = ∞`, `miss = 0.7493`). `SIEVE-TP(C)` breaks the lock in `116` requests (`C=100`, `miss = 0.0042`) and `251` requests (`C=200`, `miss = 0.0049`). Notice that under `16C` at `C=100`, `SIEVE-TP(16C)` unwraps in `2,129` requests (`miss = 0.0342`), **3.0x faster** than `SIEVE-BW(16C)` (`6,386` requests, `miss = 0.0978`), because `SIEVE-BW`'s forced wrap from `tail` can stall on recently-hit zombies whereas `SIEVE-TP` rotates `visited` zombies to `head`.

### 5.2 Symmetric Pareto Frontier Across $\{C, 2C, 4C, 8C, 16C\}$ (Figure 3)
We benchmarked all variants across 15 normal configurations (Zipf $\alpha \in \{0.6, 0.8, 1.0, 1.2\}$ and Zipf+Scan $\alpha=0.9$ with $2C$ sequential bursts, $C \in \{100, 500, 1000\}$, `200,000` requests $\times$ 3 seeds):

| Policy | Avg Miss Ratio | Avg $\Delta$ vs `SIEVE` | Max $\Delta$ | Min $\Delta$ | Pure Recov `C=1000` | `ZHit 25%` `C=100` Unwrap / Miss | `ZHit 25%` `C=1000` Unwrap / Miss |
| :--- | :---: | :---: | :---: | :---: | :---: | :---: | :---: |
| **`SIEVE`** | `0.4963` | `+0.0000` | `+0.0000` | `+0.0000` | $\infty$ | $\infty$ / `37,464` | $\infty$ / `37,464` |
| `SIEVE-BW(C)` | `0.5691` | `+0.0727` | `+0.1132` | `+0.0151` | `1,001` | `114` / `204` | `1,315` / `1,000` |
| `SIEVE-BW(2C)` | `0.5458` | `+0.0494` | `+0.0915` | `+0.0034` | `2,001` | `251` / `306` | `2,658` / `2,000` |
| `SIEVE-BW(4C)` | `0.5194` | `+0.0231` | `+0.0641` | `-0.0028` | `4,001` | `514` / `508` | `5,338` / `3,999` |
| `SIEVE-BW(8C)` | `0.4995` | `+0.0032` | `+0.0388` | `-0.0146` | `8,001` | `1,045` / `908` | `10,677` / `7,997` |
| **`SIEVE-BW(16C)`** | **`0.4924`** | **`-0.0040`** | `+0.0175` | `-0.0143` | `16,001` | `6,386` / `4,891` | `21,309` / `15,994` |
| **`SIEVE-TP(C)`** | `0.5150` | `+0.0187` | `+0.0378` | `+0.0004` | **`1,001`** | **`116` / `210`** | **`1,315` / `1,000`** |
| **`SIEVE-TP(2C)`** | `0.5099` | `+0.0136` | `+0.0334` | `+0.0002` | `2,001` | `254` / `310` | `2,658` / `2,000` |
| **`SIEVE-TP(4C)`** | `0.5052` | `+0.0088` | `+0.0303` | `+0.0001` | `4,001` | `516` / `511` | `5,340` / `3,997` |
| `SIEVE-TP(8C)` | `0.5013` | `+0.0050` | `+0.0212` | `+0.0000` | `8,001` | `1,047` / `902` | `10,679` / `7,998` |
| **`SIEVE-TP(16C)`** | `0.4989` | `+0.0026` | **`+0.0138`** | `-0.0004` | `16,001` | **`2,129` / `1,709`** | `21,312` / `15,995` |

**Key Trade-Off (Quick Demotion vs. Hand Starvation).** Vanilla SIEVE outperforms `CLOCK` (`0.5692`) and `LRU` (`0.5786`) specifically because `hand` lingers near `head` for $\gg 2C$ evictions between natural wraps, protecting hot items near `tail`. Bounding wraps at $O(C)$ in `SIEVE-BW(C/2C)` forces frequent global sweeps, collapsing performance toward `CLOCK` (`+0.0727` / `+0.0494`). Surgical tail probing (`SIEVE-TP`) dominates `SIEVE-BW` at fast recovery budgets ($C..4C$, reducing mean regression by $2.6\times$–$3.9\times$), and `SIEVE-TP(16C)` achieves the lowest worst-case regression (`max Δ = +0.0138`) across all variants. Conversely, `SIEVE-BW(16C)` shows that rare periodic tail sweeps slightly improve average Zipf+Scan performance (`avg Δ = -0.0040`, a mild natural refresh effect) at the cost of slower recovery under zombie-hit storms.

---

## 6. Limitations and Future Work

Our empirical benchmarks evaluate synthetic Zipf and Zipf+Scan traces rather than production trace suites (e.g., Twitter/CloudPhysics), and our exact state-graph solver is bounded by state-space explosion for multi-queue policies above $(k=3, C=4)$. Future work includes validating `SIEVE-TP(16C)` and `SIEVE-BW(16C)` on production traces and exploring adaptive tail-probe triggers conditioned on observed `tail` age.

## Artifact Availability

All Python reference simulators, independent Go verifiers, exact state-graph solvers, JSON experiment artifacts, SVG figure generators, and the one-command reproduction script (`python reproduce.py --quick`) are publicly available at <https://github.com/JOsee321/cache-anomalies>.
