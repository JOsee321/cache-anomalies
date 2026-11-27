package policies

import (
	"math/rand"
	"testing"
)

func TestFIFOBeladyClassic(t *testing.T) {
	trace := []int{1, 2, 3, 4, 1, 2, 5, 1, 2, 3, 4, 5}
	c3 := NewFIFO(3)
	c4 := NewFIFO(4)
	m3, m4 := 0, 0
	for _, x := range trace {
		if !c3.Access(x) {
			m3++
		}
		if !c4.Access(x) {
			m4++
		}
	}
	if m3 != 9 || m4 != 10 {
		t.Fatalf("unexpected FIFO misses: m3=%d m4=%d", m3, m4)
	}
}

func TestAllPoliciesBounds(t *testing.T) {
	rng := rand.New(rand.NewSource(123))
	names := []string{"FIFO", "CLOCK", "CLOCK2", "SecondChance", "S3FIFO"}
	for _, name := range names {
		for _, cap := range []int{2, 3, 5, 10, 15} {
			p, err := New(name, cap)
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 500; i++ {
				p.Access(rng.Intn(cap + 5))
				if p.Len() > cap {
					t.Fatalf("%s cap=%d exceeded len=%d", name, cap, p.Len())
				}
			}
		}
	}
}
