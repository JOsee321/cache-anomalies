# Direct C Source Conformance Audit Against `1a1a11a/libCacheSim`

- **Audited Repository:** <https://github.com/1a1a11a/libCacheSim>
- **HEAD Commit Hash:** `4b66d9779f93ca9a9ead72b484fdaf754cbfd809` (short: `4b66d97`)
- **Audit Scope:** Line-by-line semantic comparison of our Python (`sim/sieve.py`, `sim/policies.py`) and Go (`verifiers/sieve/sieve.go`, `verifiers/policies/policies.go`) implementations against official `libCacheSim` C source files (`Sieve.c`, `S3FIFO.c`, `S3FIFOv0.c`, `S3FIFOd.c`, `FIFO.c`, `cache.c`, and `cacheObj.h`), plus execution of an exact `S3FIFO.c` / `S3FIFOv0.c` Go conformance verifier (`verifiers/cmd/conformance_check/main.go`).

---

## 1. SIEVE (`libCacheSim/cache/eviction/Sieve.c`) — **100% Semantic Conformance**

Our SIEVE implementations (`sim/sieve.py` and `verifiers/sieve/sieve.go`, default `insert_before_hand_update=False`) match `libCacheSim`'s `Sieve.c` **1:1 across every pointer transition**.

| Semantic Operation | `libCacheSim` C Source (`4b66d97`) | Python (`sim/sieve.py`) | Go (`verifiers/sieve/sieve.go`) | Status |
| :--- | :--- | :--- | :--- | :---: |
| **List orientation** | `prepend_obj_to_head` (`Sieve.c:150`, `cacheObj.h:422-441`): `obj->queue.prev = NULL`, `*head = obj`; `prev` walks from `q_tail` toward `q_head` (`Sieve.c:239`) | `head` = newest (`node.prev is None`), `tail` = oldest (`node.next is None`), `_insert_head` (`L62-70`) | `Head` = newest (`Prev == nil`), `Tail` = oldest (`Next == nil`), `insertHead` (`L37-47`) | Exact match |
| **New object insertion & initial bit** | `Sieve_insert` (`Sieve.c:147-154`): prepends at `q_head` (`L150`) and sets `obj->sieve.freq = 0` (`L151`) | `access` (`L123-135`): `Node(key)` starts `visited = False` (`L23, L124`), calls `_insert_head(new_node)` (`L134`) | `Access` (`L97-113`): allocates `&Node{Key: key, Visited: false}` (`L97`), calls `c.insertHead(n)` (`L112`) | Exact match |
| **Cache hit behavior** | `Sieve_find` (`Sieve.c:126-134`): on hit with `update_cache == true`, sets `cache_obj->sieve.freq = 1` without moving node | `access` (`L118-121`): sets `node.visited = True` without unlinking/moving | `Access` (`L92-95`): sets `n.Visited = true` without unlinking/moving | Exact match |
| **Eviction start & `NULL` wrap** | `Sieve_evict` (`Sieve.c:223`): `obj = params->pointer == NULL ? params->q_tail : params->pointer` | `_evict` (`L94`): `o = self.hand if self.hand is not None else self.tail` | `evict` (`L65-68`): `o := c.Hand; if o == nil { o = c.Tail }` | Exact match |
| **Hand scan loop** | `Sieve_evict` (`Sieve.c:231-240`): if `obj->sieve.freq > 0`, sets `obj->sieve.freq -= 1` and advances `obj = obj->queue.prev == NULL ? params->q_tail : obj->queue.prev` (`L239`); else `break` | `_evict` (`L97-100`): `while o.visited:` sets `o.visited = False` and advances `o = o.prev if o.prev is not None else self.tail` | `evict` (`L69-76`): `for o.Visited` sets `o.Visited = false` and advances `o.Prev` wrapping to `c.Tail` | Exact match |
| **Order of `hand = o->prev`, unlink, and insert** | `cache_get_base` (`cache.c:321-326`) calls `cache->evict` (`Sieve_evict`) **before** `cache->insert` (`Sieve_insert`). Inside `Sieve_evict` (`L242-244`), `params->pointer = obj->queue.prev` (`L242`) runs **before** `remove_obj_from_list` (`L243`) and **before** the new object is inserted at `q_head`. | Default path (`insert_before_hand_update=False`): `_evict` (`L107-108`) sets `self.hand = o.prev` and calls `_remove_node(o)` **before** `access` calls `_insert_head(new_node)` (`L134`) | Default path (`!c.InsertBeforeHandUpdate`): `evict` (`L83-84`) sets `c.Hand = o.Prev` and calls `c.removeNode(o)` **before** `Access` calls `c.insertHead(n)` (`L112`) | Exact match |
| **Evicting `o == head` (`q_head`)** | Because `o->queue.prev == NULL` (`cacheObj.h:425`) before insertion, `params->pointer` (`hand`) becomes `NULL` (`Sieve.c:242`), so the next eviction wraps to `q_tail` | Because `o.prev is None` before `_insert_head`, `self.hand` becomes `None` (`L107`), wrapping to `tail` on next miss | Because `o.Prev == nil` before `insertHead`, `c.Hand` becomes `nil` (`L83`), wrapping to `Tail` on next miss | Exact match |

