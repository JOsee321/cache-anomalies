package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"sieve-verifier/policies"
	"sieve-verifier/sieve"
)

// --- Layer (a): Zombie Pinning Verification on SIEVE-BW ---

type ZombieBWRow struct {
	K               int  `json:"k"`
	C               int  `json:"C"`
	WrapMult        int  `json:"wrap_mult"`
	WrapLimit       int  `json:"wrap_limit"`
	FirstZeroKCycle int  `json:"first_zero_k_cycle"`
	FirstZeroCCycle int  `json:"first_zero_C_cycle"`
	SteadyMissesK   int  `json:"steady_misses_k"`
	SteadyMissesC   int  `json:"steady_misses_C"`
	ForcedWrapsC    int  `json:"forced_wraps_C"`
	Ok              bool `json:"ok"`
}

func runZombieBW(k, C, wrapMult, numCycles int) ZombieBWRow {
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

	ck, _ := sieve.NewBWCache(k, k*wrapMult)
	cC, _ := sieve.NewBWCache(C, C*wrapMult)
	for _, x := range prefix {
		ck.Access(x)
		cC.Access(x)
	}

	// Steady window: after 3*C cycles (guaranteed sufficient for wrap_limit <= 2*C), or check last 200 cycles
	steadyStart := numCycles - 200
	firstZeroK, firstZeroC := -1, -1
	steadyMK, steadyMC := 0, 0
	for ci := 0; ci < numCycles; ci++ {
		mk, mC := 0, 0
		for _, x := range cycle {
			if h, _, _ := ck.Access(x); !h {
				mk++
			}
			if h, _, _ := cC.Access(x); !h {
				mC++
			}
		}
		if mk == 0 && firstZeroK == -1 {
			firstZeroK = ci
		}
		if mC == 0 && firstZeroC == -1 {
			firstZeroC = ci
		}
		if ci >= steadyStart {
			steadyMK += mk
			steadyMC += mC
		}
	}

	ok := steadyMK == 0 && steadyMC == 0 && firstZeroC >= 0
	return ZombieBWRow{
		K:               k,
		C:               C,
		WrapMult:        wrapMult,
		WrapLimit:       C * wrapMult,
		FirstZeroKCycle: firstZeroK,
		FirstZeroCCycle: firstZeroC,
		SteadyMissesK:   steadyMK,
		SteadyMissesC:   steadyMC,
		ForcedWrapsC:    cC.ForcedWraps,
		Ok:              ok,
	}
}

// --- Layer (b): Exact State-Graph + Evo Search on SIEVE-BW(wrap_limit = cap) ---

type exactBW struct {
	c *sieve.BWCache
}

func newExactBW(cap int) *exactBW {
	c, _ := sieve.NewBWCache(cap, cap)
	return &exactBW{c: c}
}

func (e *exactBW) Access(key int) bool {
	h, _, _ := e.c.Access(key)
	return h
}

func (e *exactBW) Encode() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d|", e.c.EvictsSinceWrap)
	handIdx := -1
	idx := 0
	for curr := e.c.Head; curr != nil; curr = curr.Next {
		if curr == e.c.Hand {
			handIdx = idx
		}
		if idx > 0 {
			b.WriteByte(',')
		}
		v := 0
		if curr.Visited {
			v = 1
		}
		fmt.Fprintf(&b, "%d:%d", curr.Key, v)
		idx++
	}
	fmt.Fprintf(&b, "|%d", handIdx)
	return b.String()
}

func (e *exactBW) Clone() *exactBW {
	nc, _ := sieve.NewBWCache(e.c.Capacity, e.c.WrapLimit)
	nc.EvictsSinceWrap = e.c.EvictsSinceWrap
	nc.ForcedWraps = e.c.ForcedWraps
	for curr := e.c.Head; curr != nil; curr = curr.Next {
		nn := &sieve.Node{Key: curr.Key, Visited: curr.Visited}
		if nc.Tail == nil {
			nc.Head = nn
			nc.Tail = nn
		} else {
			nn.Prev = nc.Tail
			nc.Tail.Next = nn
			nc.Tail = nn
		}
		nc.Items[nn.Key] = nn
		if curr == e.c.Hand {
			nc.Hand = nn
		}
	}
	return &exactBW{c: nc}
}

