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
)

// --- Canonical State Encoding for Exact BFS/SCC Search ---

type exactPolicy interface {
	Access(key int) bool
	Encode() string
	Clone() exactPolicy
}

// stateFIFO wraps FIFO with state encoding.
type stateFIFO struct {
	cap   int
	queue []int
}

func newStateFIFO(cap int) *stateFIFO { return &stateFIFO{cap: cap} }
func (s *stateFIFO) Access(key int) bool {
	for _, k := range s.queue {
		if k == key {
			return true
		}
	}
	if len(s.queue) >= s.cap {
		s.queue = s.queue[1:]
	}
	s.queue = append(s.queue, key)
	return false
}
func (s *stateFIFO) Encode() string {
	var b strings.Builder
	b.Grow(len(s.queue) * 2)
	for i, k := range s.queue {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "%d", k)
	}
	return b.String()
}
func (s *stateFIFO) Clone() exactPolicy {
	q := make([]int, len(s.queue))
	copy(q, s.queue)
	return &stateFIFO{cap: s.cap, queue: q}
}

// stateCLOCK wraps CLOCK / CLOCK2 with maxFreq=1 or 2.
type stateCLOCK struct {
	cap     int
	maxFreq int
	slots   []int
	freq    []int
	hand    int
}

func newStateCLOCK(cap, maxFreq int) *stateCLOCK {
	return &stateCLOCK{cap: cap, maxFreq: maxFreq}
}
func (s *stateCLOCK) Access(key int) bool {
	for i, k := range s.slots {
		if k == key {
			if s.freq[i] < s.maxFreq {
				s.freq[i]++
			}
			return true
		}
	}
	if len(s.slots) < s.cap {
		s.slots = append(s.slots, key)
		s.freq = append(s.freq, 0)
		return false
	}
	for s.freq[s.hand] > 0 {
		s.freq[s.hand]--
		s.hand = (s.hand + 1) % s.cap
	}
	s.slots[s.hand] = key
	s.freq[s.hand] = 0
	s.hand = (s.hand + 1) % s.cap
	return false
}
func (s *stateCLOCK) Encode() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d|", s.hand)
	for i := range s.slots {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "%d:%d", s.slots[i], s.freq[i])
	}
	return b.String()
}
func (s *stateCLOCK) Clone() exactPolicy {
	sl := make([]int, len(s.slots))
	fr := make([]int, len(s.freq))
	copy(sl, s.slots)
	copy(fr, s.freq)
	return &stateCLOCK{cap: s.cap, maxFreq: s.maxFreq, slots: sl, freq: fr, hand: s.hand}
}

// stateSecondChance wraps SecondChance (head at index 0, tail at len-1).
type stateSecondChance struct {
	cap   int
	keys  []int
	vis   []bool
}

func newStateSC(cap int) *stateSecondChance { return &stateSecondChance{cap: cap} }
func (s *stateSecondChance) Access(key int) bool {
	for i, k := range s.keys {
		if k == key {
			s.vis[i] = true
			return true
		}
	}
	for len(s.keys) >= s.cap {
		last := len(s.keys) - 1
		tk := s.keys[last]
		tv := s.vis[last]
		s.keys = s.keys[:last]
		s.vis = s.vis[:last]
		if tv {
			s.keys = append([]int{tk}, s.keys...)
			s.vis = append([]bool{false}, s.vis...)
		} else {
			break
		}
	}
	s.keys = append([]int{key}, s.keys...)
	s.vis = append([]bool{false}, s.vis...)
	return false
}
func (s *stateSecondChance) Encode() string {
	var b strings.Builder
	for i := range s.keys {
		if i > 0 {
			b.WriteByte(',')
		}
		v := 0
		if s.vis[i] {
			v = 1
		}
		fmt.Fprintf(&b, "%d:%d", s.keys[i], v)
	}
	return b.String()
}
func (s *stateSecondChance) Clone() exactPolicy {
	ks := make([]int, len(s.keys))
	vs := make([]bool, len(s.vis))
	copy(ks, s.keys)
	copy(vs, s.vis)
	return &stateSecondChance{cap: s.cap, keys: ks, vis: vs}
}

