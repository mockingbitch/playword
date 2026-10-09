// Package csr is a compact read-only multimap from 64-bit key hashes to
// int32 values (compressed sparse rows): three flat slices, no pointers for
// the GC to scan, about 16 bytes per (key, value) pair (ADR-002 §2).
//
// Keys are hashed, so two keys may share a row; callers must verify what
// they get back. Every index using this does.
package csr

import (
	"hash/maphash"
	"slices"
	"sort"
)

// Table is the built, read-only multimap.
type Table struct {
	seed   maphash.Seed
	hashes []uint64 // sorted, unique
	offs   []uint32 // len(hashes)+1
	vals   []int32
}

type pair struct {
	h uint64
	v int32
}

// Builder collects pairs; it is not safe for concurrent use, but several
// builders sharing a seed can be merged.
type Builder struct {
	seed  maphash.Seed
	pairs []pair
}

// NewBuilder starts a table; builders meant to be merged must share seed.
func NewBuilder(seed maphash.Seed) *Builder { return &Builder{seed: seed} }

// NewSeed returns a random hash seed for a new table.
func NewSeed() maphash.Seed { return maphash.MakeSeed() }

// Add records value v under key.
func (b *Builder) Add(key string, v int32) {
	b.pairs = append(b.pairs, pair{maphash.String(b.seed, key), v})
}

// Merge appends the pairs of others (built with the same seed).
func (b *Builder) Merge(others ...*Builder) {
	for _, o := range others {
		b.pairs = append(b.pairs, o.pairs...)
		o.pairs = nil
	}
}

// Build sorts and deduplicates; values of one key come out ascending.
func (b *Builder) Build() *Table {
	slices.SortFunc(b.pairs, func(x, y pair) int {
		if x.h != y.h {
			if x.h < y.h {
				return -1
			}
			return 1
		}
		return int(x.v) - int(y.v)
	})
	b.pairs = slices.Compact(b.pairs)
	t := &Table{seed: b.seed, vals: make([]int32, len(b.pairs))}
	for i, p := range b.pairs {
		if i == 0 || p.h != b.pairs[i-1].h {
			t.hashes = append(t.hashes, p.h)
			t.offs = append(t.offs, uint32(i))
		}
		t.vals[i] = p.v
	}
	t.offs = append(t.offs, uint32(len(b.pairs)))
	t.hashes = slices.Clip(t.hashes)
	b.pairs = nil
	return t
}

// Get returns the values stored under key's hash (shared, do not modify).
func (t *Table) Get(key string) []int32 {
	h := maphash.String(t.seed, key)
	i := sort.Search(len(t.hashes), func(i int) bool { return t.hashes[i] >= h })
	if i == len(t.hashes) || t.hashes[i] != h {
		return nil
	}
	return t.vals[t.offs[i]:t.offs[i+1]]
}

// Keys is the number of distinct key hashes; Len the number of pairs.
func (t *Table) Keys() int { return len(t.hashes) }

// Len is the number of (key, value) pairs.
func (t *Table) Len() int { return len(t.vals) }

// Bytes is the memory held by the table's slices.
func (t *Table) Bytes() int { return cap(t.hashes)*8 + cap(t.offs)*4 + cap(t.vals)*4 }
