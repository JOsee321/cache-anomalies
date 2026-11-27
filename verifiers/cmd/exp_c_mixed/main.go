package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"

	"sieve-verifier/policies"
	"sieve-verifier/sieve"
)

type zipfSampler struct {
	cdf []float64
}

func newZipfSampler(n int, alpha float64) *zipfSampler {
	cdf := make([]float64, n)
	sum := 0.0
	for i := 0; i < n; i++ {
		w := 1.0 / math.Pow(float64(i+1), alpha)
		sum += w
		cdf[i] = sum
	}
	for i := range cdf {
		cdf[i] /= sum
	}
	return &zipfSampler{cdf: cdf}
}

func (z *zipfSampler) sample(rng *rand.Rand) int {
	u := rng.Float64()
	lo, hi := 0, len(z.cdf)-1
	for lo < hi {
		mid := (lo + hi) / 2
		if z.cdf[mid] < u {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return lo
}

type MixedRow struct {
	Cap               int     `json:"cap"`
	Mode              string  `json:"mode"`
	P                 float64 `json:"p"`
	Seed              int64   `json:"seed"`
	TimeToUnwrap      int     `json:"time_to_unwrap"`
	PinnedPct10k      float64 `json:"pinned_pct_10k"`
	PinnedPct50k      float64 `json:"pinned_pct_50k"`
	MissRateSieve10k  float64 `json:"miss_rate_sieve_10k"`
	MissRateSieve50k  float64 `json:"miss_rate_sieve_50k"`
	MissRateBW50k     float64 `json:"miss_rate_sieve_bw_50k"`
	MissRateClock50k  float64 `json:"miss_rate_clock_50k"`
	MissRateS3FIFO50k float64 `json:"miss_rate_s3fifo_50k"`
	MissRateLRU50k    float64 `json:"miss_rate_lru_50k"`
}

func runOne(C int, mode string, p float64, seed int64, exportTrace bool) (MixedRow, []int, []int) {
	numZombies := C - 2
	A, B, D := C-2, C-1, C

	// Build prefix + 5 warmup cycles to lock SIEVE(C) into pinned state
	prefix := make([]int, 0, 2*C-2+15)
	for z := 0; z < numZombies; z++ {
		prefix = append(prefix, z)
	}
	prefix = append(prefix, A, B)
	for z := 0; z < numZombies; z++ {
		prefix = append(prefix, z)
	}
	cyc := [3]int{D, A, B}
	for r := 0; r < 5; r++ {
		prefix = append(prefix, cyc[0], cyc[1], cyc[2])
	}

	sieveC, _ := sieve.NewCache(C, false)
	bwC, _ := sieve.NewBWCache(C, C)
	clockC, _ := policies.New("CLOCK", C)
	s3C, _ := policies.New("S3FIFO", C)
	lruC, _ := policies.New("LRU", C)

	for _, x := range prefix {
		sieveC.Access(x)
		bwC.Access(x)
		clockC.Access(x)
		s3C.Access(x)
		lruC.Access(x)
	}

	// Track which original zombies (0..numZombies-1) have NEVER been evicted after prefix
	stillPinned := make([]bool, numZombies)
	for i := range stillPinned {
		stillPinned[i] = true
	}
	pinnedCount := numZombies
	// Sanity check all zombies are in sieveC right after prefix
	for z := 0; z < numZombies; z++ {
		if _, ok := sieveC.Items[z]; !ok {
			panic(fmt.Sprintf("zombie %d missing after prefix for C=%d", z, C))
		}
	}

	rng := rand.New(rand.NewSource(seed))
	zipf := newZipfSampler(500, 0.9)

	const totalReqs = 50000
	var trace []int
	if exportTrace {
		trace = make([]int, totalReqs)
	}

	timeToUnwrap := -1
	pinned10k := -1
	mSieve10k := 0
	mSieve, mBW, mClock, mS3, mLRU := 0, 0, 0, 0, 0
	cycPos := 0

	for step := 1; step <= totalReqs; step++ {
		var key int
		if p > 0 && rng.Float64() < p {
			switch mode {
			case "zipf_overlap":
				// 500 keys 0..499 (rank 0..499 mapped via shuffled or direct IDs; shift by C+1? Wait: overlap includes zombies)
				key = zipf.sample(rng)
			case "zipf_disjoint":
				key = 1000 + zipf.sample(rng)
			case "zombie_hits_only":
				key = rng.Intn(numZombies)
			case "fresh_misses_only":
				key = 1000 + rng.Intn(500)
			default:
				panic(mode)
			}
		} else {
			key = cyc[cycPos]
			cycPos = (cycPos + 1) % 3
		}
		if exportTrace {
			trace[step-1] = key
		}

		hS, evS, hasEvS := sieveC.Access(key)
		if !hS {
			mSieve++
		}
		if hasEvS && evS >= 0 && evS < numZombies && stillPinned[evS] {
			stillPinned[evS] = false
			pinnedCount--
			if timeToUnwrap == -1 {
				timeToUnwrap = step
			}
		}

		if h, _, _ := bwC.Access(key); !h {
			mBW++
		}
		if !clockC.Access(key) {
			mClock++
		}
		if !s3C.Access(key) {
			mS3++
		}
		if !lruC.Access(key) {
			mLRU++
		}

		if step == 10000 {
			pinned10k = pinnedCount
			mSieve10k = mSieve
		}
	}

	row := MixedRow{
		Cap:               C,
		Mode:              mode,
		P:                 p,
		Seed:              seed,
		TimeToUnwrap:      timeToUnwrap,
		PinnedPct10k:      100.0 * float64(pinned10k) / float64(numZombies),
		PinnedPct50k:      100.0 * float64(pinnedCount) / float64(numZombies),
		MissRateSieve10k:  float64(mSieve10k) / 10000.0,
		MissRateSieve50k:  float64(mSieve) / float64(totalReqs),
		MissRateBW50k:     float64(mBW) / float64(totalReqs),
		MissRateClock50k:  float64(mClock) / float64(totalReqs),
		MissRateS3FIFO50k: float64(mS3) / float64(totalReqs),
		MissRateLRU50k:    float64(mLRU) / float64(totalReqs),
	}
	return row, prefix, trace
}

func main() {
	outDir := flag.String("out", "../results", "output directory")
	flag.Parse()

	caps := []int{100, 200}
	modes := []string{"zipf_overlap", "zipf_disjoint", "zombie_hits_only", "fresh_misses_only"}
	ps := []float64{0.0, 0.01, 0.05, 0.10, 0.25}
	seeds := []int64{101, 202, 303}

	var rows []MixedRow
	var sampleExports []map[string]any

	fmt.Println("=== Experiment C: Mixed Traffic Impact (50,000 reqs, mean over 3 seeds) ===")
	for _, C := range caps {
		for _, mode := range modes {
			for _, p := range ps {
				if p == 0.0 && mode != "zipf_overlap" {
					continue // p=0 is identical across all modes
				}
				var unwrapSum, unwrapCnt int
				var pin10Sum, pin50Sum, mS10Sum, mS50Sum, mBWSum, mClkSum, mS3Sum, mLRUSum float64

				for si, seed := range seeds {
					export := (C == 100 && si == 0 && ((mode == "zipf_overlap" && p == 0.05) || (mode == "zombie_hits_only" && p == 0.25) || (mode == "zipf_disjoint" && p == 0.05)))
					row, pref, tr := runOne(C, mode, p, seed, export)
					rows = append(rows, row)
					if row.TimeToUnwrap >= 0 {
						unwrapSum += row.TimeToUnwrap
						unwrapCnt++
					}
					pin10Sum += row.PinnedPct10k
					pin50Sum += row.PinnedPct50k
					mS10Sum += row.MissRateSieve10k
					mS50Sum += row.MissRateSieve50k
					mBWSum += row.MissRateBW50k
					mClkSum += row.MissRateClock50k
					mS3Sum += row.MissRateS3FIFO50k
					mLRUSum += row.MissRateLRU50k
					if export {
						sampleExports = append(sampleExports, map[string]any{
							"row":    row,
							"prefix": pref,
							"trace":  tr,
						})
					}
				}
				n := float64(len(seeds))
				unwrapStr := "INF"
				if unwrapCnt == len(seeds) {
					unwrapStr = fmt.Sprintf("%.0f", float64(unwrapSum)/n)
				} else if unwrapCnt > 0 {
					unwrapStr = fmt.Sprintf("%.0f(%d/3)", float64(unwrapSum)/float64(unwrapCnt), unwrapCnt)
				}
				fmt.Printf("C=%3d %-17s p=%4.2f | unwrap=%-7s pin10k=%5.1f%% pin50k=%5.1f%% | SIEVE=%.4f BW=%.4f CLK=%.4f S3=%.4f LRU=%.4f\n",
					C, mode, p, unwrapStr, pin10Sum/n, pin50Sum/n, mS50Sum/n, mBWSum/n, mClkSum/n, mS3Sum/n, mLRUSum/n)
			}
		}
	}

	payload := map[string]any{
		"rows":           rows,
		"sample_exports": sampleExports,
	}
	bytes, _ := json.Marshal(payload)
	if err := os.WriteFile(filepath.Join(*outDir, "exp_c_mixed_go.json"), bytes, 0o644); err != nil {
		panic(err)
	}
}
