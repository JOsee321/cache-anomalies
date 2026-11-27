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

// --- Helpers & Zipf ---

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

// --- Exp F1: Zombie Pinning on SIEVE-TP ---

type ZombieTPRow struct {
	K               int  `json:"k"`
	C               int  `json:"C"`
	Label           string `json:"label"`
	ProbeInterval   int  `json:"probe_interval"`
	FirstZeroKCycle int  `json:"first_zero_k_cycle"`
	FirstZeroCCycle int  `json:"first_zero_C_cycle"`
	SteadyMissesK   int  `json:"steady_misses_k"`
	SteadyMissesC   int  `json:"steady_misses_C"`
	TailProbesC     int  `json:"tail_probes_C"`
	DirectEvictsC   int  `json:"direct_evicts_C"`
	Ok              bool `json:"ok"`
}

func probeVal(cap int, mult float64) int {
	v := int(math.Round(float64(cap) * mult))
	if mult == 0.5 {
		v = cap / 2
	}
	if v < 1 {
		v = 1
	}
	return v
}

func runZombieTP(k, C int, label string, mult float64, numCycles int) ZombieTPRow {
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

	pk := probeVal(k, mult)
	pC := probeVal(C, mult)
	ck, _ := sieve.NewTPCache(k, pk)
	cC, _ := sieve.NewTPCache(C, pC)
	for _, x := range prefix {
		ck.Access(x)
		cC.Access(x)
	}

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
	return ZombieTPRow{
		K:               k,
		C:               C,
		Label:           label,
		ProbeInterval:   pC,
		FirstZeroKCycle: firstZeroK,
		FirstZeroCCycle: firstZeroC,
		SteadyMissesK:   steadyMK,
		SteadyMissesC:   steadyMC,
		TailProbesC:     cC.TailProbes,
		DirectEvictsC:   cC.TailDirectEvicts,
		Ok:              ok,
	}
}

// --- Exp F2: Exact State Graph & Evo on SIEVE-TP(K=C) ---

type exactTP struct {
	c *sieve.TPCache
}

func newExactTP(cap int) *exactTP {
	c, _ := sieve.NewTPCache(cap, cap)
	return &exactTP{c: c}
}

func (e *exactTP) Access(key int) bool {
	h, _, _ := e.c.Access(key)
	return h
}

