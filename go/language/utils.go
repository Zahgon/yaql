package language

import "fmt"

// ConvertInputData turns host data into yaql values: lists become tuples and
// mappings become frozen dicts, recursively.
func ConvertInputData(v Value) Value {
	switch t := v.(type) {
	case string:
		return t
	case []Value:
		out := make(Tuple, len(t))
		for i, x := range t {
			out[i] = ConvertInputData(x)
		}
		return out
	case Tuple:
		out := make(Tuple, len(t))
		for i, x := range t {
			out[i] = ConvertInputData(x)
		}
		return out
	case *List:
		out := make(Tuple, len(t.Items))
		for i, x := range t.Items {
			out[i] = ConvertInputData(x)
		}
		return out
	case *Dict:
		out := NewDict(true)
		t.Items(func(k, val Value) { out.Set(ConvertInputData(k), ConvertInputData(val)) })
		return out
	}
	return v
}

// OutputOptions controls ConvertOutputData.
type OutputOptions struct {
	SetsToLists   bool
	TuplesToLists bool
	Limit         func(Value) Value
}

// ConvertOutputData materializes iterators and normalizes containers for
// returning results to the host, as utils.convert_output_data does.
func ConvertOutputData(v Value, opts OutputOptions) Value {
	limit := opts.Limit
	if limit == nil {
		limit = func(x Value) Value { return x }
	}
	switch t := v.(type) {
	case *Dict:
		out := NewDict(false)
		var pairs []Value
		for p := range Seq(limit(&DictView{D: t, Kind: ViewItems})) {
			pairs = append(pairs, p)
		}
		for _, p := range pairs {
			kv := p.(Tuple)
			out.Set(ConvertOutputData(kv[0], opts), ConvertOutputData(kv[1], opts))
		}
		return out
	}
	if IsSet(v) {
		var items []Value
		for x := range Seq(limit(v)) {
			items = append(items, ConvertOutputData(x, opts))
		}
		if opts.SetsToLists {
			return &List{Items: items}
		}
		return &OutputSet{Items: items}
	}
	switch t := v.(type) {
	case Tuple:
		items := convertAll(limit(t), opts)
		if opts.TuplesToLists {
			return &List{Items: items}
		}
		return Tuple(items)
	case *List:
		return &List{Items: convertAll(limit(t), opts)}
	}
	if IsIterable(v) {
		return &List{Items: convertAll(limit(v), opts)}
	}
	return v
}

func convertAll(v Value, opts OutputOptions) []Value {
	var out []Value
	for x := range Seq(v) {
		out = append(out, ConvertOutputData(x, opts))
	}
	if out == nil {
		out = []Value{}
	}
	return out
}

// OutputSet is a mutable set produced by output conversion; it may hold
// unhashable members, which a frozenset cannot.
type OutputSet struct {
	Items []Value
}

// LimitIterable is utils.limit_iterable: collections pass through, other
// iterables are wrapped in a counting generator.
func LimitIterable(v Value, engine *Engine) Value {
	maxCount := engine.IntOption("yaql.limitIterators", -1)
	switch t := v.(type) {
	case Tuple, *List, *Range, *Dict, *Set, *DictView:
		if maxCount >= 0 && int64(lengthOf(t)) > maxCount {
			Raise("CollectionTooLargeException", "Collection length exceeds %d elements", maxCount)
		}
		return v
	}
	it := Iter(v)
	return Generator(func(yield func(Value) bool) {
		var i int64
		for {
			x, ok := it.Next()
			if !ok {
				return
			}
			if maxCount >= 0 && i >= maxCount {
				Raise("CollectionTooLargeException", "Collection length exceeds %d elements", maxCount)
			}
			i++
			if !yield(x) {
				return
			}
		}
	})
}

func lengthOf(v Value) int {
	switch t := v.(type) {
	case Tuple:
		return len(t)
	case *List:
		return len(t.Items)
	case *Range:
		return t.Len()
	case *Dict:
		return t.Len()
	case *Set:
		return t.Len()
	case *DictView:
		return t.D.Len()
	case string:
		return len([]rune(t))
	}
	panic(fmt.Sprintf("no length for %T", v))
}

// Len returns len(v) for sized values.
func Len(v Value) int { return lengthOf(v) }

// rememberingIterator replays elements of a shared source, as utils.memorize.
type memo struct {
	src     Iterator
	yielded []Value
}

type rememberingIterator struct {
	id    int64
	m     *memo
	index int
}

func (r *rememberingIterator) Next() (Value, bool) {
	if r.index < len(r.m.yielded) {
		r.index++
		return r.m.yielded[r.index-1], true
	}
	v, ok := r.m.src.Next()
	if !ok {
		return nil, false
	}
	r.m.yielded = append(r.m.yielded, v)
	r.index++
	return v, true
}

// Memorize returns collection unchanged unless it is an iterator, in which
// case iterating the result (and each re-iteration) replays its elements.
func Memorize(collection Value) Value {
	it, ok := collection.(Iterator)
	if !ok {
		return collection
	}
	return &memoIterable{m: &memo{src: it}, first: &rememberingIterator{id: nextIdentity()}}
}

// memoIterable behaves like Python's RememberingIterator: it is an iterator
// whose __iter__ returns a fresh replaying iterator.
type memoIterable struct {
	m     *memo
	first *rememberingIterator
}

func (mi *memoIterable) Next() (Value, bool) {
	mi.first.m = mi.m
	return mi.first.Next()
}

// Fresh returns a new replaying iterator from the start.
func (mi *memoIterable) Fresh() Iterator {
	return &rememberingIterator{id: nextIdentity(), m: mi.m}
}
