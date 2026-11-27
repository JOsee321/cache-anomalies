package sieve

import "fmt"

// Node represents an entry in SIEVE's explicit doubly-linked list.
// Head is the newest entry (Prev == nil); Tail is the oldest entry (Next == nil).
type Node struct {
	Key     int
	Visited bool
	Prev    *Node
	Next    *Node
}

// Cache implements the SIEVE eviction algorithm (Zhang et al., NSDI '24)
// using explicit Head, Tail, and Hand pointers.
type Cache struct {
	Capacity               int
	InsertBeforeHandUpdate bool
	Head                   *Node
	Tail                   *Node
	Hand                   *Node
	Items                  map[int]*Node
}

// NewCache creates a SIEVE cache with the given positive capacity.
func NewCache(capacity int, insertBeforeHandUpdate bool) (*Cache, error) {
	if capacity <= 0 {
		return nil, fmt.Errorf("capacity must be positive, got %d", capacity)
	}
	return &Cache{
		Capacity:               capacity,
		InsertBeforeHandUpdate: insertBeforeHandUpdate,
		Items:                  make(map[int]*Node, capacity),
	}, nil
}

func (c *Cache) insertHead(n *Node) {
	n.Prev = nil
	n.Next = c.Head
	if c.Head != nil {
		c.Head.Prev = n
	}
	c.Head = n
	if c.Tail == nil {
		c.Tail = n
	}
}

func (c *Cache) removeNode(n *Node) {
	if n.Prev != nil {
		n.Prev.Next = n.Next
	} else {
		c.Head = n.Next
	}
	if n.Next != nil {
		n.Next.Prev = n.Prev
	} else {
		c.Tail = n.Prev
	}
	n.Prev = nil
	n.Next = nil
}

func (c *Cache) evict(incoming *Node) int {
	o := c.Hand
	if o == nil {
		o = c.Tail
	}
	for o.Visited {
		o.Visited = false
		if o.Prev != nil {
			o = o.Prev
		} else {
			o = c.Tail
		}
	}

	if c.InsertBeforeHandUpdate && incoming != nil {
		c.insertHead(incoming)
		c.Hand = o.Prev
		c.removeNode(o)
	} else {
		c.Hand = o.Prev
		c.removeNode(o)
	}
	delete(c.Items, o.Key)
	return o.Key
}

// Access processes a single key request and returns (hit, evictedKey, hasEvicted).
func (c *Cache) Access(key int) (bool, int, bool) {
	if n, ok := c.Items[key]; ok {
		n.Visited = true
		return true, 0, false
	}

	n := &Node{Key: key, Visited: false}
	var evKey int
	var hasEvicted bool

	if len(c.Items) >= c.Capacity {
		if c.InsertBeforeHandUpdate {
			evKey = c.evict(n)
			hasEvicted = true
			c.Items[key] = n
			return false, evKey, hasEvicted
		}
		evKey = c.evict(nil)
		hasEvicted = true
	}

	c.insertHead(n)
	c.Items[key] = n
	return false, evKey, hasEvicted
}

// KeysHeadToTail returns the cached keys from Head (newest) to Tail (oldest).
func (c *Cache) KeysHeadToTail() []int {
	keys := make([]int, 0, len(c.Items))
	for curr := c.Head; curr != nil; curr = curr.Next {
		keys = append(keys, curr.Key)
	}
	return keys
}

// AssertInvariants checks structural integrity of the doubly-linked list.
func (c *Cache) AssertInvariants() error {
	if len(c.Items) > c.Capacity {
		return fmt.Errorf("len(Items)=%d > capacity=%d", len(c.Items), c.Capacity)
	}
	if len(c.Items) == 0 {
		if c.Head != nil || c.Tail != nil || c.Hand != nil {
			return fmt.Errorf("empty cache has non-nil pointers")
		}
		return nil
	}
	if c.Head == nil || c.Head.Prev != nil {
		return fmt.Errorf("invalid Head pointer")
	}
	if c.Tail == nil || c.Tail.Next != nil {
		return fmt.Errorf("invalid Tail pointer")
	}
	count := 0
	handFound := c.Hand == nil
	var prev *Node
	for curr := c.Head; curr != nil; curr = curr.Next {
		if curr.Prev != prev {
			return fmt.Errorf("broken Prev link at key %d", curr.Key)
		}
		if c.Items[curr.Key] != curr {
			return fmt.Errorf("map mismatch at key %d", curr.Key)
		}
		if curr == c.Hand {
			handFound = true
		}
		prev = curr
		count++
	}
	if prev != c.Tail {
		return fmt.Errorf("Tail mismatch")
	}
	if count != len(c.Items) {
		return fmt.Errorf("list count %d != map len %d", count, len(c.Items))
	}
	if !handFound {
		return fmt.Errorf("Hand points outside list")
	}
	return nil
}