// stateS3FIFO wraps S3-FIFO with slice queues (head=0, tail=len-1).
type stateS3FIFO struct {
	cap      int
	sCap     int
	mCap     int
	smallK   []int
	smallF   []int
	mainK    []int
	mainF    []int
	ghost    []int // oldest at 0, newest at len-1
}

func newStateS3FIFO(cap int) *stateS3FIFO {
	s := cap / 10
	if s < 1 {
		s = 1
	}
	return &stateS3FIFO{cap: cap, sCap: s, mCap: cap - s}
}
func (s *stateS3FIFO) addGhost(k int) {
	if s.mCap <= 0 {
		return
	}
	for len(s.ghost) >= s.mCap {
		s.ghost = s.ghost[1:]
	}
	s.ghost = append(s.ghost, k)
}
func (s *stateS3FIFO) evictMain() {
	for len(s.mainK) >= s.mCap && s.mCap > 0 {
		last := len(s.mainK) - 1
		k := s.mainK[last]
		f := s.mainF[last]
		s.mainK = s.mainK[:last]
		s.mainF = s.mainF[:last]
		if f > 0 {
			s.mainK = append([]int{k}, s.mainK...)
			s.mainF = append([]int{f - 1}, s.mainF...)
		} else {
			return
		}
	}
}
func (s *stateS3FIFO) insertMain(k int) {
	if s.mCap <= 0 {
		return
	}
	if len(s.mainK) >= s.mCap {
		s.evictMain()
	}
	s.mainK = append([]int{k}, s.mainK...)
	s.mainF = append([]int{0}, s.mainF...)
}
func (s *stateS3FIFO) evictSmall() {
	for len(s.smallK) >= s.sCap {
		last := len(s.smallK) - 1
		k := s.smallK[last]
		f := s.smallF[last]
		s.smallK = s.smallK[:last]
		s.smallF = s.smallF[:last]
		if f > 0 {
			s.insertMain(k)
		} else {
			s.addGhost(k)
			return
		}
	}
}
func (s *stateS3FIFO) Access(key int) bool {
	for i, k := range s.smallK {
		if k == key {
			if s.smallF[i] < 3 {
				s.smallF[i]++
			}
			return true
		}
	}
	for i, k := range s.mainK {
		if k == key {
			if s.mainF[i] < 3 {
				s.mainF[i]++
			}
			return true
		}
	}
	for i, gk := range s.ghost {
		if gk == key {
			s.ghost = append(s.ghost[:i], s.ghost[i+1:]...)
			if s.mCap > 0 {
				s.insertMain(key)
				return false
			}
		}
	}
	if len(s.smallK) >= s.sCap {
		s.evictSmall()
	}
	s.smallK = append([]int{key}, s.smallK...)
	s.smallF = append([]int{0}, s.smallF...)
	return false
}
func (s *stateS3FIFO) Encode() string {
	var b strings.Builder
	for i := range s.smallK {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "%d:%d", s.smallK[i], s.smallF[i])
	}
	b.WriteByte('|')
	for i := range s.mainK {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "%d:%d", s.mainK[i], s.mainF[i])
	}
	b.WriteByte('|')
	for i, gk := range s.ghost {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "%d", gk)
	}
	return b.String()
}
func (s *stateS3FIFO) Clone() exactPolicy {
	c := &stateS3FIFO{
		cap:    s.cap,
		sCap:   s.sCap,
		mCap:   s.mCap,
		smallK: append([]int(nil), s.smallK...),
		smallF: append([]int(nil), s.smallF...),
		mainK:  append([]int(nil), s.mainK...),
		mainF:  append([]int(nil), s.mainF...),
		ghost:  append([]int(nil), s.ghost...),
	}
	return c
}

