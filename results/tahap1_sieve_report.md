# Laporan Tahap 1: Verifikasi Anomali Belady & Fenomena *Zombie Pinning* pada Algoritma Cache **SIEVE**

## 1. Struktur Workspace
Workspace riset telah diinisialisasi secara modular dengan pemisahan implementasi referensi, verifikator independen, skrip eksperimen, dan hasil uji:

- `sim/`:
  - [`sieve.py`](file:///d:/cache-anomalies/sim/sieve.py) — Implementasi referensi Python menggunakan *doubly-linked list* eksplisit (`Node` dengan `key`, `visited`, `prev`, `next`, serta pointer `head`, `tail`, dan `hand` pada `SieveCache`).
  - [`test_sieve.py`](file:///d:/cache-anomalies/sim/test_sieve.py) — Unit test Python yang memverifikasi invarian struktur *doubly-linked list*, reproduksi saksi `k=3`, dan divergensi varian `insert(head)` vs spesifikasi resmi.
- `verifiers/`:
  - [`go.mod`](file:///d:/cache-anomalies/verifiers/go.mod) — Modul Go (`sieve-verifier`, Go 1.26).
  - [`sieve.go`](file:///d:/cache-anomalies/verifiers/sieve/sieve.go) & [`sieve_test.go`](file:///d:/cache-anomalies/verifiers/sieve/sieve_test.go) — Implementasi SIEVE independen dalam Go dengan struktur *doubly-linked list* dan unit test invarian.
  - [`main.go`](file:///d:/cache-anomalies/verifiers/cmd/verify/main.go) — CLI verifikator Go untuk replikasi saksi awal (`k=3..8`) dan pengujian teorema *Zombie Pinning*.
  - [`main.go`](file:///d:/cache-anomalies/verifiers/cmd/k2solver/main.go) — Solver graf state eksak di Go untuk batas `k = 2` (dengan rekonstruksi saksi terpendek dan verifikasi silang terhadap `sieve.Cache`).
- `experiments/`:
  - [`verify_witnesses.py`](file:///d:/cache-anomalies/experiments/verify_witnesses.py) — Replikasi 6 saksi awal (`k=3..8`) dan uji perbandingan varian pada 700 trace acak.
  - [`verify_zombie_pinning.py`](file:///d:/cache-anomalies/experiments/verify_zombie_pinning.py) — Uji konstruksi *Zombie Pinning* pada seluruh 378 pasangan $3 \le k < C \le 30$ dan 5 kasus skala ekstrem.
  - [`cross_check.py`](file:///d:/cache-anomalies/experiments/cross_check.py) — Verifikasi kesetaraan (*parity*) keluaran Python dan Go pada seluruh eksperimen.
- `results/`:
  - [`witnesses_py.json`](file:///d:/cache-anomalies/results/witnesses_py.json), [`witnesses_go.json`](file:///d:/cache-anomalies/results/witnesses_go.json)
  - [`zombie_pinning_py.json`](file:///d:/cache-anomalies/results/zombie_pinning_py.json), [`zombie_pinning_go.json`](file:///d:/cache-anomalies/results/zombie_pinning_go.json)
  - [`k2_solver_go.json`](file:///d:/cache-anomalies/results/k2_solver_go.json)
  - [`tahap1_sieve_report.md`](file:///d:/cache-anomalies/results/tahap1_sieve_report.md) — Laporan ini.

---

## 2. Validasi Semantik Spesifikasi Resmi SIEVE & Analisis Varian

### 2.1 Kesesuaian dengan Spesifikasi Paper NSDI '24
*(Catatan: Akses jaringan langsung ke `raw.githubusercontent.com` tidak tersedia di dalam sandbox eksekusi, sehingga validasi dilakukan terhadap spesifikasi resmi algoritma SIEVE pada Zhang et al., NSDI '24.)*

Kedua implementasi ([`sieve.py`](file:///d:/cache-anomalies/sim/sieve.py) dan [`sieve.go`](file:///d:/cache-anomalies/verifiers/sieve/sieve.go)) memodelkan semantik pointer 1:1 terhadap pseudocode C SIEVE:
1. **Penyisipan (*insertion*):** Node baru dimasukkan ke `head` (elemen terbaru, `head.prev = None`) dengan `visited = False`.
2. **Hit:** Jika kunci ada di tabel hash, set `node.visited = True` tanpa mengubah posisi node di list.
3. **Eviction:** Mulai dari `o = hand` (jika `None`, mulai dari `tail`/terlama). Selama `o.visited == True`, set `o.visited = False` lalu geser `o = o.prev` (menuju `head`; jika `o.prev == None`, bungkus ke `tail`).
4. **Update pointer & penghapusan:** Setelah menemukan node korban `o` dengan `visited == False`, geser `hand = o.prev` (menjadi `None` apabila `o == head`), hapus `o` dari list dan map, lalu masukkan item baru di `head`.

Unit test Python (`3/3` lulus) dan Go (`2/2` lulus) memverifikasi seluruh invarian *doubly-linked list* (`head.prev == None`, `tail.next == None`, simetri `prev`/`next`, dan keanggotaan `hand`).

### 2.2 Perbedaan Varian Edge-Case: `insert(head)` Sebelum vs Sesudah `hand = o.prev`
Pengujian eksplisit dilakukan untuk membandingkan **Spesifikasi Resmi** (`hand = o.prev` lalu `remove(o)` lalu `insert(head)`) melawan **Varian** (`insert(head)` **sebelum** `hand = o.prev` dan `remove(o)`):

- **Mekanisme divergensi:** Jika korban eviksi `o` kebetulan berada di `head`, pada spesifikasi resmi `o.prev` bernilai `None`, sehingga `hand` di-reset ke `None` (eviksi berikutnya mulai dari `tail`/terlama). Pada varian `insert(head)` terlebih dahulu, penyisipan `new_node` ke `head` mengubah `o.prev` menjadi `new_node`; akibatnya `hand = o.prev` justru menunjuk ke `new_node` (elemen paling baru di `head` dengan `visited = False`). Jika `new_node` tidak di-hit sebelum miss berikutnya, `new_node` langsung dieviksi pada miss berikutnya!
- **Saksi minimal divergensi (kapasitas 2):** Trace `[0, 1, 0, 2, 3]` menghasilkan urutan eviksi `[1, 0]` (akhir cache `[3, 2]`) pada spesifikasi resmi, tetapi menghasilkan `[1, 2]` (akhir cache `[3, 0]`) pada varian.
- **Dampak pada uji acak & saksi awal:**
  - Pada 700 trace acak (`cap = 2..8`, panjang 200), **700/700** uji mengalami divergensi antara spesifikasi resmi dan varian.
  - Pada 6 saksi awal (`k=3..8`, 10.000 siklus), **seluruh 6 saksi gagal mempertahankan 0 miss pada `Sieve(k)`** di bawah varian (mis. pada `k=3`, `Sieve(3)` melonjak dari `4` miss menjadi `30.000` miss; pada `k=7`, `Sieve(8)` turun dari `30.000` miss menjadi `1` miss!).
  - Sebaliknya, pada konstruksi *Zombie Pinning*, varian tetap lolos **378/378** pasangan karena di steady-state pointer `hand` pada `Sieve(C)` terkunci bolak-balik di posisi ke-2 dari `head` (`o != head`), sehingga kasus tepi `o == head` tidak pernah terpicu.

---

## 3. Replikasi 6 Saksi Awal (`k = 3..8`, 10.000 Putaran Siklus)

Implementasi Python dan Go menghasilkan output yang **100% identik** di seluruh metrik pada 10.000 putaran siklus untuk keenam saksi awal:

| `k` | `C = k+1` | Panjang Prefix / Siklus | `Sieve(k)` Miss Prefix | `Sieve(k)` Miss Siklus Total (10.000×) | Siklus Mulai 0 Miss (`tail_all_zero`) | `Sieve(C)` Miss Prefix | `Sieve(C)` Miss Siklus Total | `Sieve(C)` Miss Rate Steady-State |
| :-: | :-: | :-: | :-: | :-: | :-: | :-: | :-: | :-: |
| 3 | 4 | 7 / 3 | 4 | **4** | Siklus 2 (`True`) | 4 | **30.000** / 30.000 | **100,00%** (3/3 per siklus) |
| 4 | 5 | 29 / 4 | 11 | **5** | Siklus 2 (`True`) | 9 | **29.999** / 40.000 | **75,00%** (3/4 steady) |
| 5 | 6 | 12 / 5 | 5 | **4** | Siklus 2 (`True`) | 5 | **30.000** / 50.000 | **60,00%** (3/5 per siklus) |
| 6 | 7 | 39 / 6 | 12 | **3** | Siklus 2 (`True`) | 8 | **30.000** / 60.000 | **50,00%** (3/6 per siklus) |
| 7 | 8 | 34 / 7 | 12 | **4** | Siklus 2 (`True`) | 11 | **30.000** / 70.000 | **42,86%** (3/7 per siklus) |
| 8 | 9 | 16 / 7 | 7 | **4** | Siklus 2 (`True`) | 7 | **30.000** / 70.000 | **42,86%** (3/7 per siklus) |

> [!NOTE]
> Pada `k=4`, `Sieve(5)` mengalami 2 miss pada siklus pertama (`ci=0`) lalu terkunci di 3 miss per siklus pada 9.999 siklus sisanya (total `29.999` miss). Pada semua saksi lainnya, `Sieve(C)` mengalami tepat 3 miss di setiap siklus sejak siklus 0.

---

## 4. Verifikasi Konstruksi Umum Teorema *Zombie Pinning* ($3 \le k < C$)

Untuk setiap $(k, C)$ dengan $3 \le k < C$, konstruksi menggunakan $C - 2$ kunci zombie $z_1, \dots, z_{C-2}$ dan 3 kunci aktif $A, B, D$:
- `prefix` (panjang $2C - 2$): `[z_1, ..., z_{C-2}, A, B, z_1, ..., z_{C-2}]`
- `cycle` (panjang 3): `[D, A, B]`

### Hasil Uji Silang (Python & Go, 1.000 Siklus)
- **Seluruh grid $3 \le k < C \le 30$:** **378 / 378 pasangan LULUS** (baik Python maupun Go, dengan kesesuaian 100%).
  - Pada seluruh 378 pasangan, `Sieve(k)` mencapai **0 miss per siklus sejak siklus indeks 1** (`first_zero_k_cycle = 1`), sehingga miss steady-state = `0`.
  - Pada seluruh 378 pasangan, `Sieve(C)` terkunci di **100% miss rate** (`2.970 / 2.970` pada jendela steady-state 990 siklus terakhir, bahkan sejak siklus 0 yaitu `3.000 / 3.000`), dengan tepat $C - 2$ kunci zombie tetap berada di cache hingga akhir dan tidak pernah diakses maupun dieviksi setelah prefix.
- **Kasus Skala Ekstrem:**

| `(k, C)` | `Sieve(k)` Miss Steady-State | Siklus Mulai 0 Miss | `Sieve(C)` Miss Steady-State | Zombie Tertahan Permanen (`pinned / expected`) | Status Python & Go |
| :-: | :-: | :-: | :-: | :-: | :-: |
| `(3, 100)` | `0` (0,00%) | Siklus 1 | `2.970 / 2.970` (100,00%) | `98 / 98` | **PASS (Identik)** |
| `(3, 500)` | `0` (0,00%) | Siklus 1 | `2.970 / 2.970` (100,00%) | `498 / 498` | **PASS (Identik)** |
| `(3, 1000)` | `0` (0,00%) | Siklus 1 | `2.970 / 2.970` (100,00%) | `998 / 998` | **PASS (Identik)** |
| `(10, 1000)` | `0` (0,00%) | Siklus 1 | `2.970 / 2.970` (100,00%) | `998 / 998` | **PASS (Identik)** |
| `(500, 1000)` | `0` (0,00%) | Siklus 1 | `2.970 / 2.970` (100,00%) | `998 / 998` | **PASS (Identik)** |

**Mekanisme singkat:** Setelah prefix, cache `Sieve(C)` berstatus `[B(F), A(F), z_{C-2}(T), ..., z_1(T)]` dengan `hand = None`. Pada miss pertama (`D`), `hand` menyapu seluruh zombie dari `tail` (mengubah bit `visited` mereka menjadi `False`), lalu berhenti di `A(F)`, mengeviksi `A`, dan menggeser `hand = B`. Sejak saat itu, `hand` selalu tertahan di posisi kedua dari `head` (berputar hanya pada dua slot aktif di depan daftar), sehingga $C - 2$ kunci zombie di belakangnya tidak pernah dilewati `hand` lagi selamanya.

---

## 5. Investigasi Eksak Batas $k = 2$ (Go Exact State-Graph Solver)

Solver eksak di [`verifiers/cmd/k2solver/main.go`](file:///d:/cache-anomalies/verifiers/cmd/k2solver/main.go) membangun graf transisi gabungan lengkap `(Sieve(k), Sieve(C))` yang dapat dicapai dari keadaan kosong, mengecek keberadaan siklus dengan `miss(k) = 0` dan `miss(C) > 0` melalui Tarjan SCC, serta menghitung *maximum mean cycle ratio* $\max \frac{\sum \text{miss}(C)}{\sum \text{miss}(k)}$ beserta saksi terpendeknya (yang kemudian diverifikasi ulang oleh implementasi DLL di Go dan Python).

| Kasus `(k, C, m)` | State / Edge Tercapai | Ada Siklus `miss(2)=0` & `miss(C)>0`? | Rasio Maksimum `miss(C)/miss(2)` | Saksi Terpendek (`prefix` ; `cycle`) | Miss per Siklus `(missK, missC)` |
| :-: | :-: | :-: | :-: | :-: | :-: |
| `(2, 3, 3)` | `523` / `1.569` | **Tidak (`false`)** | **`0 / 1` (`0.0000`)** | `prefix=[0, 1, 1, 2, 0, 2]`, `cycle=[1, 0, 2]` | `(3, 0)` |
| `(2, 3, 4)` | `11.361` / `45.444` | **Tidak (`false`)** | **`3 / 2` (`1.5000`)** | `prefix=[0, 0, 1, 1, 2, 3, 2, 2, 3]`, `cycle=[0, 1, 0, 0, 1, 3, 2, 2, 3, 3, 1, 0, 0, 1, 1, 2, 3, 2, 2, 3]` (panjang 20) | `(8, 12)` |
| `(2, 3, 5)` | `29.791` / `148.955` | **Tidak (`false`)** | **`3 / 2` (`1.5000`)** | Sama dengan `m=4` | `(8, 12)` |
| `(2, 4, 3)` | `523` / `1.569` | **Tidak (`false`)** | **`0 / 1` (`0.0000`)** | `prefix=[0, 1, 1, 2, 0, 2]`, `cycle=[1, 0, 2]` | `(3, 0)` |
| `(2, 4, 4)` | `11.865` / `47.460` | **Tidak (`false`)** | **`0 / 1` (`0.0000`)** | `prefix=[0, 1, 1, 2, 0, 2]`, `cycle=[1, 0, 2]` | `(3, 0)` |
| `(2, 4, 5)` | `267.091` / `1.335.455` | **Tidak (`false`)** | **`4 / 3` (`1.3333`)** | `prefix=[0, 1, 2, 2, 3, 1, 3, 4]`, `cycle` panjang 48 (lihat [`k2_solver_go.json`](file:///d:/cache-anomalies/results/k2_solver_go.json)) | `(18, 24)` |

### Temuan Kunci pada Batas `k = 2`
1. **Ketidakmungkinan `0% miss(2)` dengan `>0% miss(C)` (terbukti secara eksak dan analitik):**
   - Agar `Sieve(2)` memperoleh 0% miss pada siklus steady-state, siklus tersebut hanya boleh mengakses **paling banyak 2 kunci unik** (katakanlah $\{A, B\}$).
   - Namun pada `Sieve(C)` dengan $C \ge 3$, hanya dibutuhkan maksimal dua eviksi pada $\{A, B\}$ sebelum kedua kunci berada bersamaan di `head` (atau di dalam cache berukuran $C \ge 3$), sehingga selanjutnya `Sieve(C)` juga tidak akan pernah mengalami miss pada $\{A, B\}$. Dengan demikian, *hand starvation* dengan kontras `0% vs >0%` **mustahil secara matematis untuk $k = 2$** (batas ketat berada di $k \ge 3$).
2. **Anomali Belady non-nol tetap ada di `k = 2`:**
   - Walaupun `miss(2) = 0` mustahil saat `miss(C) > 0`, anomali Belady dengan `miss(C) > miss(2) > 0` **terjadi** begitu jumlah kunci unik $m \ge C + 1$:
     - Untuk `(k=2, C=3)` pada $m=4$ (dan tetap sama pada $m=5$), rasio siklus maksimum eksak adalah **$3/2 = 1{,}5$** (`miss(3) = 12`, `miss(2) = 8` per siklus 20 akses; miss rate 60% vs 40%).
     - Untuk `(k=2, C=4)` pada $m=4$, rasio adalah `0/1` (karena seluruh 4 kunci muat di `C=4`), tetapi pada $m=5$ muncul anomali dengan rasio eksak **$4/3 \approx 1{,}3333$** (`miss(4) = 24`, `miss(2) = 18` per siklus 48 akses; miss rate 50% vs 37,5%).
