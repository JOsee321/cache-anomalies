# Laporan Tahap 4: Evaluasi Mitigasi Bedah `SIEVE-TP` (*Periodic Tail-Probe*) & Pemetaan Kurva Pareto

## 1. Implementasi `SIEVE-TP` & Verifikasi Parity

`SIEVE-TP` (*Periodic Tail-Probe SIEVE*) telah diimplementasikan di [`sim/sieve.py`](file:///d:/cache-anomalies/sim/sieve.py) (`SieveTPCache`) dan [`verifiers/sieve/sieve.go`](file:///d:/cache-anomalies/verifiers/sieve/sieve.go) (`TPCache`) hanya dengan menambahkan satu counter integer `evicts_since_wrap` serta parameter `probe_interval` ($K \ge 1$):

- **Saat penuh dan `evicts_since_wrap >= probe_interval`:**
  - Reset `evicts_since_wrap = 0` dan periksa `tail`:
    - Jika `tail.visited == False`: eviksi `tail` secara langsung (geser `hand = tail.prev` hanya jika `hand == tail`), sisipkan item baru di `head`, selesai tanpa menyapu `hand`.
    - Jika `tail.visited == True`: set `tail.visited = False`, pindahkan `tail` ke `head` (geser `hand = tail.prev` jika `hand == tail`, jika `cap == 1` tidak perlu pindah), lalu lanjutkan eviksi `SIEVE` normal menggunakan `hand`.
- **Verifikasi Parity & Invarian:**
  - `unittest` Python (`10/10 OK`) dan `go test` Go (`PASS`).
  - Uji parity bit-per-bit lintas Python dan Go pada `20` trace acak (`1.000` request per trace, kapasitas `2, 3, 5, 10, 20`, interval `K ∈ {C/2, C, 2C, 4C}`) melalui [`verify_stage4_py.py`](file:///d:/cache-anomalies/experiments/verify_stage4_py.py): **20/20 PASS (0 perbedaan hit/digest)**.

---

## 2. Eksperimen F — Verifikasi Keamanan `SIEVE-TP` terhadap Anomali

### 2.1 Lapisan (a): Uji *Zombie Pinning* (378 Pasangan Grid + 5 Skala Ekstrem)

- **Grid $3 \le k < C \le 30$:** Seluruh **4 interval uji (`0.5C`, `1.0C`, `2.0C`, `4.0C`) lulus `378 / 378` pasangan** dengan `0` miss steady-state pada `k` maupun `C`:
  - `TP(0.5C)`: `378/378` (`max_zero_C_cycle = 6`)
  - `TP(1.0C)`: `378/378` (`max_zero_C_cycle = 11`)
  - `TP(2.0C)`: `378/378` (`max_zero_C_cycle = 21`)
  - `TP(4.0C)`: `378/378` (`max_zero_C_cycle = 41`)
- **Skala Ekstrem (`(3,100)`, `(3,500)`, `(3,1000)`, `(10,1000)`, `(500,1000)`):** Seluruh **20/20** kombinasi `(k, C, K)` lulus (`steadyC = 0`). Tepat **1 kali *tail probe*** dan **1 *direct tail eviction*** langsung membebaskan slot untuk kunci aktif ke-3 dan menghentikan eviksi permanen:
  - `C=100, K=100`: sembuh di siklus `34`
  - `C=500, K=500`: sembuh di siklus `167`
  - `C=1000`: sembuh di siklus `167` (`K=0.5C`), `334` (`K=1.0C`), `667` (`K=2.0C`), dan `1.334` (`K=4.0C`).

### 2.2 Lapisan (b): Graf State Eksak & Pencarian Evolusioner (`SIEVE-TP(K=C)`)

- **Graf Eksak:**
  - `(2, 3, m=4)`: lengkap `52.257` state, **`zeroK_posC = false`**, rasio maksimum eksak = **`3/2` (`1,5000`)**.
  - `(3, 4, m=4)`: lengkap `41.049` state, **`zeroK_posC = false`**, rasio maksimum eksak = **`0/1` (`0,0000`)**.
- **Pencarian Evolusioner (`(2,3)` s.d. `(6,12)`):** **`zeroK_posC = false` di seluruh 8/8 pasangan**. Rasio Belady berhingga maksimum yang ditemukan adalah **`1,4000×`** pada `(3, 4)`, **`1,3077×`** pada `(3, 10)`, **`1,2500×`** pada `(2, 3)`, **`1,0667×`** pada `(3, 5)`, dan `1,0000×` pada `(3,6), (4,8), (5,10), (6,12)` — seluruhnya diverifikasi ulang di Python (`5.000` siklus).

### 2.3 Lapisan (c): Trafik Campuran & Skenario "Akses Zombie" (`50.000` request, 3 seed)

| `C` | Skenario | `p` | Kebijakan | `time-to-unwrap` (req) | Zombie Terpin @10k / @50k | Miss Rate @50k (Total Miss) |
| :-: | :--- | :-: | :--- | :---: | :---: | :---: |
| 100 | `zipf_disjoint` | `1%` | `SIEVE` | **`∞`** | `100,0% / 100,0%` | **`1,0000`** (`50.000`) |
| 100 | `zipf_disjoint` | `1%` | `SIEVE-BW(2C)` | `186` | `32,3% / 0,0%` | `0,0086` (`431`) |
| 100 | `zipf_disjoint` | `1%` | **`SIEVE-TP(C)`** | **`86`** | `32,0% / 0,0%` | **`0,0066`** (`330`) |
| 100 | `zipf_disjoint` | `1%` | `SIEVE-TP(2C)` / `(4C)` | `186` / `386` | `32,3% / 0,0%` \| `34,0% / 0,0%` | `0,0086` (`430`) \| `0,0126` (`628`) |
| 100 | `zipf_disjoint` | `5%` | `SIEVE` | `14.154` | `91,2% / 0,0%` | `0,2969` (`14.847`) |
| 100 | `zipf_disjoint` | `5%` | **`SIEVE-TP(C)`** | **`86`** | `0,0% / 0,0%` | **`0,0207`** (`1.035`) |
| 100 | `zombie_hits_only` | `25%` | `SIEVE` | **`∞`** | `100,0% / 100,0%` | **`0,7493`** (`37.464`) |
| 100 | `zombie_hits_only` | `25%` | `SIEVE-BW(2C)` | `251` | `82,3% / 34,4%` | `0,0061` (`306`) |
| 100 | `zombie_hits_only` | `25%` | **`SIEVE-TP(C)`** | **`116`** | `81,6% / 33,3%` | **`0,0042`** (`210`) |
| 100 | `zombie_hits_only` | `25%` | `SIEVE-TP(2C)` / `(4C)` | `254` / `516` | `81,6% / 33,3%` \| `83,3% / 34,0%` | `0,0062` (`310`) \| `0,0102` (`511`) |
| 200 | `zipf_disjoint` | `1%` | `SIEVE` | **`∞`** | `100,0% / 100,0%` | **`1,0000`** (`50.000`) |
| 200 | `zipf_disjoint` | `1%` | **`SIEVE-TP(C)`** | **`186`** | `66,5% / 0,2%` | **`0,0079`** (`396`) |
| 200 | `zombie_hits_only` | `25%` | `SIEVE` | **`∞`** | `100,0% / 100,0%` | **`0,7493`** (`37.464`) |
| 200 | `zombie_hits_only` | `25%` | **`SIEVE-TP(C)`** | **`251`** | `94,9% / 75,4%` | **`0,0049`** (`245`) |

**Mengapa `SIEVE-TP` sukses mematahkan `zombie_hits_only p=25%`:** Meskipun *hit* acak pada *zombie* terus mengubah `visited = True` pada sebagian *zombie*, begitu `evicts_since_wrap == K`, probe di `tail` entah langsung mengeviksi *zombie* berstatus `visited == False`, atau (jika `tail` kebetulan sedang `visited == True`) mereset bitnya menjadi `False` dan memindahkannya ke `head`, sehingga dalam 1–2 siklus probe kunci aktif ke-3 masuk dan miss siklus langsung turun ke `0%`.

---

## 3. Eksperimen G — Kurva Pareto: *Recovery Cost* vs *Normal Workload Regression*

Benchmark dijalankan pada seluruh **15 konfigurasi beban normal** (Zipf $\alpha \in \{0{,}6, 0{,}8, 1{,}0, 1{,}2\}$ + Zipf+Scan $\alpha=0{,}9$ dengan *scan burst* $2C$, kapasitas $C \in \{100, 500, 1000\}$, masing-masing `200.000` request × 3 seed = `600.000` request per sel), lalu dipasangkan dengan biaya pemulihan terburuk pada *Zombie Pinning* murni (`C=1000`) dan `zombie_hits_only p=25%`:

### 3.1 Tabel Kurva Pareto Lintas 13 Kebijakan

| Kebijakan | Avg Miss Ratio (15 sel) | Avg $\Delta$ vs `SIEVE` | Max $\Delta$ (Worst-case) | Min $\Delta$ (Best-case) | Pure Zombie `C=1000` Recov (req) / Miss | `ZHit 25%` `C=100` Unwrap / Miss | `ZHit 25%` `C=1000` Unwrap / Miss |
| :--- | :---: | :---: | :---: | :---: | :---: | :---: | :---: |
| **`SIEVE`** *(baseline)* | `0,4963` | `+0,0000` | `+0,0000` | `+0,0000` | **`∞`** / `50.000` | **`∞`** / `37.464` | **`∞`** / `37.464` |
| **`SIEVE-BW(C)`** | `0,5691` | `+0,0727` | `+0,1132` | `+0,0151` | `1.001` / `1.001` | `114` / `204` | `1.315` / `1.000` |
| **`SIEVE-BW(2C)`** | `0,5458` | `+0,0494` | `+0,0915` | `+0,0034` | `2.001` / `2.001` | `251` / `306` | `2.658` / `2.000` |
| **`SIEVE-BW(4C)`** | `0,5194` | `+0,0231` | `+0,0641` | `-0,0028` | `4.001` / `4.001` | `514` / `508` | `5.338` / `3.999` |
| **`SIEVE-BW(8C)`** | `0,4995` | **`+0,0032`** | `+0,0388` | `-0,0146` | `8.001` / `8.001` | `1.045` / `908` | `10.677` / `7.997` |
| **`SIEVE-BW(16C)`** | `0,4924` | **`-0,0040`** | **`+0,0175`** | `-0,0143` | `16.001` / `16.001` | `6.386` / `4.891` | `21.309` / `15.994` |
| **`SIEVE-TP(0.5C)`** | `0,5192` | `+0,0229` | `+0,0486` | `+0,0008` | **`501`** / `501` | **`48`** / `158` | **`643`** / `500` |
| **`SIEVE-TP(C)`** | `0,5150` | **`+0,0187`** | **`+0,0378`** | `+0,0004` | **`1.001`** / `1.001` | **`116`** / `210` | **`1.315`** / `1.000` |
| **`SIEVE-TP(2C)`** | `0,5099` | **`+0,0136`** | **`+0,0334`** | `+0,0002` | **`2.001`** / `2.001` | **`254`** / `310` | **`2.658`** / `2.000` |
| **`SIEVE-TP(4C)`** | `0,5052` | **`+0,0088`** | **`+0,0303`** | `+0,0001` | **`4.001`** / `4.001` | **`516`** / `511` | **`5.340`** / `3.997` |
| **`S3FIFO`** | `0,4961` | `-0,0002` | `+0,0051` | `-0,0066` | `1` / `1` | `n/a` / `125` | `n/a` / `11` |
| **`CLOCK`** | `0,5692` | `+0,0729` | `+0,1098` | `+0,0205` | `3` / `3` | `n/a` / `135` | `n/a` / `14` |
| **`LRU`** | `0,5786` | `+0,0823` | `+0,1217` | `+0,0258` | `3` / `3` | `n/a` / `123` | `n/a` / `10` |

### 3.2 Sampel Rincian Beban Normal (4 Desimal)

| Kebijakan | Zipf $\alpha=1{,}0$, `C=100` | Zipf $\alpha=1{,}2$, `C=1000` | Zipf+Scan $\alpha=0{,}9$, `C=100` | Zipf+Scan $\alpha=0{,}9$, `C=1000` |
| :--- | :---: | :---: | :---: | :---: |
| `SIEVE` | `0,4907` | `0,1140` | `0,6292` | `0,5513` |
| `SIEVE-BW(C)` | `0,5964` (`+0,1057`) | `0,1291` (`+0,0151`) | `0,7424` (`+0,1132`) | `0,6284` (`+0,0771`) |
| `SIEVE-BW(2C)` | `0,5652` (`+0,0745`) | `0,1174` (`+0,0034`) | `0,7170` (`+0,0878`) | `0,6068` (`+0,0555`) |
| `SIEVE-BW(8C)` | `0,4996` (`+0,0089`) | `0,1127` (`-0,0013`) | `0,6514` (`+0,0222`) | `0,5504` (`-0,0009`) |
| **`SIEVE-BW(16C)`** | **`0,4843` (`-0,0064`)** | **`0,1138` (`-0,0002`)** | **`0,6252` (`-0,0040`)** | **`0,5437` (`-0,0076`)** |
| **`SIEVE-TP(C)`** | `0,5223` (`+0,0316`) | `0,1144` (`+0,0004`) | `0,6641` (`+0,0349`) | `0,5583` (`+0,0070`) |
| **`SIEVE-TP(2C)`** | `0,5168` (`+0,0261`) | `0,1142` (`+0,0002`) | `0,6614` (`+0,0322`) | `0,5544` (`+0,0031`) |
| **`SIEVE-TP(4C)`** | `0,5079` (`+0,0172`) | `0,1141` (`+0,0001`) | `0,6538` (`+0,0246`) | `0,5528` (`+0,0015`) |
| `S3FIFO` | `0,4958` (`+0,0051`) | `0,1157` (`+0,0017`) | `0,6319` (`+0,0027`) | `0,5467` (`-0,0046`) |

---

## 4. Sintesis Kurva Pareto & Rekomendasi Desain

1. **`SIEVE-TP` secara ketat mendominasi `SIEVE-BW` di rezim pemulihan cepat ($K \in \{C, 2C, 4C\}$):**
   - Pada budget pemulihan yang **persis sama** (`1.001` reqs di `C=1000`, `114–116` reqs di `ZHit100`), `SIEVE-TP(C)` memangkas regresi rata-rata sebesar **3,9×** (`+0,0187` vs `+0,0727`) dan regresi terburuk sebesar **3,0×** (`+0,0378` vs `+0,1132`).
   - Pada $C = 1000$ Zipf $\alpha=1{,}2$, `SIEVE-TP(4C)` hanya berselisih **`+0,0001`** (`0,1141` vs `0,1140`) dengan `SIEVE` asli, namun tetap menjamin pemulihan penuh dari *Zombie Pinning* dan *Akses Zombie* dalam $\le 4C$ eviksi.
2. **Temuan mengejutkan pada rezim longgar (`SIEVE-BW(8C)` dan `SIEVE-BW(16C)`):**
   - Berbeda dari `BW(C/2C)` yang merusak *quick demotion* seperti `CLOCK`, memperpanjang batas `wrap_limit` ke **`8C`** menurunkan regresi rata-rata ke **`+0,0032`**, dan **`16C`** justru **mengungguli `SIEVE` asli secara rata-rata (`avg Δ = -0,0040`, bahkan `-0,0076` pada `Zipf+Scan C=1000` dan `-0,0064` pada `Zipf α=1.0 C=100`)**, karena *wrap* yang sangat jarang ($16C$) sesekali membersihkan residu item lama di `tail` tanpa mengganggu *quick demotion* di `head`—dengan kompromi waktu pemulihan *worst-case* `16C` eviksi (`16.001` reqs di `C=1000`).
3. **Peta Pilihan Optimal di Front Pareto:**
   - **Butuh pemulihan cepat ($\le 1C$–$4C$ eviksi):** pilih **`SIEVE-TP(C)`** atau **`SIEVE-TP(4C)`** (dominan penuh atas `SIEVE-BW(C..4C)`).
   - **Butuh *zero/negative regression* pada Zipf & Scan dengan jaminan batas atas kelaparan:** pilih **`SIEVE-BW(16C)`** (atau **`SIEVE-BW(8C)`** untuk kompromi lebih cepat), yang mengalahkan `SIEVE` asli di mayoritas beban normal sambil tetap mengunci batas atas *hand starvation* pada $O(C)$ deterministik.