func makeExact(name string, cap int) exactPolicy {
	switch name {
	case "FIFO":
		return newStateFIFO(cap)
	case "CLOCK":
		return newStateCLOCK(cap, 1)
	case "CLOCK2":
		return newStateCLOCK(cap, 2)
	case "SecondChance":
		return newStateSC(cap)
	case "S3FIFO":
		return newStateS3FIFO(cap)
	default:
		panic(name)
	}
}

// --- Exact Graph Analysis ---

type gEdge struct {
	to    int
	key   int
	missK int
	missC int
}

type ExactResult struct {
	Policy                string  `json:"policy"`
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
	CycleMissK            int     `json:"cycle_miss_k"`
	CycleMissC            int     `json:"cycle_miss_C"`
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

func runExactSearch(pol string, k, C, m, maxStates int) ExactResult {
	type pairState struct {
		sk exactPolicy
		sc exactPolicy
	}
	initP := pairState{makeExact(pol, k), makeExact(pol, C)}
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
		return ExactResult{
			Policy:       pol,
			K:            k,
			C:            C,
			UniverseSize: m,
			Complete:     false,
			States:       len(states),
		}
	}

	n := len(adj)
	if checkZeroMissK(adj) {
		return ExactResult{
			Policy:                pol,
			K:                     k,
			C:                     C,
			UniverseSize:          m,
			Complete:              true,
			States:                n,
			ZeroMissKWithPosMissC: true,
			MaxRatio:              math.Inf(1),
		}
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
		// Fallback to lo rational if rounding hit slightly low
		p, q = bestRationalApprox(hi, int64(n))
		pi, pos = computePotentials(adj, p, q)
		if pos {
			panic(fmt.Sprintf("%s (%d,%d,%d): cannot lock rational %d/%d", pol, k, C, m, p, q))
		}
	}

	tightAdj := make([][]gEdge, n)
	for u := 0; u < n; u++ {
		for _, e := range adj[u] {
			cost := p*int64(e.missK) - q*int64(e.missC)
			if pi[e.to] == pi[u]+cost {
				tightAdj[u] = append(tightAdj[u], e)
			}
		}
	}

	// Tarjan SCC on tightAdj
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
		for _, e := range tightAdj[v] {
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

	// Shortest prefix BFS
	prefDist := make([]int, n)
	prefParent := make([]int, n)
	prefKey := make([]int, n)
	for i := range prefDist {
		prefDist[i] = -1
		prefParent[i] = -1
	}
	prefDist[0] = 0
	bfsQ := []int{0}
	for h := 0; h < len(bfsQ); h++ {
		u := bfsQ[h]
		for _, e := range adj[u] {
			if prefDist[e.to] == -1 {
				prefDist[e.to] = prefDist[u] + 1
				prefParent[e.to] = u
				prefKey[e.to] = e.key
				bfsQ = append(bfsQ, e.to)
			}
		}
	}

	// Bucket sort nodes by prefDist
	maxPref := 0
	for _, d := range prefDist {
		if d > maxPref {
			maxPref = d
		}
	}
	buckets := make([][]int, maxPref+1)
	for u := 0; u < n; u++ {
		if prefDist[u] >= 0 {
			buckets[prefDist[u]] = append(buckets[prefDist[u]], u)
		}
	}

	bestCycleLen := math.MaxInt
	bestPrefLen := math.MaxInt
	var bestCycleEdges []gEdge
	bestStart := -1
	dist := make([]int, n)
	prevNode := make([]int, n)
	prevEdge := make([]gEdge, n)
	visStamp := make([]int, n)
	stamp := 0

	for d := 0; d <= maxPref; d++ {
		for _, u := range buckets[d] {
			for _, e0 := range tightAdj[u] {
				if e0.missK == 0 || sccID[u] != sccID[e0.to] {
					continue
				}
				if e0.to == u {
					if 1 < bestCycleLen || (1 == bestCycleLen && prefDist[u] < bestPrefLen) {
						bestCycleLen = 1
						bestPrefLen = prefDist[u]
						bestStart = u
						bestCycleEdges = []gEdge{e0}
					}
					continue
				}
				maxPathLen := bestCycleLen - 1
				if prefDist[u] >= bestPrefLen && bestCycleLen < math.MaxInt {
					maxPathLen = bestCycleLen - 2
				}
				if maxPathLen < 1 {
					continue
				}
				stamp++
				q := []int{e0.to}
				visStamp[e0.to] = stamp
				dist[e0.to] = 0
				found := false
				for h := 0; h < len(q); h++ {
					cur := q[h]
					if dist[cur] >= maxPathLen {
						break
					}
					for _, ne := range tightAdj[cur] {
						if sccID[ne.to] != sccID[u] {
							continue
						}
						if visStamp[ne.to] != stamp {
							visStamp[ne.to] = stamp
							dist[ne.to] = dist[cur] + 1
							prevNode[ne.to] = cur
							prevEdge[ne.to] = ne
							if ne.to == u {
								found = true
								break
							}
							if dist[ne.to] < maxPathLen {
								q = append(q, ne.to)
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
						bestStart = u
						rev := make([]gEdge, 0, dist[u])
						cur := u
						for cur != e0.to {
							rev = append(rev, prevEdge[cur])
							cur = prevNode[cur]
						}
						cyc := make([]gEdge, 0, cLen)
						cyc = append(cyc, e0)
						for i := len(rev) - 1; i >= 0; i-- {
							cyc = append(cyc, rev[i])
						}
						bestCycleEdges = cyc
					}
				}
			}
		}
	}

	var prefix, cycle []int
	cMK, cMC := 0, 0
	if bestStart >= 0 {
		revPref := []int{}
		cur := bestStart
		for cur != 0 {
			revPref = append(revPref, prefKey[cur])
			cur = prefParent[cur]
		}
		prefix = make([]int, len(revPref))
		for i := range revPref {
			prefix[i] = revPref[len(revPref)-1-i]
		}
		for _, e := range bestCycleEdges {
			cycle = append(cycle, e.key)
			cMK += e.missK
			cMC += e.missC
		}
	}

	return ExactResult{
		Policy:                pol,
		K:                     k,
		C:                     C,
		UniverseSize:          m,
		Complete:              true,
		States:                n,
		ZeroMissKWithPosMissC: false,
		MaxRatioNum:           int(p),
		MaxRatioDen:           int(q),
		MaxRatio:              float64(p) / float64(q),
		Prefix:                prefix,
		Cycle:                 cycle,
		CycleMissK:            cMK,
		CycleMissC:            cMC,
	}
}

// --- Evolutionary / Directed Trace Search ---

type candidate struct {
	prefix     []int
	cycle      []int
	steadyMK   int
	steadyMC   int
	steadyAcc  int
	score      float64
	ratio      float64
	missRateK  float64
	missRateC  float64
}

type EvoResult struct {
	Policy              string  `json:"policy"`
	K                   int     `json:"k"`
	C                   int     `json:"C"`
	FoundZeroMissK      bool    `json:"found_zero_miss_k_pos_C"`
	ZeroWitnessPrefix   []int   `json:"zero_witness_prefix,omitempty"`
	ZeroWitnessCycle    []int   `json:"zero_witness_cycle,omitempty"`
	ZeroWitnessRateC    float64 `json:"zero_witness_rate_C,omitempty"`
	BestRatio           float64 `json:"best_ratio"`
	BestMissRateK       float64 `json:"best_miss_rate_k"`
	BestMissRateC       float64 `json:"best_miss_rate_C"`
	BestPrefix          []int   `json:"best_prefix,omitempty"`
	BestCycle           []int   `json:"best_cycle,omitempty"`
	BestSteadyMissK     int     `json:"best_steady_miss_k"`
	BestSteadyMissC     int     `json:"best_steady_miss_C"`
	BestSteadyAccesses  int     `json:"best_steady_accesses"`
}

func evalCandidate(pol string, k, C int, prefix, cycle []int, totalCycles, steadyStart int) candidate {
	pk, _ := policies.New(pol, k)
	pC, _ := policies.New(pol, C)
	for _, x := range prefix {
		pk.Access(x)
		pC.Access(x)
	}
	smk, smc, sacc := 0, 0, 0
	for ci := 0; ci < totalCycles; ci++ {
		for _, x := range cycle {
			hk := pk.Access(x)
			hC := pC.Access(x)
			if ci >= steadyStart {
				if !hk {
					smk++
				}
				if !hC {
					smc++
				}
				sacc++
			}
		}
	}
	rk := float64(smk) / float64(sacc)
	rC := float64(smc) / float64(sacc)
	var ratio float64
	if smk == 0 {
		if smc > 0 {
			ratio = math.Inf(1)
		} else {
			ratio = 0
		}
	} else {
		ratio = float64(smc) / float64(smk)
	}
	return candidate{
		prefix:    prefix,
		cycle:     cycle,
		steadyMK:  smk,
		steadyMC:  smc,
		steadyAcc: sacc,
		ratio:     ratio,
		missRateK: rk,
		missRateC: rC,
	}
}

func mutateSlice(rng *rand.Rand, src []int, minLen, maxLen, maxKey int) []int {
	dst := append([]int(nil), src...)
	op := rng.Intn(5)
	switch op {
	case 0: // change one element
		if len(dst) > 0 {
			dst[rng.Intn(len(dst))] = rng.Intn(maxKey)
		}
	case 1: // insert element
		if len(dst) < maxLen {
			pos := rng.Intn(len(dst) + 1)
			dst = append(dst, 0)
			copy(dst[pos+1:], dst[pos:])
			dst[pos] = rng.Intn(maxKey)
		}
	case 2: // delete element
		if len(dst) > minLen {
			pos := rng.Intn(len(dst))
			dst = append(dst[:pos], dst[pos+1:]...)
		}
	case 3: // swap two elements
		if len(dst) >= 2 {
			i, j := rng.Intn(len(dst)), rng.Intn(len(dst))
			dst[i], dst[j] = dst[j], dst[i]
		}
	case 4: // block mutate / append
		if len(dst)+2 <= maxLen {
			dst = append(dst, rng.Intn(maxKey), rng.Intn(maxKey))
		} else if len(dst) > 0 {
			for t := 0; t < 2; t++ {
				dst[rng.Intn(len(dst))] = rng.Intn(maxKey)
			}
		}
	}
	return dst
}

func runEvoPair(pol string, k, C int, seed int64) EvoResult {
	rng := rand.New(rand.NewSource(seed))
	prefMaxKey := C + 3
	prefMaxLen := 3*C + 4
	if prefMaxLen < 12 {
		prefMaxLen = 12
	}

	// Phase 1: Search specifically for zero_miss_k (cycle keys in 0..k-1) & pos_miss_C
	var zeroWitness *candidate
	popSize := 48
	pop := make([]candidate, popSize)
	for i := 0; i < popSize; i++ {
		plen := k + rng.Intn(prefMaxLen-k+1)
		clen := 2 + rng.Intn(k*2+4)
		p := make([]int, plen)
		for j := range p {
			p[j] = rng.Intn(prefMaxKey)
		}
		c := make([]int, clen)
		for j := range c {
			c[j] = rng.Intn(k)
		}
		cand := evalCandidate(pol, k, C, p, c, 40, 20)
		// Score: heavily reward smc > 0 and smk == 0
		cand.score = float64(cand.steadyMC)*10.0 - float64(cand.steadyMK)*50.0
		pop[i] = cand
	}

	for gen := 0; gen < 120; gen++ {
		sort.Slice(pop, func(i, j int) bool { return pop[i].score > pop[j].score })
		if pop[0].steadyMK == 0 && pop[0].steadyMC > 0 {
			ver := evalCandidate(pol, k, C, pop[0].prefix, pop[0].cycle, 5000, 1000)
			if ver.steadyMK == 0 && ver.steadyMC > 0 {
				zeroWitness = &ver
				break
			}
		}
		next := make([]candidate, popSize)
		copy(next[:12], pop[:12])
		for i := 12; i < popSize; i++ {
			parent := pop[rng.Intn(12)]
			np := mutateSlice(rng, parent.prefix, 1, prefMaxLen, prefMaxKey)
			nc := mutateSlice(rng, parent.cycle, 2, k*3+6, k)
			cand := evalCandidate(pol, k, C, np, nc, 40, 20)
			cand.score = float64(cand.steadyMC)*10.0 - float64(cand.steadyMK)*50.0
			next[i] = cand
		}
		pop = next
	}

	// Phase 2: Search for maximum general Belady ratio miss(C)/miss(k)
	cycMaxKey := C + 3
	cycMaxLen := C*2 + 12
	if cycMaxLen < 32 {
		cycMaxLen = 32
	}
	pop2 := make([]candidate, popSize)
	for i := 0; i < popSize; i++ {
		plen := rng.Intn(prefMaxLen + 1)
		minCLen := 3
		if C >= 12 {
			minCLen = C - 2
		}
		clen := minCLen + rng.Intn(cycMaxLen-minCLen+1)
		p := make([]int, plen)
		for j := range p {
			p[j] = rng.Intn(prefMaxKey)
		}
		c := make([]int, clen)
		for j := range c {
			c[j] = rng.Intn(cycMaxKey)
		}
		cand := evalCandidate(pol, k, C, p, c, 45, 20)
		if cand.steadyMK == 0 {
			if cand.steadyMC > 0 {
				cand.score = 1e9
			} else {
				cand.score = 0
			}
		} else {
			cand.score = float64(cand.steadyMC) / float64(cand.steadyMK)
		}
		pop2[i] = cand
	}

	for gen := 0; gen < 180; gen++ {
		sort.Slice(pop2, func(i, j int) bool { return pop2[i].score > pop2[j].score })
		next := make([]candidate, popSize)
		copy(next[:12], pop2[:12])
		for i := 12; i < popSize; i++ {
			parent := pop2[rng.Intn(12)]
			np := mutateSlice(rng, parent.prefix, 0, prefMaxLen, prefMaxKey)
			nc := mutateSlice(rng, parent.cycle, 3, cycMaxLen, cycMaxKey)
			cand := evalCandidate(pol, k, C, np, nc, 45, 20)
			if cand.steadyMK == 0 {
				if cand.steadyMC > 0 {
					cand.score = 1e9
				} else {
					cand.score = 0
				}
			} else {
				cand.score = float64(cand.steadyMC) / float64(cand.steadyMK)
			}
			next[i] = cand
		}
		pop2 = next
	}
	sort.Slice(pop2, func(i, j int) bool { return pop2[i].score > pop2[j].score })

	// Verify best general candidate over 5000 cycles!
	bestVer := evalCandidate(pol, k, C, pop2[0].prefix, pop2[0].cycle, 5000, 1000)

	res := EvoResult{
		Policy:             pol,
		K:                  k,
		C:                  C,
		BestRatio:          bestVer.ratio,
		BestMissRateK:      bestVer.missRateK,
		BestMissRateC:      bestVer.missRateC,
		BestSteadyMissK:    bestVer.steadyMK,
		BestSteadyMissC:    bestVer.steadyMC,
		BestSteadyAccesses: bestVer.steadyAcc,
	}
	if bestVer.ratio > 1.0 {
		res.BestPrefix = bestVer.prefix
		res.BestCycle = bestVer.cycle
	}
	if zeroWitness != nil {
		res.FoundZeroMissK = true
		res.ZeroWitnessPrefix = zeroWitness.prefix
		res.ZeroWitnessCycle = zeroWitness.cycle
		res.ZeroWitnessRateC = zeroWitness.missRateC
	} else if bestVer.steadyMK == 0 && bestVer.steadyMC > 0 {
		res.FoundZeroMissK = true
		res.ZeroWitnessPrefix = bestVer.prefix
		res.ZeroWitnessCycle = bestVer.cycle
		res.ZeroWitnessRateC = bestVer.missRateC
		res.BestRatio = 999999 // JSON-safe sentinel for infinity
	}
	if math.IsInf(res.BestRatio, 1) {
		res.BestRatio = 999999
	}
	return res
}

func main() {
	outDir := flag.String("out", "../results", "output directory")
	flag.Parse()

	polNames := []string{"FIFO", "CLOCK", "CLOCK2", "SecondChance", "S3FIFO"}

	// 1. Exact small-state search
	exactCases := [][3]int{
		{2, 3, 4},
		{3, 4, 4},
		{3, 4, 5},
	}
	var exactResults []ExactResult
	fmt.Println("=== Experiment B1: Exact State-Graph Search ===")
	for _, pol := range polNames {
		for _, c := range exactCases {
			maxS := 150000
			if pol == "S3FIFO" && c[0] == 2 && c[1] == 3 {
				maxS = 600000
			}
			r := runExactSearch(pol, c[0], c[1], c[2], maxS)
			if r.Complete && math.IsInf(r.MaxRatio, 1) {
				r.MaxRatio = 999999
			}
			exactResults = append(exactResults, r)
			fmt.Printf("%-12s (%d,%d,m=%d): complete=%v states=%d zeroK_posC=%v max_ratio=%d/%d (%.4f)\n",
				r.Policy, r.K, r.C, r.UniverseSize, r.Complete, r.States, r.ZeroMissKWithPosMissC,
				r.MaxRatioNum, r.MaxRatioDen, r.MaxRatio)
		}
	}

	// 2. Evolutionary search across standard + S3FIFO boundary pairs
	stdPairs := [][2]int{
		{2, 3}, {3, 4}, {3, 5}, {3, 6}, {3, 10}, {4, 8}, {5, 10}, {6, 12},
	}
	s3ExtraPairs := [][2]int{
		{4, 5}, {8, 10}, {9, 10}, {9, 11}, {10, 11}, {10, 20}, {18, 20}, {19, 20}, {19, 21}, {4, 25}, {10, 25},
	}

	var evoResults []EvoResult
	fmt.Println("\n=== Experiment B2: Evolutionary Trace Search (5,000-cycle verified) ===")
	seed := int64(202602)
	for _, pol := range polNames {
		pairs := append([][2]int(nil), stdPairs...)
		if pol == "S3FIFO" {
			pairs = append(pairs, s3ExtraPairs...)
		}
		for _, pair := range pairs {
			seed++
			r := runEvoPair(pol, pair[0], pair[1], seed)
			evoResults = append(evoResults, r)
			if r.FoundZeroMissK || r.BestRatio > 1.05 {
				fmt.Printf("%-12s (%2d,%2d): zeroK_posC=%v best_ratio=%.4f (k=%.4f, C=%.4f, cyc_len=%d)\n",
					r.Policy, r.K, r.C, r.FoundZeroMissK, r.BestRatio, r.BestMissRateK, r.BestMissRateC, len(r.BestCycle))
			} else {
				fmt.Printf("%-12s (%2d,%2d): zeroK_posC=%v best_ratio=%.4f\n",
					r.Policy, r.K, r.C, r.FoundZeroMissK, r.BestRatio)
			}
		}
	}

	payload := map[string]any{
		"exact": exactResults,
		"evo":   evoResults,
	}
	bytes, _ := json.MarshalIndent(payload, "", "  ")
	if err := os.WriteFile(filepath.Join(*outDir, "atlas_search_go.json"), bytes, 0o644); err != nil {
		panic(err)
	}
}
