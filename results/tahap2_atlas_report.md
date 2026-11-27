# Laporan Tahap 2: Atlas Komparatif Anomali Belady pada Algoritma Cache Modern & Uji Orisinalitas

## 1. Cek Orisinalitas & Tinjauan Literatur (2024–2026)

Pencarian web terarah dilakukan dengan kueri:
- `"SIEVE" "Belady" anomaly worst case OR unbounded OR starvation`
- `"S3-FIFO" "Belady" anomaly proof OR worst case`
- `libCacheSim Sieve.c sieve_evict S3FIFO.c`
- `"SR-Gadgets" cache Belady SIEVE OR S3-FIFO`
- `"zombie pinning" OR "hand starvation" "SIEVE" cache`
- `"unbounded" "Belady" "SIEVE" OR "CLOCK" OR "S3-FIFO" arxiv`

### Temuan Literatur Relevan
1. **Fornai & Iványi (2010), *"FIFO anomaly is unbounded"* (`arXiv:1003.1336`):** Membuktikan bahwa algoritma klasik **FIFO** memiliki rasio anomali Belady tak terbatas ketika kedua kapasitas masih mengalami miss ($\text{miss}(C)/\text{miss}(k) \to \infty$ saat $k, C$ membesar), mematahkan dugaan awal Belady et al. (1969) bahwa rasio FIFO terbatas di sekitar faktor 2.
2. **Yang et al. (SOSP '23, S3-FIFO) & Zhang et al. (NSDI '24, SIEVE):** Memperkenalkan S3-FIFO dan SIEVE beserta implementasi di repositori `1a1a11a/libCacheSim` (`S3FIFO.c` dan `Sieve.c`).
3. **Harvard University (arXiv 2026), *"SR-Gadgets: Make Scan-Resistant Caching Practical"*:** Menunjukkan bahwa berbagai algoritma modern (*multi-queue* maupun berbasis pemindaian seperti **S3-FIFO**, **SIEVE**, ARC, 2Q, TinyLFU) mengalami *miss-ratio cliffs* dan **anomali Belady** pada beban kerja *repeated scans*, lalu mengusulkan modul regulasi antrean (*SR-Gadgets*) untuk menghilangkan anomali tersebut.
4. **Celah Orisinalitas (*Zombie Pinning / Hand Starvation* pada SIEVE):** Pencarian tidak menemukan publikasi maupun isu GitHub yang mendokumentasikan mekanisme eksplisit ***hand starvation / zombie pinning*** pada SIEVE (di mana pointer `hand` terperangkap berosilasi pada 2 slot aktif di dekat `head` sementara $C-2$ kunci *zombie* yang tidak pernah di-hit tetap terkunci permanen dengan bit `visited = False`), ataupun konstruksi deterministik umum untuk seluruh $3 \le k < C$ yang menghasilkan kontras **`0% miss(k)` vs `100% miss(C)`**.

---

## 2. Implementasi Keluarga Algoritma Cache Modern & Verifikasi Parity

