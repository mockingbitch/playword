package csr

import (
	"fmt"
	"slices"
	"testing"
)

func TestTable(t *testing.T) {
	seed := NewSeed()
	a, b := NewBuilder(seed), NewBuilder(seed)
	a.Add("x", 3)
	a.Add("x", 1)
	a.Add("y", 2)
	b.Add("x", 3) // duplicate pair across builders
	b.Add("z", 9)
	a.Merge(b)
	tab := a.Build()
	if got := tab.Get("x"); !slices.Equal(got, []int32{1, 3}) {
		t.Errorf("x = %v", got)
	}
	if got := tab.Get("z"); !slices.Equal(got, []int32{9}) {
		t.Errorf("z = %v", got)
	}
	if tab.Get("nope") != nil || tab.Keys() != 3 || tab.Len() != 4 || tab.Bytes() == 0 {
		t.Errorf("keys=%d len=%d", tab.Keys(), tab.Len())
	}
	if empty := NewBuilder(seed).Build(); empty.Get("x") != nil {
		t.Error("empty table")
	}
}

func BenchmarkGet(b *testing.B) {
	bl := NewBuilder(NewSeed())
	for i := range 1_000_000 {
		bl.Add(fmt.Sprint("k", i%300_000), int32(i))
	}
	tab := bl.Build()
	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		tab.Get(fmt.Sprint("k", i%300_000))
	}
}
