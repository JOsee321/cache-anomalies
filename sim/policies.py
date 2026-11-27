"""Reference implementations of 5 cache eviction algorithms for Stage 2 Atlas comparison.

Algorithms:
- FIFO
- CLOCK (1-bit circular buffer)
- CLOCK2 (2-bit frequency circular buffer, max freq = 2)
- SecondChance (FIFO with tail-to-head reinsertion on visited=True)
- S3FIFO (SOSP '23: Small=max(1, C//10), Main=C-S, Ghost=M, hit freq capped at 3)
"""

from __future__ import annotations

from collections import deque
from typing import Any, Deque, Dict, List, Optional, Tuple


class FIFOCache:
    """Classic FIFO cache."""

    def __init__(self, capacity: int) -> None:
        if capacity <= 0:
            raise ValueError(f"capacity must be positive, got {capacity}")
        self.capacity = capacity
        self.queue: Deque[Any] = deque()
        self.keys: set[Any] = set()

    def access(self, key: Any) -> bool:
        if key in self.keys:
            return True
        if len(self.queue) >= self.capacity:
            ev = self.queue.popleft()
            self.keys.remove(ev)
        self.queue.append(key)
        self.keys.add(key)
        return False


class CLOCKCache:
    """1-bit CLOCK circular buffer cache.

    New item enters directly at the evicted slot, then `hand` advances by 1.
    """

    def __init__(self, capacity: int) -> None:
        if capacity <= 0:
            raise ValueError(f"capacity must be positive, got {capacity}")
        self.capacity = capacity
        self.slots: List[Any] = []
        self.bits: List[int] = []
        self.pos: Dict[Any, int] = {}
        self.hand: int = 0

    def access(self, key: Any) -> bool:
        idx = self.pos.get(key)
        if idx is not None:
            self.bits[idx] = 1
            return True

        if len(self.slots) < self.capacity:
            idx = len(self.slots)
            self.slots.append(key)
            self.bits.append(0)
            self.pos[key] = idx
            return False

        while self.bits[self.hand] == 1:
            self.bits[self.hand] = 0
            self.hand = (self.hand + 1) % self.capacity

        ev_key = self.slots[self.hand]
        del self.pos[ev_key]
        self.slots[self.hand] = key
        self.bits[self.hand] = 0
        self.pos[key] = self.hand
        self.hand = (self.hand + 1) % self.capacity
        return False


class CLOCK2Cache:
    """2-bit frequency CLOCK cache (freq in 0..2)."""

    def __init__(self, capacity: int) -> None:
        if capacity <= 0:
            raise ValueError(f"capacity must be positive, got {capacity}")
        self.capacity = capacity
        self.slots: List[Any] = []
        self.freq: List[int] = []
        self.pos: Dict[Any, int] = {}
        self.hand: int = 0

    def access(self, key: Any) -> bool:
        idx = self.pos.get(key)
        if idx is not None:
            if self.freq[idx] < 2:
                self.freq[idx] += 1
            return True

        if len(self.slots) < self.capacity:
            idx = len(self.slots)
            self.slots.append(key)
            self.freq.append(0)
            self.pos[key] = idx
            return False

        while self.freq[self.hand] > 0:
            self.freq[self.hand] -= 1
            self.hand = (self.hand + 1) % self.capacity

        ev_key = self.slots[self.hand]
        del self.pos[ev_key]
        self.slots[self.hand] = key
        self.freq[self.hand] = 0
        self.pos[key] = self.hand
        self.hand = (self.hand + 1) % self.capacity
        return False


class _SCNode:
    __slots__ = ("key", "visited", "prev", "next")

    def __init__(self, key: Any) -> None:
        self.key: Any = key
        self.visited: bool = False
        self.prev: Optional[_SCNode] = None
        self.next: Optional[_SCNode] = None


