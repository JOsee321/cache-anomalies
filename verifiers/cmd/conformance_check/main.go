package main

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"os"
	"sort"
)

// Exact mirror of libCacheSim S3FIFO.c / S3FIFOv0.c (commit 4b66d977):
// - smallCap = int(0.10 * C), mainCap = C - smallCap, ghostCap = int(0.90 * C)
// - total-capacity eviction: evict while len(small)+len(main) >= C
// - if len(main) > mainCap || len(small) == 0: evict_main() else: evict_small()
// - move_to_main_threshold = 2 (S3FIFO.c / S3FIFOv0.c default)
// - coldStartToMain = true in S3FIFO.c (!hasEvicted && len(small) >= smallCap -> main)
// - freq unbounded on hit, clamped via MIN(freq, 3) - 1 on main reinsertion.
type s3Node struct {
	key  int
	freq int
}

type LCS_S3FIFO struct {
	cap             int
	smallCap        int
	mainCap         int
	ghostCap        int
	threshold       int
	coldStartToMain bool
	hasEvicted      bool

	small []s3Node // index 0 = newest (head), len-1 = oldest (tail)
	main  []s3Node
	ghost []int // index 0 = oldest, len-1 = newest
}

func newLCSS3FIFO(C int, coldStartToMain bool) *LCS_S3FIFO {
	s := int(float64(C) * 0.10)
	if s <= 0 {
		s = 1
	}
	m := C - s
	g := int(float64(C) * 0.90)
	return &LCS_S3FIFO{
		cap:             C,
		smallCap:        s,
		mainCap:         m,
		ghostCap:        g,
		threshold:       2,
		coldStartToMain: coldStartToMain,
	}
}

func (c *LCS_S3FIFO) addGhost(k int) {
	if c.ghostCap <= 0 {
		return
	}
	// FIFO_insert + eviction if ghost full
	for len(c.ghost) >= c.ghostCap {
		c.ghost = c.ghost[1:]
	}
	c.ghost = append(c.ghost, k)
}

func (c *LCS_S3FIFO) evictSmall() {
	for len(c.small) > 0 {
		last := len(c.small) - 1
		obj := c.small[last]
		c.small = c.small[:last]
		if obj.freq >= c.threshold {
			// main_fifo->insert (note: libCacheSim sets freq=0? Wait: S3FIFO.c:363 calls main_fifo->insert which leaves S3FIFO.freq=0!)
			c.main = append([]s3Node{{key: obj.key, freq: 0}}, c.main...)
		} else {
			c.addGhost(obj.key)
			return
		}
	}
}

func (c *LCS_S3FIFO) evictMain() {
	for len(c.main) > 0 {
		last := len(c.main) - 1
		obj := c.main[last]
		c.main = c.main[:last]
		if obj.freq >= 1 {
			nf := obj.freq
			if nf > 3 {
				nf = 3
			}
			nf -= 1
			c.main = append([]s3Node{{key: obj.key, freq: nf}}, c.main...)
		} else {
			return
		}
	}
}

func (c *LCS_S3FIFO) Access(key int) bool {
	for i := range c.small {
		if c.small[i].key == key {
			c.small[i].freq++
			return true
		}
	}
	hitGhost := false
	for i, gk := range c.ghost {
		if gk == key {
			c.ghost = append(c.ghost[:i], c.ghost[i+1:]...)
			hitGhost = true
			break
		}
	}
	for i := range c.main {
		if c.main[i].key == key {
			c.main[i].freq++
			return true
		}
	}

	// Evict while occupied + 1 > cap
	for len(c.small)+len(c.main) >= c.cap {
		c.hasEvicted = true
		if len(c.main) > c.mainCap || len(c.small) == 0 {
			c.evictMain()
		} else {
			c.evictSmall()
		}
	}

	if hitGhost || (c.coldStartToMain && !c.hasEvicted && len(c.small) >= c.smallCap) {
		c.main = append([]s3Node{{key: key, freq: 0}}, c.main...)
	} else {
		c.small = append([]s3Node{{key: key, freq: 0}}, c.small...)
	}
	return false
}

