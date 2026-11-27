package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"

	"sieve-verifier/policies"
)

type S3Result struct {
	Kind           string  `json:"kind"` // "boundary" or "neighbor" or "analytical"
	Name           string  `json:"name"`
	K              int     `json:"k"`
	C              int     `json:"C"`
	SmallK         int     `json:"small_k"`
	MainK          int     `json:"main_k"`
	SmallC         int     `json:"small_C"`
	MainC          int     `json:"main_C"`
	BestRatio      float64 `json:"best_ratio"`
	MissRateK      float64 `json:"miss_rate_k"`
	MissRateC      float64 `json:"miss_rate_C"`
	SteadyMissK    int     `json:"steady_miss_k"`
	SteadyMissC    int     `json:"steady_miss_C"`
	SteadyAccesses int     `json:"steady_accesses"`
	Prefix         []int   `json:"prefix"`
	Cycle          []int   `json:"cycle"`
}

func evalTrace(k, C int, prefix, cycle []int, totalCycles, steadyStart int) (int, int, int, float64) {
	pk := policies.NewS3FIFO(k)
	pC := policies.NewS3FIFO(C)
	for _, x := range prefix {
		pk.Access(x)
		pC.Access(x)
	}
	mk, mC, acc := 0, 0, 0
	for ci := 0; ci < totalCycles; ci++ {
		for _, x := range cycle {
			hk := pk.Access(x)
			hC := pC.Access(x)
			if ci >= steadyStart {
				if !hk {
					mk++
				}
				if !hC {
					mC++
				}
				acc++
			}
		}
	}
	ratio := 0.0
	if mk > 0 {
		ratio = float64(mC) / float64(mk)
	} else if mC > 0 {
		ratio = 999999.0
	}
	return mk, mC, acc, ratio
}

// Analytical parametric family exploiting Small jump (S_k = m-1 -> S_C = m) with Main fixed at M = 9m.
// Pattern:
// - Warm Main with M hot keys (0..M-1)
// - Cycle combines: (1) M hot keys (accessed once per cycle, or subset), (2) a pollution burst of m keys
//   where a key re-appears after m-1 steps (hitting Small in C=10m vs Ghost in k=10m-1) or phase-shifts Main.
func testAnalyticalFamilies() []S3Result {
	var out []S3Result
	for m := 1; m <= 5; m++ {
		k := 10*m - 1
		C := 10 * m
		sk := k / 10
		if sk < 1 {
			sk = 1
		}
		mkCap := k - sk
		sC := C / 10
		mCCap := C - sC

		bestRatio := 0.0
		var bestRes S3Result

		// Family 1: Cyclic permutation of N = C + d keys (d = 1..3) with a double-hit probe window of size w
		for d := 1; d <= 3; d++ {
			univ := C + d
			var prefix []int
			for x := 0; x < C; x++ {
				prefix = append(prefix, x)
				prefix = append(prefix, x) // promote to Main
			}
			// Cycle: sweep keys 0..univ-1, repeating key 0 after offset w
			for w := 1; w <= m+2; w++ {
				var cycle []int
				for x := 0; x < univ; x++ {
					cycle = append(cycle, x)
					if x == w {
						cycle = append(cycle, 0)
					}
				}
				smk, smC, sacc, ratio := evalTrace(k, C, prefix, cycle, 2000, 500)
				if ratio > bestRatio {
					bestRatio = ratio
					bestRes = S3Result{
						Kind:           "analytical",
						Name:           fmt.Sprintf("A1_sweep_m%d_d%d_w%d", m, d, w),
						K:              k,
						C:              C,
						SmallK:         sk,
						MainK:          mkCap,
						SmallC:         sC,
						MainC:          mCCap,
						BestRatio:      ratio,
						MissRateK:      float64(smk) / float64(sacc),
						MissRateC:      float64(smC) / float64(sacc),
						SteadyMissK:    smk,
						SteadyMissC:    smC,
						SteadyAccesses: sacc,
						Prefix:         prefix,
						Cycle:          cycle,
					}
				}
			}
		}

		// Family 2: Main-saturation (M_C = 9m) + Small-phase pollution:
		// Warm 9m hot keys H = {0..9m-1} twice each into Main.
		// Cycle: access H_0..H_{9m-1}, plus a rotating window of (m+1) probe keys that hit twice within m steps.
		M := mCCap
		var pref2 []int
		for x := 0; x < M; x++ {
			pref2 = append(pref2, x, x)
		}
		for extra := 1; extra <= m+2; extra++ {
			var cyc2 []int
			for x := 0; x < M; x++ {
				cyc2 = append(cyc2, x)
			}
			for j := 0; j < extra; j++ {
				pKey := M + j
				cyc2 = append(cyc2, pKey, pKey)
			}
			// Add 1 separator key between repeats of probe keys
			var cyc3 []int
			for x := 0; x < M; x++ {
				cyc3 = append(cyc3, x)
			}
			for j := 0; j < extra; j++ {
				cyc3 = append(cyc3, M+j)
			}
			for j := 0; j < extra; j++ {
				cyc3 = append(cyc3, M+j)
			}
			for _, cyc := range [][]int{cyc2, cyc3} {
				smk, smC, sacc, ratio := evalTrace(k, C, pref2, cyc, 2000, 500)
				if ratio > bestRatio {
					bestRatio = ratio
					bestRes = S3Result{
						Kind:           "analytical",
						Name:           fmt.Sprintf("A2_main_sat_m%d_e%d", m, extra),
						K:              k,
						C:              C,
						SmallK:         sk,
						MainK:          mkCap,
						SmallC:         sC,
						MainC:          mCCap,
						BestRatio:      ratio,
						MissRateK:      float64(smk) / float64(sacc),
						MissRateC:      float64(smC) / float64(sacc),
						SteadyMissK:    smk,
						SteadyMissC:    smC,
						SteadyAccesses: sacc,
						Prefix:         pref2,
						Cycle:          cyc,
					}
				}
			}
		}

		// Family 3: Exact (M + 1) Main contention + Small holding m vs m-1 keys
		// Keys: H = {0 .. C} (C+1 keys). Prefix fills Main. Cycle sweeps H with hot subset repeated.
		for hot := 1; hot <= m+3; hot++ {
			var cyc []int
			for x := 0; x < C+1; x++ {
				cyc = append(cyc, x)
			}
			for x := 0; x < hot; x++ {
				cyc = append(cyc, x)
			}
			smk, smC, sacc, ratio := evalTrace(k, C, pref2, cyc, 2000, 500)
			if ratio > bestRatio {
				bestRatio = ratio
				bestRes = S3Result{
					Kind:           "analytical",
					Name:           fmt.Sprintf("A3_contention_m%d_h%d", m, hot),
					K:              k,
					C:              C,
					SmallK:         sk,
					MainK:          mkCap,
					SmallC:         sC,
					MainC:          mCCap,
					BestRatio:      ratio,
					MissRateK:      float64(smk) / float64(sacc),
					MissRateC:      float64(smC) / float64(sacc),
					SteadyMissK:    smk,
					SteadyMissC:    smC,
					SteadyAccesses: sacc,
					Prefix:         pref2,
					Cycle:          cyc,
				}
			}
		}

		if bestRes.K == 0 {
			bestRes = S3Result{
				Kind:      "analytical",
				Name:      fmt.Sprintf("none_m%d", m),
				K:         k,
				C:         C,
				SmallK:    sk,
				MainK:     mkCap,
				SmallC:    sC,
				MainC:     mCCap,
				BestRatio: 1.0,
			}
		}
		out = append(out, bestRes)
	}
	return out
}