// BWCache implements Bounded-Wrap SIEVE (SIEVE-BW).
// It adds one integer counter EvictsSinceWrap at the cache level (zero per-node overhead).
type BWCache struct {
	Capacity        int
	WrapLimit       int
	Head            *Node
	Tail            *Node
	Hand            *Node
	Items           map[int]*Node
	EvictsSinceWrap int
	ForcedWraps     int
}

// NewBWCache creates a SIEVE-BW cache. If wrapLimit <= 0, defaults to capacity.
func NewBWCache(capacity, wrapLimit int) (*BWCache, error) {
	if capacity <= 0 {
		return nil, fmt.Errorf("capacity must be positive, got %d", capacity)
	}
	if wrapLimit <= 0 {
		wrapLimit = capacity
	}
	return &BWCache{
		Capacity:  capacity,
		WrapLimit: wrapLimit,
		Items:     make(map[int]*Node, capacity),
	}, nil
}

func (c *BWCache) insertHead(n *Node) {
	n.Prev = nil
	n.Next = c.Head
	if c.Head != nil {
		c.Head.Prev = n
	}
	c.Head = n
	if c.Tail == nil {
		c.Tail = n
	}
}

func (c *BWCache) removeNode(n *Node) {
	if n.Prev != nil {
		n.Prev.Next = n.Next
	} else {
		c.Head = n.Next
	}
	if n.Next != nil {
		n.Next.Prev = n.Prev
	} else {
		c.Tail = n.Prev
	}
	n.Prev = nil
	n.Next = nil
}

func (c *BWCache) evict() int {
	o := c.Hand
	if o == nil {
		o = c.Tail
		c.EvictsSinceWrap = 0
	}
	for o.Visited {
		o.Visited = false
		if o.Prev != nil {
			o = o.Prev
		} else {
			o = c.Tail
			c.EvictsSinceWrap = 0
		}
	}

	c.Hand = o.Prev
	c.removeNode(o)
	delete(c.Items, o.Key)

	if c.Hand == nil {
		c.EvictsSinceWrap = 0
	} else {
		c.EvictsSinceWrap++
		if c.EvictsSinceWrap >= c.WrapLimit {
			c.Hand = nil
			c.EvictsSinceWrap = 0
			c.ForcedWraps++
		}
	}
	return o.Key
}

// Access processes a single key request and returns (hit, evictedKey, hasEvicted).
func (c *BWCache) Access(key int) (bool, int, bool) {
	if n, ok := c.Items[key]; ok {
		n.Visited = true
		return true, 0, false
	}

	n := &Node{Key: key, Visited: false}
	var evKey int
	var hasEvicted bool
	if len(c.Items) >= c.Capacity {
		evKey = c.evict()
		hasEvicted = true
	}
	c.insertHead(n)
	c.Items[key] = n
	return false, evKey, hasEvicted
}

// TPCache implements Periodic Tail-Probe SIEVE (SIEVE-TP).
// Every ProbeInterval evictions without a natural wrap, it inspects Tail:
// - If !Tail.Visited: evicts Tail directly without disturbing Hand (unless Hand == Tail).
// - If Tail.Visited: clears Tail.Visited, moves Tail to Head, and proceeds with normal Hand eviction.
type TPCache struct {
	Capacity         int
	ProbeInterval    int
	Head             *Node
	Tail             *Node
	Hand             *Node
	Items            map[int]*Node
	EvictsSinceWrap  int
	TailProbes       int
	TailDirectEvicts int
}

