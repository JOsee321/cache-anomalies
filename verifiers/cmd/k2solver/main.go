package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"

	"sieve-verifier/sieve"
)

// compactState encodes a single SIEVE(cap) state (cap <= 4, keys in 0..7) into uint32.
// Layout:
//
//	bits 0..2:   len (0..4)
//	bits 3..5:   handIdx (0..3, or 7 for nil)
//	bits 6..9:   slot 0 (key 3 bits + visited 1 bit)
//	bits 10..13: slot 1
//	bits 14..17: slot 2
//	bits 18..21: slot 3
type compactState uint32

type unpackedCache struct {
	cap     int
	n       int
	handIdx int // -1 if nil
	keys    [4]int
	vis     [4]bool
}

func unpack(s compactState, cap int) unpackedCache {
	u := unpackedCache{cap: cap}
	u.n = int(s & 0x7)
	h := int((s >> 3) & 0x7)
	if h == 7 {
		u.handIdx = -1
	} else {
		u.handIdx = h
	}
	for i := 0; i < u.n; i++ {
		slot := (s >> (6 + 4*i)) & 0xF
		u.keys[i] = int(slot & 0x7)
		u.vis[i] = ((slot >> 3) & 0x1) != 0
	}
	return u
}

func (u unpackedCache) pack() compactState {
	var s compactState
	s |= compactState(u.n & 0x7)
	h := 7
	if u.handIdx >= 0 {
		h = u.handIdx
	}
	s |= compactState(h&0x7) << 3
	for i := 0; i < u.n; i++ {
		slot := u.keys[i] & 0x7
		if u.vis[i] {
			slot |= 0x8
		}
		s |= compactState(slot) << (6 + 4*i)
	}
	return s
}

// step simulates one SIEVE access (official NSDI '24 pointer semantics) on the unpacked cache.
// Index 0 is Head (newest), index n-1 is Tail (oldest).
func (u unpackedCache) step(key int) (compactState, int) {
	for i := 0; i < u.n; i++ {
		if u.keys[i] == key {
			u.vis[i] = true
			return u.pack(), 0
		}
	}

	// Miss
	if u.n >= u.cap {
		o := u.handIdx
		if o < 0 {
			o = u.n - 1
		}
		for u.vis[o] {
			u.vis[o] = false
			o--
			if o < 0 {
				o = u.n - 1
			}
		}
		// Official: hand = o.prev (which is o-1, or -1 if o == 0 (Head))
		newHand := o - 1
		// Remove o by shifting [o+1 .. n-1] left
		for i := o; i < u.n-1; i++ {
			u.keys[i] = u.keys[i+1]
			u.vis[i] = u.vis[i+1]
		}
		u.n--
		// Insert new item at Head (index 0); shift [0 .. n-1] right by 1
		for i := u.n; i > 0; i-- {
			u.keys[i] = u.keys[i-1]
			u.vis[i] = u.vis[i-1]
		}
		u.keys[0] = key
		u.vis[0] = false
		u.n++
		if newHand >= 0 {
			// Every element before o shifted right by 1 when inserting at Head!
			newHand++
		}
		u.handIdx = newHand
		return u.pack(), 1
	}

	// Not full: insert at Head (index 0)
	for i := u.n; i > 0; i-- {
		u.keys[i] = u.keys[i-1]
		u.vis[i] = u.vis[i-1]
	}
	u.keys[0] = key
	u.vis[0] = false
	u.n++
	if u.handIdx >= 0 {
		u.handIdx++
	}
	return u.pack(), 1
}

type edge struct {
	to    int
	key   int
	missK int
	missC int
}