func mutateSlice(rng *rand.Rand, src []int, minLen, maxLen, maxKey int) []int {
	dst := append([]int(nil), src...)
	switch rng.Intn(5) {
	case 0:
		if len(dst) > 0 {
			dst[rng.Intn(len(dst))] = rng.Intn(maxKey)
		}
	case 1:
		if len(dst) < maxLen {
			pos := rng.Intn(len(dst) + 1)
			dst = append(dst, 0)
			copy(dst[pos+1:], dst[pos:])
			dst[pos] = rng.Intn(maxKey)
		}
	case 2:
		if len(dst) > minLen {
			pos := rng.Intn(len(dst))
			dst = append(dst[:pos], dst[pos+1:]...)
		}
	case 3:
		if len(dst) >= 2 {
			i, j := rng.Intn(len(dst)), rng.Intn(len(dst))
			dst[i], dst[j] = dst[j], dst[i]
		}
	case 4:
		if len(dst)+2 <= maxLen {
			dst = append(dst, rng.Intn(maxKey), rng.Intn(maxKey))
		} else if len(dst) > 0 {
			dst[rng.Intn(len(dst))] = rng.Intn(maxKey)
		}
	}
	return dst
}

type evoCand struct {
	prefix []int
	cycle  []int
	score  float64
}

