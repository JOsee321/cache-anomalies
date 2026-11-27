package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"sieve-verifier/sieve"
)

type Witness struct {
	K      int   `json:"k"`
	Prefix []int `json:"prefix"`
	Cycle  []int `json:"cycle"`
}

var initialWitnesses = []Witness{
	{
		K:      3,
		Prefix: []int{0, 4, 1, 1, 3, 3, 3},
		Cycle:  []int{2, 0, 4},
	},
	{
		K:      4,
		Prefix: []int{4, 3, 1, 0, 3, 1, 5, 1, 1, 5, 0, 3, 3, 4, 2, 4, 2, 5, 3, 2, 0, 1, 5, 2, 0, 0, 0, 2, 5},
		Cycle:  []int{2, 3, 4, 1},
	},
	{
		K:      5,
		Prefix: []int{1, 5, 3, 3, 1, 3, 0, 5, 6, 1, 6, 5},
		Cycle:  []int{3, 2, 4, 1, 0},
	},
	{
		K:      6,
		Prefix: []int{1, 2, 5, 4, 5, 2, 0, 3, 4, 2, 1, 1, 7, 2, 6, 2, 3, 4, 3, 5, 7, 7, 1, 5, 4, 2, 1, 5, 3, 5, 4, 6, 5, 3, 6, 3, 6, 5, 5},
		Cycle:  []int{1, 2, 0, 3, 6, 7},
	},
	{
		K:      7,
		Prefix: []int{3, 1, 1, 4, 5, 6, 5, 5, 6, 8, 1, 3, 6, 5, 8, 7, 5, 1, 7, 5, 0, 3, 4, 6, 2, 3, 4, 8, 0, 2, 4, 0, 1, 4},
		Cycle:  []int{1, 2, 5, 6, 4, 3, 7},
	},
	{
		K:      8,
		Prefix: []int{9, 1, 4, 1, 7, 7, 7, 6, 3, 1, 7, 0, 6, 6, 9, 0},
		Cycle:  []int{7, 4, 3, 8, 0, 2, 5},
	},
}

type WitnessResult struct {
	K                     int     `json:"k"`
	C                     int     `json:"C"`
	Variant               bool    `json:"variant_insert_before_hand"`
	NumCycles             int     `json:"num_cycles"`
	KPrefixMisses         int     `json:"k_prefix_misses"`
	KCycleMisses          int     `json:"k_cycle_misses"`
	KFirstZeroCycle       int     `json:"k_first_zero_cycle"`
	KTailAllZero          bool    `json:"k_tail_all_zero"`
	KFinalKeys            []int   `json:"k_final_keys"`
	Kp1PrefixMisses       int     `json:"kp1_prefix_misses"`
	Kp1CycleMisses        int     `json:"kp1_cycle_misses"`
	Kp1MinCycleMisses     int     `json:"kp1_min_cycle_misses"`
	Kp1MaxCycleMisses     int     `json:"kp1_max_cycle_misses"`
	Kp1SteadyMissRate     float64 `json:"kp1_steady_miss_rate"`
	Kp1FinalKeys          []int   `json:"kp1_final_keys"`
}

func runOneWitness(w Witness, numCycles int, variant bool) WitnessResult {
	ck, _ := sieve.NewCache(w.K, variant)
	cC, _ := sieve.NewCache(w.K+1, variant)

	kPref, cPref := 0, 0
	for _, x := range w.Prefix {
		if h, _, _ := ck.Access(x); !h {
			kPref++
		}
		if h, _, _ := cC.Access(x); !h {
			cPref++
		}
	}

	kCycleMisses, cCycleMisses := 0, 0
	firstZeroK := -1
	missesPerCycleK := make([]int, numCycles)
	cMin, cMax := len(w.Cycle)+1, -1
	steadyMissC, steadyAcc := 0, 0

	for ci := 0; ci < numCycles; ci++ {
		mk, mC := 0, 0
		for _, x := range w.Cycle {
			if h, _, _ := ck.Access(x); !h {
				mk++
			}
			if h, _, _ := cC.Access(x); !h {
				mC++
			}
		}
		missesPerCycleK[ci] = mk
		kCycleMisses += mk
		cCycleMisses += mC
		if mk == 0 && firstZeroK == -1 {
			firstZeroK = ci
		}
		if mC < cMin {
			cMin = mC
		}
		if mC > cMax {
			cMax = mC
		}
		if ci >= 10 {
			steadyMissC += mC
			steadyAcc += len(w.Cycle)
		}
	}

	tailAllZero := firstZeroK >= 0
	if tailAllZero {
		for ci := firstZeroK; ci < numCycles; ci++ {
			if missesPerCycleK[ci] != 0 {
				tailAllZero = false
				break
			}
		}
	}

	return WitnessResult{
		K:                 w.K,
		C:                 w.K + 1,
		Variant:           variant,
		NumCycles:         numCycles,
		KPrefixMisses:     kPref,
		KCycleMisses:      kCycleMisses,
		KFirstZeroCycle:   firstZeroK,
		KTailAllZero:      tailAllZero,
		KFinalKeys:        ck.KeysHeadToTail(),
		Kp1PrefixMisses:   cPref,
		Kp1CycleMisses:    cCycleMisses,
		Kp1MinCycleMisses: cMin,
		Kp1MaxCycleMisses: cMax,
		Kp1SteadyMissRate: float64(steadyMissC) / float64(steadyAcc),
		Kp1FinalKeys:      cC.KeysHeadToTail(),
	}
}

