package stdlib

import (
	"math/big"

	. "yaql/language"
)

func islice(c Value, start, stop *big.Int) Iterator {
	check := func(v *big.Int) {
		if v != nil && v.Sign() < 0 {
			Raise("ValueError", "Indices for islice() must be None or an integer: 0 <= x <= sys.maxsize.")
		}
	}
	if stop == nil {
		check(start)
	} else {
		check(stop)
	}
	it := Iter(c)
	return generator(func(yield func(Value) bool) {
		i := big.NewInt(0)
		for {
			if stop != nil && i.Cmp(stop) >= 0 {
				return
			}
			x, ok := it.Next()
			if !ok {
				return
			}
			if start == nil || i.Cmp(start) >= 0 {
				if !yield(x) {
					return
				}
			}
			i = new(big.Int).Add(i, big.NewInt(1))
		}
	})
}

// reduce is functools.reduce.
func reduce(fn func(a, b Value) Value, collection Value, seed Value) Value {
	it := Iter(collection)
	acc := seed
	if acc == NoValue {
		v, ok := it.Next()
		if !ok {
			Raise("TypeError", "reduce() of empty iterable with no initial value")
		}
		acc = v
	}
	for {
		x, ok := it.Next()
		if !ok {
			return acc
		}
		acc = fn(acc, x)
	}
}

func distinctIter(collection Value, keySelector Value) Iterator {
	return generator(func(yield func(Value) bool) {
		seen := NewDict(false)
		for t := range Seq(collection) {
			key := t
			if keySelector != nil {
				key = call(keySelector, t)
			}
			if !seen.Contains(key) {
				seen.Set(key, nil)
				if !yield(t) {
					return
				}
			}
		}
	})
}

func toListDelegate(fn Value, v Value) Value { return call(fn, v) }

func sliceOf(v Value, start, end int) Value {
	items := SequenceItems(v)
	n := len(items)
	s, e := sliceBounds(n, start, end)
	if s > e {
		s = e
	}
	part := append([]Value{}, items[s:e]...)
	if _, ok := v.(*List); ok {
		return &List{Items: part}
	}
	return Tuple(part)
}

type groupAggregator struct {
	aggregator    Value
	allowFallback bool
	failure       *Error
}

func (g *groupAggregator) apply(item Value) Value {
	if g.aggregator == nil {
		return item
	}
	pair := item.(Tuple)
	if g.failure == nil {
		var result Value
		err := Catch(func() { result = call(g.aggregator, pair[1]) })
		if err != nil {
			if err.Class == "NoMatchingMethodException" || err.Class == "NoMatchingFunctionException" || err.IsA("IndexError") {
				g.failure = err
			} else {
				Throw(err)
			}
		} else {
			valueList := SequenceItems(pair[1])
			ok := false
			if len(valueList) == 2 {
				if _, isStr := result.(string); !isStr && PySequence.IsInstance(result) && Len(result) == 2 {
					ok = Equal(SequenceItems(result)[0], valueList[0])
				}
			}
			if !ok {
				g.allowFallback = false
			}
			return Tuple{pair[0], result}
		}
	}
	if g.allowFallback {
		var result Value
		err := Catch(func() {
			result = call(g.aggregator, item)
			if Len(result) != 2 {
				result = nil
			}
		})
		// A failed fallback is dropped on purpose: the aggregator's own failure
		// below is the error to report.
		if err == nil && result != nil {
			return result
		}
	}
	Throw(g.failure)
	return nil
}

func mergeDicts(d1, d2 *Dict, listMerge func(a, b Value) Value, itemMerge func(a, b Value) Value, maxLevels *big.Int) *Dict {
	result := NewDict(false)
	one := big.NewInt(1)
	d1.Items(func(key, value1 Value) {
		result.Set(key, value1)
		value2, ok := d2.Get(key)
		if !ok {
			return
		}
		if maxLevels.Cmp(one) != 0 && IsMapping(value2) {
			m1, isMap := value1.(*Dict)
			if !isMap {
				Raise("TypeError", "Cannot merge %s with %s", pyTypeRepr(value1), pyTypeRepr(value2))
			}
			next := big.NewInt(0)
			if maxLevels.Sign() != 0 {
				next = new(big.Int).Sub(maxLevels, one)
			}
			result.Set(key, mergeDicts(m1, value2.(*Dict), listMerge, itemMerge, next))
		} else if maxLevels.Cmp(one) != 0 && IsSequence(value2) {
			if !IsSequence(value1) {
				Raise("TypeError", "Cannot merge %s with %s", pyTypeRepr(value1), pyTypeRepr(value2))
			}
			result.Set(key, listMerge(value1, value2))
		} else {
			result.Set(key, itemMerge(value1, value2))
		}
	})
	d2.Items(func(key, value2 Value) {
		if !result.Contains(key) {
			result.Set(key, value2)
		}
	})
	return result
}