type solverResult struct {
	K                     int     `json:"k"`
	C                     int     `json:"C"`
	UniverseSize          int     `json:"universe_size"`
	ReachableStates       int     `json:"reachable_states"`
	ReachableEdges        int     `json:"reachable_edges"`
	ZeroMissKWithPosMissC bool    `json:"zero_miss_k_with_pos_miss_C"`
	MaxRatioNum           int     `json:"max_ratio_num"`
	MaxRatioDen           int     `json:"max_ratio_den"`
	MaxRatio              float64 `json:"max_ratio"`
	ShortestPrefix        []int   `json:"shortest_prefix"`
	ShortestCycle         []int   `json:"shortest_cycle"`
	CycleMissK            int     `json:"cycle_miss_k"`
	CycleMissC            int     `json:"cycle_miss_C"`
}

func buildGraph(k, C, m int) ([][]edge, []int) {
	emptyK := unpackedCache{cap: k, n: 0, handIdx: -1}.pack()
	emptyC := unpackedCache{cap: C, n: 0, handIdx: -1}.pack()
	initJoint := (uint64(emptyK) << 24) | uint64(emptyC)

	stateMap := map[uint64]int{initJoint: 0}
	states := []uint64{initJoint}
	parent := []int{-1}
	parentKey := []int{-1}

	var adj [][]edge

	for idx := 0; idx < len(states); idx++ {
		joint := states[idx]
		sk := compactState(joint >> 24)
		sc := compactState(joint & 0xFFFFFF)
		uk := unpack(sk, k)
		uc := unpack(sc, C)

		edges := make([]edge, 0, m)
		for key := 0; key < m; key++ {
			nsk, mk := uk.step(key)
			nsc, mC := uc.step(key)
			nJoint := (uint64(nsk) << 24) | uint64(nsc)
			toIdx, ok := stateMap[nJoint]
			if !ok {
				toIdx = len(states)
				stateMap[nJoint] = toIdx
				states = append(states, nJoint)
				parent = append(parent, idx)
				parentKey = append(parentKey, key)
			}
			edges = append(edges, edge{
				to:    toIdx,
				key:   key,
				missK: mk,
				missC: mC,
			})
		}
		adj = append(adj, edges)
	}
	_ = parent
	_ = parentKey
	return adj, nil
}

// checkZeroMissKWithPosMissC checks if any directed cycle in the reachable graph
// has missK == 0 on every edge and missC == 1 on at least one edge.
func checkZeroMissKWithPosMissC(adj [][]edge) bool {
	n := len(adj)
	// Tarjan SCC on subgraph of missK == 0 edges.
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

	var strongConnect func(v int)
	strongConnect = func(v int) {
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
				strongConnect(w)
				if lowlink[w] < lowlink[v] {
					lowlink[v] = lowlink[w]
				}
			} else if onStack[w] {
				if indices[w] < lowlink[v] {
					lowlink[v] = indices[w]
				}
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
			strongConnect(v)
		}
	}

	// Any missK == 0 edge with missC == 1 inside the same SCC yields a cycle!
	for u := 0; u < n; u++ {
		for _, e := range adj[u] {
			if e.missK == 0 && e.missC > 0 && sccID[u] == sccID[e.to] {
				return true
			}
		}
	}
	return false
}