Seluruh 5 algoritma pembanding telah diimplementasikan secara independen di Python ([`sim/policies.py`](file:///d:/cache-anomalies/sim/policies.py)) dan Go ([`verifiers/policies/policies.go`](file:///d:/cache-anomalies/verifiers/policies/policies.go)):
- `FIFO`, `CLOCK` (1-bit), `CLOCK2` (2-bit, `freq` 0..2), `SecondChance` (FIFO-Reinsertion), dan `S3FIFO` ($S = \max(1, \lfloor 0.1 \cdot C \rfloor)$, $M = C - S$, $G = M$, `freq` maks 3).
- **Unit test:** `unittest` Python (`8/8 OK`) dan `go test` (`PASS`).
- **Uji Parity Lintas Bahasa:** Diverifikasi oleh [`exp_a_zombie_and_parity.py`](file:///d:/cache-anomalies/experiments/exp_a_zombie_and_parity.py) dan [`cmd/parity/main.go`](file:///d:/cache-anomalies/verifiers/cmd/parity/main.go) pada 1.000 akses acak di 8 kapasitas (`2, 3, 4, 5, 9, 10, 19, 20`) untuk seluruh 6 kebijakan (`SIEVE` + 5 pembanding) — **100% parity (0 perbedaan hit/digest)**.

---

## 3. Eksperimen A — Uji Silang Jejak *Zombie Pinning* SIEVE

Konstruksi *Zombie Pinning* SIEVE (`prefix = [z_1..z_{C-2}, A, B, z_1..z_{C-2}]`, `cycle = [D, A, B]`) dijalankan selama 1.000 siklus (jendela steady-state: 800 siklus terakhir) pada 4 pasangan $(k, C)$:

| Kebijakan | `(3, 4)` `rate_k / rate_C` | `(3, 10)` `rate_k / rate_C` | `(3, 20)` `rate_k / rate_C` | `(10, 20)` `rate_k / rate_C` | Terjebak (*Starvation*)? |
| :--- | :---: | :---: | :---: | :---: | :---: |
| **`SIEVE`** | `0,00% / 100,00%` | `0,00% / 100,00%` | `0,00% / 100,00%` | `0,00% / 100,00%` | **YA (Seluruhnya)** |
| **`FIFO`** | `0,00% / 0,00%` | `0,00% / 0,00%` | `0,00% / 0,00%` | `0,00% / 0,00%` | Tidak (0 miss sejak siklus 1) |
| **`CLOCK`** | `0,00% / 0,00%` | `0,00% / 0,00%` | `0,00% / 0,00%` | `0,00% / 0,00%` | Tidak (0 miss sejak siklus 1) |
| **`CLOCK2`** | `0,00% / 0,00%` | `0,00% / 0,00%` | `0,00% / 0,00%` | `0,00% / 0,00%` | Tidak (0 miss sejak siklus 1) |
| **`SecondChance`** | `0,00% / 0,00%` | `0,00% / 0,00%` | `0,00% / 0,00%` | `0,00% / 0,00%` | Tidak (0 miss sejak siklus 1) |
| **`S3FIFO`** | `0,00% / 0,00%` | `0,00% / 0,00%` | `0,00% / 0,00%` | `0,00% / 0,00%` | Tidak (0 miss `C` siklus 1, `k` siklus 2) |

**Kesimpulan Eksperimen A:** Seluruh 5 algoritma pembanding **kebal** terhadap pola *Zombie Pinning* SIEVE. Alasannya: `FIFO`, `CLOCK`, `CLOCK2`, dan `SecondChance` selalu memajukan eviksi melewati kunci yang tidak pernah diakses lagi, sedangkan di `S3FIFO` kunci aktif `D` yang keluar dari `Small` masuk ke `Ghost` dan pada akses berikutnya langsung dipromosikan ke `Main`, mendepak kunci *zombie* keluar dari `Main`.

---

## 4. Eksperimen B — Perburuan Saksi Anomali Tak Terbatas & Rasio Maksimum ([`atlas_search/main.go`](file:///d:/cache-anomalies/verifiers/cmd/atlas_search/main.go))

### 4.1 Jawaban Eksplisit tentang Anomali Tak Terbatas (`0% miss(k)` vs `>0% miss(C)`)
- **Hasil Empiris & Eksak:** Untuk seluruh 5 algoritma pembanding (`FIFO`, `CLOCK`, `CLOCK2`, `SecondChance`, `S3FIFO`), **TIDAK ditemukan** satu pun saksi dengan `miss_steady(k) == 0` dan `miss_steady(C) > 0` — baik pada graf state eksak maupun pencarian evolusioner di seluruh pasangan $(k, C)$ (termasuk batas `Small` S3-FIFO `C = 4..25`).
- **Mengapa 0% vs >0% mustahil pada ke-5 algoritma pembanding (Penjelasan Struktural):**
  Agar `cache(k)` mendapat `0%` miss pada siklus steady-state $W$, $W$ hanya boleh mengakses paling banyak $k$ kunci unik ($|D(W)| \le k < C$). Pada `cache(C)`:
  - **`FIFO`**: setiap kunci non-$D(W)$ bergerak maju ke ujung eviksi dan habis terbuang dalam $\le C$ miss.
  - **`CLOCK` & `CLOCK2`**: `hand` bergerak melingkar penuh `hand = (hand + 1) % C` pada setiap eviksi, sehingga kunci non-$D(W)$ pasti tersapu habis dalam $\le 2C$ / $\le 3C$ miss.
  - **`SecondChance`**: setiap item `visited = True` dipindahkan ke `head` dengan `visited = False`, sehingga kunci non-$D(W)$ pasti dieviksi setelah maksimal satu putaran.
  - **`S3FIFO`**: kapasitas `Small + Ghost` berjumlah $S + M = C > k \ge |D(W)|$. Oleh karena itu, kunci aktif yang keluar dari `Small` (dengan `freq = 0`) tidak pernah bisa tergeser keluar dari `Ghost` sebelum diakses kembali, sehingga setiap kunci di $D(W)$ pasti masuk ke `Small ∪ Main` dan menyingkirkan kunci sampah di `Main`.
  - Hanya **`SIEVE`** yang memisahkan titik penyisipan (`head`) dari posisi eviksi lokal (`hand = o.prev`) tanpa jaminan rotasi global, sehingga `hand` dapat terperangkap di sub-region `head` dan melaparkan sisa $C-2$ slot.

### 4.2 Anomali Belady Umum (`miss(C) > miss(k) > 0`) — Hasil Pencarian Eksak & Evolusioner

Seluruh saksi yang ditemukan Go (`9` saksi eksak + `25` saksi evolusioner) telah diverifikasi ulang di Python selama **5.000 putaran siklus** melalui [`verify_atlas_witnesses.py`](file:///d:/cache-anomalies/experiments/verify_atlas_witnesses.py) (`PASS` 100%).

#### A. Hasil Graf State Eksak (BFS + Tarjan SCC + Rasio Siklus Maksimum)

| Kebijakan | `(k, C, m)` | Jumlah State (Lengkap?) | `zeroK_posC` | Rasio Maksimum Eksak `miss(C)/miss(k)` | Miss per Siklus `(mK, mC)` |
| :--- | :---: | :---: | :---: | :---: | :---: |
| **`FIFO`** | `(2, 3, 4)` | `185` (`Ya`) | `false` | **`1 / 1` (`1,0000`)** (tidak ada anomali) | `(4, 4)` |
| **`FIFO`** | `(3, 4, 4)` | `137` (`Ya`) | `false` | **`0 / 1` (`0,0000`)** | `(4, 0)` |
| **`FIFO`** | `(3, 4, 5)` | `6.566` (`Ya`) | `false` | **`2 / 1` (`2,0000`)** | `(5, 10)` |
| **`CLOCK`** | `(2, 3, 4)` | `14.313` (`Ya`) | `false` | **`3 / 2` (`1,5000`)** | `(24, 36)` |
| **`CLOCK`** | `(3, 4, 4)` | `18.585` (`Ya`) | `false` | **`0 / 1` (`0,0000`)** | `(2, 0)` |
| **`CLOCK2`** | `(2, 3, 4)` | `88.249` (`Ya`) | `false` | **`2 / 1` (`2,0000`)** | `(12, 24)` |
| **`SecondChance`** | `(2, 3, 4)` | `2.433` (`Ya`) | `false` | **`3 / 2` (`1,5000`)** | `(8, 12)` |
| **`SecondChance`** | `(3, 4, 4)` | `8.217` (`Ya`) | `false` | **`0 / 1` (`0,0000`)** | `(4, 0)` |
| **`S3FIFO`** | `(2, 3, 4)` | `582.113` (`Ya`) | `false` | **`15 / 8` (`1,8750`)** | `(32, 60)` |

> [!TIP]
> Berbeda dengan `FIFO` yang tidak memiliki anomali pada `(2, 3, m=4)` (rasio maksimum `1/1`, baru muncul pada `(3, 4, m=5)` dengan rasio `2/1`), seluruh kebijakan berbasis `visited`/frekuensi (**`CLOCK`**, **`SecondChance`**, **`S3FIFO`**, **`CLOCK2`**) **sudah menunjukkan anomali Belady sejak `(k=2, C=3, m=4)`** dengan rasio `1,5×` hingga `2,0×`.

#### B. Hasil Pencarian Evolusioner & Uji Batas `Small` S3-FIFO (`C = 4..25`)

| Kebijakan | Pasangan `(k, C)` Terbaik | Rasio Tertinggi Ditemukan | Miss Rate Steady-State `k` vs `C` | Saksi Terverifikasi (`prefix` ; `cycle`) |
| :--- | :---: | :---: | :---: | :--- |
| **`FIFO`** | `(3, 4)` (dari Eksak) | **`2,0000`** (`2/1`) | `50,00%` vs `100,00%` | `prefix=[0,1,2,3,0,4,1,3]`, `cycle=[0,1,2,3,4,0,1,2,3,4]` |
| **`CLOCK`** | `(3, 4)` (Evo) / `(2,3)` (Eksak) | **`1,3333`** / **`1,5000`** | `50,00%` vs `66,67%` | `(3,4)`: `prefix=[1,4,1,6,2,4,3,1]`, `cycle=[5,0,6,2,0,6,2,5,3,1,1,3]` |
| **`CLOCK2`** | `(3, 4)` (Evo) & `(2,3)` (Eksak) | **`2,0000`** | `20,00%` vs `40,00%` | `(3,4)`: `prefix=[6,5,0,6,3,4,2,4,1,4,3,0]`, `cycle` panjang 30 (di [`atlas_search_go.json`](file:///d:/cache-anomalies/results/atlas_search_go.json)) |
| **`SecondChance`** | `(2, 3)` (Evo & Eksak) | **`1,5000`** | `28,57%` vs `42,86%` | `prefix=[0,0,1,2,3,0,0,3]`, `cycle` panjang 20 (`8` vs `12` miss) |
| **`S3FIFO`** | `(2, 3)` (Eksak) | **`1,8750`** (`15/8`) | — | `prefix=[0,0,1,2,3,2,2,2,2,3]`, `cycle` panjang 112 (`32` vs `60` miss) |
| **`S3FIFO`** | `(3, 4)` & `(4, 5)` | **`1,6000`** | `55,56%` vs `88,89%` (`3,4`) | `(3,4)`: `prefix=[6,3,0,0,1,6]`, `cycle=[4,1,3,0,5,6,5,2,1]` |
| **`S3FIFO`** | `(9, 10)` (`S=1->1, M=8->9`) | **`1,2245`** | `16,80%` vs `20,57%` | `prefix` 25 akses, `cycle` 25 akses (`16.800` vs `20.572` miss / 4.000 siklus) |
| **`S3FIFO`** | `(10, 11)` (`S=1->1, M=9->10`) | **`1,2245`** | `13,55%` vs `16,59%` | `prefix` 32 akses, `cycle` 31 akses |
| **`S3FIFO`** | **`(19, 20)` (`S=1->2, M=18->18`)** | **`1,6000`** | `6,13%` vs `9,80%` | `prefix` 39 akses, `cycle` 51 akses (`12.500` vs `20.000` miss / 4.000 siklus) |
| **`S3FIFO`** | `(18, 20)` & `(19, 21)` | **`1,2839`** & **`1,2245`** | `11,08%` vs `14,22%` (`18,20`) | Lihat [`atlas_search_go.json`](file:///d:/cache-anomalies/results/atlas_search_go.json) |

**Temuan khusus S3-FIFO pada batas ukuran `Small`:**
Pada transisi `k = 19` ($S = 1, M = 18$) ke `C = 20` ($S = 2, M = 18$), kapasitas `Main` **tidak bertambah sama sekali** ($M = 18$), melainkan seluruh tambahan 1 slot masuk ke `Small`. Perubahan dimensi `Small` tanpa penambahan `Main` ini memicu lonjakan anomali Belady hingga rasio **`1,6000×`** (`6,13%` miss pada `S3FIFO(19)` vs `9,80%` miss pada `S3FIFO(20)`). Selain itu, rasio non-integer seperti `1,2245` (`20.572 / 16.800`) dan `1,2839` muncul karena kombinasi siklus panjang / periode transisi `freq` 2-bit di `Main`.

---

## 5. Klarifikasi Catatan `k2solver` Tahap 1

Pada [`tahap1_sieve_report.md`](file:///d:/cache-anomalies/results/tahap1_sieve_report.md), `k2solver` melaporkan `max_ratio = 0/1 (0.0000)` untuk `(k=2, C=3, m=3)`, `(k=2, C=4, m=3)`, dan `(k=2, C=4, m=4)`. Penjelasan eksaknya adalah:
- Nilai `0/1` tersebut **benar-benar bermakna rasio maksimum $\max \frac{\text{miss}(C)}{\text{miss}(2)} = 0$** (bukan sekadar "tidak ada rasio $> 1$").
- Penyebabnya: ketika ukuran semesta kunci $m \le C$, setelah fase pemanasan (*warm-up*) seluruh $m$ kunci berada permanen di dalam `Sieve(C)`, sehingga `Sieve(C)` mengalami **`0` miss pada setiap siklus steady-state mana pun**, sementara `Sieve(2)` masih miss apabila siklus menyentuh $\ge 3$ kunci (`cycle=[1, 0, 2]` menghasilkan `missK = 3, missC = 0`, rasio $0/3 = 0/1$). Anomali dengan rasio $> 0$ pada `Sieve(2)` baru mungkin terjadi ketika $m \ge C + 1$ (terbukti pada `(2,3,m=4)` dengan rasio `3/2` dan `(2,4,m=5)` dengan rasio `4/3`).

---

## 6. Atlas Komparatif Lintas 6 Algoritma Cache

| Algoritma | Mekanisme Inti | Kebal Pola *Zombie Pinning* SIEVE? | Anomali Tak Terbatas `0% vs >0%` | Anomali Umum `miss(C) > miss(k) > 0` | $(k, C)$ Terkecil Ditemukan | Rasio Maksimum Teramati |
| :--- | :--- | :---: | :---: | :---: | :---: | :---: |
| **`SIEVE`** | 1 queue FIFO + `visited` + `hand` lokal (`o.prev`) | **Tidak** (terjebak) | **YA** (untuk semua $3 \le k < C$, hingga **100% vs 0%**) | **YA** (termasuk `k=2`: `3/2` di `(2,3)`, `4/3` di `(2,4)`) | `(2, 3)` (umum), `(3, 4)` (`0% vs 100%`) | **`∞` (`0%` vs `100%`)** |
| **`FIFO`** | 1 queue FIFO murni | **Ya** | **Tidak** (mustahil secara struktural) | **YA** (rasio $>1$ mulai `k=3, C=4`; tak terbatas saat `miss(k)>0` menurut Fornai & Iványi 2010) | `(3, 4)` (`(2,3,m=4)` maks `1/1`) | **`2,0000`** pada `(3,4,m=5)` |
| **`CLOCK`** | Buffer melingkar 1-bit + rotasi `hand` penuh | **Ya** | **Tidak** (mustahil secara struktural) | **YA** | `(2, 3)` | **`1,5000`** pada `(2,3,m=4)` |
| **`CLOCK2`** | Buffer melingkar 2-bit (`freq` 0..2) + rotasi penuh | **Ya** | **Tidak** (mustahil secara struktural) | **YA** | `(2, 3)` | **`2,0000`** pada `(2,3,m=4)` & `(3,4)` |
| **`SecondChance`** | FIFO-Reinsertion (`tail` -> `head` saat `visited=True`) | **Ya** | **Tidak** (mustahil secara struktural) | **YA** | `(2, 3)` | **`1,5000`** pada `(2,3,m=4)` |
| **`S3FIFO`** | 3 FIFO statis (`Small` 10%, `Main` 90%, `Ghost`) | **Ya** | **Tidak** (dijamin oleh $S + M = C > k$) | **YA** (diperkuat di batas lonjakan `Small`, mis. `19->20`) | `(2, 3)` | **`1,8750`** pada `(2,3,m=4)`, **`1,6000`** pada `(3,4)`, `(4,5)`, `(19,20)` |