func (e *exactTP) Encode() string {
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

func (e *exactTP) Clone() *exactTP {
	nc, _ := sieve.NewTPCache(e.c.Capacity, e.c.ProbeInterval)
	nc.EvictsSinceWrap = e.c.EvictsSinceWrap
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
	return &exactTP{c: nc}
}

type gEdge struct {
	to    int
	key   int
	missK int
	missC int
}

type ExactTPResult struct {
	K                     int     `json:"k"`
	C                     int     `json:"C"`
	UniverseSize          int     `json:"universe_size"`
	Complete              bool    `json:"complete"`
	States                int     `json:"states"`
	ZeroMissKWithPosMissC bool    `json:"zero_miss_k_with_pos_miss_C"`
	MaxRatioNum           int     `json:"max_ratio_num"`
	MaxRatioDen           int     `json:"max_ratio_den"`
	MaxRatio              float64 `json:"max_ratio"`
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

func runExactTP(k, C, m, maxStates int) ExactTPResult {
	type pairState struct {
		sk *exactTP
		sc *exactTP
	}
	initP := pairState{newExactTP(k), newExactTP(C)}
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
		return ExactTPResult{K: k, C: C, UniverseSize: m, Complete: false, States: len(states)}
	}
	n := len(adj)
	if checkZeroMissK(adj) {
		return ExactTPResult{K: k, C: C, UniverseSize: m, Complete: true, States: n, ZeroMissKWithPosMissC: true, MaxRatio: 999999}
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
	_, pos := computePotentials(adj, p, q)
	if pos {
		p, q = bestRationalApprox(hi, int64(n))
		_, pos = computePotentials(adj, p, q)
		if pos {
			panic("cannot lock rational")
		}
	}
	return ExactTPResult{
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

type EvoTPResult struct {
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

func evalTP(k, C int, prefix, cycle []int, totalCycles, steadyStart int) (int, int, int, float64) {
	ck, _ := sieve.NewTPCache(k, k)
	cC, _ := sieve.NewTPCache(C, C)
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

func runEvoTP(k, C int, seed int64) EvoTPResult {
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
		mk, mC, _, _ := evalTP(k, C, p, c, 40, 20)
		pop[i] = cand{p: p, c: c, score: float64(mC)*10 - float64(mk)*50, mk: mk, mC: mC}
	}
	for gen := 0; gen < 120; gen++ {
		sort.Slice(pop, func(i, j int) bool { return pop[i].score > pop[j].score })
		if pop[0].mk == 0 && pop[0].mC > 0 {
			vmk, vmC, _, _ := evalTP(k, C, pop[0].p, pop[0].c, 5000, 1000)
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
			mk, mC, _, _ := evalTP(k, C, np, nc, 40, 20)
			next[i] = cand{p: np, c: nc, score: float64(mC)*10 - float64(mk)*50, mk: mk, mC: mC}
		}
		pop = next
	}

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
		mk, mC, _, r := evalTP(k, C, p, c, 45, 20)
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
			mk, mC, _, r := evalTP(k, C, np, nc, 45, 20)
			next[i] = cand{p: np, c: nc, score: r, mk: mk, mC: mC}
		}
		pop2 = next
	}
	sort.Slice(pop2, func(i, j int) bool { return pop2[i].score > pop2[j].score })
	vmk, vmC, vacc, vratio := evalTP(k, C, pop2[0].p, pop2[0].c, 5000, 1000)

	return EvoTPResult{
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

// --- Exp F3: Mixed Traffic & Zombie Hits ---

type MixedTPRow struct {
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

func makeVariantAccessor(variant string, C int) func(int) (bool, int, bool) {
	switch variant {
	case "SIEVE":
		c, _ := sieve.NewCache(C, false)
		return c.Access
	case "SIEVE-BW(2C)":
		c, _ := sieve.NewBWCache(C, 2*C)
		return c.Access
	case "SIEVE-TP(0.5C)":
		c, _ := sieve.NewTPCache(C, probeVal(C, 0.5))
		return c.Access
	case "SIEVE-TP(C)":
		c, _ := sieve.NewTPCache(C, probeVal(C, 1.0))
		return c.Access
	case "SIEVE-TP(2C)":
		c, _ := sieve.NewTPCache(C, probeVal(C, 2.0))
		return c.Access
	case "SIEVE-TP(4C)":
		c, _ := sieve.NewTPCache(C, probeVal(C, 4.0))
		return c.Access
	default:
		panic(variant)
	}
}

func runMixedSuite() ([]MixedTPRow, []map[string]any) {
	caps := []int{100, 200}
	scenarios := []struct {
		mode string
		p    float64
	}{
		{"zipf_disjoint", 0.01},
		{"zipf_disjoint", 0.05},
		{"zombie_hits_only", 0.25},
	}
	variants := []string{"SIEVE", "SIEVE-BW(2C)", "SIEVE-TP(C)", "SIEVE-TP(2C)", "SIEVE-TP(4C)"}
	seeds := []int64{101, 202, 303}

	var rows []MixedTPRow
	var exports []map[string]any

	for _, C := range caps {
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
			// Pre-generate 3 traces (50k reqs each)
			traces := make([][]int, len(seeds))
			for si, seed := range seeds {
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

			for _, vName := range variants {
				var unwrapSum, unwrapCnt int
				var pin10Sum, pin50Sum, m50Sum float64
				for si, tr := range traces {
					acc := makeVariantAccessor(vName, C)
					for _, x := range prefix {
						acc(x)
					}
					stillPinned := make([]bool, numZombies)
					for i := range stillPinned {
						stillPinned[i] = true
					}
					pinnedCnt := numZombies
					pinned10k := -1
					unwrap := -1
					misses := 0
					for step, key := range tr {
						h, ev, hasEv := acc(key)
						if !h {
							misses++
						}
						if hasEv && ev >= 0 && ev < numZombies && stillPinned[ev] {
							stillPinned[ev] = false
							pinnedCnt--
							if unwrap == -1 {
								unwrap = step + 1
							}
						}
						if step+1 == 10000 {
							pinned10k = pinnedCnt
						}
					}
					if unwrap >= 0 {
						unwrapSum += unwrap
						unwrapCnt++
					}
					pin10Sum += 100.0 * float64(pinned10k) / float64(numZombies)
					pin50Sum += 100.0 * float64(pinnedCnt) / float64(numZombies)
					m50Sum += float64(misses)

					if C == 100 && si == 0 && sc.mode == "zombie_hits_only" && vName == "SIEVE-TP(C)" {
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
				n := float64(len(seeds))
				avgUnwrap := -1.0
				if unwrapCnt > 0 {
					avgUnwrap = float64(unwrapSum) / float64(unwrapCnt)
				}
				rows = append(rows, MixedTPRow{
					Cap:          C,
					Mode:         sc.mode,
					P:            sc.p,
					Variant:      vName,
					TimeToUnwrap: avgUnwrap,
					UnwrappedCnt: unwrapCnt,
					PinnedPct10k: pin10Sum / n,
					PinnedPct50k: pin50Sum / n,
					MissRate50k:  (m50Sum / n) / 50000.0,
					TotalMiss50k: m50Sum / n,
				})
			}
		}
	}
	return rows, exports
}

// --- Exp G: Pareto Curve (Normal Benchmarks + Worst-Case Recovery) ---

type ParetoRow struct {
	Policy              string  `json:"policy"`
	MaxDelta            float64 `json:"max_delta_vs_sieve"`
	MinDelta            float64 `json:"min_delta_vs_sieve"`
	AvgDelta            float64 `json:"avg_delta_vs_sieve"`
	AvgMissRatio        float64 `json:"avg_miss_ratio"`
	Zipf10_C100         float64 `json:"zipf10_c100"`
	ZipfScan_C100       float64 `json:"zipf_scan_c100"`
	ZipfScan_C1000      float64 `json:"zipf_scan_c1000"`
	RecovReqsPure1000   int     `json:"recov_reqs_pure_c1000"`
	Misses50kPure1000   int     `json:"misses_50k_pure_c1000"`
	RecovReqsZHit100    float64 `json:"avg_unwrap_zhit25_c100"`
	Misses50kZHit100    float64 `json:"avg_misses_50k_zhit25_c100"`
	RecovReqsZHit1000   float64 `json:"avg_unwrap_zhit25_c1000"`
	Misses50kZHit1000   float64 `json:"avg_misses_50k_zhit25_c1000"`
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

	acc := makeParetoPolicy(pol, C)
	for _, x := range prefix {
		acc(x)
	}
	totalMisses := 0
	lastMissReq := 0
	for step := 1; step <= 50000; step++ {
		key := cyc[(step-1)%3]
		if !acc(key) {
			totalMisses++
			lastMissReq = step
		}
	}
	// If still missing near 50000, did not recover
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
		acc := makeParetoPolicy(pol, C)
		for _, x := range prefix {
			acc(x)
		}
		cycPos := 0
		unwrap := -1
		misses := 0
		for step := 1; step <= 50000; step++ {
			var key int
			if rng.Float64() < 0.25 {
				key = rng.Intn(numZombies)
			} else {
				key = cyc[cycPos]
				cycPos = (cycPos + 1) % 3
			}
			h := acc(key)
			if !h {
				misses++
			} else if unwrap == -1 && step > 3 && key >= C-2 {
				// wait: better track exact zombie eviction via non-hit or direct SIEVE types;
				// actually let's measure "requests until cycle miss rate stabilizes" OR exact zombie eviction!
			}
		}
		_ = unwrap
		missSum += misses
		// Also measure exact first zombie eviction for SIEVE variants:
		if strings.HasPrefix(pol, "SIEVE") {
			acc2 := makeParetoSieveExact(pol, C)
			for _, x := range prefix {
				acc2(x)
			}
			rng2 := rand.New(rand.NewSource(seed))
			cycPos = 0
			for step := 1; step <= 50000; step++ {
				var key int
				if rng2.Float64() < 0.25 {
					key = rng2.Intn(numZombies)
				} else {
					key = cyc[cycPos]
					cycPos = (cycPos + 1) % 3
				}
				_, ev, hasEv := acc2(key)
				if hasEv && ev >= 0 && ev < numZombies {
					unwrapSum += step
					unwrapCnt++
					break
				}
			}
		}
	}
	avgUnwrap := -1.0
	if unwrapCnt > 0 {
		avgUnwrap = float64(unwrapSum) / float64(unwrapCnt)
	}
	return avgUnwrap, float64(missSum) / float64(len(seeds))
}

func makeParetoSieveExact(pol string, C int) func(int) (bool, int, bool) {
	switch pol {
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
	case "SIEVE-TP(0.5C)":
		c, _ := sieve.NewTPCache(C, probeVal(C, 0.5))
		return c.Access
	case "SIEVE-TP(C)":
		c, _ := sieve.NewTPCache(C, probeVal(C, 1.0))
		return c.Access
	case "SIEVE-TP(2C)":
		c, _ := sieve.NewTPCache(C, probeVal(C, 2.0))
		return c.Access
	case "SIEVE-TP(4C)":
		c, _ := sieve.NewTPCache(C, probeVal(C, 4.0))
		return c.Access
	default:
		panic(pol)
	}
}

func makeParetoPolicy(pol string, C int) func(int) bool {
	if strings.HasPrefix(pol, "SIEVE") {
		fn := makeParetoSieveExact(pol, C)
		return func(k int) bool {
			h, _, _ := fn(k)
			return h
		}
	}
	p, err := policies.New(pol, C)
	if err != nil {
		panic(err)
	}
	return p.Access
}

type BenchDetail struct {
	Workload  string             `json:"workload"`
	Alpha     float64            `json:"alpha"`
	Cap       int                `json:"cap"`
	MissRatio map[string]float64 `json:"miss_ratio"`
}

func runParetoSuite() ([]ParetoRow, []BenchDetail) {
	polNames := []string{
		"SIEVE",
		"SIEVE-BW(C)", "SIEVE-BW(2C)", "SIEVE-BW(4C)", "SIEVE-BW(8C)", "SIEVE-BW(16C)",
		"SIEVE-TP(0.5C)", "SIEVE-TP(C)", "SIEVE-TP(2C)", "SIEVE-TP(4C)",
		"S3FIFO", "CLOCK", "LRU",
	}

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
	var details []BenchDetail

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
				acc := makeParetoPolicy(pName, sp.cap)
				m := 0
				for _, x := range trace {
					if !acc(x) {
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
		details = append(details, BenchDetail{
			Workload:  sp.name,
			Alpha:     sp.alpha,
			Cap:       sp.cap,
			MissRatio: mr,
		})
	}

	var pareto []ParetoRow
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
		rec1000, mPure1000 := runPureRecovery1000(pName)
		u100, mZ100 := runZHitRecovery(pName, 100)
		u1000, mZ1000 := runZHitRecovery(pName, 1000)

		// Extract key benchmark cells (zipf a=1.0 C=100 is index 6, zipf_scan C=100 is index 12, C=1000 is index 14)
		pareto = append(pareto, ParetoRow{
			Policy:            pName,
			MaxDelta:          maxD,
			MinDelta:          minD,
			AvgDelta:          sumD / float64(len(ds)),
			AvgMissRatio:      sumR / float64(len(rs)),
			Zipf10_C100:       details[6].MissRatio[pName],
			ZipfScan_C100:     details[12].MissRatio[pName],
			ZipfScan_C1000:    details[14].MissRatio[pName],
			RecovReqsPure1000: rec1000,
			Misses50kPure1000: mPure1000,
			RecovReqsZHit100:  u100,
			Misses50kZHit100:  mZ100,
			RecovReqsZHit1000: u1000,
			Misses50kZHit1000: mZ1000,
		})
	}
	return pareto, details
}

func main() {
	outDir := flag.String("out", "../results", "output directory")
	flag.Parse()

	// --- Exp F1: Zombie Pinning on SIEVE-TP ---
	fmt.Println("=== Experiment F1: SIEVE-TP Zombie Pinning Verification ===")
	mults := []struct {
		label string
		mult  float64
	}{
		{"0.5C", 0.5},
		{"1.0C", 1.0},
		{"2.0C", 2.0},
		{"4.0C", 4.0},
	}
	gridSummary := map[string]any{}
	for _, ml := range mults {
		pass := 0
		maxZeroC := 0
		for k := 3; k < 30; k++ {
			for C := k + 1; C <= 30; C++ {
				r := runZombieTP(k, C, ml.label, ml.mult, 1000)
				if r.Ok {
					pass++
				}
				if r.FirstZeroCCycle > maxZeroC {
					maxZeroC = r.FirstZeroCCycle
				}
			}
		}
		gridSummary[ml.label] = map[string]any{"pass": pass, "total": 378, "max_zero_C_cycle": maxZeroC}
		fmt.Printf("Grid 3<=k<C<=30 TP(%s): %d/378 (max_zero_C_cycle=%d)\n", ml.label, pass, maxZeroC)
	}

	extremes := [][2]int{{3, 100}, {3, 500}, {3, 1000}, {10, 1000}, {500, 1000}}
	var extRows []ZombieTPRow
	for _, p := range extremes {
		for _, ml := range mults {
			r := runZombieTP(p[0], p[1], ml.label, ml.mult, 3*p[1]+500)
			extRows = append(extRows, r)
			if p[1] == 1000 && p[0] == 3 || (ml.label == "1.0C") {
				fmt.Printf("Extreme (%3d,%4d) TP(%s K=%4d): ok=%v first_zero_C_cycle=%d steadyC=%d probes=%d direct_ev=%d\n",
					r.K, r.C, ml.label, r.ProbeInterval, r.Ok, r.FirstZeroCCycle, r.SteadyMissesC, r.TailProbesC, r.DirectEvictsC)
			}
		}
	}

	// --- Exp F2: Exact & Evo on SIEVE-TP(C) ---
	fmt.Println("\n=== Experiment F2: SIEVE-TP(C) Exact & Evolutionary Atlas Check ===")
	var exactTPRes []ExactTPResult
	for _, c := range [][3]int{{2, 3, 4}, {3, 4, 4}} {
		r := runExactTP(c[0], c[1], c[2], 300000)
		exactTPRes = append(exactTPRes, r)
		fmt.Printf("Exact SIEVE-TP (%d,%d,m=%d): complete=%v states=%d zeroK_posC=%v max_ratio=%d/%d (%.4f)\n",
			r.K, r.C, r.UniverseSize, r.Complete, r.States, r.ZeroMissKWithPosMissC, r.MaxRatioNum, r.MaxRatioDen, r.MaxRatio)
	}
	evoPairs := [][2]int{{2, 3}, {3, 4}, {3, 5}, {3, 6}, {3, 10}, {4, 8}, {5, 10}, {6, 12}}
	var evoTPRes []EvoTPResult
	for i, p := range evoPairs {
		r := runEvoTP(p[0], p[1], int64(8800+i))
		evoTPRes = append(evoTPRes, r)
		fmt.Printf("Evo   SIEVE-TP (%2d,%2d): zeroK_posC=%v best_ratio=%.4f (k=%.4f, C=%.4f)\n",
			r.K, r.C, r.FoundZeroMissK, r.BestRatio, r.BestMissRateK, r.BestMissRateC)
	}

	// --- Exp F3: Mixed Traffic & Zombie Hits ---
	fmt.Println("\n=== Experiment F3: Mixed Traffic & Zombie Hits (50k reqs, 3 seeds) ===")
	mixedRows, mixedExports := runMixedSuite()
	for _, r := range mixedRows {
		uStr := "INF"
		if r.TimeToUnwrap >= 0 {
			uStr = fmt.Sprintf("%.0f(%d/3)", r.TimeToUnwrap, r.UnwrappedCnt)
		}
		fmt.Printf("C=%3d %-16s p=%4.2f %-13s | unwrap=%-9s pin10k=%5.1f%% pin50k=%5.1f%% | miss50k=%.4f (%.0f)\n",
			r.Cap, r.Mode, r.P, r.Variant, uStr, r.PinnedPct10k, r.PinnedPct50k, r.MissRate50k, r.TotalMiss50k)
	}

	// --- Exp G: Pareto Curve ---
	fmt.Println("\n=== Experiment G: Pareto Curve (200k x 3 seeds Zipf/Scan vs Recovery) ===")
	paretoRows, benchDetails := runParetoSuite()
	for _, r := range paretoRows {
		fmt.Printf("%-14s | avgMR=%.4f avgD=%+.4f maxD=%+.4f minD=%+.4f | Pure1k:recov=%5d miss=%5d | ZHit100:u=%5.0f m=%5.0f | ZHit1000:u=%5.0f m=%5.0f\n",
			r.Policy, r.AvgMissRatio, r.AvgDelta, r.MaxDelta, r.MinDelta,
			r.RecovReqsPure1000, r.Misses50kPure1000,
			r.RecovReqsZHit100, r.Misses50kZHit100,
			r.RecovReqsZHit1000, r.Misses50kZHit1000)
	}

	// Parity trace exports for SIEVE-TP (K in {C/2, C, 2C, 4C})
	type tpParity struct {
		Cap    int    `json:"cap"`
		K      int    `json:"k"`
		Trace  []int  `json:"trace"`
		Hits   int    `json:"hits"`
		Digest uint64 `json:"digest"`
	}
	var parityExports []tpParity
	pRng := rand.New(rand.NewSource(7777))
	for _, capVal := range []int{2, 3, 5, 10, 20} {
		for _, mult := range []float64{0.5, 1.0, 2.0, 4.0} {
			kv := probeVal(capVal, mult)
			tr := make([]int, 1000)
			for i := range tr {
				tr[i] = pRng.Intn(capVal + 4)
			}
			c, _ := sieve.NewTPCache(capVal, kv)
			hits := 0
			var dig uint64
			for i, x := range tr {
				if h, _, _ := c.Access(x); h {
					hits++
					dig ^= uint64(i+1) * 2654435761
				}
			}
			parityExports = append(parityExports, tpParity{Cap: capVal, K: kv, Trace: tr, Hits: hits, Digest: dig})
		}
	}

	payload := map[string]any{
		"grid_tp":        gridSummary,
		"extremes_tp":    extRows,
		"exact_tp":       exactTPRes,
		"evo_tp":         evoTPRes,
		"mixed_tp":       mixedRows,
		"mixed_exports":  mixedExports,
		"pareto":         paretoRows,
		"bench_details":  benchDetails,
		"parity_tp":      parityExports,
	}
	bytes, _ := json.MarshalIndent(payload, "", "  ")
	if err := os.WriteFile(filepath.Join(*outDir, "exp_fg_sieve_tp_go.json"), bytes, 0o644); err != nil {
		panic(err)
	}
}