type gEdge struct {
	to    int
	key   int
	missK int
	missC int
}

type ExactBWResult struct {
	K                     int     `json:"k"`
	C                     int     `json:"C"`
	UniverseSize          int     `json:"universe_size"`
	Complete              bool    `json:"complete"`
	States                int     `json:"states"`
	ZeroMissKWithPosMissC bool    `json:"zero_miss_k_with_pos_miss_C"`
	MaxRatioNum           int     `json:"max_ratio_num"`
	MaxRatioDen           int     `json:"max_ratio_den"`
	MaxRatio              float64 `json:"max_ratio"`
	Prefix                []int   `json:"prefix"`
	Cycle                 []int   `json:"cycle"`
}

func checkZeroMissK(adj [][]gEdge) bool {
	n := len(adj)
	index := 0
	indices := make([]int, n)
	lowlink := make([]int, n)
	onStack := make([]bool, n)
	sccID := make([]int, n)
	for i := range indices {
		indices[i] = -1
		sccID[i] = -1
	}
	var stack []int
	numSCC := 0
	var tarjan func(int)
	tarjan = func(v int) {
		indices[v] = index
		lowlink[v] = index
		index++
		stack = append(stack, v)
		onStack[v] = true
		for _, e := range adj[v] {
			if e.missK != 0 {
				continue
			}
			w := e.to
			if indices[w] == -1 {
				tarjan(w)
				if lowlink[w] < lowlink[v] {
					lowlink[v] = lowlink[w]
				}
			} else if onStack[w] && indices[w] < lowlink[v] {
				lowlink[v] = indices[w]
			}
		}
		if lowlink[v] == indices[v] {
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[w] = false
				sccID[w] = numSCC
				if w == v {
					break
				}
			}
			numSCC++
		}
	}
	for v := 0; v < n; v++ {
		if indices[v] == -1 {
			tarjan(v)
		}
	}
	for u := 0; u < n; u++ {
		for _, e := range adj[u] {
			if e.missK == 0 && e.missC > 0 && sccID[u] == sccID[e.to] {
				return true
			}
		}
	}
	return false
}

func hasPositiveCycle(adj [][]gEdge, R float64) bool {
	n := len(adj)
	dist := make([]float64, n)
	parent := make([]int, n)
	inQueue := make([]bool, n)
	queue := make([]int, 0, n*2)
	for i := 0; i < n; i++ {
		parent[i] = -1
		queue = append(queue, i)
		inQueue[i] = true
	}
	seen := make([]int, n)
	hasParentCycle := func() bool {
		for i := range seen {
			seen[i] = 0
		}
		for i := 0; i < n; i++ {
			if seen[i] != 0 {
				continue
			}
			v := i
			tag := i + 1
			for v != -1 && seen[v] == 0 {
				seen[v] = tag
				v = parent[v]
			}
			if v != -1 && seen[v] == tag {
				return true
			}
		}
		return false
	}
	const eps = 1e-11
	relax := 0
	for h := 0; h < len(queue); h++ {
		u := queue[h]
		inQueue[u] = false
		du := dist[u]
		for _, e := range adj[u] {
			w := float64(e.missC) - R*float64(e.missK)
			if du+w > dist[e.to]+eps {
				dist[e.to] = du + w
				parent[e.to] = u
				relax++
				if relax%n == 0 && hasParentCycle() {
					return true
				}
				if !inQueue[e.to] {
					queue = append(queue, e.to)
					inQueue[e.to] = true
				}
			}
		}
	}
	return hasParentCycle()
}

