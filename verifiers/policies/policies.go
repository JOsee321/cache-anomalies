package policies

import "fmt"

// Policy is the common interface for all cache eviction algorithms.
type Policy interface {
	Access(key int) bool
	Cap() int
	Len() int
}

// New creates a cache policy by name: FIFO, CLOCK, CLOCK2, SecondChance, S3FIFO.
func New(name string, capacity int) (Policy, error) {
	if capacity <= 0 {
		return nil, fmt.Errorf("capacity must be positive, got %d", capacity)
	}
	switch name {
	case "FIFO":
		return NewFIFO(capacity), nil
	case "LRU":
		return NewLRU(capacity), nil
	case "CLOCK":
		return NewCLOCK(capacity), nil
	case "CLOCK2":
		return NewCLOCK2(capacity), nil
	case "SecondChance":
		return NewSecondChance(capacity), nil
	case "S3FIFO":
		return NewS3FIFO(capacity), nil
	default:
		return nil, fmt.Errorf("unknown policy %q", name)
	}
}

type lruNode struct {
	key  int
	prev *lruNode
	next *lruNode
}

// LRU implements exact Least Recently Used replacement.
type LRU struct {
	capacity int
	head     *lruNode
	tail     *lruNode
	items    map[int]*lruNode
}

func NewLRU(capacity int) *LRU {
	return &LRU{
		capacity: capacity,
		items:    make(map[int]*lruNode, capacity),
	}
}

func (c *LRU) Cap() int { return c.capacity }
func (c *LRU) Len() int { return len(c.items) }

func (c *LRU) insertHead(n *lruNode) {
	n.prev = nil
	n.next = c.head
	if c.head != nil {
		c.head.prev = n
	}
	c.head = n
	if c.tail == nil {
		c.tail = n
	}
}

func (c *LRU) remove(n *lruNode) {
	if n.prev != nil {
		n.prev.next = n.next
	} else {
		c.head = n.next
	}
	if n.next != nil {
		n.next.prev = n.prev
	} else {
		c.tail = n.prev
	}
	n.prev = nil
	n.next = nil
}

func (c *LRU) Access(key int) bool {
	if n, ok := c.items[key]; ok {
		c.remove(n)
		c.insertHead(n)
		return true
	}
	if len(c.items) >= c.capacity {
		ev := c.tail
		c.remove(ev)
		delete(c.items, ev.key)
	}
	n := &lruNode{key: key}
	c.insertHead(n)
	c.items[key] = n
	return false
}

// FIFO implements classic First-In, First-Out caching.
type FIFO struct {
	capacity int
	queue    []int
	items    map[int]struct{}
}

func NewFIFO(capacity int) *FIFO {
	return &FIFO{
		capacity: capacity,
		queue:    make([]int, 0, capacity),
		items:    make(map[int]struct{}, capacity),
	}
}

func (c *FIFO) Cap() int { return c.capacity }
func (c *FIFO) Len() int { return len(c.items) }

func (c *FIFO) Access(key int) bool {
	if _, ok := c.items[key]; ok {
		return true
	}
	if len(c.queue) >= c.capacity {
		ev := c.queue[0]
		c.queue = c.queue[1:]
		delete(c.items, ev)
	}
	c.queue = append(c.queue, key)
	c.items[key] = struct{}{}
	return false
}

// CLOCK implements 1-bit circular buffer CLOCK.
type CLOCK struct {
	capacity int
	slots    []int
	bits     []uint8
	pos      map[int]int
	hand     int
}

func NewCLOCK(capacity int) *CLOCK {
	return &CLOCK{
		capacity: capacity,
		slots:    make([]int, 0, capacity),
		bits:     make([]uint8, 0, capacity),
		pos:      make(map[int]int, capacity),
	}
}

func (c *CLOCK) Cap() int { return c.capacity }
func (c *CLOCK) Len() int { return len(c.pos) }

func (c *CLOCK) Access(key int) bool {
	if idx, ok := c.pos[key]; ok {
		c.bits[idx] = 1
		return true
	}
	if len(c.slots) < c.capacity {
		idx := len(c.slots)
		c.slots = append(c.slots, key)
		c.bits = append(c.bits, 0)
		c.pos[key] = idx
		return false
	}
	for c.bits[c.hand] == 1 {
		c.bits[c.hand] = 0
		c.hand++
		if c.hand == c.capacity {
			c.hand = 0
		}
	}
	ev := c.slots[c.hand]
	delete(c.pos, ev)
	c.slots[c.hand] = key
	c.bits[c.hand] = 0
	c.pos[key] = c.hand
	c.hand++
	if c.hand == c.capacity {
		c.hand = 0
	}
	return false
}

// CLOCK2 implements 2-bit frequency CLOCK (max freq = 2).
type CLOCK2 struct {
	capacity int
	slots    []int
	freq     []uint8
	pos      map[int]int
	hand     int
}

func NewCLOCK2(capacity int) *CLOCK2 {
	return &CLOCK2{
		capacity: capacity,
		slots:    make([]int, 0, capacity),
		freq:     make([]uint8, 0, capacity),
		pos:      make(map[int]int, capacity),
	}
}

func (c *CLOCK2) Cap() int { return c.capacity }
func (c *CLOCK2) Len() int { return len(c.pos) }

func (c *CLOCK2) Access(key int) bool {
	if idx, ok := c.pos[key]; ok {
		if c.freq[idx] < 2 {
			c.freq[idx]++
		}
		return true
	}
	if len(c.slots) < c.capacity {
		idx := len(c.slots)
		c.slots = append(c.slots, key)
		c.freq = append(c.freq, 0)
		c.pos[key] = idx
		return false
	}
	for c.freq[c.hand] > 0 {
		c.freq[c.hand]--
		c.hand++
		if c.hand == c.capacity {
			c.hand = 0
		}
	}
	ev := c.slots[c.hand]
	delete(c.pos, ev)
	c.slots[c.hand] = key
	c.freq[c.hand] = 0
	c.pos[key] = c.hand
	c.hand++
	if c.hand == c.capacity {
		c.hand = 0
	}
	return false
}