// hasPositiveCycle checks (via SPFA + periodic parent-cycle detection) whether
// there exists a directed cycle with sum(missC - R*missK) > eps.
func hasPositiveCycle(adj [][]edge, R float64) bool {
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

	// Cycle detection in parent pointer graph (O(n) time).
	seen := make([]int, n)
	stamp := 0
	hasParentCycle := func() bool {
		stamp++
		curStamp := stamp
		for i := 0; i < n; i++ {
			if seen[i] != 0 {
				continue
			}
			v := i
			pathStamp := curStamp + i + 1
			for v != -1 && seen[v] == 0 {
				seen[v] = pathStamp
				v = parent[v]
			}
			if v != -1 && seen[v] == pathStamp {
				return true
			}
		}
		stamp += n + 1
		// Reset seen lazily by resetting array when needed
		for i := range seen {
			seen[i] = 0
		}
		return false
	}

	const eps = 1e-11
	relaxCount := 0
	for head := 0; head < len(queue); head++ {
		u := queue[head]
		inQueue[u] = false
		du := dist[u]
		for _, e := range adj[u] {
			w := float64(e.missC) - R*float64(e.missK)
			if du+w > dist[e.to]+eps {
				dist[e.to] = du + w
				parent[e.to] = u
				relaxCount++
				if relaxCount%n == 0 {
					if hasParentCycle() {
						return true
					}
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

// computePotentialsExact runs SPFA with negated integer weights w = q*missC - p*missK
// (minimizing -w, i.e., finding pi such that pi[v] <= pi[u] - w).
// Returns (pi, hasStrictlyPositiveCycle).
func computePotentialsExact(adj [][]edge, p, q int64) ([]int64, bool) {
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

	relaxCount := 0
	for head := 0; head < len(queue); head++ {
		u := queue[head]
		inQueue[u] = false
		pu := pi[u]
		for _, e := range adj[u] {
			cost := p*int64(e.missK) - q*int64(e.missC) // -w
			if pu+cost < pi[e.to] {
				pi[e.to] = pu + cost
				parent[e.to] = u
				relaxCount++
				if relaxCount%n == 0 {
					if hasParentCycle() {
						return nil, true
					}
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
	if a < 0 {
		a = -a
	}
	if b < 0 {
		b = -b
	}
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

// bestRationalApproximation finds the exact fraction p/q near x with q <= maxDenom
// using Stern-Brocot / continued fractions.
func bestRationalApprox(x float64, maxDenom int64) (int64, int64) {
	if x == 0 {
		return 0, 1
	}
	a := int64(math.Floor(x))
	frac := x - float64(a)
	if frac < 1e-12 {
		return a, 1
	}
	// Continued fraction expansion
	p0, q0 := int64(1), int64(0)
	p1, q1 := a, int64(1)
	rem := frac
	for iter := 0; iter < 40; iter++ {
		if rem < 1e-12 {
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

func solveExact(k, C, m int) solverResult {
	adj, _ := buildGraph(k, C, m)
	n := len(adj)
	totalEdges := 0
	for _, es := range adj {
		totalEdges += len(es)
	}

	hasInf := checkZeroMissKWithPosMissC(adj)
	if hasInf {
		return solverResult{
			K:                     k,
			C:                     C,
			UniverseSize:          m,
			ReachableStates:       n,
			ReachableEdges:        totalEdges,
			ZeroMissKWithPosMissC: true,
			MaxRatio:              math.Inf(1),
		}
	}

	// Binary search for max ratio R*
	lo, hi := 0.0, 1.0
	for hasPositiveCycle(adj, hi) {
		lo = hi
		hi *= 2.0
		if hi > float64(n) {
			break
		}
	}
	for iter := 0; iter < 55; iter++ {
		mid := 0.5 * (lo + hi)
		if hasPositiveCycle(adj, mid) {
			lo = mid
		} else {
			hi = mid
		}
	}

	// Recover exact rational p/q using continued fractions and verify exact optimality.
	p, q := bestRationalApprox(0.5*(lo+hi), int64(n))
	pi, pos := computePotentialsExact(adj, p, q)
	if pos {
		panic(fmt.Sprintf("rational approx %d/%d still has positive cycle!", p, q))
	}
	// Also verify p/q is achievable (there is a cycle in tight subgraph with missK > 0)
	// First, build tight subgraph E_tight: pi[v] == pi[u] + p*missK - q*missC
	tightAdj := make([][]edge, n)
	for u := 0; u < n; u++ {
		for _, e := range adj[u] {
			cost := p*int64(e.missK) - q*int64(e.missC)
			if pi[e.to] == pi[u]+cost {
				tightAdj[u] = append(tightAdj[u], e)
			}
		}
	}

	// Find SCCs of tightAdj so we only search within nontrivial SCCs
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
	var tarjan func(v int)
	tarjan = func(v int) {
		indices[v] = index
		lowlink[v] = index
		index++
		stack = append(stack, v)
		onStack[v] = true
		for _, e := range tightAdj[v] {
			w := e.to
			if indices[w] == -1 {
				tarjan(w)
				if lowlink[w] < lowlink[v] {
					lowlink[v] = lowlink[w]
				}
			} else if onStack[w] {
				if indices[w] < lowlink[v] {
					lowlink[v] = indices[w]
				}
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

	// BFS from node 0 (initial empty state) in full graph to get shortest prefix distances to every state
	prefDist := make([]int, n)
	prefParent := make([]int, n)
	prefKey := make([]int, n)
	for i := range prefDist {
		prefDist[i] = -1
		prefParent[i] = -1
		prefKey[i] = -1
	}
	prefDist[0] = 0
	bfsQ := []int{0}
	for head := 0; head < len(bfsQ); head++ {
		u := bfsQ[head]
		for _, e := range adj[u] {
			if prefDist[e.to] == -1 {
				prefDist[e.to] = prefDist[u] + 1
				prefParent[e.to] = u
				prefKey[e.to] = e.key
				bfsQ = append(bfsQ, e.to)
			}
		}
	}

	// Find shortest optimal cycle (minimum cycle len; tie-break by shortest prefix to cycle start)
	bestCycleLen := math.MaxInt
	bestPrefLen := math.MaxInt
	var bestCycleEdges []edge
	bestStartState := -1

	// Reusable BFS arrays inside tightAdj
	dist := make([]int, n)
	prevNode := make([]int, n)
	prevEdge := make([]edge, n)
	visitedStamp := make([]int, n)
	stamp := 0

	// Order nodes by prefDist ascending so shorter prefixes are found early
	nodesByPref := make([]int, n)
	for i := 0; i < n; i++ {
		nodesByPref[i] = i
	}
	// Simple bucket sort by prefDist
	maxPref := 0
	for _, d := range prefDist {
		if d > maxPref {
			maxPref = d
		}
	}
	buckets := make([][]int, maxPref+1)
	for u := 0; u < n; u++ {
		buckets[prefDist[u]] = append(buckets[prefDist[u]], u)
	}
	posIdx := 0
	for d := 0; d <= maxPref; d++ {
		for _, u := range buckets[d] {
			nodesByPref[posIdx] = u
			posIdx++
		}
	}

	for _, u := range nodesByPref {
		for _, e0 := range tightAdj[u] {
			if e0.missK == 0 {
				continue
			}
			if sccID[u] != sccID[e0.to] {
				continue
			}
			// Shortest path from e0.to back to u in tightAdj
			if e0.to == u {
				cLen := 1
				if cLen < bestCycleLen || (cLen == bestCycleLen && prefDist[u] < bestPrefLen) {
					bestCycleLen = cLen
					bestPrefLen = prefDist[u]
					bestStartState = u
					bestCycleEdges = []edge{e0}
				}
				continue
			}
			// Max path length from e0.to to u we care about:
			// need 1 + d <= bestCycleLen (and < bestCycleLen if prefDist[u] >= bestPrefLen)
			maxPathLen := bestCycleLen - 1
			if prefDist[u] >= bestPrefLen && bestCycleLen < math.MaxInt {
				maxPathLen = bestCycleLen - 2
			}
			if maxPathLen < 1 {
				continue
			}

			stamp++
			qBfs := []int{e0.to}
			visitedStamp[e0.to] = stamp
			dist[e0.to] = 0
			found := false
			for h := 0; h < len(qBfs); h++ {
				cur := qBfs[h]
				if dist[cur] >= maxPathLen {
					break
				}
				for _, ne := range tightAdj[cur] {
					if sccID[ne.to] != sccID[u] {
						continue
					}
					if visitedStamp[ne.to] != stamp {
						visitedStamp[ne.to] = stamp
						dist[ne.to] = dist[cur] + 1
						prevNode[ne.to] = cur
						prevEdge[ne.to] = ne
						if ne.to == u {
							found = true
							break
						}
						if dist[ne.to] < maxPathLen {
							qBfs = append(qBfs, ne.to)
						}
					}
				}
				if found {
					break
				}
			}
			if found {
				cLen := 1 + dist[u]
				if cLen < bestCycleLen || (cLen == bestCycleLen && prefDist[u] < bestPrefLen) {
					bestCycleLen = cLen
					bestPrefLen = prefDist[u]
					bestStartState = u
					rev := make([]edge, 0, dist[u])
					cur := u
					for cur != e0.to {
						rev = append(rev, prevEdge[cur])
						cur = prevNode[cur]
					}
					cyc := make([]edge, 0, cLen)
					cyc = append(cyc, e0)
					for i := len(rev) - 1; i >= 0; i-- {
						cyc = append(cyc, rev[i])
					}
					bestCycleEdges = cyc
				}
			}
		}
	}

	if bestStartState == -1 {
		panic(fmt.Sprintf("no tight cycle found for p=%d q=%d", p, q))
	}

	// Reconstruct prefix from 0 to bestStartState
	revPref := []int{}
	cur := bestStartState
	for cur != 0 {
		revPref = append(revPref, prefKey[cur])
		cur = prefParent[cur]
	}
	prefix := make([]int, len(revPref))
	for i := range revPref {
		prefix[i] = revPref[len(revPref)-1-i]
	}

	cycleKeys := make([]int, len(bestCycleEdges))
	cMissK, cMissC := 0, 0
	for i, e := range bestCycleEdges {
		cycleKeys[i] = e.key
		cMissK += e.missK
		cMissC += e.missC
	}

	return solverResult{
		K:                     k,
		C:                     C,
		UniverseSize:          m,
		ReachableStates:       n,
		ReachableEdges:        totalEdges,
		ZeroMissKWithPosMissC: false,
		MaxRatioNum:           int(p),
		MaxRatioDen:           int(q),
		MaxRatio:              float64(p) / float64(q),
		ShortestPrefix:        prefix,
		ShortestCycle:         cycleKeys,
		CycleMissK:            cMissK,
		CycleMissC:            cMissC,
	}
}

func main() {
	outDir := flag.String("out", "../results", "output directory")
	flag.Parse()

	cases := [][3]int{
		{2, 3, 3},
		{2, 3, 4},
		{2, 4, 3},
		{2, 4, 4},
		{2, 3, 5},
		{2, 4, 5},
	}

	results := make([]solverResult, 0, len(cases))
	for _, c := range cases {
		res := solveExact(c[0], c[1], c[2])
		// Cross-verify witness with explicit doubly-linked list SieveCache
		if !res.ZeroMissKWithPosMissC {
			ck, _ := sieve.NewCache(res.K, false)
			cC, _ := sieve.NewCache(res.C, false)
			for _, x := range res.ShortestPrefix {
				ck.Access(x)
				cC.Access(x)
			}
			for rep := 0; rep < 100; rep++ {
				mk, mC := 0, 0
				for _, x := range res.ShortestCycle {
					if h, _, _ := ck.Access(x); !h {
						mk++
					}
					if h, _, _ := cC.Access(x); !h {
						mC++
					}
				}
				if rep >= 10 && (mk != res.CycleMissK || mC != res.CycleMissC) {
					panic(fmt.Sprintf("DLL cross-check mismatch on rep %d: got (%d,%d), want (%d,%d)",
						rep, mk, mC, res.CycleMissK, res.CycleMissC))
				}
			}
		}
		results = append(results, res)
		fmt.Printf("(k=%d, C=%d, m=%d): states=%d, zero_k_pos_C=%v, max_ratio=%d/%d (%.6f), prefix=%v, cycle=%v (missK=%d, missC=%d)\n",
			res.K, res.C, res.UniverseSize, res.ReachableStates, res.ZeroMissKWithPosMissC,
			res.MaxRatioNum, res.MaxRatioDen, res.MaxRatio, res.ShortestPrefix, res.ShortestCycle, res.CycleMissK, res.CycleMissC)
	}

	bytes, _ := json.MarshalIndent(results, "", "  ")
	if err := os.WriteFile(filepath.Join(*outDir, "k2_solver_go.json"), bytes, 0o644); err != nil {
		panic(err)
	}
}