func computePotentials(adj [][]gEdge, p, q int64) ([]int64, bool) {
	n := len(adj)
	pi := make([]int64, n)
	parent := make([]int, n)
	inQueue := make([]bool, n)
	queue := make([]int, 0, n*2)
	for i := 0; i < n; i++ {
		parent[i] = -1
		queue = append(queue, i)
		inQueue[i] = true
	}
	seen := make([]int, n)
	hasParentCycle := func() bool {
		for i := range seen {
			seen[i] = 0
		}
		for i := 0; i < n; i++ {
			if seen[i] != 0 {
				continue
			}
			v := i
			tag := i + 1
			for v != -1 && seen[v] == 0 {
				seen[v] = tag
				v = parent[v]
			}
			if v != -1 && seen[v] == tag {
				return true
			}
		}
		return false
	}
	relax := 0
	for h := 0; h < len(queue); h++ {
		u := queue[h]
		inQueue[u] = false
		pu := pi[u]
		for _, e := range adj[u] {
			cost := p*int64(e.missK) - q*int64(e.missC)
			if pu+cost < pi[e.to] {
				pi[e.to] = pu + cost
				parent[e.to] = u
				relax++
				if relax%n == 0 && hasParentCycle() {
					return nil, true
				}
				if !inQueue[e.to] {
					queue = append(queue, e.to)
					inQueue[e.to] = true
				}
			}
		}
	}
	if hasParentCycle() {
		return nil, true
	}
	return pi, false
}