func evalPair(k, C int, prefix, cycle []int, totalCycles, steadyStart int, coldStart bool) (int, int, int, float64) {
	pk := newLCSS3FIFO(k, coldStart)
	pC := newLCSS3FIFO(C, coldStart)
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
	if mk == 0 {
		if mC > 0 {
			return mk, mC, acc, math.Inf(1)
		}
		return mk, mC, acc, 0.0
	}
	return mk, mC, acc, float64(mC) / float64(mk)
}

func main() {
	// 1. Check Zombie Pinning immunity under exact libCacheSim S3FIFO.c and S3FIFOv0.c
	fmt.Println("=== 1. Zombie Pinning Immunity under Exact libCacheSim S3FIFO.c & S3FIFOv0.c ===")
	for _, cold := range []bool{true, false} {
		label := "S3FIFOv0.c"
		if cold {
			label = "S3FIFO.c"
		}
		for _, p := range [][2]int{{10, 20}, {19, 20}, {10, 100}, {10, 1000}} {
			k, C := p[0], p[1]
			zombies := C - 2
			A, B, D := C-2, C-1, C
			var pref []int
			for z := 0; z < zombies; z++ {
				pref = append(pref, z)
			}
			pref = append(pref, A, B)
			for z := 0; z < zombies; z++ {
				pref = append(pref, z)
			}
			cyc := []int{D, A, B}
			mk, mC, acc, _ := evalPair(k, C, pref, cyc, 500, 100, cold)
			fmt.Printf("%-10s (%2d,%4d): steady_miss_k=%d/%d, steady_miss_C=%d/%d\n", label, k, C, mk, acc, mC, acc)
		}
	}

	// 2. Check Stage 3 boundary witnesses & quick evo search under exact libCacheSim S3FIFO.c
	fmt.Println("\n=== 2. Periodic Multiple-of-10 Boundary Belady Spikes under Exact libCacheSim S3FIFO.c ===")
	pairs := [][2]int{{19, 20}, {29, 30}, {39, 40}, {49, 50}, {20, 21}, {30, 31}}
	for _, p := range pairs {
		k, C := p[0], p[1]
		rng := rand.New(rand.NewSource(int64(4242 + C)))
		maxKey := C + 4
		prefMaxLen := 2*C + 6
		minCLen := C
		maxCLen := 2*C + 16

		type cand struct {
			p, c  []int
			score float64
		}
		pop := make([]cand, 40)
		for i := range pop {
			pr := make([]int, C/2+rng.Intn(prefMaxLen-C/2+1))
			for j := range pr {
				pr[j] = rng.Intn(maxKey)
			}
			cy := make([]int, minCLen+rng.Intn(maxCLen-minCLen+1))
			for j := range cy {
				cy[j] = rng.Intn(maxKey)
			}
			_, _, _, r := evalPair(k, C, pr, cy, 35, 15, true)
			pop[i] = cand{pr, cy, r}
		}
		for g := 0; g < 150; g++ {
			sort.Slice(pop, func(i, j int) bool { return pop[i].score > pop[j].score })
			next := make([]cand, 40)
			copy(next[:10], pop[:10])
			for i := 10; i < 40; i++ {
				par := pop[rng.Intn(10)]
				np := append([]int(nil), par.p...)
				nc := append([]int(nil), par.c...)
				if len(np) > 0 {
					np[rng.Intn(len(np))] = rng.Intn(maxKey)
				}
				if len(nc) > 0 {
					nc[rng.Intn(len(nc))] = rng.Intn(maxKey)
				}
				_, _, _, r := evalPair(k, C, np, nc, 35, 15, true)
				next[i] = cand{np, nc, r}
			}
			pop = next
		}
		sort.Slice(pop, func(i, j int) bool { return pop[i].score > pop[j].score })
		mk, mC, acc, r := evalPair(k, C, pop[0].p, pop[0].c, 3000, 500, true)
		fmt.Printf("Exact S3FIFO.c (%2d,%2d): ratio=%.4f (k=%.4f, C=%.4f, cyc=%d, mk=%d, mC=%d, acc=%d)\n",
			k, C, r, float64(mk)/float64(acc), float64(mC)/float64(acc), len(pop[0].c), mk, mC, acc)
	}
	_ = json.Marshal
	_ = os.Stdout
}