// pyConcat is Python "a + b" for two sequences.
func pyConcat(a, b Value) Value {
	switch x := a.(type) {
	case Tuple:
		if y, ok := b.(Tuple); ok {
			return append(append(Tuple{}, x...), y...)
		}
		Raise("TypeError", "can only concatenate tuple (not \"%s\") to tuple", TypeName(b))
	case *List:
		if y, ok := b.(*List); ok {
			return &List{Items: append(append([]Value{}, x.Items...), y.Items...)}
		}
		Raise("TypeError", "can only concatenate list (not \"%s\") to list", TypeName(b))
	}
	Raise("TypeError", "unsupported operand type(s) for +: '%s' and '%s'", TypeName(a), TypeName(b))
	return nil
}

// RegisterQueries registers the query functions.
func RegisterQueries(ctx Context, allowGroupByAggFallback bool) {
	where := Define("where", sig("collection", "predicate"), func(a args, _ kwargs) Value {
		return generator(func(yield func(Value) bool) {
			for t := range Seq(a[0]) {
				if Truthy(call(a[1], t)) && !yield(t) {
					return
				}
			}
		})
	}, Param("collection", Iterable(false)), Param("predicate", LambdaType{}), Method)
	register(ctx, where)
	registerAs(ctx, where, "filter")
	selectFn := func(c, selector Value) Iterator {
		return generator(func(yield func(Value) bool) {
			for t := range Seq(c) {
				if !yield(call(selector, t)) {
					return
				}
			}
		})
	}
	sel := Define("select", sig("collection", "selector"), func(a args, _ kwargs) Value {
		return selectFn(a[0], a[1])
	}, Param("collection", Iterable(false)), Param("selector", LambdaType{}), Method)
	register(ctx, sel)
	registerAs(ctx, sel, "map")
	register(ctx, Define("collection_attribution", sig("collection", "attribute", "operator"), func(a args, _ kwargs) Value {
		return generator(func(yield func(Value) bool) {
			for t := range Seq(a[0]) {
				if !yield(call(a[2], t, a[1])) {
					return
				}
			}
		})
	}, Param("collection", Iterable(false)), Param("attribute", KeywordType(false)),
		Inject("operator", Delegate("#operator_.", false)), Name("#operator_.")))
	limit := Define("limit", sig("collection", "count"), func(a args, _ kwargs) Value {
		return islice(a[0], nil, bigInt(a[1]))
	}, Param("collection", Iterable(false)), ParamPy("count", PyInt, false), Method)
	register(ctx, limit)
	registerAs(ctx, limit, "take")
	register(ctx, Define("skip", sig("collection", "count"), func(a args, _ kwargs) Value {
		return islice(a[0], bigInt(a[1]), nil)
	}, Param("collection", Iterable(false)), ParamPy("count", PyInt, false), Method))
	register(ctx, Define("append", varargs(sig("collection"), "args"), func(a args, _ kwargs) Value {
		return chain(a[0], Tuple(a[1:]))
	}, Param("collection", Iterable(false)), ExtensionMethod))
	register(ctx, Define("distinct", withDefaults(sig("engine", "collection", "key_selector"), "key_selector", nil),
		func(a args, _ kwargs) Value {
			return distinctIter(a[1], a[2])
		}, Param("collection", Iterable(false)), Param("key_selector", LambdaType{}), ExtensionMethod))
	register(ctx, Define("enumerate_", withDefaults(sig("collection", "start"), "start", Int(0)), func(a args, _ kwargs) Value {
		var start Value = a[1]
		return generator(func(yield func(Value) bool) {
			i := start
			for t := range Seq(a[0]) {
				if !yield(&List{Items: []Value{i, t}}) {
					return
				}
				i = new(big.Int).Add(bigInt(i), big.NewInt(1))
			}
		})
	}, Param("collection", Iterable(false)), ExtensionMethod))
	register(ctx, Define("any_", withDefaults(sig("collection", "predicate"), "predicate", nil), func(a args, _ kwargs) Value {
		for t := range Seq(a[0]) {
			if a[1] == nil || Truthy(call(a[1], t)) {
				return true
			}
		}
		return false
	}, Param("collection", Iterable(false)), Param("predicate", LambdaType{}), ExtensionMethod))
	register(ctx, Define("all_", withDefaults(sig("collection", "predicate"), "predicate", nil), func(a args, _ kwargs) Value {
		for t := range Seq(a[0]) {
			var r Value = t
			if a[1] != nil {
				r = call(a[1], t)
			}
			if !Truthy(r) {
				return false
			}
		}
		return true
	}, Param("collection", Iterable(false)), Param("predicate", LambdaType{}), ExtensionMethod))
	register(ctx, Define("concat", varargs(sig(), "collections"), func(a args, _ kwargs) Value {
		return chain(a...)
	}, Param("collections", Iterable(false)), ExtensionMethod))
	count := func(c Value) Value {
		n := int64(0)
		for range Seq(c) {
			n++
		}
		return Int(n)
	}
	register(ctx, Define("count_", sig("collection"), func(a args, _ kwargs) Value { return count(a[0]) },
		ParamPy("collection", PyIterator), Name("len"), ExtensionMethod))
	register(ctx, Define("count", sig("collection"), func(a args, _ kwargs) Value { return count(a[0]) },
		Param("collection", Iterable(false)), Method))
	register(ctx, Define("memorize", sig("collection", "engine"), func(a args, _ kwargs) Value {
		return Memorize(a[0])
	}, Param("collection", Iterable(false)), Method))
	aggregateWith := func(fn, collection, seed Value) Value {
		return reduce(func(x, y Value) Value { return call(fn, x, y) }, collection, seed)
	}
	register(ctx, Define("sum_", withDefaults(sig("operator", "collection", "initial"), "initial", NoValue), func(a args, _ kwargs) Value {
		return aggregateWith(a[0], a[1], a[2])
	}, Param("collection", Iterable(false)), Inject("operator", Delegate("#operator_+", false)), Method))
	register(ctx, Define("max_", withDefaults(sig("func", "collection", "initial"), "initial", NoValue), func(a args, _ kwargs) Value {
		return aggregateWith(a[0], a[1], a[2])
	}, Param("collection", Iterable(false)), Inject("func", Delegate("max", false)), Method))
	register(ctx, Define("min_", withDefaults(sig("func", "collection", "initial"), "initial", NoValue), func(a args, _ kwargs) Value {
		return aggregateWith(a[0], a[1], a[2])
	}, Param("collection", Iterable(false)), Inject("func", Delegate("min", false)), Method))
	register(ctx, Define("first", withDefaults(sig("collection", "default"), "default", NoValue), func(a args, _ kwargs) Value {
		v, ok := Iter(a[0]).Next()
		if !ok {
			if a[1] == NoValue {
				Raise("StopIteration", "")
			}
			return a[1]
		}
		return v
	}, Param("collection", Iterable(false)), ParamNullable("default", true), Method))
	register(ctx, Define("single", sig("collection"), func(a args, _ kwargs) Value {
		it := Iter(a[0])
		v, ok := it.Next()
		if !ok {
			Raise("StopIteration", "")
		}
		if _, more := it.Next(); more {
			Raise("StopIteration", "Collection contains more than one item")
		}
		return v
	}, Param("collection", Iterable(false)), Method))
	register(ctx, Define("last", withDefaults(sig("collection", "default"), "default", NoValue), func(a args, _ kwargs) Value {
		if PySequence.IsInstance(a[0]) {
			items := SequenceItems(a[0])
			if len(items) == 0 {
				if a[1] == NoValue {
					Raise("StopIteration", "")
				}
				return a[1]
			}
			return items[len(items)-1]
		}
		last := a[1]
		for t := range Seq(a[0]) {
			last = t
		}
		if last == NoValue {
			Raise("StopIteration", "")
		}
		return last
	}, Param("collection", Iterable(false)), ParamNullable("default", true), Method))
	register(ctx, Define("select_many", sig("collection", "selector"), func(a args, _ kwargs) Value {
		return generator(func(yield func(Value) bool) {
			for item := range Seq(a[0]) {
				inner := call(a[1], item)
				if IsIterable(inner) {
					for x := range Seq(inner) {
						if !yield(x) {
							return
						}
					}
				} else if !yield(inner) {
					return
				}
			}
		})
	}, Param("collection", Iterable(false)), Param("selector", LambdaType{}), Method))
	register(ctx, Define("range_", sig("stop"), func(a args, _ kwargs) Value {
		return Iter(&Range{Start: big.NewInt(0), Stop: bigInt(a[0]), Step: big.NewInt(1)})
	}, ParamPy("stop", PyInt)))
	register(ctx, Define("range__", withDefaults(sig("start", "stop", "step"), "step", Int(1)), func(a args, _ kwargs) Value {
		step := bigInt(a[2])
		if step.Sign() == 0 {
			Raise("ValueError", "range() arg 3 must not be zero")
		}
		return Iter(&Range{Start: bigInt(a[0]), Stop: bigInt(a[1]), Step: step})
	}, ParamPy("start", PyInt), ParamPy("stop", PyInt), ParamPy("step", PyInt)))
	orderBy := func(name string, ascending bool) *FunctionDefinition {
		return Define(name, sig("collection", "selector", "operator_lt", "operator_gt"), func(a args, _ kwargs) Value {
			lt, gt := a[2], a[3]
			oi := &OrderingIterable{Collection: a[0],
				Lt: func(x, y Value) bool { return Truthy(call(lt, x, y)) },
				Gt: func(x, y Value) bool { return Truthy(call(gt, x, y)) }}
			sel := a[1]
			oi.AppendField(func(v Value) Value { return call(sel, v) }, ascending)
			return oi
		}, Param("collection", Iterable(false)), Param("selector", LambdaType{}),
			Inject("operator_gt", Delegate("#operator_>", false)), Inject("operator_lt", Delegate("#operator_<", false)), Method)
	}
	register(ctx, orderBy("order_by", true))
	register(ctx, orderBy("order_by_descending", false))
	thenBy := func(name string, ascending bool) *FunctionDefinition {
		return Define(name, sig("collection", "selector", "context"), func(a args, _ kwargs) Value {
			oi := a[0].(*OrderingIterable)
			sel := a[1]
			oi.AppendField(func(v Value) Value { return call(sel, v) }, ascending)
			return oi
		}, ParamPy("collection", PyOrderingIterable), Param("selector", LambdaType{}), Method)
	}
	register(ctx, thenBy("then_by", true))
	register(ctx, thenBy("then_by_descending", false))
	register(ctx, Define("group_by", withDefaults(sig("engine", "collection", "key_selector", "value_selector", "aggregator"),
		"value_selector", nil, "aggregator", nil), func(a args, _ kwargs) Value {
		groups := NewDict(false)
		for t := range Seq(a[1]) {
			value := t
			if a[3] != nil {
				value = call(a[3], t)
			}
			key := call(a[2], t)
			list, ok := groups.Get(key)
			if !ok {
				list = &List{}
				groups.Set(key, list)
			}
			l := list.(*List)
			l.Items = append(l.Items, value)
		}
		agg := &groupAggregator{aggregator: a[4], allowFallback: allowGroupByAggFallback}
		return generator(func(yield func(Value) bool) {
			for _, item := range (&DictView{D: groups, Kind: ViewItems}).Items() {
				if !yield(agg.apply(item)) {
					return
				}
			}
		})
	}, Param("collection", Iterable(false)), Param("key_selector", LambdaType{}), Param("value_selector", LambdaType{}),
		Param("aggregator", LambdaType{}), Method))
	register(ctx, Define("join", sig("engine", "collection1", "collection2", "predicate", "selector"), func(a args, _ kwargs) Value {
		c2 := Memorize(a[2])
		return generator(func(yield func(Value) bool) {
			for x := range Seq(a[1]) {
				for y := range Seq(c2) {
					if Truthy(call(a[3], x, y)) && !yield(call(a[4], x, y)) {
						return
					}
				}
			}
		})
	}, Method, Param("collection1", Iterable(false)), Param("collection2", Iterable(false)),
		Param("predicate", LambdaType{}), Param("selector", LambdaType{})))
	zipIter := func(collections []Value, longest bool, fill Value) Iterator {
		return generator(func(yield func(Value) bool) {
			its := make([]Iterator, len(collections))
			for i, c := range collections {
				its[i] = Iter(c)
			}
			if len(its) == 0 {
				return
			}
			done := make([]bool, len(its))
			active := len(its)
			for {
				row := make(Tuple, len(its))
				for i, it := range its {
					if done[i] {
						row[i] = fill
						continue
					}
					v, ok := it.Next()
					if !ok {
						if !longest {
							return
						}
						done[i] = true
						active--
						if active == 0 {
							return
						}
						v = fill
					}
					row[i] = v
				}
				if !yield(row) {
					return
				}
			}
		})
	}
	register(ctx, Define("zip_", varargs(sig(), "collections"), func(a args, _ kwargs) Value {
		return zipIter(a, false, nil)
	}, Method, Param("collections", Iterable(false))))
	register(ctx, Define("zip_longest", varkw(varargs(sig(), "collections"), "kwargs"), func(a args, kw kwargs) Value {
		fill, ok := kw["default"]
		if !ok {
			fill = nil
		}
		return zipIter(a, true, fill)
	}, Method, Param("collections", Iterable(false))))
	register(ctx, Define("repeat", withDefaults(sig("value", "times"), "times", Int(-1)), func(a args, _ kwargs) Value {
		times := bigInt(a[1])
		v := a[0]
		return generator(func(yield func(Value) bool) {
			for i := big.NewInt(0); times.Sign() < 0 || i.Cmp(times) < 0; i = new(big.Int).Add(i, big.NewInt(1)) {
				if !yield(v) {
					return
				}
			}
		})
	}, Method, ParamNullable("value", true), ParamPy("times", PyInt)))
	register(ctx, Define("cycle", sig("collection"), func(a args, _ kwargs) Value {
		return generator(func(yield func(Value) bool) {
			var saved []Value
			for x := range Seq(a[0]) {
				saved = append(saved, x)
				if !yield(x) {
					return
				}
			}
			if len(saved) == 0 {
				return
			}
			for {
				for _, x := range saved {
					if !yield(x) {
						return
					}
				}
			}
		})
	}, Method, Param("collection", Iterable(false))))
	register(ctx, Define("take_while", sig("collection", "predicate"), func(a args, _ kwargs) Value {
		return generator(func(yield func(Value) bool) {
			for x := range Seq(a[0]) {
				if !Truthy(call(a[1], x)) || !yield(x) {
					return
				}
			}
		})
	}, Method, Param("collection", Iterable(false)), Param("predicate", LambdaType{})))
	register(ctx, Define("skip_while", sig("collection", "predicate"), func(a args, _ kwargs) Value {
		return generator(func(yield func(Value) bool) {
			dropping := true
			for x := range Seq(a[0]) {
				if dropping && Truthy(call(a[1], x)) {
					continue
				}
				dropping = false
				if !yield(x) {
					return
				}
			}
		})
	}, Method, Param("collection", Iterable(false)), Param("predicate", LambdaType{})))
	register(ctx, Define("index_of", sig("collection", "item"), func(a args, _ kwargs) Value {
		i := int64(0)
		for t := range Seq(a[0]) {
			if Equal(t, a[1]) {
				return Int(i)
			}
			i++
		}
		return Int(-1)
	}, Method, Param("collection", Iterable(false))))
	register(ctx, Define("last_index_of", sig("collection", "item"), func(a args, _ kwargs) Value {
		i, idx := int64(0), int64(-1)
		for t := range Seq(a[0]) {
			if Equal(t, a[1]) {
				idx = i
			}
			i++
		}
		return Int(idx)
	}, Method, Param("collection", Iterable(false))))
	register(ctx, Define("index_where", sig("collection", "predicate"), func(a args, _ kwargs) Value {
		i := int64(0)
		for t := range Seq(a[0]) {
			if Truthy(call(a[1], t)) {
				return Int(i)
			}
			i++
		}
		return Int(-1)
	}, Method, Param("collection", Iterable(false)), Param("predicate", LambdaType{})))
	register(ctx, Define("last_index_where", sig("collection", "predicate"), func(a args, _ kwargs) Value {
		i, idx := int64(0), int64(-1)
		for t := range Seq(a[0]) {
			if Truthy(call(a[1], t)) {
				idx = i
			}
			i++
		}
		return Int(idx)
	}, Method, Param("collection", Iterable(false)), Param("predicate", LambdaType{})))
	register(ctx, Define("slice_", sig("collection", "length", "to_list"), func(a args, _ kwargs) Value {
		length := bigInt(a[1])
		return generator(func(yield func(Value) bool) {
			it := Iter(a[0])
			for {
				res := toListDelegate(a[2], islice(it, nil, length))
				if !Truthy(res) {
					return
				}
				if !yield(res) {
					return
				}
			}
		})
	}, Method, Param("collection", Iterable(false)), ParamPy("length", PyInt), Inject("to_list", Delegate("to_list", true))))
	register(ctx, Define("split_where", sig("collection", "predicate", "to_list"), func(a args, _ kwargs) Value {
		return generator(func(yield func(Value) bool) {
			lst := toListDelegate(a[2], a[0])
			n := Len(lst)
			start, end := 0, 0
			for end < n {
				if Truthy(call(a[1], SequenceItems(lst)[end])) {
					if !yield(sliceOf(lst, start, end)) {
						return
					}
					start = end + 1
				}
				end++
			}
			if start != end {
				yield(sliceOf(lst, start, end))
			}
		})
	}, Method, Param("collection", Iterable(false)), Param("predicate", LambdaType{}), Inject("to_list", Delegate("to_list", true))))
	register(ctx, Define("slice_where", sig("collection", "predicate", "to_list"), func(a args, _ kwargs) Value {
		return generator(func(yield func(Value) bool) {
			lst := toListDelegate(a[2], a[0])
			n := Len(lst)
			start, end := 0, 0
			var p1 Value = NoValue
			for end < n {
				p2 := call(a[1], SequenceItems(lst)[end])
				if p1 != NoValue && !Equal(p2, p1) {
					if !yield(sliceOf(lst, start, end)) {
						return
					}
					start = end
				}
				end++
				p1 = p2
			}
			if start != end {
				yield(sliceOf(lst, start, end))
			}
		})
	}, Method, Param("collection", Iterable(false)), Param("predicate", LambdaType{}), Inject("to_list", Delegate("to_list", true))))
	register(ctx, Define("split_at", sig("collection", "index", "to_list"), func(a args, _ kwargs) Value {
		lst := toListDelegate(a[2], a[0])
		idx := smallInt(a[1])
		n := Len(lst)
		return &List{Items: []Value{sliceOf(lst, 0, idx), sliceOf(lst, idx, n)}}
	}, Method, Param("collection", Iterable(false)), ParamPy("index", PyInt), Inject("to_list", Delegate("to_list", true))))
	aggregate := Define("aggregate", withDefaults(sig("collection", "selector", "seed"), "seed", NoValue), func(a args, _ kwargs) Value {
		return aggregateWith(a[1], a[0], a[2])
	}, Method, Param("collection", Iterable(false)), Param("selector", LambdaType{}))
	register(ctx, aggregate)
	registerAs(ctx, aggregate, "reduce")
	register(ctx, Define("accumulate", withDefaults(sig("collection", "selector", "seed"), "seed", NoValue), func(a args, _ kwargs) Value {
		return generator(func(yield func(Value) bool) {
			it := Iter(a[0])
			seed := a[2]
			if seed == NoValue {
				v, ok := it.Next()
				if !ok {
					Raise("TypeError", "accumulate() of empty sequence with no initial value")
				}
				seed = v
			}
			if !yield(seed) {
				return
			}
			total := seed
			for {
				x, ok := it.Next()
				if !ok {
					return
				}
				total = call(a[1], total, x)
				if !yield(total) {
					return
				}
			}
		})
	}, Method, Param("collection", Iterable(false)), Param("selector", LambdaType{})))
	register(ctx, Define("reverse", sig("collection", "to_list"), func(a args, _ kwargs) Value {
		lst := toListDelegate(a[1], a[0])
		items := SequenceItems(lst)
		out := make([]Value, len(items))
		for i, v := range items {
			out[len(items)-1-i] = v
		}
		return SliceIterator(out)
	}, Method, Param("collection", Iterable(false)), Inject("to_list", Delegate("to_list", true))))
	register(ctx, Define("merge_with", withDefaults(sig("engine", "to_list", "d", "another", "list_merger", "item_merger", "max_levels"),
		"list_merger", nil, "item_merger", nil, "max_levels", Int(0)), func(a args, _ kwargs) Value {
		toList := a[1]
		listMerger := func(x, y Value) Value {
			if a[4] != nil {
				return call(a[4], x, y)
			}
			return toListDelegate(toList, distinctIter(pyConcat(x, y), nil))
		}
		itemMerger := func(x, y Value) Value {
			if a[5] != nil {
				return call(a[5], x, y)
			}
			return y
		}
		return mergeDicts(a[2].(*Dict), a[3].(*Dict), listMerger, itemMerger, bigInt(a[6]))
	}, Method, ParamPyAlias("d", PyMapping, "dict"), ParamPy("another", PyMapping), Param("list_merger", LambdaType{}),
		Param("item_merger", LambdaType{}), ParamPy("max_levels", PyInt), Inject("to_list", Delegate("to_list", true))))
	register(ctx, Define("is_iterable", sig("value"), func(a args, _ kwargs) Value { return IsIterable(a[0]) }))
	register(ctx, Define("sequence", withDefaults(sig("start", "step"), "start", Int(0), "step", Int(1)), func(a args, _ kwargs) Value {
		return Count(bigInt(a[0]), bigInt(a[1]))
	}, ParamPy("start", PyInt), ParamPy("step", PyInt)))
	register(ctx, Define("generate", withDefaults(sig("engine", "initial", "predicate", "producer", "selector", "decycle"),
		"selector", nil, "decycle", false), func(a args, _ kwargs) Value {
		return generator(func(yield func(Value) bool) {
			initial := a[1]
			var past *Dict
			if Truthy(a[5]) {
				past = NewDict(false)
			}
			for Truthy(call(a[2], initial)) {
				if past != nil {
					if past.Contains(initial) {
						return
					}
					past.Set(initial, nil)
				}
				out := initial
				if a[4] != nil {
					out = call(a[4], initial)
				}
				if !yield(out) {
					return
				}
				initial = call(a[3], initial)
			}
		})
	}, Param("predicate", LambdaType{}), Param("producer", LambdaType{}), Param("selector", LambdaType{}), ParamPy("decycle", PyBool)))
	register(ctx, Define("generate_many", withDefaults(sig("engine", "initial", "producer", "selector", "decycle", "depth_first"),
		"selector", nil, "decycle", false, "depth_first", false), func(a args, _ kwargs) Value {
		return generator(func(yield func(Value) bool) {
			var past *Dict
			if Truthy(a[4]) {
				past = NewDict(false)
			}
			queue := []Value{a[1]}
			for len(queue) > 0 {
				item := queue[0]
				queue = queue[1:]
				if past != nil {
					if past.Contains(item) {
						continue
					}
					past.Set(item, nil)
				}
				out := item
				if a[3] != nil {
					out = call(a[3], item)
				}
				if !yield(out) {
					return
				}
				produced := ToSlice(call(a[2], item))
				if Truthy(a[5]) {
					queue = append(append([]Value{}, produced...), queue...)
				} else {
					queue = append(queue, produced...)
				}
			}
		})
	}, Param("producer", LambdaType{}), Param("selector", LambdaType{}), ParamPy("decycle", PyBool), ParamPy("depth_first", PyBool)))
	register(ctx, Define("default_if_empty", sig("engine", "collection", "default"), func(a args, _ kwargs) Value {
		switch a[1].(type) {
		case Tuple, *List, *Range, *Set:
			if Len(a[1]) == 0 {
				return a[2]
			}
			return a[1]
		}
		if IsSet(a[1]) {
			if Len(a[1]) == 0 {
				return a[2]
			}
			return a[1]
		}
		m := Memorize(a[1])
		if _, ok := Iter(m).Next(); ok {
			return m
		}
		return a[2]
	}, Method, Param("collection", Iterable(false)), Param("default", Iterable(false))))
}