func gcd(a, b int64) int64 {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

func bestRationalApprox(x float64, maxDenom int64) (int64, int64) {
	if x <= 0 {
		return 0, 1
	}
	a := int64(math.Floor(x))
	frac := x - float64(a)
	if frac < 1e-11 {
		return a, 1
	}
	p0, q0 := int64(1), int64(0)
	p1, q1 := a, int64(1)
	rem := frac
	for iter := 0; iter < 40; iter++ {
		if rem < 1e-11 {
			break
		}
		inv := 1.0 / rem
		ai := int64(math.Floor(inv))
		p2 := ai*p1 + p0
		q2 := ai*q1 + q0
		if q2 > maxDenom {
			break
		}
		p0, q0 = p1, q1
		p1, q1 = p2, q2
		rem = inv - float64(ai)
	}
	g := gcd(p1, q1)
	return p1 / g, q1 / g
}

func runExactBW(k, C, m, maxStates int) ExactBWResult {
	type pairState struct {
		sk *exactBW
		sc *exactBW
	}
	initP := pairState{newExactBW(k), newExactBW(C)}
	initKey := initP.sk.Encode() + "#" + initP.sc.Encode()
	stateMap := map[string]int{initKey: 0}
	states := []pairState{initP}
	var adj [][]gEdge
	complete := true

	for idx := 0; idx < len(states); idx++ {
		cur := states[idx]
		edges := make([]gEdge, 0, m)
		for key := 0; key < m; key++ {
			nk := cur.sk.Clone()
			nc := cur.sc.Clone()
			hk := nk.Access(key)
			hc := nc.Access(key)
			mk, mc := 0, 0
			if !hk {
				mk = 1
			}
			if !hc {
				mc = 1
			}
			nKey := nk.Encode() + "#" + nc.Encode()
			toIdx, ok := stateMap[nKey]
			if !ok {
				if len(states) >= maxStates {
					complete = false
					continue
				}
				toIdx = len(states)
				stateMap[nKey] = toIdx
				states = append(states, pairState{nk, nc})
			}
			edges = append(edges, gEdge{to: toIdx, key: key, missK: mk, missC: mc})
		}
		adj = append(adj, edges)
	}
	if !complete {
		return ExactBWResult{K: k, C: C, UniverseSize: m, Complete: false, States: len(states)}
	}
	n := len(adj)
	if checkZeroMissK(adj) {
		return ExactBWResult{K: k, C: C, UniverseSize: m, Complete: true, States: n, ZeroMissKWithPosMissC: true, MaxRatio: 999999}
	}

	lo, hi := 0.0, 1.0
	for hasPositiveCycle(adj, hi) {
		lo = hi
		hi *= 2.0
		if hi > float64(n) {
			break
		}
	}
	for iter := 0; iter < 50; iter++ {
		mid := 0.5 * (lo + hi)
		if hasPositiveCycle(adj, mid) {
			lo = mid
		} else {
			hi = mid
		}
	}
	p, q := bestRationalApprox(0.5*(lo+hi), int64(n))
	pi, pos := computePotentials(adj, p, q)
	if pos {
		p, q = bestRationalApprox(hi, int64(n))
		pi, pos = computePotentials(adj, p, q)
		if pos {
			panic("cannot lock rational")
		}
	}
	_ = pi

	return ExactBWResult{
		K:                     k,
		C:                     C,
		UniverseSize:          m,
		Complete:              true,
		States:                n,
		ZeroMissKWithPosMissC: false,
		MaxRatioNum:           int(p),
		MaxRatioDen:           int(q),
		MaxRatio:              float64(p) / float64(q),
	}
}

type EvoBWResult struct {
	K               int     `json:"k"`
	C               int     `json:"C"`
	FoundZeroMissK  bool    `json:"found_zero_miss_k_pos_C"`
	BestRatio       float64 `json:"best_ratio"`
	BestMissRateK   float64 `json:"best_miss_rate_k"`
	BestMissRateC   float64 `json:"best_miss_rate_C"`
	BestSteadyMissK int     `json:"best_steady_miss_k"`
	BestSteadyMissC int     `json:"best_steady_miss_C"`
	BestSteadyAcc   int     `json:"best_steady_accesses"`
	BestPrefix      []int   `json:"best_prefix,omitempty"`
	BestCycle       []int   `json:"best_cycle,omitempty"`
}

func evalBW(k, C int, prefix, cycle []int, totalCycles, steadyStart int) (int, int, int, float64) {
	ck, _ := sieve.NewBWCache(k, k)
	cC, _ := sieve.NewBWCache(C, C)
	for _, x := range prefix {
		ck.Access(x)
		cC.Access(x)
	}
	mk, mC, acc := 0, 0, 0
	for ci := 0; ci < totalCycles; ci++ {
		for _, x := range cycle {
			hk, _, _ := ck.Access(x)
			hC, _, _ := cC.Access(x)
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

func runEvoBW(k, C int, seed int64) EvoBWResult {
	rng := rand.New(rand.NewSource(seed))
	prefMaxLen := 3*C + 4
	if prefMaxLen < 12 {
		prefMaxLen = 12
	}
	type cand struct {
		p, c  []int
		score float64
		mk    int
		mC    int
	}

	// Phase 1: check zeroK_posC specifically (cycle keys in 0..k-1)
	foundZero := false
	pop := make([]cand, 48)
	for i := range pop {
		p := make([]int, k+rng.Intn(prefMaxLen-k+1))
		for j := range p {
			p[j] = rng.Intn(C + 3)
		}
		c := make([]int, 2+rng.Intn(k*2+4))
		for j := range c {
			c[j] = rng.Intn(k)
		}
		mk, mC, _, _ := evalBW(k, C, p, c, 40, 20)
		pop[i] = cand{p: p, c: c, score: float64(mC)*10 - float64(mk)*50, mk: mk, mC: mC}
	}
	for gen := 0; gen < 120; gen++ {
		sort.Slice(pop, func(i, j int) bool { return pop[i].score > pop[j].score })
		if pop[0].mk == 0 && pop[0].mC > 0 {
			vmk, vmC, _, _ := evalBW(k, C, pop[0].p, pop[0].c, 5000, 1000)
			if vmk == 0 && vmC > 0 {
				foundZero = true
				break
			}
		}
		next := make([]cand, 48)
		copy(next[:12], pop[:12])
		for i := 12; i < 48; i++ {
			par := pop[rng.Intn(12)]
			np := mutateSlice(rng, par.p, 1, prefMaxLen, C+3)
			nc := mutateSlice(rng, par.c, 2, k*3+6, k)
			mk, mC, _, _ := evalBW(k, C, np, nc, 40, 20)
			next[i] = cand{p: np, c: nc, score: float64(mC)*10 - float64(mk)*50, mk: mk, mC: mC}
		}
		pop = next
	}

	// Phase 2: general Belady ratio
	cycMaxLen := C*2 + 10
	if cycMaxLen < 32 {
		cycMaxLen = 32
	}
	pop2 := make([]cand, 48)
	for i := range pop2 {
		p := make([]int, rng.Intn(prefMaxLen+1))
		for j := range p {
			p[j] = rng.Intn(C + 3)
		}
		c := make([]int, 3+rng.Intn(cycMaxLen-2))
		for j := range c {
			c[j] = rng.Intn(C + 3)
		}
		mk, mC, _, r := evalBW(k, C, p, c, 45, 20)
		pop2[i] = cand{p: p, c: c, score: r, mk: mk, mC: mC}
	}
	for gen := 0; gen < 180; gen++ {
		sort.Slice(pop2, func(i, j int) bool { return pop2[i].score > pop2[j].score })
		next := make([]cand, 48)
		copy(next[:12], pop2[:12])
		for i := 12; i < 48; i++ {
			par := pop2[rng.Intn(12)]
			np := mutateSlice(rng, par.p, 0, prefMaxLen, C+3)
			nc := mutateSlice(rng, par.c, 3, cycMaxLen, C+3)
			mk, mC, _, r := evalBW(k, C, np, nc, 45, 20)
			next[i] = cand{p: np, c: nc, score: r, mk: mk, mC: mC}
		}
		pop2 = next
	}
	sort.Slice(pop2, func(i, j int) bool { return pop2[i].score > pop2[j].score })
	vmk, vmC, vacc, vratio := evalBW(k, C, pop2[0].p, pop2[0].c, 5000, 1000)

	return EvoBWResult{
		K:               k,
		C:               C,
		FoundZeroMissK:  foundZero || (vmk == 0 && vmC > 0),
		BestRatio:       vratio,
		BestMissRateK:   float64(vmk) / float64(vacc),
		BestMissRateC:   float64(vmC) / float64(vacc),
		BestSteadyMissK: vmk,
		BestSteadyMissC: vmC,
		BestSteadyAcc:   vacc,
		BestPrefix:      pop2[0].p,
		BestCycle:       pop2[0].c,
	}
}

// --- Layer (c): Normal Workload Benchmark (200,000 reqs x 3 seeds) ---

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

type BenchSummary struct {
	Workload      string             `json:"workload"`
	Alpha         float64            `json:"alpha"`
	Cap           int                `json:"cap"`
	ReqsPerSeed   int                `json:"reqs_per_seed"`
	NumSeeds      int                `json:"num_seeds"`
	MissRatio     map[string]float64 `json:"miss_ratio"`
	DeltaBW1      float64            `json:"delta_bw1"`
	DeltaBW2      float64            `json:"delta_bw2"`
	ForcedWraps1  float64            `json:"avg_forced_wraps_bw1"`
	ForcedWraps2  float64            `json:"avg_forced_wraps_bw2"`
	ForcedPctBW1  float64            `json:"forced_wrap_pct_evicts_bw1"`
	ForcedPctBW2  float64            `json:"forced_wrap_pct_evicts_bw2"`
}

func runBench(workload string, alpha float64, C int) BenchSummary {
	const numReqs = 200000
	seeds := []int64{11, 22, 33}
	zipf := newZipfSampler(10000, alpha)

	polNames := []string{"SIEVE", "SIEVE-BW(C)", "SIEVE-BW(2C)", "S3FIFO", "CLOCK", "FIFO", "LRU"}
	missTotals := make(map[string]int, len(polNames))
	var fw1Total, fw2Total, ev1Total, ev2Total int

	for _, seed := range seeds {
		rng := rand.New(rand.NewSource(seed))
		// Pre-generate trace so all 7 algorithms see exact same requests
		trace := make([]int, 0, numReqs)
		scanCursor := 1000000
		if workload == "zipf" {
			for i := 0; i < numReqs; i++ {
				trace = append(trace, zipf.sample(rng))
			}
		} else {
			// zipf + scan burst: every 5000 Zipf requests, inject a sequential 2*C scan burst (until 200,000 total)
			for len(trace) < numReqs {
				for j := 0; j < 5000 && len(trace) < numReqs; j++ {
					trace = append(trace, zipf.sample(rng))
				}
				for s := 0; s < 2*C && len(trace) < numReqs; s++ {
					trace = append(trace, scanCursor)
					scanCursor++
				}
			}
		}

		sv, _ := sieve.NewCache(C, false)
		bw1, _ := sieve.NewBWCache(C, C)
		bw2, _ := sieve.NewBWCache(C, 2*C)
		s3 := policies.NewS3FIFO(C)
		clk := policies.NewCLOCK(C)
		fifo := policies.NewFIFO(C)
		lru := policies.NewLRU(C)

		for _, x := range trace {
			if h, _, _ := sv.Access(x); !h {
				missTotals["SIEVE"]++
			}
			if h, _, hasEv := bw1.Access(x); !h {
				missTotals["SIEVE-BW(C)"]++
				if hasEv {
					ev1Total++
				}
			}
			if h, _, hasEv := bw2.Access(x); !h {
				missTotals["SIEVE-BW(2C)"]++
				if hasEv {
					ev2Total++
				}
			}
			if !s3.Access(x) {
				missTotals["S3FIFO"]++
			}
			if !clk.Access(x) {
				missTotals["CLOCK"]++
			}
			if !fifo.Access(x) {
				missTotals["FIFO"]++
			}
			if !lru.Access(x) {
				missTotals["LRU"]++
			}
		}
		fw1Total += bw1.ForcedWraps
		fw2Total += bw2.ForcedWraps
	}

	denom := float64(numReqs * len(seeds))
	mr := make(map[string]float64, len(polNames))
	for _, p := range polNames {
		mr[p] = float64(missTotals[p]) / denom
	}
	return BenchSummary{
		Workload:     workload,
		Alpha:        alpha,
		Cap:          C,
		ReqsPerSeed:  numReqs,
		NumSeeds:     len(seeds),
		MissRatio:    mr,
		DeltaBW1:     mr["SIEVE-BW(C)"] - mr["SIEVE"],
		DeltaBW2:     mr["SIEVE-BW(2C)"] - mr["SIEVE"],
		ForcedWraps1: float64(fw1Total) / float64(len(seeds)),
		ForcedWraps2: float64(fw2Total) / float64(len(seeds)),
		ForcedPctBW1: 100.0 * float64(fw1Total) / float64(ev1Total),
		ForcedPctBW2: 100.0 * float64(fw2Total) / float64(ev2Total),
	}
}

func main() {
	outDir := flag.String("out", "../results", "output directory")
	flag.Parse()

	// Layer (a): Zombie Pinning on SIEVE-BW(C) and SIEVE-BW(2C)
	fmt.Println("=== Experiment E1: SIEVE-BW Zombie Pinning Verification ===")
	var grid1, grid2 []ZombieBWRow
	pass1, pass2 := 0, 0
	maxSweep1, maxSweep2 := 0, 0
	for k := 3; k < 30; k++ {
		for C := k + 1; C <= 30; C++ {
			r1 := runZombieBW(k, C, 1, 1000)
			r2 := runZombieBW(k, C, 2, 1000)
			grid1 = append(grid1, r1)
			grid2 = append(grid2, r2)
			if r1.Ok {
				pass1++
			}
			if r2.Ok {
				pass2++
			}
			if r1.FirstZeroCCycle > maxSweep1 {
				maxSweep1 = r1.FirstZeroCCycle
			}
			if r2.FirstZeroCCycle > maxSweep2 {
				maxSweep2 = r2.FirstZeroCCycle
			}
		}
	}
	extremes := [][2]int{{3, 100}, {3, 500}, {3, 1000}, {10, 1000}, {500, 1000}}
	var extRows []ZombieBWRow
	for _, p := range extremes {
		for _, mult := range []int{1, 2} {
			r := runZombieBW(p[0], p[1], mult, 3*p[1]+500)
			extRows = append(extRows, r)
			fmt.Printf("Extreme (%3d,%4d) wrap=%dxC: ok=%v first_zero_C_cycle=%d steadyC=%d forced_wraps=%d\n",
				r.K, r.C, mult, r.Ok, r.FirstZeroCCycle, r.SteadyMissesC, r.ForcedWrapsC)
		}
	}
	fmt.Printf("Grid 3<=k<C<=30: BW(C)=%d/%d (max_zero_C_cycle=%d), BW(2C)=%d/%d (max_zero_C_cycle=%d)\n",
		pass1, len(grid1), maxSweep1, pass2, len(grid2), maxSweep2)

	// Layer (b): Exact & Evo on SIEVE-BW(C)
	fmt.Println("\n=== Experiment E2: SIEVE-BW(C) Exact & Evolutionary Atlas Check ===")
	exactBWCases := [][3]int{{2, 3, 4}, {3, 4, 4}}
	var exactBWRes []ExactBWResult
	for _, c := range exactBWCases {
		r := runExactBW(c[0], c[1], c[2], 250000)
		exactBWRes = append(exactBWRes, r)
		fmt.Printf("Exact SIEVE-BW (%d,%d,m=%d): complete=%v states=%d zeroK_posC=%v max_ratio=%d/%d (%.4f)\n",
			r.K, r.C, r.UniverseSize, r.Complete, r.States, r.ZeroMissKWithPosMissC, r.MaxRatioNum, r.MaxRatioDen, r.MaxRatio)
	}
	evoPairs := [][2]int{{2, 3}, {3, 4}, {3, 5}, {3, 6}, {3, 10}, {4, 8}, {5, 10}, {6, 12}}
	var evoBWRes []EvoBWResult
	for i, p := range evoPairs {
		r := runEvoBW(p[0], p[1], int64(7700+i))
		evoBWRes = append(evoBWRes, r)
		fmt.Printf("Evo   SIEVE-BW (%2d,%2d): zeroK_posC=%v best_ratio=%.4f (k=%.4f, C=%.4f)\n",
			r.K, r.C, r.FoundZeroMissK, r.BestRatio, r.BestMissRateK, r.BestMissRateC)
	}

	// Layer (c): Normal Workload Benchmarks
	fmt.Println("\n=== Experiment E3: Normal Workload Benchmarks (200k reqs x 3 seeds) ===")
	var benchRows []BenchSummary
	for _, alpha := range []float64{0.6, 0.8, 1.0, 1.2} {
		for _, C := range []int{100, 500, 1000} {
			b := runBench("zipf", alpha, C)
			benchRows = append(benchRows, b)
			fmt.Printf("Zipf a=%.1f C=%4d | SIEVE=%.4f BW1=%.4f(d=%+.4f) BW2=%.4f(d=%+.4f) S3=%.4f CLK=%.4f LRU=%.4f | fw1=%.1f(%.2f%%)\n",
				alpha, C, b.MissRatio["SIEVE"], b.MissRatio["SIEVE-BW(C)"], b.DeltaBW1,
				b.MissRatio["SIEVE-BW(2C)"], b.DeltaBW2, b.MissRatio["S3FIFO"], b.MissRatio["CLOCK"], b.MissRatio["LRU"],
				b.ForcedWraps1, b.ForcedPctBW1)
		}
	}
	for _, C := range []int{100, 500, 1000} {
		b := runBench("zipf_scan", 0.9, C)
		benchRows = append(benchRows, b)
		fmt.Printf("Zipf+Scan C=%4d | SIEVE=%.4f BW1=%.4f(d=%+.4f) BW2=%.4f(d=%+.4f) S3=%.4f CLK=%.4f LRU=%.4f | fw1=%.1f(%.2f%%)\n",
			C, b.MissRatio["SIEVE"], b.MissRatio["SIEVE-BW(C)"], b.DeltaBW1,
			b.MissRatio["SIEVE-BW(2C)"], b.DeltaBW2, b.MissRatio["S3FIFO"], b.MissRatio["CLOCK"], b.MissRatio["LRU"],
			b.ForcedWraps1, b.ForcedPctBW1)
	}

	payload := map[string]any{
		"grid_bw1_pass": pass1,
		"grid_bw2_pass": pass2,
		"grid_total":    len(grid1),
		"max_sweep_bw1": maxSweep1,
		"max_sweep_bw2": maxSweep2,
		"extremes":      extRows,
		"exact_bw":      exactBWRes,
		"evo_bw":        evoBWRes,
		"benchmarks":    benchRows,
	}
	bytes, _ := json.MarshalIndent(payload, "", "  ")
	if err := os.WriteFile(filepath.Join(*outDir, "exp_e_sievebw_go.json"), bytes, 0o644); err != nil {
		panic(err)
	}
}
