package language

import (
	"iter"
	"math/big"
	"runtime"
	"weak"
)

// Iterator is a single-pass lazy sequence (a Python iterator). Next panics
// with *Error when producing the next element fails.
type Iterator interface {
	Next() (Value, bool)
}

type sliceIterator struct {
	id    int64
	items []Value
	pos   int
}

func (it *sliceIterator) Next() (Value, bool) {
	if it.pos >= len(it.items) {
		return nil, false
	}
	it.pos++
	return it.items[it.pos-1], true
}

// SliceIterator iterates over items.
func SliceIterator(items []Value) Iterator {
	return &sliceIterator{id: nextIdentity(), items: items}
}

type generator struct {
	id   int64
	next func() (Value, bool)
	stop func()
	done bool
}

func (g *generator) Next() (Value, bool) {
	if g.done {
		return nil, false
	}
	v, ok := g.next()
	if !ok {
		g.close()
	}
	return v, ok
}

// close marks the generator finished and releases its body. Stopping a body
// that has already returned does nothing, so close can be called repeatedly.
func (g *generator) close() {
	g.done = true
	g.stop()
}

// Generator turns a push-style sequence into a lazy Iterator, like a Python
// generator function: the body runs only as elements are requested. Its pull
// worker is released on every path: when the body finishes or panics, when an
// open GeneratorScope closes, and otherwise once the iterator becomes
// unreachable, as Python closes a collected generator. Stopping only makes the
// pending yield return false, and every body returns at that point.
func Generator(seq iter.Seq[Value]) Iterator {
	next, stop := iter.Pull(seq)
	g := &generator{id: nextIdentity(), next: next, stop: stop}
	runtime.AddCleanup(g, func(stop func()) { stop() }, stop)
	if openScope != nil {
		openScope.generators = append(openScope.generators, weak.Make(g))
	}
	return g
}

// GeneratorScope stops the generators created while it is open, including any
// a consumer abandoned early and still holds. A query cannot stop its source
// itself, because another part of the expression may read that source later.
// Scopes do not nest: they are for a caller that runs one evaluation at a time,
// as the conformance command does. Weak pointers keep an abandoned generator
// collectable while the scope is open.
type GeneratorScope struct {
	generators []weak.Pointer[generator]
}

var openScope *GeneratorScope

// OpenGeneratorScope starts recording the generators created from now on.
func OpenGeneratorScope() *GeneratorScope {
	openScope = &GeneratorScope{}
	return openScope
}

// Close stops every generator of the scope. Call it once nothing will read
// the iterators of the evaluation again.
func (s *GeneratorScope) Close() {
	openScope = nil
	for _, p := range s.generators {
		if g := p.Value(); g != nil {
			g.close()
		}
	}
}

type countIterator struct {
	id        int64
	cur, step *big.Int
}

func (c *countIterator) Next() (Value, bool) {
	v := c.cur
	c.cur = new(big.Int).Add(c.cur, c.step)
	return v, true
}

// Count is itertools.count(start, step).
func Count(start, step *big.Int) Iterator {
	return &countIterator{id: nextIdentity(), cur: start, step: step}
}

// Iter returns an iterator over v, like Python's iter(). Iterators are
// returned as is.
func Iter(v Value) Iterator {
	switch t := v.(type) {
	case *memoIterable:
		return t.Fresh()
	case Iterator:
		return t
	case Tuple:
		return SliceIterator(t)
	case *List:
		return SliceIterator(t.Items)
	case *Dict:
		return SliceIterator(t.Keys())
	case *Set:
		return SliceIterator(t.Items())
	case *DictView:
		return SliceIterator(t.Items())
	case *Range:
		r := t
		return Generator(func(yield func(Value) bool) {
			n := r.Len()
			for i := 0; i < n; i++ {
				if !yield(r.At(i)) {
					return
				}
			}
		})
	case *OrderingIterable:
		return SliceIterator(t.Sorted())
	case string:
		runes := []rune(t)
		items := make([]Value, len(runes))
		for i, r := range runes {
			items[i] = string(r)
		}
		return SliceIterator(items)
	}
	Raise("TypeError", "'%s' object is not iterable", TypeName(v))
	return nil
}

// Seq adapts a value to a range-over-func sequence.
func Seq(v Value) iter.Seq[Value] {
	it := Iter(v)
	return func(yield func(Value) bool) {
		for {
			x, ok := it.Next()
			if !ok || !yield(x) {
				return
			}
		}
	}
}

// ToSlice drains v into a slice.
func ToSlice(v Value) []Value {
	switch t := v.(type) {
	case Tuple:
		return append([]Value(nil), t...)
	case *List:
		return append([]Value(nil), t.Items...)
	}
	var out []Value
	for x := range Seq(v) {
		out = append(out, x)
	}
	return out
}

// OrderingIterable is the re-iterable result of orderBy, sorted lazily on
// first iteration with the yaql comparison operators.
type OrderingIterable struct {
	Collection Value
	Lt, Gt     func(a, b Value) bool
	order      []orderField
	sorted     []Value
	done       bool
}

type orderField struct {
	selector  func(Value) Value
	ascending bool
}

// AppendField adds a sort key.
func (o *OrderingIterable) AppendField(selector func(Value) Value, ascending bool) {
	o.order = append(o.order, orderField{selector, ascending})
}

func (o *OrderingIterable) compare(left, right Value) int {
	result := 0
	for _, f := range o.order {
		a := f.selector(left)
		b := f.selector(right)
		if o.Lt(a, b) {
			result = -1
		} else if o.Gt(a, b) {
			result = 1
		} else {
			continue
		}
		if !f.ascending {
			result = -result
		}
		break
	}
	return result
}

// Sorted returns the sorted elements, sorting on first use.
func (o *OrderingIterable) Sorted() []Value {
	if !o.done {
		items := ToSlice(o.Collection)
		o.sorted = timsortStable(items, func(a, b Value) bool { return o.compare(a, b) < 0 })
		o.done = true
	}
	return o.sorted
}

// timsortStable sorts like Python's sorted(), asking only "less than". Short
// inputs follow CPython's run detection and binary insertion exactly, so even
// inconsistent comparisons order elements the same way.
func timsortStable(items []Value, less func(a, b Value) bool) []Value {
	n := len(items)
	if n < 2 {
		return items
	}
	if n < 64 {
		run := 1
		if less(items[1], items[0]) {
			for run = 2; run < n && less(items[run], items[run-1]); run++ {
			}
			for i, j := 0, run-1; i < j; i, j = i+1, j-1 {
				items[i], items[j] = items[j], items[i]
			}
		} else {
			for run = 2; run < n && !less(items[run], items[run-1]); run++ {
			}
		}
		for start := run; start < n; start++ {
			pivot := items[start]
			l, r := 0, start
			for l < r {
				p := l + (r-l)>>1
				if less(pivot, items[p]) {
					r = p
				} else {
					l = p + 1
				}
			}
			copy(items[l+1:start+1], items[l:start])
			items[l] = pivot
		}
		return items
	}
	mid := n / 2
	left := timsortStable(append([]Value(nil), items[:mid]...), less)
	right := timsortStable(append([]Value(nil), items[mid:]...), less)
	out := make([]Value, 0, len(items))
	i, j := 0, 0
	for i < len(left) && j < len(right) {
		if less(right[j], left[i]) {
			out = append(out, right[j])
			j++
		} else {
			out = append(out, left[i])
			i++
		}
	}
	out = append(out, left[i:]...)
	return append(out, right[j:]...)
}
