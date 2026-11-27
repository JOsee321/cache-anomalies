package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"

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

func makeVariant(name string, C int) func(int) (bool, int, bool) {
	switch name {
	case "SIEVE":
		c, _ := sieve.NewCache(C, false)
		return c.Access
	case "SIEVE-BW(C)":
		c, _ := sieve.NewBWCache(C, C)
		return c.Access
	case "SIEVE-BW(2C)":
		c, _ := sieve.NewBWCache(C, 2*C)
		return c.Access
	case "SIEVE-BW(4C)":
		c, _ := sieve.NewBWCache(C, 4*C)
		return c.Access
	case "SIEVE-BW(8C)":
		c, _ := sieve.NewBWCache(C, 8*C)
		return c.Access
	case "SIEVE-BW(16C)":
		c, _ := sieve.NewBWCache(C, 16*C)
		return c.Access
	case "SIEVE-TP(C)":
		c, _ := sieve.NewTPCache(C, C)
		return c.Access
	case "SIEVE-TP(2C)":
		c, _ := sieve.NewTPCache(C, 2*C)
		return c.Access
	case "SIEVE-TP(4C)":
		c, _ := sieve.NewTPCache(C, 4*C)
		return c.Access
	case "SIEVE-TP(8C)":
		c, _ := sieve.NewTPCache(C, 8*C)
		return c.Access
	case "SIEVE-TP(16C)":
		c, _ := sieve.NewTPCache(C, 16*C)
		return c.Access
	default:
		panic(name)
	}
}

type ParetoSymRow struct {
	Policy            string  `json:"policy"`
	AvgMissRatio      float64 `json:"avg_miss_ratio"`
	AvgDelta          float64 `json:"avg_delta_vs_sieve"`
	MaxDelta          float64 `json:"max_delta_vs_sieve"`
	MinDelta          float64 `json:"min_delta_vs_sieve"`
	Zipf10_C100       float64 `json:"zipf10_c100"`
	Zipf12_C1000      float64 `json:"zipf12_c1000"`
	ZipfScan_C100     float64 `json:"zipf_scan_c100"`
	ZipfScan_C1000    float64 `json:"zipf_scan_c1000"`
	RecovReqsPure1000 int     `json:"recov_reqs_pure_c1000"`
	Misses50kPure1000 int     `json:"misses_50k_pure_c1000"`
	UnwrapZHit100     float64 `json:"avg_unwrap_zhit25_c100"`
	Misses50kZHit100  float64 `json:"avg_misses_50k_zhit25_c100"`
	UnwrapZHit1000    float64 `json:"avg_unwrap_zhit25_c1000"`
	Misses50kZHit1000 float64 `json:"avg_misses_50k_zhit25_c1000"`
}

type MixedSymRow struct {
	Cap          int     `json:"cap"`
	Mode         string  `json:"mode"`
	P            float64 `json:"p"`
	Variant      string  `json:"variant"`
	TimeToUnwrap float64 `json:"avg_time_to_unwrap"`
	UnwrappedCnt int     `json:"unwrapped_seeds"`
	PinnedPct10k float64 `json:"avg_pinned_pct_10k"`
	PinnedPct50k float64 `json:"avg_pinned_pct_50k"`
	MissRate50k  float64 `json:"avg_miss_rate_50k"`
	TotalMiss50k float64 `json:"avg_total_misses_50k"`
}

func runPureRecovery1000(pol string) (int, int) {
	C := 1000
	numZombies := C - 2
	A, B, D := C-2, C-1, C
	prefix := make([]int, 0, 2*C-2)
	for z := 0; z < numZombies; z++ {
		prefix = append(prefix, z)
	}
	prefix = append(prefix, A, B)
	for z := 0; z < numZombies; z++ {
		prefix = append(prefix, z)
	}
	cyc := [3]int{D, A, B}
	acc := makeVariant(pol, C)
	for _, x := range prefix {
		acc(x)
	}
	totalMisses, lastMissReq := 0, 0
	for step := 1; step <= 50000; step++ {
		if h, _, _ := acc(cyc[(step-1)%3]); !h {
			totalMisses++
			lastMissReq = step
		}
	}
	if lastMissReq > 49900 {
		return -1, totalMisses
	}
	return lastMissReq, totalMisses
}

func runZHitRecovery(pol string, C int) (float64, float64) {
	numZombies := C - 2
	A, B, D := C-2, C-1, C
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
	seeds := []int64{101, 202, 303}
	var unwrapSum, unwrapCnt, missSum int
	for _, seed := range seeds {
		rng := rand.New(rand.NewSource(seed))
		acc := makeVariant(pol, C)
		for _, x := range prefix {
			acc(x)
		}
		cycPos, unwrap, misses := 0, -1, 0
		for step := 1; step <= 50000; step++ {
			var key int
			if rng.Float64() < 0.25 {
				key = rng.Intn(numZombies)
			} else {
				key = cyc[cycPos]
				cycPos = (cycPos + 1) % 3
			}
			h, ev, hasEv := acc(key)
			if !h {
				misses++
			}
			if unwrap == -1 && hasEv && ev >= 0 && ev < numZombies {
				unwrap = step
			}
		}
		if unwrap >= 0 {
			unwrapSum += unwrap
			unwrapCnt++
		}
		missSum += misses
	}
	avgU := -1.0
	if unwrapCnt > 0 {
		avgU = float64(unwrapSum) / float64(unwrapCnt)
	}
	return avgU, float64(missSum) / float64(len(seeds))
}