class SecondChanceCache:
    """FIFO-Reinsertion (SecondChance) cache.

    - `head` is newest, `tail` is oldest.
    - Hit sets `visited = True` without moving node.
    - Eviction inspects `tail`: if `visited`, clears bit and moves `tail` to `head`;
      otherwise evicts `tail`. New item enters at `head`.
    """

    def __init__(self, capacity: int) -> None:
        if capacity <= 0:
            raise ValueError(f"capacity must be positive, got {capacity}")
        self.capacity = capacity
        self.head: Optional[_SCNode] = None
        self.tail: Optional[_SCNode] = None
        self.table: Dict[Any, _SCNode] = {}

    def _insert_head(self, node: _SCNode) -> None:
        node.prev = None
        node.next = self.head
        if self.head is not None:
            self.head.prev = node
        self.head = node
        if self.tail is None:
            self.tail = node

    def _pop_tail(self) -> _SCNode:
        node = self.tail
        assert node is not None
        if node.prev is not None:
            node.prev.next = None
            self.tail = node.prev
        else:
            self.head = None
            self.tail = None
        node.prev = None
        node.next = None
        return node

    def access(self, key: Any) -> bool:
        node = self.table.get(key)
        if node is not None:
            node.visited = True
            return True

        while len(self.table) >= self.capacity:
            t = self._pop_tail()
            if t.visited:
                t.visited = False
                self._insert_head(t)
            else:
                del self.table[t.key]
                break

        new_node = _SCNode(key)
        self._insert_head(new_node)
        self.table[key] = new_node
        return False


class _S3Node:
    __slots__ = ("key", "freq", "in_main", "prev", "next")

    def __init__(self, key: Any, in_main: bool) -> None:
        self.key: Any = key
        self.freq: int = 0
        self.in_main: bool = in_main
        self.prev: Optional[_S3Node] = None
        self.next: Optional[_S3Node] = None


class _FIFOQueue:
    """Explicit doubly-linked FIFO queue (head = newest, tail = oldest)."""

    def __init__(self) -> None:
        self.head: Optional[_S3Node] = None
        self.tail: Optional[_S3Node] = None
        self.size: int = 0

    def push_head(self, node: _S3Node) -> None:
        node.prev = None
        node.next = self.head
        if self.head is not None:
            self.head.prev = node
        self.head = node
        if self.tail is None:
            self.tail = node
        self.size += 1

    def pop_tail(self) -> _S3Node:
        node = self.tail
        assert node is not None
        if node.prev is not None:
            node.prev.next = None
            self.tail = node.prev
        else:
            self.head = None
            self.tail = None
        node.prev = None
        node.next = None
        self.size -= 1
        return node


