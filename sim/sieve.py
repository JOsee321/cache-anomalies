"""Explicit doubly-linked list reference implementation of SIEVE (Zhang et al., NSDI '24).

Pointer semantics match the C specification 1:1:
- `head` is the newest node (`head.prev is None`).
- `tail` is the oldest node (`tail.next is None`).
- `prev` points toward `head`; `next` points toward `tail`.
- `hand` starts at `None`; on eviction, if `hand is None`, scanning starts at `tail`.
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import Any, Dict, Iterable, List, Optional, Tuple


class Node:
    """Doubly-linked list node for SIEVE."""

    __slots__ = ("key", "visited", "prev", "next")

    def __init__(self, key: Any) -> None:
        self.key: Any = key
        self.visited: bool = False
        self.prev: Optional[Node] = None
        self.next: Optional[Node] = None


@dataclass
class TraceStats:
    """Summary statistics for a trace or prefix+cycle run."""

    capacity: int
    total_accesses: int
    total_misses: int
    prefix_misses: int
    cycle_misses: int
    steady_state_misses: int
    steady_state_accesses: int
    final_keys: List[Any]
    evicted_keys: set[Any]

    @property
    def steady_state_miss_rate(self) -> float:
        if self.steady_state_accesses == 0:
            return 0.0
        return self.steady_state_misses / self.steady_state_accesses


class SieveCache:
    """Reference SIEVE cache using an explicit doubly-linked list."""

    def __init__(self, capacity: int, insert_before_hand_update: bool = False) -> None:
        if capacity <= 0:
            raise ValueError(f"capacity must be positive, got {capacity}")
        self.capacity: int = capacity
        self.insert_before_hand_update: bool = insert_before_hand_update
        self.head: Optional[Node] = None
        self.tail: Optional[Node] = None
        self.hand: Optional[Node] = None
        self.table: Dict[Any, Node] = {}

    def _insert_head(self, node: Node) -> None:
        """Insert a node at the head (newest position) of the list."""
        node.prev = None
        node.next = self.head
        if self.head is not None:
            self.head.prev = node
        self.head = node
        if self.tail is None:
            self.tail = node

    def _remove_node(self, node: Node) -> None:
        """Unlink a node from the doubly-linked list."""
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

    def _evict(self, incoming_node: Optional[Node] = None) -> Any:
        """Evict one item using the SIEVE hand scan.

        If `insert_before_hand_update` is True and `incoming_node` is provided,
        simulates the edge-case variant where `insert(head)` happens before
        `hand = o.prev` (and before removing `o`).
        """
        o = self.hand if self.hand is not None else self.tail
        assert o is not None, "cannot evict from empty cache"

        while o.visited:
            o.visited = False
            o = o.prev if o.prev is not None else self.tail
            assert o is not None

        if self.insert_before_hand_update and incoming_node is not None:
            self._insert_head(incoming_node)
            self.hand = o.prev
            self._remove_node(o)
        else:
            self.hand = o.prev
            self._remove_node(o)

        del self.table[o.key]
        return o.key

    def access(self, key: Any) -> Tuple[bool, Optional[Any]]:
        """Process one cache request.

        Returns `(is_hit, evicted_key)`.
        """
        node = self.table.get(key)
        if node is not None:
            node.visited = True
            return True, None

        new_node = Node(key)
        new_node.visited = False
        evicted_key: Optional[Any] = None

        if len(self.table) >= self.capacity:
            if self.insert_before_hand_update:
                evicted_key = self._evict(incoming_node=new_node)
                self.table[key] = new_node
                return False, evicted_key
            evicted_key = self._evict()

        self._insert_head(new_node)
        self.table[key] = new_node
        return False, evicted_key

    def keys_head_to_tail(self) -> List[Any]:
        """Return cached keys ordered from head (newest) to tail (oldest)."""
        out: List[Any] = []
        curr = self.head
        while curr is not None:
            out.append(curr.key)
            curr = curr.next
        return out

    def state_snapshot(self) -> Tuple[Tuple[Tuple[Any, bool], ...], Optional[int]]:
        """Return an immutable snapshot `(((key, visited), ...), hand_index)`."""
        items: List[Tuple[Any, bool]] = []
        hand_idx: Optional[int] = None
        curr = self.head
        idx = 0
        while curr is not None:
            if curr is self.hand:
                hand_idx = idx
            items.append((curr.key, curr.visited))
            curr = curr.next
            idx += 1
        return tuple(items), hand_idx

    def assert_invariants(self) -> None:
        """Verify doubly-linked list structural invariants."""
        assert len(self.table) <= self.capacity
        if not self.table:
            assert self.head is None and self.tail is None and self.hand is None
            return

        assert self.head is not None and self.head.prev is None
        assert self.tail is not None and self.tail.next is None

        seen_forward: List[Node] = []
        curr = self.head
        prev: Optional[Node] = None
        while curr is not None:
            assert curr.prev is prev
            assert self.table.get(curr.key) is curr
            seen_forward.append(curr)
            prev = curr
            curr = curr.next

        assert prev is self.tail
        assert len(seen_forward) == len(self.table)
        if self.hand is not None:
            assert self.hand in seen_forward


class SieveBWCache:
    """Bounded-Wrap SIEVE (SIEVE-BW) mitigation.

    Adds a single integer counter `evicts_since_wrap` at the cache level
    (O(1) time, zero per-node memory overhead):
    - Whenever `hand` wraps back to `tail` (either because `hand` was `None`
      at eviction start or the scan wrapped from `head` to `tail`),
      `evicts_since_wrap` is reset to 0.
    - After each normal eviction, `evicts_since_wrap` increments by 1.
    - If `hand` becomes `None` (evicting `head`), `evicts_since_wrap` resets to 0.
    - If `evicts_since_wrap >= wrap_limit`, `hand` is forced to `None`
      (so the next eviction starts at `tail`) and `evicts_since_wrap` resets to 0.
    """

    def __init__(self, capacity: int, wrap_limit: Optional[int] = None) -> None:
        if capacity <= 0:
            raise ValueError(f"capacity must be positive, got {capacity}")
        self.capacity: int = capacity
        self.wrap_limit: int = capacity if wrap_limit is None else wrap_limit
        if self.wrap_limit <= 0:
            raise ValueError(f"wrap_limit must be positive, got {self.wrap_limit}")
        self.head: Optional[Node] = None
        self.tail: Optional[Node] = None
        self.hand: Optional[Node] = None
        self.table: Dict[Any, Node] = {}
        self.evicts_since_wrap: int = 0
        self.forced_wraps: int = 0

    def _insert_head(self, node: Node) -> None:
        node.prev = None
        node.next = self.head
        if self.head is not None:
            self.head.prev = node
        self.head = node
        if self.tail is None:
            self.tail = node

    def _remove_node(self, node: Node) -> None:
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

    def _evict(self) -> Any:
        o = self.hand
        if o is None:
            o = self.tail
            self.evicts_since_wrap = 0
        assert o is not None, "cannot evict from empty cache"

        while o.visited:
            o.visited = False
            if o.prev is not None:
                o = o.prev
            else:
                o = self.tail
                self.evicts_since_wrap = 0
                assert o is not None

        self.hand = o.prev
        self._remove_node(o)
        del self.table[o.key]

        if self.hand is None:
            self.evicts_since_wrap = 0
        else:
            self.evicts_since_wrap += 1
            if self.evicts_since_wrap >= self.wrap_limit:
                self.hand = None
                self.evicts_since_wrap = 0
                self.forced_wraps += 1
        return o.key

    def access(self, key: Any) -> Tuple[bool, Optional[Any]]:
        node = self.table.get(key)
        if node is not None:
            node.visited = True
            return True, None

        new_node = Node(key)
        new_node.visited = False
        evicted_key: Optional[Any] = None
        if len(self.table) >= self.capacity:
            evicted_key = self._evict()

        self._insert_head(new_node)
        self.table[key] = new_node
        return False, evicted_key

    def keys_head_to_tail(self) -> List[Any]:
        out: List[Any] = []
        curr = self.head
        while curr is not None:
            out.append(curr.key)
            curr = curr.next
        return out


class SieveTPCache:
    """Periodic Tail-Probe SIEVE (SIEVE-TP) surgical mitigation.

    Same structure as SIEVE plus one integer counter `evicts_since_wrap` and
    parameter `probe_interval` (K >= 1).
    When full and `evicts_since_wrap >= probe_interval`:
    - Reset `evicts_since_wrap = 0`.
    - If `tail.visited == False`: evict `tail` directly (updating `hand = tail.prev`
      only if `hand == tail`), insert new item at `head`, and return.
    - If `tail.visited == True`: set `tail.visited = False`, move `tail` to `head`
      (updating `hand = tail.prev` if `hand == tail`), then fall through to normal
      SIEVE eviction using `hand`.
    """

    def __init__(self, capacity: int, probe_interval: Optional[int] = None) -> None:
        if capacity <= 0:
            raise ValueError(f"capacity must be positive, got {capacity}")
        self.capacity: int = capacity
        self.probe_interval: int = max(1, capacity if probe_interval is None else probe_interval)
        self.head: Optional[Node] = None
        self.tail: Optional[Node] = None
        self.hand: Optional[Node] = None
        self.table: Dict[Any, Node] = {}
        self.evicts_since_wrap: int = 0
        self.tail_probes: int = 0
        self.tail_direct_evicts: int = 0

    def _insert_head(self, node: Node) -> None:
        node.prev = None
        node.next = self.head
        if self.head is not None:
            self.head.prev = node
        self.head = node
        if self.tail is None:
            self.tail = node

    def _remove_node(self, node: Node) -> None:
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

    def _evict(self) -> Any:
        if self.evicts_since_wrap >= self.probe_interval:
            self.evicts_since_wrap = 0
            self.tail_probes += 1
            t = self.tail
            assert t is not None
            if not t.visited:
                if self.hand is t:
                    self.hand = t.prev
                self._remove_node(t)
                del self.table[t.key]
                self.tail_direct_evicts += 1
                return t.key
            # t.visited == True: clear visited, move tail to head (if cap > 1), then normal SIEVE eviction
            t.visited = False
            if self.capacity > 1:
                if self.hand is t:
                    self.hand = t.prev
                self._remove_node(t)
                self._insert_head(t)

        o = self.hand
        if o is None:
            o = self.tail
            self.evicts_since_wrap = 0
        assert o is not None

        while o.visited:
            o.visited = False
            if o.prev is not None:
                o = o.prev
            else:
                o = self.tail
                self.evicts_since_wrap = 0
                assert o is not None

        self.hand = o.prev
        self._remove_node(o)
        del self.table[o.key]

        if self.hand is None:
            self.evicts_since_wrap = 0
        else:
            self.evicts_since_wrap += 1
        return o.key

    def access(self, key: Any) -> Tuple[bool, Optional[Any]]:
        node = self.table.get(key)
        if node is not None:
            node.visited = True
            return True, None

        new_node = Node(key)
        new_node.visited = False
        evicted_key: Optional[Any] = None
        if len(self.table) >= self.capacity:
            evicted_key = self._evict()

        self._insert_head(new_node)
        self.table[key] = new_node
        return False, evicted_key

    def keys_head_to_tail(self) -> List[Any]:
        out: List[Any] = []
        curr = self.head
        while curr is not None:
            out.append(curr.key)
            curr = curr.next
        return out

    def assert_invariants(self) -> None:
        assert len(self.table) <= self.capacity
        if not self.table:
            assert self.head is None and self.tail is None and self.hand is None
            return
        assert self.head is not None and self.head.prev is None
        assert self.tail is not None and self.tail.next is None
        seen: List[Node] = []
        curr = self.head
        prev: Optional[Node] = None
        while curr is not None:
            assert curr.prev is prev
            assert self.table.get(curr.key) is curr
            seen.append(curr)
            prev = curr
            curr = curr.next
        assert prev is self.tail
        assert len(seen) == len(self.table)
        if self.hand is not None:
            assert self.hand in seen


def run_prefix_cycle(
    capacity: int,
    prefix: Iterable[Any],
    cycle: Iterable[Any],
    num_cycles: int,
    steady_tail_cycles: Optional[int] = None,
    insert_before_hand_update: bool = False,
) -> Tuple[TraceStats, List[int]]:
    """Run `prefix` followed by `num_cycles` repetitions of `cycle`.

    Returns `(TraceStats, per_cycle_misses)`.
    If `steady_tail_cycles` is None, uses `max(1, num_cycles // 2)` tail cycles
    for steady-state measurement.
    """
    prefix_list = list(prefix)
    cycle_list = list(cycle)
    if steady_tail_cycles is None:
        steady_tail_cycles = max(1, num_cycles // 2)

    cache = SieveCache(capacity, insert_before_hand_update=insert_before_hand_update)
    prefix_misses = 0
    for k in prefix_list:
        hit, _ = cache.access(k)
        if not hit:
            prefix_misses += 1

    per_cycle_misses: List[int] = []
    evicted_after_prefix: set[Any] = set()
    steady_start = num_cycles - steady_tail_cycles
    steady_misses = 0
    steady_accesses = 0

    for c_idx in range(num_cycles):
        c_misses = 0
        for k in cycle_list:
            hit, ev = cache.access(k)
            if not hit:
                c_misses += 1
            if ev is not None and c_idx >= steady_start:
                evicted_after_prefix.add(ev)
        per_cycle_misses.append(c_misses)
        if c_idx >= steady_start:
            steady_misses += c_misses
            steady_accesses += len(cycle_list)

    cycle_misses = sum(per_cycle_misses)
    stats = TraceStats(
        capacity=capacity,
        total_accesses=len(prefix_list) + num_cycles * len(cycle_list),
        total_misses=prefix_misses + cycle_misses,
        prefix_misses=prefix_misses,
        cycle_misses=cycle_misses,
        steady_state_misses=steady_misses,
        steady_state_accesses=steady_accesses,
        final_keys=cache.keys_head_to_tail(),
        evicted_keys=evicted_after_prefix,
    )
    return stats, per_cycle_misses
