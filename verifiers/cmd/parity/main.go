package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"sieve-verifier/policies"
	"sieve-verifier/sieve"
)

type ParityTrace struct {
	Trace    []int          `json:"trace"`
	Expected map[string]any `json:"expected"`
}

func makePolicy(name string, cap int) (func(int) bool, error) {
	if name == "SIEVE" {
		c, err := sieve.NewCache(cap, false)
		if err != nil {
			return nil, err
		}
		return func(k int) bool {
			h, _, _ := c.Access(k)
			return h
		}, nil
	}
	p, err := policies.New(name, cap)
	if err != nil {
		return nil, err
	}
	return p.Access, nil
}

func main() {
	resultsDir := flag.String("results", "../results", "results directory")
	flag.Parse()

	raw, err := os.ReadFile(filepath.Join(*resultsDir, "parity_traces.json"))
	if err != nil {
		panic(err)
	}
	var traces map[string]ParityTrace
	if err := json.Unmarshal(raw, &traces); err != nil {
		panic(err)
	}

	policyNames := []string{"SIEVE", "FIFO", "CLOCK", "CLOCK2", "SecondChance", "S3FIFO"}
	for capStr, pt := range traces {
		var cap int
		fmt.Sscanf(capStr, "%d", &cap)
		for _, pol := range policyNames {
			acc, err := makePolicy(pol, cap)
			if err != nil {
				panic(err)
			}
			hits := 0
			var digest uint64
			for i, k := range pt.Trace {
				if acc(k) {
					hits++
					digest ^= uint64(i+1) * 2654435761
				}
			}
			expHits := int(pt.Expected[pol].(float64))
			expDigest := uint64(pt.Expected[pol+"_digest"].(float64))
			if hits != expHits || digest != expDigest {
				panic(fmt.Sprintf("PARITY MISMATCH %s cap=%d: hits %d vs %d, digest %d vs %d",
					pol, cap, hits, expHits, digest, expDigest))
			}
		}
	}
	fmt.Println("PASS: 100% Python vs Go parity across all 6 policies (capacities 2,3,4,5,9,10,19,20; 1000 accesses each).")

	// Also cross-verify Experiment A in Go
	pairs := [][2]int{{3, 4}, {3, 10}, {3, 20}, {10, 20}}
	rawA, err := os.ReadFile(filepath.Join(*resultsDir, "exp_a_zombie_py.json"))
	if err != nil {
		panic(err)
	}
	var pyRows []map[string]any
	if err := json.Unmarshal(rawA, &pyRows); err != nil {
		panic(err)
	}

	idx := 0
	for _, pair := range pairs {
		k, C := pair[0], pair[1]
		zombies := C - 2
		A, B, D := C-2, C-1, C
		prefix := make([]int, 0, 2*C-2)
		for z := 0; z < zombies; z++ {
			prefix = append(prefix, z)
		}
		prefix = append(prefix, A, B)
		for z := 0; z < zombies; z++ {
			prefix = append(prefix, z)
		}
		cycle := [3]int{D, A, B}

		for _, pol := range policyNames {
			accK, _ := makePolicy(pol, k)
			accC, _ := makePolicy(pol, C)
			for _, x := range prefix {
				accK(x)
				accC(x)
			}
			steadyMK, steadyMC := 0, 0
			for ci := 0; ci < 1000; ci++ {
				for _, x := range cycle {
					hk := accK(x)
					hC := accC(x)
					if ci >= 200 {
						if !hk {
							steadyMK++
						}
						if !hC {
							steadyMC++
						}
					}
				}
			}
			pyMK := int(pyRows[idx]["steady_misses_k"].(float64))
			pyMC := int(pyRows[idx]["steady_misses_C"].(float64))
			if steadyMK != pyMK || steadyMC != pyMC {
				panic(fmt.Sprintf("EXP A MISMATCH %s (%d,%d): go=(%d,%d) py=(%d,%d)",
					pol, k, C, steadyMK, steadyMC, pyMK, pyMC))
			}
			idx++
		}
	}
	fmt.Println("PASS: 100% Python vs Go parity on Experiment A (Zombie Pinning across all 6 policies).")
}