class S3FIFOCache:
    """S3-FIFO (SOSP '23) cache implementation.

    - Small cap S = max(1, C // 10)
    - Main cap  M = C - S
    - Ghost cap G = M (keys only, FIFO)
    """

    def __init__(self, capacity: int) -> None:
        if capacity <= 0:
            raise ValueError(f"capacity must be positive, got {capacity}")
        self.capacity = capacity
        self.small_cap = max(1, capacity // 10)
        self.main_cap = capacity - self.small_cap
        self.ghost_cap = self.main_cap

        self.small = _FIFOQueue()
        self.main = _FIFOQueue()
        self.ghost_q: Deque[Any] = deque()
        self.ghost_set: set[Any] = set()
        self.table: Dict[Any, _S3Node] = {}

    def _add_ghost(self, key: Any) -> None:
        if self.ghost_cap <= 0:
            return
        while len(self.ghost_q) >= self.ghost_cap:
            old = self.ghost_q.popleft()
            self.ghost_set.discard(old)
        self.ghost_q.append(key)
        self.ghost_set.add(key)

    def _evict_main(self) -> None:
        while self.main.size >= self.main_cap and self.main_cap > 0:
            node = self.main.pop_tail()
            if node.freq > 0:
                node.freq -= 1
                self.main.push_head(node)
            else:
                del self.table[node.key]
                return

    def _insert_main(self, node: _S3Node) -> None:
        if self.main_cap <= 0:
            del self.table[node.key]
            return
        if self.main.size >= self.main_cap:
            self._evict_main()
        node.in_main = True
        node.freq = 0
        self.main.push_head(node)

    def _evict_small(self) -> None:
        while self.small.size >= self.small_cap:
            node = self.small.pop_tail()
            if node.freq > 0:
                self._insert_main(node)
            else:
                del self.table[node.key]
                self._add_ghost(node.key)
                return

    def access(self, key: Any) -> bool:
        node = self.table.get(key)
        if node is not None:
            if node.freq < 3:
                node.freq += 1
            return True

        if key in self.ghost_set:
            self.ghost_set.remove(key)
            self.ghost_q.remove(key)
            if self.main_cap > 0:
                new_node = _S3Node(key, in_main=True)
                self.table[key] = new_node
                self._insert_main(new_node)
                return False
            # Fallback if main_cap == 0 (C == 1)
        if self.small.size >= self.small_cap:
            self._evict_small()

        new_node = _S3Node(key, in_main=False)
        self.table[key] = new_node
        self.small.push_head(new_node)
        return False


class _LRUNode:
    __slots__ = ("key", "prev", "next")

    def __init__(self, key: Any) -> None:
        self.key: Any = key
        self.prev: Optional[_LRUNode] = None
        self.next: Optional[_LRUNode] = None


class LRUCache:
    """Exact Least Recently Used cache using explicit DLL + hash map."""

    def __init__(self, capacity: int) -> None:
        if capacity <= 0:
            raise ValueError(f"capacity must be positive, got {capacity}")
        self.capacity = capacity
        self.head: Optional[_LRUNode] = None
        self.tail: Optional[_LRUNode] = None
        self.table: Dict[Any, _LRUNode] = {}

    def _insert_head(self, node: _LRUNode) -> None:
        node.prev = None
        node.next = self.head
        if self.head is not None:
            self.head.prev = node
        self.head = node
        if self.tail is None:
            self.tail = node

    def _remove(self, node: _LRUNode) -> None:
        if node.prev is not None:
            node.prev.next = node.next
        else:
            self.head = node.next
        if node.next is not None:
            node.next.prev = node.prev
        else:
            self.tail = node.prev
        node.prev = None
        node.next = None

    def access(self, key: Any) -> bool:
        node = self.table.get(key)
        if node is not None:
            self._remove(node)
            self._insert_head(node)
            return True
        if len(self.table) >= self.capacity:
            assert self.tail is not None
            ev = self.tail
            self._remove(ev)
            del self.table[ev.key]
        new_node = _LRUNode(key)
        self._insert_head(new_node)
        self.table[key] = new_node
        return False


class _SieveAdapter:
    """Adapter exposing `.access(key) -> bool` for SieveCache."""

    def __init__(self, capacity: int) -> None:
        from sim.sieve import SieveCache

        self._cache = SieveCache(capacity)

    def access(self, key: Any) -> bool:
        hit, _ = self._cache.access(key)
        return hit


class _SieveBWAdapter:
    """Adapter exposing `.access(key) -> bool` for SieveBWCache."""

    def __init__(self, capacity: int, wrap_limit: Optional[int] = None) -> None:
        from sim.sieve import SieveBWCache

        self._cache = SieveBWCache(capacity, wrap_limit=wrap_limit)

    def access(self, key: Any) -> bool:
        hit, _ = self._cache.access(key)
        return hit


def make_cache(name: str, capacity: int) -> Any:
    """Factory for all comparison policies plus SIEVE and SIEVE-BW."""
    key = name.upper()
    if key == "FIFO":
        return FIFOCache(capacity)
    if key == "LRU":
        return LRUCache(capacity)
    if key == "CLOCK":
        return CLOCKCache(capacity)
    if key == "CLOCK2":
        return CLOCK2Cache(capacity)
    if key in ("SECONDCHANCE", "SECOND_CHANCE"):
        return SecondChanceCache(capacity)
    if key == "S3FIFO":
        return S3FIFOCache(capacity)
    if key == "SIEVE":
        return _SieveAdapter(capacity)
    if key in ("SIEVE-BW", "SIEVE_BW", "SIEVEBW"):
        return _SieveBWAdapter(capacity, wrap_limit=capacity)
    if key in ("SIEVE-BW2", "SIEVE_BW2", "SIEVEBW2"):
        return _SieveBWAdapter(capacity, wrap_limit=2 * capacity)
    raise ValueError(f"Unknown policy: {name}")