// NewTPCache creates a SIEVE-TP cache with ProbeInterval clamped to >= 1.
func NewTPCache(capacity, probeInterval int) (*TPCache, error) {
	if capacity <= 0 {
		return nil, fmt.Errorf("capacity must be positive, got %d", capacity)
	}
	if probeInterval < 1 {
		probeInterval = 1
	}
	return &TPCache{
		Capacity:      capacity,
		ProbeInterval: probeInterval,
		Items:         make(map[int]*Node, capacity),
	}, nil
}

func (c *TPCache) insertHead(n *Node) {
	n.Prev = nil
	n.Next = c.Head
	if c.Head != nil {
		c.Head.Prev = n
	}
	c.Head = n
	if c.Tail == nil {
		c.Tail = n
	}
}

func (c *TPCache) removeNode(n *Node) {
	if n.Prev != nil {
		n.Prev.Next = n.Next
	} else {
		c.Head = n.Next
	}
	if n.Next != nil {
		n.Next.Prev = n.Prev
	} else {
		c.Tail = n.Prev
	}
	n.Prev = nil
	n.Next = nil
}

func (c *TPCache) evict() int {
	if c.EvictsSinceWrap >= c.ProbeInterval {
		c.EvictsSinceWrap = 0
		c.TailProbes++
		t := c.Tail
		if !t.Visited {
			if c.Hand == t {
				c.Hand = t.Prev
			}
			c.removeNode(t)
			delete(c.Items, t.Key)
			c.TailDirectEvicts++
			return t.Key
		}
		t.Visited = false
		if c.Capacity > 1 {
			if c.Hand == t {
				c.Hand = t.Prev
			}
			c.removeNode(t)
			c.insertHead(t)
		}
	}

	o := c.Hand
	if o == nil {
		o = c.Tail
		c.EvictsSinceWrap = 0
	}
	for o.Visited {
		o.Visited = false
		if o.Prev != nil {
			o = o.Prev
		} else {
			o = c.Tail
			c.EvictsSinceWrap = 0
		}
	}

	c.Hand = o.Prev
	c.removeNode(o)
	delete(c.Items, o.Key)

	if c.Hand == nil {
		c.EvictsSinceWrap = 0
	} else {
		c.EvictsSinceWrap++
	}
	return o.Key
}

// Access processes a single key request and returns (hit, evictedKey, hasEvicted).
func (c *TPCache) Access(key int) (bool, int, bool) {
	if n, ok := c.Items[key]; ok {
		n.Visited = true
		return true, 0, false
	}

	n := &Node{Key: key, Visited: false}
	var evKey int
	var hasEvicted bool
	if len(c.Items) >= c.Capacity {
		evKey = c.evict()
		hasEvicted = true
	}
	c.insertHead(n)
	c.Items[key] = n
	return false, evKey, hasEvicted
}

// AssertInvariants checks structural integrity of the doubly-linked list.
func (c *TPCache) AssertInvariants() error {
	if len(c.Items) > c.Capacity {
		return fmt.Errorf("len(Items)=%d > capacity=%d", len(c.Items), c.Capacity)
	}
	if len(c.Items) == 0 {
		if c.Head != nil || c.Tail != nil || c.Hand != nil {
			return fmt.Errorf("empty cache has non-nil pointers")
		}
		return nil
	}
	if c.Head == nil || c.Head.Prev != nil {
		return fmt.Errorf("invalid Head pointer")
	}
	if c.Tail == nil || c.Tail.Next != nil {
		return fmt.Errorf("invalid Tail pointer")
	}
	count := 0
	handFound := c.Hand == nil
	var prev *Node
	for curr := c.Head; curr != nil; curr = curr.Next {
		if curr.Prev != prev {
			return fmt.Errorf("broken Prev link at key %d", curr.Key)
		}
		if c.Items[curr.Key] != curr {
			return fmt.Errorf("map mismatch at key %d", curr.Key)
		}
		if curr == c.Hand {
			handFound = true
		}
		prev = curr
		count++
	}
	if prev != c.Tail {
		return fmt.Errorf("Tail mismatch")
	}
	if count != len(c.Items) {
		return fmt.Errorf("list count %d != map len %d", count, len(c.Items))
	}
	if !handFound {
		return fmt.Errorf("Hand points outside list")
	}
	return nil
}
