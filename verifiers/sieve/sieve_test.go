package sieve

import (
	"math/rand"
	"reflect"
	"testing"
)

func TestInvariantsRandom(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	for _, cap := range []int{1, 2, 3, 5, 8} {
		for _, variant := range []bool{false, true} {
			c, err := NewCache(cap, variant)
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 500; i++ {
				c.Access(rng.Intn(cap + 4))
				if err := c.AssertInvariants(); err != nil {
					t.Fatalf("cap=%d variant=%v step=%d: %v", cap, variant, i, err)
				}
			}
		}
	}
}

func TestOfficialVsVariantHeadEvictionDivergence(t *testing.T) {
	trace := []int{0, 1, 0, 2, 3}
	cOff, _ := NewCache(2, false)
	cVar, _ := NewCache(2, true)

	var evOff, evVar []int
	for _, x := range trace {
		_, ev1, has1 := cOff.Access(x)
		if has1 {
			evOff = append(evOff, ev1)
		}
		_, ev2, has2 := cVar.Access(x)
		if has2 {
			evVar = append(evVar, ev2)
		}
	}

	if !reflect.DeepEqual(evOff, []int{1, 0}) {
		t.Fatalf("unexpected official evictions: %v", evOff)
	}
	if !reflect.DeepEqual(evVar, []int{1, 2}) {
		t.Fatalf("unexpected variant evictions: %v", evVar)
	}
	if !reflect.DeepEqual(cOff.KeysHeadToTail(), []int{3, 2}) {
		t.Fatalf("unexpected official keys: %v", cOff.KeysHeadToTail())
	}
	if !reflect.DeepEqual(cVar.KeysHeadToTail(), []int{3, 0}) {
		t.Fatalf("unexpected variant keys: %v", cVar.KeysHeadToTail())
	}
}

func TestSieveBWCuresZombiePinning(t *testing.T) {
	for _, C := range []int{4, 10, 20} {
		zombies := C - 2
		A, B, D := C-2, C-1, C
		var prefix []int
		for z := 0; z < zombies; z++ {
			prefix = append(prefix, z)
		}
		prefix = append(prefix, A, B)
		for z := 0; z < zombies; z++ {
			prefix = append(prefix, z)
		}
		cycle := [3]int{D, A, B}
		for _, mult := range []int{1, 2} {
			c, _ := NewBWCache(C, C*mult)
			for _, x := range prefix {
				c.Access(x)
			}
			for i := 0; i < 50; i++ {
				for _, x := range cycle {
					c.Access(x)
				}
			}
			misses := 0
			for i := 0; i < 10; i++ {
				for _, x := range cycle {
					if h, _, _ := c.Access(x); !h {
						misses++
					}
				}
			}
			if misses != 0 {
				t.Fatalf("C=%d mult=%d still had %d misses", C, mult, misses)
			}
		}
	}
}

func TestSieveTPInvariantsAndCuresZombiePinning(t *testing.T) {
	rng := rand.New(rand.NewSource(99))
	for _, cap := range []int{1, 2, 3, 5, 8} {
		for _, kVal := range []int{1, cap / 2, cap, 2 * cap} {
			if kVal < 1 {
				kVal = 1
			}
			c, err := NewTPCache(cap, kVal)
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 500; i++ {
				c.Access(rng.Intn(cap + 4))
				if err := c.AssertInvariants(); err != nil {
					t.Fatalf("cap=%d K=%d step=%d: %v", cap, kVal, i, err)
				}
			}
		}
	}

	for _, C := range []int{4, 10, 20} {
		zombies := C - 2
		A, B, D := C-2, C-1, C
		var prefix []int
		for z := 0; z < zombies; z++ {
			prefix = append(prefix, z)
		}
		prefix = append(prefix, A, B)
		for z := 0; z < zombies; z++ {
			prefix = append(prefix, z)
		}
		cycle := [3]int{D, A, B}
		for _, K := range []int{C / 2, C, 2 * C, 4 * C} {
			if K < 1 {
				K = 1
			}
			c, _ := NewTPCache(C, K)
			for _, x := range prefix {
				c.Access(x)
			}
			for i := 0; i < 5*C+20; i++ {
				for _, x := range cycle {
					c.Access(x)
				}
			}
			misses := 0
			for i := 0; i < 10; i++ {
				for _, x := range cycle {
					if h, _, _ := c.Access(x); !h {
						misses++
					}
				}
			}
			if misses != 0 {
				t.Fatalf("TP C=%d K=%d still had %d misses", C, K, misses)
			}
		}
	}
}