func main() {
	outDir := flag.String("out", "../results", "output directory")
	flag.Parse()

	polNames := []string{
		"SIEVE",
		"SIEVE-BW(C)", "SIEVE-BW(2C)", "SIEVE-BW(4C)", "SIEVE-BW(8C)", "SIEVE-BW(16C)",
		"SIEVE-TP(C)", "SIEVE-TP(2C)", "SIEVE-TP(4C)", "SIEVE-TP(8C)", "SIEVE-TP(16C)",
	}

	// 1. Complete symmetric normal benchmarks (15 configs x 200k x 3 seeds)
	type wlSpec struct {
		name  string
		alpha float64
		cap   int
	}
	var specs []wlSpec
	for _, a := range []float64{0.6, 0.8, 1.0, 1.2} {
		for _, c := range []int{100, 500, 1000} {
			specs = append(specs, wlSpec{"zipf", a, c})
		}
	}
	for _, c := range []int{100, 500, 1000} {
		specs = append(specs, wlSpec{"zipf_scan", 0.9, c})
	}

	const numReqs = 200000
	seeds := []int64{11, 22, 33}
	deltas := make(map[string][]float64, len(polNames))
	ratios := make(map[string][]float64, len(polNames))
	details := make([]map[string]any, 0, len(specs))

	for _, sp := range specs {
		zipf := newZipfSampler(10000, sp.alpha)
		missTotals := make(map[string]int, len(polNames))
		for _, seed := range seeds {
			rng := rand.New(rand.NewSource(seed))
			trace := make([]int, 0, numReqs)
			scanCur := 1000000
			if sp.name == "zipf" {
				for i := 0; i < numReqs; i++ {
					trace = append(trace, zipf.sample(rng))
				}
			} else {
				for len(trace) < numReqs {
					for j := 0; j < 5000 && len(trace) < numReqs; j++ {
						trace = append(trace, zipf.sample(rng))
					}
					for s := 0; s < 2*sp.cap && len(trace) < numReqs; s++ {
						trace = append(trace, scanCur)
						scanCur++
					}
				}
			}
			for _, pName := range polNames {
				acc := makeVariant(pName, sp.cap)
				m := 0
				for _, x := range trace {
					if h, _, _ := acc(x); !h {
						m++
					}
				}
				missTotals[pName] += m
			}
		}
		denom := float64(numReqs * len(seeds))
		mr := make(map[string]float64, len(polNames))
		for _, pName := range polNames {
			mr[pName] = float64(missTotals[pName]) / denom
		}
		sieveMR := mr["SIEVE"]
		for _, pName := range polNames {
			ratios[pName] = append(ratios[pName], mr[pName])
			deltas[pName] = append(deltas[pName], mr[pName]-sieveMR)
		}
		details = append(details, map[string]any{
			"workload":   sp.name,
			"alpha":      sp.alpha,
			"cap":        sp.cap,
			"miss_ratio": mr,
		})
	}

	fmt.Println("=== Stage 5 Symmetric Pareto Summary ({C, 2C, 4C, 8C, 16C}) ===")
	var pareto []ParetoSymRow
	for _, pName := range polNames {
		ds := deltas[pName]
		rs := ratios[pName]
		maxD, minD, sumD, sumR := -1e9, 1e9, 0.0, 0.0
		for i := range ds {
			if ds[i] > maxD {
				maxD = ds[i]
			}
			if ds[i] < minD {
				minD = ds[i]
			}
			sumD += ds[i]
			sumR += rs[i]
		}
		rec1k, mP1k := runPureRecovery1000(pName)
		u100, mZ100 := runZHitRecovery(pName, 100)
		u1k, mZ1k := runZHitRecovery(pName, 1000)

		row := ParetoSymRow{
			Policy:            pName,
			AvgMissRatio:      sumR / float64(len(rs)),
			AvgDelta:          sumD / float64(len(ds)),
			MaxDelta:          maxD,
			MinDelta:          minD,
			Zipf10_C100:       details[6]["miss_ratio"].(map[string]float64)[pName],
			Zipf12_C1000:      details[11]["miss_ratio"].(map[string]float64)[pName],
			ZipfScan_C100:     details[12]["miss_ratio"].(map[string]float64)[pName],
			ZipfScan_C1000:    details[14]["miss_ratio"].(map[string]float64)[pName],
			RecovReqsPure1000: rec1k,
			Misses50kPure1000: mP1k,
			UnwrapZHit100:     u100,
			Misses50kZHit100:  mZ100,
			UnwrapZHit1000:    u1k,
			Misses50kZHit1000: mZ1k,
		}
		pareto = append(pareto, row)
		fmt.Printf("%-14s | avgMR=%.4f avgD=%+.4f maxD=%+.4f minD=%+.4f | Pure1k=%5d | ZHit100:u=%5.0f m=%5.0f | ZHit1k:u=%5.0f m=%5.0f\n",
			row.Policy, row.AvgMissRatio, row.AvgDelta, row.MaxDelta, row.MinDelta,
			row.RecovReqsPure1000, row.UnwrapZHit100, row.Misses50kZHit100, row.UnwrapZHit1000, row.Misses50kZHit1000)
	}

	// 2. Mixed traffic for BW(8C, 16C) & TP(8C, 16C) on C=100,200 (zipf_disjoint p=1% and zombie_hits_only p=25%)
	fmt.Println("\n=== Stage 5 Mixed Traffic Symmetry Check (8C, 16C) ===")
	mixedVars := []string{"SIEVE-BW(8C)", "SIEVE-BW(16C)", "SIEVE-TP(8C)", "SIEVE-TP(16C)"}
	scenarios := []struct {
		mode string
		p    float64
	}{
		{"zipf_disjoint", 0.01},
		{"zombie_hits_only", 0.25},
	}
	mixSeeds := []int64{101, 202, 303}
	var mixedRows []MixedSymRow
	var exports []map[string]any

	for _, C := range []int{100, 200} {
		numZombies := C - 2
		A, B, D := C-2, C-1, C
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
		for _, sc := range scenarios {
			traces := make([][]int, len(mixSeeds))
			for si, seed := range mixSeeds {
				rng := rand.New(rand.NewSource(seed))
				zipf := newZipfSampler(500, 0.9)
				tr := make([]int, 50000)
				cycPos := 0
				for i := 0; i < 50000; i++ {
					if rng.Float64() < sc.p {
						if sc.mode == "zipf_disjoint" {
							tr[i] = 1000 + zipf.sample(rng)
						} else {
							tr[i] = rng.Intn(numZombies)
						}
					} else {
						tr[i] = cyc[cycPos]
						cycPos = (cycPos + 1) % 3
					}
				}
				traces[si] = tr
			}
			for _, vName := range mixedVars {
				var uSum, uCnt int
				var p10Sum, p50Sum, mSum float64
				for si, tr := range traces {
					acc := makeVariant(vName, C)
					for _, x := range prefix {
						acc(x)
					}
					stillPinned := make([]bool, numZombies)
					for i := range stillPinned {
						stillPinned[i] = true
					}
					pCnt, p10k, unwrap, misses := numZombies, -1, -1, 0
					for step, key := range tr {
						h, ev, hasEv := acc(key)
						if !h {
							misses++
						}
						if hasEv && ev >= 0 && ev < numZombies && stillPinned[ev] {
							stillPinned[ev] = false
							pCnt--
							if unwrap == -1 {
								unwrap = step + 1
							}
						}
						if step+1 == 10000 {
							p10k = pCnt
						}
					}
					if unwrap >= 0 {
						uSum += unwrap
						uCnt++
					}
					p10Sum += 100.0 * float64(p10k) / float64(numZombies)
					p50Sum += 100.0 * float64(pCnt) / float64(numZombies)
					mSum += float64(misses)

					if C == 100 && si == 0 && (vName == "SIEVE-TP(8C)" || vName == "SIEVE-TP(16C)" || vName == "SIEVE-BW(16C)") {
						exports = append(exports, map[string]any{
							"cap":     C,
							"mode":    sc.mode,
							"p":       sc.p,
							"variant": vName,
							"unwrap":  unwrap,
							"misses":  misses,
							"prefix":  prefix,
							"trace":   tr,
						})
					}
				}
				n := float64(len(mixSeeds))
				avgU := -1.0
				if uCnt > 0 {
					avgU = float64(uSum) / float64(uCnt)
				}
				row := MixedSymRow{
					Cap:          C,
					Mode:         sc.mode,
					P:            sc.p,
					Variant:      vName,
					TimeToUnwrap: avgU,
					UnwrappedCnt: uCnt,
					PinnedPct10k: p10Sum / n,
					PinnedPct50k: p50Sum / n,
					MissRate50k:  (mSum / n) / 50000.0,
					TotalMiss50k: mSum / n,
				}
				mixedRows = append(mixedRows, row)
				fmt.Printf("C=%3d %-16s p=%4.2f %-13s | unwrap=%6.0f(%d/3) pin10k=%5.1f%% pin50k=%5.1f%% | miss50k=%.4f (%.0f)\n",
					row.Cap, row.Mode, row.P, row.Variant, row.TimeToUnwrap, row.UnwrappedCnt, row.PinnedPct10k, row.PinnedPct50k, row.MissRate50k, row.TotalMiss50k)
			}
		}
	}

	payload := map[string]any{
		"pareto_symmetric": pareto,
		"bench_details":    details,
		"mixed_8c_16c":     mixedRows,
		"mixed_exports":    exports,
	}
	bytes, _ := json.MarshalIndent(payload, "", "  ")
	if err := os.WriteFile(filepath.Join(*outDir, "tahap5_symmetry.json"), bytes, 0o644); err != nil {
		panic(err)
	}
}
