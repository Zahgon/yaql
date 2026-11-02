package language

import "math/big"

// Dict is an insertion-ordered mapping with Python key semantics. A frozen
// dict (yaql's FrozenDict) is hashable; a mutable one is not.
type Dict struct {
	keys   []Value
	values []Value
	index  map[uint64][]int
	live   int
	Frozen bool
}

// NewDict returns an empty dict.
func NewDict(frozen bool) *Dict {
	return &Dict{index: map[uint64][]int{}, Frozen: frozen}
}

func (d *Dict) find(key Value, h uint64) int {
	for _, i := range d.index[h] {
		if d.keys[i] != deleted && Equal(d.keys[i], key) {
			return i
		}
	}
	return -1
}

type deletedMarker struct{}

var deleted Value = &deletedMarker{}

// Set stores value under key, keeping the position and key object of an
// existing equal key.
func (d *Dict) Set(key, value Value) {
	h := Hash(key)
	if i := d.find(key, h); i >= 0 {
		d.values[i] = value
		return
	}
	d.index[h] = append(d.index[h], len(d.keys))
	d.keys = append(d.keys, key)
	d.values = append(d.values, value)
	d.live++
}

// Get returns the value stored under key.
func (d *Dict) Get(key Value) (Value, bool) {
	i := d.find(key, Hash(key))
	if i < 0 {
		return nil, false
	}
	return d.values[i], true
}

// Contains reports whether key is present.
func (d *Dict) Contains(key Value) bool {
	return d.find(key, Hash(key)) >= 0
}

// Delete removes key if present.
func (d *Dict) Delete(key Value) {
	h := Hash(key)
	if i := d.find(key, h); i >= 0 {
		d.keys[i] = deleted
		d.values[i] = nil
		d.live--
	}
}

// Len returns the number of entries.
func (d *Dict) Len() int { return d.live }

// Keys returns the keys in insertion order.
func (d *Dict) Keys() []Value {
	out := make([]Value, 0, d.live)
	for _, k := range d.keys {
		if k != deleted {
			out = append(out, k)
		}
	}
	return out
}

// Values returns the values in insertion order.
func (d *Dict) Values() []Value {
	out := make([]Value, 0, d.live)
	for i, k := range d.keys {
		if k != deleted {
			out = append(out, d.values[i])
		}
	}
	return out
}

// Items calls fn for every entry in insertion order.
func (d *Dict) Items(fn func(k, v Value)) {
	for i, k := range d.keys {
		if k != deleted {
			fn(k, d.values[i])
		}
	}
}

// Copy returns a copy with the given frozenness.
func (d *Dict) Copy(frozen bool) *Dict {
	out := NewDict(frozen)
	d.Items(func(k, v Value) { out.Set(k, v) })
	return out
}

// Set is an immutable set (Python frozenset) keeping insertion order.
type Set struct {
	items []Value
	index map[uint64][]int
}

// NewSet builds a set from values; every value must be hashable.
func NewSet(values []Value) *Set {
	s := &Set{index: map[uint64][]int{}}
	for _, v := range values {
		s.add(v)
	}
	return s
}

func (s *Set) add(v Value) {
	h := Hash(v)
	for _, i := range s.index[h] {
		if Equal(s.items[i], v) {
			return
		}
	}
	s.index[h] = append(s.index[h], len(s.items))
	s.items = append(s.items, v)
}

// Contains reports set membership.
func (s *Set) Contains(v Value) bool {
	h := Hash(v)
	for _, i := range s.index[h] {
		if Equal(s.items[i], v) {
			return true
		}
	}
	return false
}

// Items returns the members.
func (s *Set) Items() []Value { return s.items }

// Len returns the number of members.
func (s *Set) Len() int { return len(s.items) }

// Union returns s | other.
func (s *Set) Union(other *Set) *Set {
	return NewSet(append(append([]Value{}, s.items...), other.items...))
}

// Intersection returns s & other.
func (s *Set) Intersection(other *Set) *Set {
	var out []Value
	for _, v := range s.items {
		if other.Contains(v) {
			out = append(out, v)
		}
	}
	return NewSet(out)
}

// Difference returns s - other.
func (s *Set) Difference(other *Set) *Set {
	var out []Value
	for _, v := range s.items {
		if !other.Contains(v) {
			out = append(out, v)
		}
	}
	return NewSet(out)
}

// SymmetricDifference returns s ^ other.
func (s *Set) SymmetricDifference(other *Set) *Set {
	out := s.Difference(other).items
	out = append(out, other.Difference(s).items...)
	return NewSet(out)
}

// IsSubset reports s <= other.
func (s *Set) IsSubset(other *Set) bool {
	if s.Len() > other.Len() {
		return false
	}
	for _, v := range s.items {
		if !other.Contains(v) {
			return false
		}
	}
	return true
}

// View kinds.
const (
	ViewKeys = iota
	ViewValues
	ViewItems
)

// DictView is a live view over a mapping's keys, values or items.
type DictView struct {
	D    *Dict
	Kind int
}

// Items returns the view's elements in order.
func (v *DictView) Items() []Value {
	switch v.Kind {
	case ViewKeys:
		return v.D.Keys()
	case ViewValues:
		return v.D.Values()
	}
	var out []Value
	v.D.Items(func(k, val Value) { out = append(out, Tuple{k, val}) })
	return out
}

// asSet returns the view as a set, for the set-like keys and items views.
func (v *DictView) asSet() *Set { return NewSet(v.Items()) }

// Range is Python's range object.
type Range struct {
	Start, Stop, Step *big.Int
}

// Len returns the number of elements.
func (r *Range) Len() int {
	n := new(big.Int)
	if r.Step.Sign() > 0 {
		if r.Start.Cmp(r.Stop) >= 0 {
			return 0
		}
		n.Sub(r.Stop, r.Start)
		n.Sub(n, bigOne)
		n.Quo(n, r.Step)
		n.Add(n, bigOne)
	} else {
		if r.Start.Cmp(r.Stop) <= 0 {
			return 0
		}
		n.Sub(r.Start, r.Stop)
		n.Sub(n, bigOne)
		n.Quo(n, new(big.Int).Neg(r.Step))
		n.Add(n, bigOne)
	}
	return int(n.Int64())
}

// At returns element i (0 <= i < Len()).
func (r *Range) At(i int) *big.Int {
	v := new(big.Int).Mul(r.Step, big.NewInt(int64(i)))
	return v.Add(v, r.Start)
}

// Items returns all elements.
func (r *Range) Items() []Value {
	n := r.Len()
	out := make([]Value, n)
	for i := 0; i < n; i++ {
		out[i] = r.At(i)
	}
	return out
}