type ZombieResult struct {
	K                         int  `json:"k"`
	C                         int  `json:"C"`
	Ok                        bool `json:"ok"`
	FirstZeroKCycle           int  `json:"first_zero_k_cycle"`
	SteadyMissesK             int  `json:"steady_misses_k"`
	SteadyMissesC             int  `json:"steady_misses_C"`
	SteadyAccesses            int  `json:"steady_accesses"`
	PinnedZombieCount         int  `json:"pinned_zombie_count"`
	ExpectedZombieCount       int  `json:"expected_zombie_count"`
	ZombiesNeverEvictedCycles bool `json:"zombies_never_evicted_in_cycles"`
}

func runZombiePair(k, C, numCycles int, variant bool) ZombieResult {
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
	cycle := [3]int{D, A, B}

	ck, _ := sieve.NewCache(k, variant)
	cC, _ := sieve.NewCache(C, variant)

	for _, x := range prefix {
		ck.Access(x)
		cC.Access(x)
	}

	firstZeroK := -1
	steadyMissK, steadyMissC, steadyAcc := 0, 0, 0
	zombieEvicted := false

	for ci := 0; ci < numCycles; ci++ {
		mk, mC := 0, 0
		for _, x := range cycle {
			if h, _, _ := ck.Access(x); !h {
				mk++
			}
			if h, ev, hasEv := cC.Access(x); !h {
				mC++
				if hasEv && ev < numZombies {
					zombieEvicted = true
				}
			}
		}
		if mk == 0 && firstZeroK == -1 {
			firstZeroK = ci
		}
		if ci >= 10 {
			steadyMissK += mk
			steadyMissC += mC
			steadyAcc += 3
		}
	}

	pinnedCount := 0
	for _, x := range cC.KeysHeadToTail() {
		if x < numZombies {
			pinnedCount++
		}
	}

	ok := steadyMissK == 0 && steadyMissC == steadyAcc && pinnedCount == numZombies && !zombieEvicted
	return ZombieResult{
		K:                         k,
		C:                         C,
		Ok:                        ok,
		FirstZeroKCycle:           firstZeroK,
		SteadyMissesK:             steadyMissK,
		SteadyMissesC:             steadyMissC,
		SteadyAccesses:            steadyAcc,
		PinnedZombieCount:         pinnedCount,
		ExpectedZombieCount:       numZombies,
		ZombiesNeverEvictedCycles: !zombieEvicted,
	}
}

func main() {
	outDir := flag.String("out", "../results", "output directory for JSON results")
	flag.Parse()

	// 1. Witnesses (10,000 cycles)
	wOff := make([]WitnessResult, 0, len(initialWitnesses))
	wVar := make([]WitnessResult, 0, len(initialWitnesses))
	for _, w := range initialWitnesses {
		wOff = append(wOff, runOneWitness(w, 10000, false))
		wVar = append(wVar, runOneWitness(w, 10000, true))
	}
	wPayload := map[string]any{
		"num_cycles":                 10000,
		"official":                   wOff,
		"variant_insert_before_hand": wVar,
	}
	wBytes, _ := json.MarshalIndent(wPayload, "", "  ")
	if err := os.WriteFile(filepath.Join(*outDir, "witnesses_go.json"), wBytes, 0o644); err != nil {
		panic(err)
	}

	fmt.Println("=== Go Witness Replication (10,000 cycles) ===")
	for _, r := range wOff {
		fmt.Printf("k=%d vs C=%d: k_cycle_misses=%d (zero_from=%d, tail_zero=%v), C_cycle_misses=%d (rate=%.4f)\n",
			r.K, r.C, r.KCycleMisses, r.KFirstZeroCycle, r.KTailAllZero, r.Kp1CycleMisses, r.Kp1SteadyMissRate)
	}

	// 2. Zombie Pinning Grid 3 <= k < C <= 30 + Extremes (1,000 cycles)
	gridOff := make([]ZombieResult, 0, 378)
	gridVar := make([]ZombieResult, 0, 378)
	offPass, varPass := 0, 0
	for k := 3; k < 30; k++ {
		for C := k + 1; C <= 30; C++ {
			ro := runZombiePair(k, C, 1000, false)
			gridOff = append(gridOff, ro)
			if ro.Ok {
				offPass++
			}
			rv := runZombiePair(k, C, 1000, true)
			gridVar = append(gridVar, rv)
			if rv.Ok {
				varPass++
			}
		}
	}

	extremes := [][2]int{{3, 100}, {3, 500}, {3, 1000}, {10, 1000}, {500, 1000}}
	extOff := make([]ZombieResult, 0, len(extremes))
	extVar := make([]ZombieResult, 0, len(extremes))
	for _, p := range extremes {
		extOff = append(extOff, runZombiePair(p[0], p[1], 1000, false))
		extVar = append(extVar, runZombiePair(p[0], p[1], 1000, true))
	}

	zPayload := map[string]any{
		"num_cycles":       1000,
		"total_grid_pairs": len(gridOff),
		"official_passed":  offPass,
		"variant_passed":   varPass,
		"extreme_official": extOff,
		"extreme_variant":  extVar,
	}
	zBytes, _ := json.MarshalIndent(zPayload, "", "  ")
	if err := os.WriteFile(filepath.Join(*outDir, "zombie_pinning_go.json"), zBytes, 0o644); err != nil {
		panic(err)
	}

	fmt.Printf("Go Zombie Pinning Grid (3<=k<C<=30): official=%d/%d, variant=%d/%d\n", offPass, len(gridOff), varPass, len(gridVar))
	for _, r := range extOff {
		fmt.Printf("Go Extreme (%d, %d): ok=%v, k_steady=%d, C_steady=%d/%d, zombies=%d/%d\n",
			r.K, r.C, r.Ok, r.SteadyMissesK, r.SteadyMissesC, r.SteadyAccesses, r.PinnedZombieCount, r.ExpectedZombieCount)
	}
}