type scNode struct {
	key     int
	visited bool
	prev    *scNode
	next    *scNode
}

// SecondChance implements FIFO-Reinsertion (moves visited tail node to head).
type SecondChance struct {
	capacity int
	head     *scNode
	tail     *scNode
	items    map[int]*scNode
}

func NewSecondChance(capacity int) *SecondChance {
	return &SecondChance{
		capacity: capacity,
		items:    make(map[int]*scNode, capacity),
	}
}

func (c *SecondChance) Cap() int { return c.capacity }
func (c *SecondChance) Len() int { return len(c.items) }

func (c *SecondChance) insertHead(n *scNode) {
	n.prev = nil
	n.next = c.head
	if c.head != nil {
		c.head.prev = n
	}
	c.head = n
	if c.tail == nil {
		c.tail = n
	}
}

func (c *SecondChance) popTail() *scNode {
	n := c.tail
	if n.prev != nil {
		n.prev.next = nil
		c.tail = n.prev
	} else {
		c.head = nil
		c.tail = nil
	}
	n.prev = nil
	n.next = nil
	return n
}

func (c *SecondChance) Access(key int) bool {
	if n, ok := c.items[key]; ok {
		n.visited = true
		return true
	}
	for len(c.items) >= c.capacity {
		t := c.popTail()
		if t.visited {
			t.visited = false
			c.insertHead(t)
		} else {
			delete(c.items, t.key)
			break
		}
	}
	n := &scNode{key: key}
	c.insertHead(n)
	c.items[key] = n
	return false
}

type s3Node struct {
	key    int
	freq   uint8
	inMain bool
	prev   *s3Node
	next   *s3Node
}

type fifoDLL struct {
	head *s3Node
	tail *s3Node
	size int
}

func (q *fifoDLL) pushHead(n *s3Node) {
	n.prev = nil
	n.next = q.head
	if q.head != nil {
		q.head.prev = n
	}
	q.head = n
	if q.tail == nil {
		q.tail = n
	}
	q.size++
}

func (q *fifoDLL) popTail() *s3Node {
	n := q.tail
	if n.prev != nil {
		n.prev.next = nil
		q.tail = n.prev
	} else {
		q.head = nil
		q.tail = nil
	}
	n.prev = nil
	n.next = nil
	q.size--
	return n
}

// S3FIFO implements S3-FIFO (SOSP '23).
type S3FIFO struct {
	capacity int
	smallCap int
	mainCap  int
	ghostCap int

	small    fifoDLL
	main     fifoDLL
	ghostQ   []int
	ghostSet map[int]struct{}
	items    map[int]*s3Node
}

func NewS3FIFO(capacity int) *S3FIFO {
	sCap := capacity / 10
	if sCap < 1 {
		sCap = 1
	}
	mCap := capacity - sCap
	return &S3FIFO{
		capacity: capacity,
		smallCap: sCap,
		mainCap:  mCap,
		ghostCap: mCap,
		ghostQ:   make([]int, 0, mCap+1),
		ghostSet: make(map[int]struct{}, mCap+1),
		items:    make(map[int]*s3Node, capacity),
	}
}

func (c *S3FIFO) Cap() int { return c.capacity }
func (c *S3FIFO) Len() int { return len(c.items) }

func (c *S3FIFO) addGhost(key int) {
	if c.ghostCap <= 0 {
		return
	}
	for len(c.ghostQ) >= c.ghostCap {
		old := c.ghostQ[0]
		c.ghostQ = c.ghostQ[1:]
		delete(c.ghostSet, old)
	}
	c.ghostQ = append(c.ghostQ, key)
	c.ghostSet[key] = struct{}{}
}

func (c *S3FIFO) evictMain() {
	for c.main.size >= c.mainCap && c.mainCap > 0 {
		n := c.main.popTail()
		if n.freq > 0 {
			n.freq--
			c.main.pushHead(n)
		} else {
			delete(c.items, n.key)
			return
		}
	}
}

func (c *S3FIFO) insertMain(n *s3Node) {
	if c.mainCap <= 0 {
		delete(c.items, n.key)
		return
	}
	if c.main.size >= c.mainCap {
		c.evictMain()
	}
	n.inMain = true
	n.freq = 0
	c.main.pushHead(n)
}

func (c *S3FIFO) evictSmall() {
	for c.small.size >= c.smallCap {
		n := c.small.popTail()
		if n.freq > 0 {
			c.insertMain(n)
		} else {
			delete(c.items, n.key)
			c.addGhost(n.key)
			return
		}
	}
}

func (c *S3FIFO) Access(key int) bool {
	if n, ok := c.items[key]; ok {
		if n.freq < 3 {
			n.freq++
		}
		return true
	}

	if _, inGhost := c.ghostSet[key]; inGhost {
		delete(c.ghostSet, key)
		for i, gk := range c.ghostQ {
			if gk == key {
				c.ghostQ = append(c.ghostQ[:i], c.ghostQ[i+1:]...)
				break
			}
		}
		if c.mainCap > 0 {
			n := &s3Node{key: key, inMain: true}
			c.items[key] = n
			c.insertMain(n)
			return false
		}
	}

	if c.small.size >= c.smallCap {
		c.evictSmall()
	}
	n := &s3Node{key: key, inMain: false}
	c.items[key] = n
	c.small.pushHead(n)
	return false
}