### Implication for Stage 1–5 Results
- Our `insert_before_hand_update=False` model—on which **Theorem 1** (378 pairs + `(500, 1000)` Zombie Pinning), **Theorem 2** (`k=2` phase transition), mixed-traffic experiments, and Pareto benchmarks (`SIEVE-BW` / `SIEVE-TP`) rely—matches official `libCacheSim` `Sieve.c` (`4b66d97`) identically.
- Note on unit-size objects: `Sieve.c:231-240` includes a guard `budget = cache->n_obj + 1` skipping `n_pinned` objects, which is never triggered when all objects are unpinned and unit-sized.

---

## 2. S3-FIFO (`S3FIFO.c` & `S3FIFOv0.c`) — **Documented Semantic Differences & Verification**

Whereas `Sieve.c` matches 1:1, our Stage 2–4 S3-FIFO implementation (`sim/policies.py:191-315`, `verifiers/policies/policies.go:321-472`) follows the **paper prose description** of S3-FIFO (Yang et al., SOSP '23, `move-to-main-threshold = 1` / `freq > 0`, also used by `S3FIFOd.c:45-46` `"fifo-size-ratio=0.10,main-cache=Clock2,move-to-main-threshold=1"`) and uses strict per-queue bounds, whereas `libCacheSim`'s default C implementation `S3FIFO.c` (and `S3FIFOv0.c`) differs in **four specific details**:

| Dimension | `libCacheSim` `S3FIFO.c` / `S3FIFOv0.c` (`4b66d97`) | Our Stage 2–4 Simulator (`sim/policies.py`, `policies.go`) |
| :--- | :--- | :--- |
| **1. `Small` $\to$ `Main` promotion threshold** | `DEFAULT_CACHE_PARAMS` (`S3FIFO.c:53-54`, `S3FIFOv0.c:52`): `"move-to-main-threshold=2"` (`S3FIFO_evict_small`, `S3FIFO.c:356` / `S3FIFOv0.c:332`). An item in `Small` must receive **2 hits** (`freq >= 2`) before eviction from `Small` to promote to `Main`. (`S3FIFOd.c:46` uses `1`.) | `freq > 0` (`threshold = 1`, `sim/policies.py:286`, `policies.go:431`), requiring 1 hit in `Small`. |
| **2. Hit `freq` capping** | `S3FIFO_find` (`S3FIFO.c:265,277`, `S3FIFOv0.c:241,253`) increments `obj->S3FIFO.freq += 1` unboundedly on hit, and clamps only upon `Main` reinsertion via `MIN(freq, 3) - 1` (`S3FIFO.c:399`, `S3FIFOv0.c:389`). | Increments `freq` up to `3` on hit (`if freq < 3`, `sim/policies.py:296-297`, `policies.go:443-445`). Wait behavior on `Main` reinsertion is identical whenever `freq >= 3` (next freq becomes `2`). |
| **3. Queue capacities & rounding** | `small_fifo_size = (int64_t)(cache_size * 0.10)`, `main_fifo_size = cache_size - small_fifo_size`, `ghost_fifo_size = (int64_t)(cache_size * 0.90)` (`S3FIFO.c:139-145`, `S3FIFOv0.c:112-116`). Errors if `small_fifo_size <= 0` (so `C < 10` is invalid under default `0.10` ratio). At `C=19`, `S=1, M=18, G=17`. | `smallCap = max(1, C // 10)`, `mainCap = C - smallCap`, `ghostCap = mainCap` (`sim/policies.py:244-246`, `policies.go:370-379`). At multiples of 10 (`C = 10, 20, 30, 40, ...`), `(S, M, G)` are **identical** (`S=C/10, M=G=0.9C`). At `C=19`, ours gives `G=18` vs `S3FIFO.c`'s `G=17`. |
| **4. Eviction dispatch & cold-start overflow** | `cache_get_base` (`cache.c:321-325`) checks total `occupied >= cache_size` and calls `S3FIFO_evict` (`S3FIFO.c:422-435`), which dispatches `S3FIFO_evict_main` when `main > M \|\| small == 0` else `S3FIFO_evict_small`. Promotion in `evict_small` (`S3FIFO.c:363`) inserts into `Main` (`freq = 0` via `memset` in `cacheObj.h:271`) without setting `has_evicted = true`, looping until a real eviction sets `has_evicted = true`. In `S3FIFO.c:315-317` (not `S3FIFOv0.c:280-288`), cold-start misses when `!has_evicted && small >= S` spill directly to `Main`. | Strict per-queue overflow eviction (`small` evicts at `small.size >= smallCap`; promotion evicts `main` at `main.size >= mainCap` if needed). Every non-ghost miss inserts into `small`. |

*(Minor upstream C quirk observed in `4b66d97`: `S3FIFO.c:311` checks `req->obj_size >= small_fifo->cache_size` while `S3FIFO_can_insert` at `S3FIFO.c:476` checks `<=`, which does not affect `C >= 10` unit-size object workloads.)*

### Verification Under Exact `libCacheSim` `S3FIFO.c` & `S3FIFOv0.c` Semantics
We implemented exact Go ports of both `S3FIFO.c` (with `!has_evicted` cold-start route to `Main`) and `S3FIFOv0.c` (without cold-start spill), including `move-to-main-threshold=2`, `(int64_t)(C * 0.90)` ghost sizing, and total-capacity eviction dispatch (`verifiers/cmd/conformance_check/main.go`). Running `go run ./cmd/conformance_check` confirms:

1. **Theorem 3 (Zombie Pinning Immunity) holds verbatim under exact `S3FIFO.c` and `S3FIFOv0.c`:**
   - `S3FIFO.c` at `(10, 20)`, `(19, 20)`, `(10, 100)`, `(10, 1000)`: `steady_miss_k = 0/1200`, `steady_miss_C = 0/1200`.
   - `S3FIFOv0.c` at `(10, 20)`, `(19, 20)`, `(10, 100)`, `(10, 1000)`: `steady_miss_k = 0/1200`, `steady_miss_C = 0/1200`.
2. **Multiple-of-10 Boundary Belady Spikes persist under exact `S3FIFO.c`:**
   Even a short 150-generation evolutionary check (`conformance_check`) immediately discovers Belady spikes across the exact `S3FIFO.c` multiple-of-10 transitions where `Small` grows while `Main` stagnates (note `C <= 9` is excluded by `S3FIFO.c:144` since `int64_t(0.10 * 9) == 0`):
   - `(19, 20)`: Belady ratio **`1.2833x`** (`0.1029` at `k=19` vs `0.1321` at `C=20`, cycle len `53`)
   - `(29, 30)`: Belady ratio **`1.4999x`** (`0.0533` at `k=29` vs `0.0800` at `C=30`, cycle len `75`)
   - `(39, 40)`: Belady ratio **`1.7044x`** (`0.0667` at `k=39` vs `0.1136` at `C=40`, cycle len `88`)

Thus, our `Small = max(1, C // 10)` model (`move-to-main-threshold=1`) remains faithfully reported as the Stage 2–4 S3-FIFO baseline alongside `CLOCK`/`CLOCK2`/`SecondChance`/`FIFO`, while the core structural findings—SIEVE Zombie Pinning conformance (1:1 with `Sieve.c`), S3-FIFO starvation immunity, and multiple-of-10 capacity-boundary Belady spikes—hold directly against upstream `libCacheSim` C code (`4b66d97`).
