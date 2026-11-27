# Laporan Tahap 3: Dampak Trafik Campuran, Batas Periodik S3-FIFO, dan Mitigasi `SIEVE-BW`

## 1. Implementasi & Verifikasi Silang Baru
- **`LRU`** ditambahkan di [`sim/policies.py`](file:///d:/cache-anomalies/sim/policies.py) dan [`verifiers/policies/policies.go`](file:///d:/cache-anomalies/verifiers/policies/policies.go).
- **`SIEVE-BW`** (*Bounded-Wrap SIEVE*) ditambahkan di [`sim/sieve.py`](file:///d:/cache-anomalies/sim/sieve.py) dan [`verifiers/sieve/sieve.go`](file:///d:/cache-anomalies/verifiers/sieve/sieve.go) dengan satu integer counter `evicts_since_wrap` (tanpa overhead memori per-node, $O(1)$ per akses).
- **Unit test:** Python `unittest` (`9/9 OK`) dan Go `go test` (`PASS`).
- **Verifikasi silang Tahap 3 ([`verify_stage3_py.py`](file:///d:/cache-anomalies/experiments/verify_stage3_py.py)):**
  - `3` trace penuh `50.000` request Eksperimen C (lintas `SIEVE`, `SIEVE-BW`, `CLOCK`, `S3FIFO`, `LRU`): **100% identik** antara Python dan Go.
  - `14` saksi S3-FIFO Eksperimen D (`2.000–5.000` siklus): **100% terverifikasi** di Python.
  - `10` uji ekstrem *Zombie Pinning* `SIEVE-BW` dan `8` saksi evolusioner `SIEVE-BW`: **100% terverifikasi** di Python.

---

## 2. Eksperimen C — Dampak *Zombie Pinning* di Trafik Campuran ([`exp_c_mixed/main.go`](file:///d:/cache-anomalies/verifiers/cmd/exp_c_mixed/main.go))

Setelah `SIEVE(C)` memasuki kondisi terpin ($C - 2$ *zombie* di belakang `hand` + siklus `[D, A, B]`), setiap langkah dengan peluang $p$ berasal dari trafik latar belakang dan peluang $1 - p$ dari siklus 3 kunci selama `50.000` request (rata-rata 3 seed):

### 2.1 Ringkasan Hasil (`C = 100` dan `C = 200`)

| `C` | Mode Latar Belakang | `p` | `time-to-unwrap` (req) | Zombie Terpin @10k / @50k | Miss `SIEVE` @50k | Miss `SIEVE-BW(C)` | Miss `CLOCK` | Miss `S3-FIFO` | Miss `LRU` |
| :-: | :--- | :-: | :-: | :-: | :-: | :-: | :-: | :-: | :-: |
| 100 | *(baseline)* | `0%` | **`∞`** | `100,0%` / `100,0%` | **`1,0000`** | `0,0017` | `0,0000` | `0,0000` | `0,0000` |
| 100 | `zipf_overlap` (0..499) | `1%` | `11.602` *(2/3 seed)* | `91,8%` / `69,0%` | **`0,4865`** | `0,0062` | `0,0044` | `0,0040` | `0,0045` |
| 100 | `zipf_overlap` (0..499) | `5%` | `5.210` | `59,9%` / `58,5%` | **`0,1157`** | `0,0211` | `0,0201` | `0,0177` | `0,0206` |
| 100 | `zipf_overlap` (0..499) | `10%` | `2.642` | `56,8%` / `54,4%` | **`0,0799`** | `0,0402` | `0,0399` | `0,0350` | `0,0413` |
| 100 | `zipf_overlap` (0..499) | `25%` | `988` | `58,2%` / `37,4%` | `0,0962` | `0,0978` | `0,1000` | `0,0873` | `0,1029` |
| 100 | `zipf_disjoint` (1000..1499) | `1%` | **`∞`** | `100,0%` / `100,0%` | **`1,0000`** | `0,0067` | `0,0050` | `0,0056` | `0,0051` |
| 100 | `zipf_disjoint` (1000..1499) | `5%` | `14.154` | `91,2%` / `0,0%` | **`0,2969`** | `0,0217` | `0,0207` | `0,0194` | `0,0212` |
| 100 | `zipf_disjoint` (1000..1499) | `10%` | `4.405` | `4,1%` / `0,0%` | **`0,1206`** | `0,0409` | `0,0409` | `0,0372` | `0,0422` |
| 100 | `zipf_disjoint` (1000..1499) | `25%` | `529` | `0,0%` / `0,0%` | `0,0956` | `0,1003` | `0,1019` | `0,0903` | `0,1049` |
| 100 | `zombie_hits_only` (`visited=T`) | `1%`–`25%` | **`∞` (semua `p`)** | **`100,0%` / `100,0%`** | **`0,9896 → 0,7493`** | `0,0018 → 0,0041` | `0,0001 → 0,0027` | `0,0001 → 0,0025` | `0,0001 → 0,0025` |
| 100 | `fresh_misses_only` | `1%` | **`∞`** | `100,0%` / `100,0%` | **`1,0000`** | `0,0103` | `0,0085` | `0,0093` | `0,0086` |
| 100 | `fresh_misses_only` | `5%` | `27.584` | `100,0%` / `0,0%` | **`0,5699`** | `0,0421` | `0,0406` | `0,0414` | `0,0405` |
| 200 | `zipf_overlap` | `1%` / `5%` | `6.300 (2/3)` / `1.959` | `95,6%→63,3%` / `50,5%→42,8%` | `0,4153` / `0,0484` | `0,0065` / `0,0145` | `0,0028` / `0,0124` | `0,0027` / `0,0115` | `0,0028` / `0,0128` |
| 200 | `zipf_disjoint` | `1%` / `5%` | **`∞`** / `14.154` | `100%` / `95,6%→0%` | **`1,0000`** / `0,2932` | `0,0079` / `0,0172` | `0,0042` / `0,0139` | `0,0054` / `0,0151` | `0,0042` / `0,0142` |
| 200 | `zombie_hits_only` | `1%`–`25%` | **`∞` (semua `p`)** | **`100,0%` / `100,0%`** | **`0,9896 → 0,7493`** | `0,0038 → 0,0049` | `0,0000 → 0,0012` | `0,0000 → 0,0012` | `0,0000 → 0,0011` |

### 2.2 Penjelasan Skenario "Akses Zombie" & Ketahanan Noise
1. **Hit pada Zombie (`zombie_hits_only`) GAGAL TOTAL membuka kuncian `hand`:** Meskipun `25%` trafik mengakses kunci-kunci *zombie* (mengubah `visited = True`), akses tersebut seluruhnya merupakan *cache hit* sehingga tidak memicu eviksi. Sementara itu, siklus `[D, A, B]` terus mengunci `hand` berosilasi di dua posisi terdepan (`index 0` dan `1`), sehingga `hand` tidak pernah mundur ke zona *zombie* (`time-to-unwrap = ∞`, `100%` *zombie* bertahan selamanya).
2. **Noise `1%` miss baru tetap gagal membebaskan SIEVE dalam 50.000 request:** Agar `hand` di `index 1` terlepas dan melakukan *wrap* ke `tail`, **kedua slot aktif terdepan (`index 0` dan `index 1`) harus sama-sama berstatus `visited = True` secara bersamaan sebelum eviksi berikutnya terjadi**. Pada `p = 1%` (`zipf_disjoint` & `fresh_misses_only`), peristiwa kebetulan tersebut sangat jarang sehingga `SIEVE(100)` dan `SIEVE(200)` terkunci di **`100%` miss rate selama seluruh 50.000 request** (`SIEVE-BW`, `CLOCK`, `S3FIFO`, `LRU` hanya `<1,1%` miss rate).

---

## 3. Eksperimen D — Uji Batas Periodik `Small` pada `S3-FIFO` ([`exp_d_s3fifo/main.go`](file:///d:/cache-anomalies/verifiers/cmd/exp_d_s3fifo/main.go))

Ketika $C$ melompati kelipatan $10$ ($k = 10m - 1 \to C = 10m$ untuk $m \ge 2$), alokasi antrean `Small` bertambah 1 ($m - 1 \to m$) sedangkan kapasitas `Main` **stagnan** di $M = 9m$:

| Jenis | Pasangan `(k, C)` | `Small` (`k -> C`) | `Main` (`k -> C`) | Rasio Tertinggi (5.000 siklus) | Miss Rate `k` vs `C` | Panjang Siklus |
| :--- | :---: | :---: | :---: | :---: | :---: | :---: |
| Batas ($m=1$) | `(9, 10)` | `1 -> 1` | `8 -> 9` | **`1,3334×`** | `16,93%` vs `22,58%` | `31` |
| **Batas ($m=2$)** | **`(19, 20)`** | **`1 -> 2`** | **`18 -> 18`** | **`1,6000×`** | `5,48%` vs `8,77%` | `57` |
| Tetangga | `(18, 19)` | `1 -> 1` | `17 -> 18` | `1,2245×` | `7,24%` vs `8,87%` | `58` |
| Tetangga | `(20, 21)` | `2 -> 2` | `18 -> 19` | `1,4482×` | `8,19%` vs `11,86%` | `59` |
| **Batas ($m=3$)** | **`(29, 30)`** | **`2 -> 3`** | **`27 -> 27`** | **`2,0738×`** | `4,57%` vs `9,48%` | `75` |
| Tetangga | `(28, 29)` | `2 -> 2` | `26 -> 27` | `1,5555×` | `4,57%` vs `7,11%` | `75` |
| Tetangga | `(30, 31)` | `3 -> 3` | `27 -> 28` | `1,8181×` | `4,94%` vs `8,98%` | `81` |
| **Batas ($m=4$)** | **`(39, 40)`** | **`3 -> 4`** | **`36 -> 36`** | **`2,2727×`** | `4,04%` vs `9,18%` | `99` |
| **Batas ($m=5$)** | **`(49, 50)`** | **`4 -> 5`** | **`45 -> 45`** | **`1,6531×`** | `5,69%` vs `9,41%` | `120` |

**Konstruksi Analitis (`exp_d_s3fifo_go.json`):** Keluarga analitis `A3_contention` (memanaskan $9m$ kunci di `Main` lalu memutar sapuan $C + 1$ kunci dengan $h$ pengulangan) langsung menghasilkan rasio **`1,2583×`** pada `(29, 30)` (`14,44%` vs `18,18%`), **`1,0423×`** pada `(39, 40)`, dan **`1,0187×`** pada `(49, 50)`; sementara pencarian evolusioner menunjukkan bahwa tepat di batas kelipatan 10 (`m=2,3,4`), **rasio Belady selalu melonjak melampaui tetangga non-batas di kiri dan kanannya** (`1,60×` vs `1,22×/1,45×`; `2,07×` vs `1,56×/1,82×`; mencapai puncak `2,2727×` di `(39, 40)`).

---

## 4. Eksperimen E — Evaluasi Mitigasi `SIEVE-BW` ([`exp_e_sievebw/main.go`](file:///d:/cache-anomalies/verifiers/cmd/exp_e_sievebw/main.go))

### 4.1 Lapisan (a): Uji *Zombie Pinning* (378 Pasangan Grid + 5 Skala Ekstrem)
- **Grid $3 \le k < C \le 30$:** **`SIEVE-BW(C)` = 378/378 LULUS** (sembuh paling lambat pada siklus ke-11), **`SIEVE-BW(2C)` = 378/378 LULUS** (paling lambat siklus ke-21).
- **Skala Ekstrem `(3,100)`, `(3,500)`, `(3,1000)`, `(10,1000)`, `(500,1000)`:**
  - `SIEVE-BW(C)`: seluruh **5/5 LULUS** (`steadyC = 0`); seluruh *zombie* tersapu dan mencapai 0% miss pada siklus ke-`34` (`C=100`), `167` (`C=500`), dan `334` (`C=1000`), yakni tepat $\lceil C/3 \rceil$ siklus — **hanya membutuhkan tepat `1` kali *forced wrap***!
  - `SIEVE-BW(2C)`: seluruh **5/5 LULUS** (`steadyC = 0`) pada siklus ke-`67` (`C=100`), `334` (`C=500`), `667` (`C=1000`), juga dengan **`1` kali *forced wrap***.

### 4.2 Lapisan (b): Graf Eksak & Pencarian Evolusioner `SIEVE-BW(cap)`
- **Graf Eksak:**
  - `(2, 3, m=4)`: lengkap `18.945` state, **`zeroK_posC = false`**, rasio maksimum eksak = **`3/2` (`1,5000`)** (identik dengan `SIEVE` asli di batas `k=2`).
  - `(3, 4, m=4)`: lengkap `26.841` state, **`zeroK_posC = false`**, rasio maksimum eksak = **`0/1` (`0,0000`)**.
- **Pencarian Evolusioner (`(2,3)` s.d. `(6,12)`):** **`zeroK_posC = false` di seluruh 8 pasangan**. Rasio Belady berhingga maksimum turun drastis dari `∞` menjadi **`1,5000×`** (`(2,3)` dan `(3,6)`), **`1,4000×`** (`(3,4)`), **`1,2500×`** (`(3,10)`), dan **`1,2308×`** (`(4,8)`) — setara dengan profil `CLOCK`.

### 4.3 Lapisan (c): Benchmark Trafik Normal (200.000 request × 3 seed, Semesta 10.000)

| Beban Kerja | `C` | `SIEVE` | `SIEVE-BW(C)` ($\Delta$) | `SIEVE-BW(2C)` ($\Delta$) | `S3-FIFO` | `CLOCK` | `FIFO` | `LRU` | Forced Wraps `BW(C)` |
| :--- | :-: | :-: | :---: | :---: | :-: | :-: | :-: | :-: | :---: |
| Zipf $\alpha=0{,}6$ | 100 | `0,8834` | `0,9560` (`+0,0726`) | `0,9501` (`+0,0667`) | `0,8795` | `0,9538` | `0,9603` | `0,9568` | `1.910,3` (`1,00%`) |
| Zipf $\alpha=0{,}6$ | 500 | `0,7642` | `0,8443` (`+0,0802`) | `0,8263` (`+0,0622`) | `0,7576` | `0,8410` | `0,8643` | `0,8488` | `336,3` (`0,20%`) |
| Zipf $\alpha=0{,}6$ | 1000 | `0,6769` | `0,7493` (`+0,0724`) | `0,7232` (`+0,0463`) | `0,6735` | `0,7474` | `0,7806` | `0,7574` | `148,3` (`0,10%`) |
| Zipf $\alpha=0{,}8$ | 100 | `0,7250` | `0,8372` (`+0,1123`) | `0,8164` (`+0,0915`) | `0,7263` | `0,8329` | `0,8662` | `0,8434` | `1.673,0` (`1,00%`) |
| Zipf $\alpha=0{,}8$ | 500 | `0,5667` | `0,6588` (`+0,0921`) | `0,6279` (`+0,0613`) | `0,5659` | `0,6569` | `0,7068` | `0,6697` | `262,0` (`0,20%`) |
| Zipf $\alpha=0{,}8$ | 1000 | `0,4806` | `0,5497` (`+0,0691`) | `0,5149` (`+0,0342`) | `0,4791` | `0,5516` | `0,6068` | `0,5645` | `108,3` (`0,10%`) |
| Zipf $\alpha=1{,}0$ | 100 | `0,4907` | `0,5964` (`+0,1057`) | `0,5652` (`+0,0745`) | `0,4958` | `0,5959` | `0,6573` | `0,6097` | `1.191,3` (`1,00%`) |
| Zipf $\alpha=1{,}0$ | 500 | `0,3336` | `0,3990` (`+0,0654`) | `0,3693` (`+0,0357`) | `0,3365` | `0,4020` | `0,4612` | `0,4141` | `158,0` (`0,20%`) |
| Zipf $\alpha=1{,}0$ | 1000 | `0,2686` | `0,3096` (`+0,0410`) | `0,2835` (`+0,0149`) | `0,2694` | `0,3154` | `0,3700` | `0,3257` | `60,0` (`0,10%`) |
| Zipf $\alpha=1{,}2$ | 100 | `0,2655` | `0,3275` (`+0,0621`) | `0,3002` (`+0,0347`) | `0,2699` | `0,3325` | `0,3964` | `0,3438` | `654,0` (`1,00%`) |
| Zipf $\alpha=1{,}2$ | 500 | `0,1521` | `0,1801` (`+0,0280`) | `0,1629` (`+0,0108`) | `0,1531` | `0,1853` | `0,2284` | `0,1924` | `70,3` (`0,20%`) |
| Zipf $\alpha=1{,}2$ | 1000 | `0,1140` | `0,1291` (`+0,0151`) | `0,1174` (`+0,0034`) | `0,1157` | `0,1345` | `0,1690` | `0,1398` | `24,0` (`0,10%`) |
| Zipf+Scan $\alpha=0{,}9$ | 100 | `0,6292` | `0,7424` (`+0,1132`) | `0,7170` (`+0,0878`) | `0,6319` | `0,7390` | `0,7853` | `0,7510` | `1.483,3` (`1,00%`) |
| Zipf+Scan $\alpha=0{,}9$ | 500 | `0,5433` | `0,6279` (`+0,0846`) | `0,6052` (`+0,0619`) | `0,5405` | `0,6247` | `0,6658` | `0,6333` | `249,3` (`0,20%`) |
| Zipf+Scan $\alpha=0{,}9$ | 1000 | `0,5513` | `0,6284` (`+0,0771`) | `0,6068` (`+0,0555`) | `0,5467` | `0,6252` | `0,6520` | `0,6291` | `124,0` (`0,10%`) |

### 4.4 Analisis Trade-Off Struktural (*Zero-Regression Check* Negatif)
> [!IMPORTANT]
> Hipotesis bahwa `SIEVE-BW(C)` atau `SIEVE-BW(2C)` memiliki *zero regression* di beban normal **terbantahkan**: memaksa `hand = None` setiap `C` atau `2*C` eviksi justru **menghancurkan keunggulan utama `SIEVE` pada trafik Zipf dan Zipf+Scan** (`Δ = +0,0151` hingga `+0,1132` untuk `BW(C)` dan `+0,0034` hingga `+0,0915` untuk `BW(2C)`), menurunkan performa `SIEVE` hingga nyaris identik dengan `CLOCK`.

**Mengapa hal ini terjadi?**
1. Pada beban Zipf normal, keunggulan `SIEVE` terhadap `CLOCK`/`LRU` (`~7–11%` lebih rendah miss ratio pada `C=100`) berasal tepat dari kenyataan bahwa **`hand` bertahan lama di dekat `head`** untuk membuang *one-hit wonders* (*quick demotion*) tanpa sering kembali ke `tail`. Akibatnya, pada trafik Zipf murni pun `evicts_since_wrap` selalu mencapai `C` (tepat `1/C` dari total eviksi = `1,00%` pada `C=100`, `0,20%` pada `C=500`, `0,10%` pada `C=1000`).
2. Setiap kali `SIEVE-BW` memaksa `hand = None`, sapuan kembali dari `tail` membersihkan bit `visited` milik seluruh *hot working set* lama, mengubah dinamika `SIEVE` menjadi ekuivalen `CLOCK`.
3. **Implikasi Riset Lanjutan:** Agar *Zombie Pinning* sembuh tanpa regresi Zipf, pemicu *forced wrap* tidak boleh memakai batas seketat $O(C)$ eviksi (`C` atau `2*C`), melainkan memerlukan ambang jauh lebih longgar (mis. $K \cdot C$ eviksi dengan $K \gg 2$, atau deteksi kelaparan/ketiadaan kemajuan *tail* secara adaptif).