func runEvoPair(kind string, k, C int) S3Result {
	sk := k / 10
	if sk < 1 {
		sk = 1
	}
	sC := C / 10
	if sC < 1 {
		sC = 1
	}
	mkCap, mCCap := k-sk, C-sC

	bestGlobalRatio := -1.0
	var bestGlobalPrefix, bestGlobalCycle []int
	bestMK, bestMC, bestAcc := 0, 0, 0

	// Multi-seed evolutionary search tailored to large C (up to 50)
	for seedIdx := 0; seedIdx < 3; seedIdx++ {
		rng := rand.New(rand.NewSource(int64(9000 + C*100 + k*7 + seedIdx)))
		maxKey := C + 4
		prefMaxLen := 2*C + 10
		minCLen := C
		maxCLen := 2*C + 20
		if C < 15 {
			minCLen = 8
			maxCLen = 36
		}

		popSize := 48
		pop := make([]evoCand, popSize)
		for i := 0; i < popSize; i++ {
			plen := C/2 + rng.Intn(prefMaxLen-C/2+1)
			clen := minCLen + rng.Intn(maxCLen-minCLen+1)
			p := make([]int, plen)
			for j := range p {
				p[j] = rng.Intn(maxKey)
			}
			c := make([]int, clen)
			for j := range c {
				c[j] = rng.Intn(maxKey)
			}
			_, _, _, r := evalTrace(k, C, p, c, 35, 15)
			pop[i] = evoCand{prefix: p, cycle: c, score: r}
		}

		for gen := 0; gen < 180; gen++ {
			sort.Slice(pop, func(i, j int) bool { return pop[i].score > pop[j].score })
			next := make([]evoCand, popSize)
			copy(next[:12], pop[:12])
			for i := 12; i < popSize; i++ {
				par := pop[rng.Intn(12)]
				np := mutateSlice(rng, par.prefix, 1, prefMaxLen, maxKey)
				nc := mutateSlice(rng, par.cycle, minCLen, maxCLen, maxKey)
				_, _, _, r := evalTrace(k, C, np, nc, 35, 15)
				next[i] = evoCand{prefix: np, cycle: nc, score: r}
			}
			pop = next
		}
		sort.Slice(pop, func(i, j int) bool { return pop[i].score > pop[j].score })

		// Verify top 3 candidates over 5000 cycles
		for t := 0; t < 3; t++ {
			smk, smC, sacc, ratio := evalTrace(k, C, pop[t].prefix, pop[t].cycle, 5000, 1000)
			if ratio > bestGlobalRatio {
				bestGlobalRatio = ratio
				bestGlobalPrefix = pop[t].prefix
				bestGlobalCycle = pop[t].cycle
				bestMK = smk
				bestMC = smC
				bestAcc = sacc
			}
		}
	}

	return S3Result{
		Kind:           kind,
		Name:           fmt.Sprintf("evo_%s_%d_%d", kind, k, C),
		K:              k,
		C:              C,
		SmallK:         sk,
		MainK:          mkCap,
		SmallC:         sC,
		MainC:          mCCap,
		BestRatio:      bestGlobalRatio,
		MissRateK:      float64(bestMK) / float64(bestAcc),
		MissRateC:      float64(bestMC) / float64(bestAcc),
		SteadyMissK:    bestMK,
		SteadyMissC:    bestMC,
		SteadyAccesses: bestAcc,
		Prefix:         bestGlobalPrefix,
		Cycle:          bestGlobalCycle,
	}
}

func main() {
	outDir := flag.String("out", "../results", "output directory")
	flag.Parse()

	fmt.Println("=== Experiment D1: S3-FIFO Analytical Boundary Families ===")
	analyt := testAnalyticalFamilies()
	for _, r := range analyt {
		fmt.Printf("%-26s (%2d,%2d) S:%d->%d M:%d->%d | ratio=%.4f (k=%.4f, C=%.4f, cyc=%d)\n",
			r.Name, r.K, r.C, r.SmallK, r.SmallC, r.MainK, r.MainC, r.BestRatio, r.MissRateK, r.MissRateC, len(r.Cycle))
	}

	pairs := []struct {
		kind string
		k, C int
	}{
		{"boundary", 9, 10},
		{"boundary", 19, 20},
		{"boundary", 29, 30},
		{"boundary", 39, 40},
		{"boundary", 49, 50},
		{"neighbor", 18, 19},
		{"neighbor", 20, 21},
		{"neighbor", 28, 29},
		{"neighbor", 30, 31},
	}

	fmt.Println("\n=== Experiment D2: S3-FIFO Evolutionary Boundary vs Neighbor Comparison (5,000 cycles) ===")
	var evoRes []S3Result
	for _, p := range pairs {
		r := runEvoPair(p.kind, p.k, p.C)
		evoRes = append(evoRes, r)
		fmt.Printf("%-8s (%2d,%2d) S:%d->%d M:%d->%d | ratio=%.4f (k=%.4f, C=%.4f, cyc=%d)\n",
			r.Kind, r.K, r.C, r.SmallK, r.SmallC, r.MainK, r.MainC, r.BestRatio, r.MissRateK, r.MissRateC, len(r.Cycle))
	}

	payload := map[string]any{
		"analytical": analyt,
		"evo":        evoRes,
	}
	bytes, _ := json.MarshalIndent(payload, "", "  ")
	if err := os.WriteFile(filepath.Join(*outDir, "exp_d_s3fifo_go.json"), bytes, 0o644); err != nil {
		panic(err)
	}
}
