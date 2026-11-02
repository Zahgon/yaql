package stdlib

import (
	"math/big"

	. "yaql/language"
)

// pyContains is Python's "value in collection".
func pyContains(collection, value Value) bool {
	switch t := collection.(type) {
	case *Set:
		return t.Contains(value)
	case *Dict:
		return t.Contains(value)
	case *DictView:
		switch t.Kind {
		case ViewKeys:
			return t.D.Contains(value)
		case ViewItems:
			pair, ok := value.(Tuple)
			if !ok || len(pair) != 2 {
				return false
			}
			v, found := t.D.Get(pair[0])
			return found && Equal(v, pair[1])
		}
	}
	for x := range Seq(collection) {
		if Equal(x, value) {
			return true
		}
	}
	return false
}

func pyGetItem(container, key Value) Value {
	switch t := container.(type) {
	case *Dict:
		if v, ok := t.Get(key); ok {
			return v
		}
		Raise("KeyError", "%s", Repr(key))
	}
	items := SequenceItems(container)
	idx, ok := key.(*big.Int)
	if b, isBool := key.(bool); isBool {
		idx, ok = bigInt(b), true
	}
	if !ok {
		Raise("TypeError", "%s indices must be integers or slices, not %s", TypeName(container), TypeName(key))
	}
	n := big.NewInt(int64(len(items)))
	i := new(big.Int).Set(idx)
	if i.Sign() < 0 {
		i.Add(i, n)
	}
	if i.Sign() < 0 || i.Cmp(n) >= 0 {
		Raise("IndexError", "%s index out of range", TypeName(container))
	}
	return items[i.Int64()]
}

func frozenFromPairs(pairs func(add func(k, v Value))) *Dict {
	d := NewDict(true)
	pairs(func(k, v Value) { d.Set(k, v) })
	return d
}

func flattenIterators(a []Value) Iterator {
	var rec func(seq []Value, yield func(Value) bool) bool
	rec = func(seq []Value, yield func(Value) bool) bool {
		for _, t := range seq {
			if IsIterator(t) {
				for x := range Seq(t) {
					if IsIterator(x) {
						if !rec([]Value{x}, yield) {
							return false
						}
					} else if !yield(x) {
						return false
					}
				}
			} else if !yield(t) {
				return false
			}
		}
		return true
	}
	return generator(func(yield func(Value) bool) { rec(a, yield) })
}

func multiplySequence(seq Value, n Value) Value {
	count := bigInt(n)
	if _, isRange := seq.(*Range); isRange {
		Raise("TypeError", "unsupported operand type(s) for *: 'range' and 'int'")
	}
	items := SequenceItems(seq)
	var out []Value
	if count.Sign() > 0 {
		if !count.IsInt64() || int64(len(items))*count.Int64() > 1<<24 {
			Raise("MemoryError", "")
		}
		for i := int64(0); i < count.Int64(); i++ {
			out = append(out, items...)
		}
	}
	if _, isList := seq.(*List); isList {
		return &List{Items: out}
	}
	if out == nil {
		return Tuple{}
	}
	return Tuple(out)
}

func pyTypeRepr(v Value) string {
	switch v.(type) {
	case *Dict:
		if v.(*Dict).Frozen {
			return "<class 'yaql.language.utils.FrozenDict'>"
		}
	}
	return "<class '" + TypeName(v) + "'>"
}

// RegisterCollections registers list, dict and set functions.
func RegisterCollections(ctx Context, noSets bool) {
	register(ctx, Define("list_", varargs(sig("delegate"), "args"), func(a args, _ kwargs) Value {
		return call(a[0], flattenIterators(a[1:]))
	}, ParamNullable("args", true), Inject("delegate", Delegate("to_list", true))))
	register(ctx, Define("build_list", varargs(sig("engine"), "args"), func(a args, _ kwargs) Value {
		return Tuple(append([]Value{}, a[1:]...))
	}, ParamNullable("args", true), Name("#list")))
	register(ctx, Define("to_list", sig("collection"), func(a args, _ kwargs) Value {
		if t, ok := a[0].(Tuple); ok {
			return t
		}
		out := ToSlice(a[0])
		if out == nil {
			return Tuple{}
		}
		return Tuple(out)
	}, Method, Param("collection", Iterable(false))))
	var flatten func(c Value) Iterator
	flatten = func(c Value) Iterator {
		return generator(func(yield func(Value) bool) {
			for t := range Seq(c) {
				if IsIterable(t) {
					for x := range Seq(flatten(t)) {
						if !yield(x) {
							return
						}
					}
				} else if !yield(t) {
					return
				}
			}
		})
	}
	register(ctx, Define("flatten", sig("collection"), func(a args, _ kwargs) Value {
		return flatten(a[0])
	}, Method, Param("collection", Iterable(false))))
	register(ctx, Define("list_indexer", sig("lst", "index"), func(a args, _ kwargs) Value {
		return pyGetItem(a[0], a[1])
	}, ParamAlias("lst", Sequence(false), "list"), ParamPy("index", PyInt, false), Name("#indexer")))
	dictFromRules := Define("dict_", varargs(sig("engine"), "args"), func(a args, _ kwargs) Value {
		return frozenFromPairs(func(add func(k, v Value)) {
			for _, r := range a[1:] {
				m := r.(*MappingRule)
				add(m.Source, m.Destination)
			}
		})
	}, NoKwargs, ParamPy("args", PyMappingRule))
	register(ctx, dictFromRules)
	registerAs(ctx, dictFromRules, "#map")
	register(ctx, Define("dict__", sig("items", "engine"), func(a args, _ kwargs) Value {
		result := NewDict(false)
		for t := range Seq(a[0]) {
			it := Iter(t)
			k, ok := it.Next()
			if !ok {
				Raise("StopIteration", "")
			}
			v, ok := it.Next()
			if !ok {
				Raise("StopIteration", "")
			}
			result.Set(k, v)
		}
		return result.Copy(true)
	}, Param("items", Iterable(false)), NoKwargs))
	register(ctx, Define("to_dict", withDefaults(sig("collection", "engine", "key_selector", "value_selector"), "value_selector", nil),
		func(a args, _ kwargs) Value {
			result := NewDict(false)
			for t := range Seq(a[0]) {
				key := call(a[2], t)
				value := t
				if a[3] != nil {
					value = call(a[3], t)
				}
				result.Set(key, value)
			}
			return result
		}, Param("collection", Iterable(false)), Param("key_selector", LambdaType{}), Param("value_selector", LambdaType{}), Method))
	register(ctx, Define("dict_keyword_access", sig("d", "key"), func(a args, _ kwargs) Value {
		return pyGetItem(a[0], a[1])
	}, ParamPyAlias("d", PyMapping, "dict"), Param("key", KeywordType(true)), Name("#operator_.")))
	register(ctx, Define("dict_indexer", sig("d", "key"), func(a args, _ kwargs) Value {
		return pyGetItem(a[0], a[1])
	}, ParamPyAlias("d", PyMapping, "dict"), Name("#indexer")))
	register(ctx, Define("dict_indexer_with_default", sig("d", "key", "default"), func(a args, _ kwargs) Value {
		if v, ok := a[0].(*Dict).Get(a[1]); ok {
			return v
		}
		return a[2]
	}, ParamPyAlias("d", PyMapping, "dict"), Name("#indexer")))
	register(ctx, Define("dict_get", withDefaults(sig("d", "key", "default"), "default", nil), func(a args, _ kwargs) Value {
		if v, ok := a[0].(*Dict).Get(a[1]); ok {
			return v
		}
		return a[2]
	}, ParamPyAlias("d", PyMapping, "dict"), Name("get"), Method))
	register(ctx, Define("dict_set", sig("engine", "d", "key", "value"), func(a args, _ kwargs) Value {
		d := a[1].(*Dict).Copy(true)
		d.Set(a[2], a[3])
		return d
	}, ParamPyAlias("d", PyMapping, "dict"), Name("set"), Method, NoKwargs))
	register(ctx, Define("dict_set_many", sig("engine", "d", "replacements"), func(a args, _ kwargs) Value {
		d := a[1].(*Dict).Copy(true)
		a[2].(*Dict).Items(func(k, v Value) { d.Set(k, v) })
		return d
	}, ParamPyAlias("d", PyMapping, "dict"), ParamPy("replacements", PyMapping), Name("set"), Method, NoKwargs))
	register(ctx, Define("dict_set_many_inline", varargs(sig("engine", "d"), "args"), func(a args, _ kwargs) Value {
		d := a[1].(*Dict).Copy(true)
		for _, r := range a[2:] {
			m := r.(*MappingRule)
			d.Set(m.Source, m.Destination)
		}
		return d
	}, NoKwargs, Method, ParamPy("args", PyMappingRule), ParamPyAlias("d", PyMapping, "dict"), Name("set")))
	view := func(pyName, name string, kind int) *FunctionDefinition {
		return Define(pyName, sig("d"), func(a args, _ kwargs) Value {
			return &DictView{D: a[0].(*Dict), Kind: kind}
		}, ParamPyAlias("d", PyMapping, "dict"), Name(name), Method)
	}
	register(ctx, view("dict_keys", "keys", ViewKeys))
	register(ctx, view("dict_values", "values", ViewValues))
	register(ctx, view("dict_items", "items", ViewItems))
	register(ctx, Define("in_", sig("value", "collection"), func(a args, _ kwargs) Value {
		return pyContains(a[1], a[0])
	}, ParamNullable("value", true), Param("collection", Iterable(false)), Name("#operator_in")))
	register(ctx, Define("contains", sig("collection", "value"), func(a args, _ kwargs) Value {
		return pyContains(a[0], a[1])
	}, ParamNullable("value", true), Param("collection", Iterable(false)), Method))
	register(ctx, Define("contains_key", sig("d", "key"), func(a args, _ kwargs) Value {
		return a[0].(*Dict).Contains(a[1])
	}, ParamNullable("key", true), ParamPyAlias("d", PyMapping, "dict"), Method))
	register(ctx, Define("contains_value", sig("d", "value"), func(a args, _ kwargs) Value {
		for _, v := range a[0].(*Dict).Values() {
			if Equal(v, a[1]) {
				return true
			}
		}
		return false
	}, ParamNullable("value", true), ParamPyAlias("d", PyMapping, "dict"), Method))
	register(ctx, Define("combine_lists", sig("left", "right", "engine"), func(a args, _ kwargs) Value {
		lt, lok := a[0].(Tuple)
		rt, rok := a[1].(Tuple)
		if lok && rok {
			return append(append(Tuple{}, lt...), rt...)
		}
		ls, lok := a[0].(*Set)
		rs, rok := a[1].(*Set)
		if lok && rok {
			return ls.Union(rs)
		}
		return chain(a[0], a[1])
	}, Param("left", Iterable(false)), Param("right", Iterable(false)), Name("#operator_+")))
	register(ctx, Define("list_by_int", sig("left", "right", "engine"), func(a args, _ kwargs) Value {
		return multiplySequence(a[0], a[1])
	}, Param("left", Sequence(false)), ParamPy("right", PyInt), Name("#operator_*")))
	register(ctx, Define("int_by_list", sig("left", "right", "engine"), func(a args, _ kwargs) Value {
		return multiplySequence(a[1], a[0])
	}, ParamPy("left", PyInt), Param("right", Sequence(false)), Name("#operator_*")))
	register(ctx, Define("combine_dicts", sig("left", "right", "engine"), func(a args, _ kwargs) Value {
		d := a[0].(*Dict).Copy(true)
		a[1].(*Dict).Items(func(k, v Value) { d.Set(k, v) })
		return d
	}, ParamPy("left", PyMapping), ParamPy("right", PyMapping), Name("#operator_+")))
	register(ctx, Define("is_dict", sig("arg"), func(a args, _ kwargs) Value { return IsMapping(a[0]) }))
	register(ctx, Define("is_list", sig("arg"), func(a args, _ kwargs) Value { return IsSequence(a[0]) }))
	register(ctx, Define("dict_len", sig("d"), func(a args, _ kwargs) Value {
		return Int(int64(a[0].(*Dict).Len()))
	}, ParamPyAlias("d", PyMapping, "dict"), ExtensionMethod, Name("len")))
	register(ctx, Define("sequence_len", sig("sequence"), func(a args, _ kwargs) Value {
		return Int(int64(Len(a[0])))
	}, Param("sequence", Sequence(false)), ExtensionMethod, Name("len")))
	register(ctx, Define("delete", withDefaults(sig("collection", "position", "count"), "count", Int(1)), func(a args, _ kwargs) Value {
		pos, count := bigInt(a[1]), bigInt(a[2])
		return generator(func(yield func(Value) bool) {
			i := int64(0)
			for t := range Seq(a[0]) {
				bi := big.NewInt(i)
				end := new(big.Int).Add(pos, count)
				keep := false
				if count.Sign() >= 0 {
					keep = !(pos.Cmp(bi) <= 0 && bi.Cmp(end) < 0)
				} else {
					keep = !(bi.Cmp(pos) >= 0)
				}
				if keep && !yield(t) {
					return
				}
				i++
			}
		})
	}, Method, Param("collection", Iterable(false)), ParamPy("position", PyInt), ParamPy("count", PyInt)))
	inRange := func(i int64, pos, count *big.Int) bool {
		bi := big.NewInt(i)
		end := new(big.Int).Add(pos, count)
		return count.Sign() >= 0 && pos.Cmp(bi) <= 0 && bi.Cmp(end) < 0 || count.Sign() < 0 && bi.Cmp(pos) >= 0
	}
	replaceRange := func(collection Value, pos, count *big.Int, emit func(yield func(Value) bool) bool) Iterator {
		return generator(func(yield func(Value) bool) {
			emitted := false
			i := int64(0)
			for t := range Seq(collection) {
				if inRange(i, pos, count) {
					if !emitted {
						emitted = true
						if !emit(yield) {
							return
						}
					}
				} else if !yield(t) {
					return
				}
				i++
			}
		})
	}
	register(ctx, Define("replace", withDefaults(sig("collection", "position", "value", "count"), "count", Int(1)), func(a args, _ kwargs) Value {
		return replaceRange(a[0], bigInt(a[1]), bigInt(a[3]), func(yield func(Value) bool) bool {
			return yield(a[2])
		})
	}, Method, Param("collection", Iterable(false)), ParamPy("position", PyInt), ParamPy("count", PyInt)))
	register(ctx, Define("replace_many", withDefaults(sig("collection", "position", "values", "count"), "count", Int(1)), func(a args, _ kwargs) Value {
		return replaceRange(a[0], bigInt(a[1]), bigInt(a[3]), func(yield func(Value) bool) bool {
			for v := range Seq(a[2]) {
				if !yield(v) {
					return false
				}
			}
			return true
		})
	}, Method, Param("collection", Iterable(false)), ParamPy("position", PyInt), ParamPy("count", PyInt), Param("values", Iterable(false))))
	deleteKeys := func(d *Dict, keys Value) Value {
		out := d.Copy(false)
		for k := range Seq(keys) {
			out.Delete(k)
		}
		return out
	}
	register(ctx, Define("delete_keys", varargs(sig("d"), "keys"), func(a args, _ kwargs) Value {
		return deleteKeys(a[0].(*Dict), Tuple(a[1:]))
	}, Method, Name("delete"), ParamPyAlias("d", PyMapping, "dict")))
	register(ctx, Define("delete_keys_seq", sig("d", "keys"), func(a args, _ kwargs) Value {
		return deleteKeys(a[0].(*Dict), a[1])
	}, Method, Name("deleteAll"), ParamPyAlias("d", PyMapping, "dict"), Param("keys", Iterable(false))))
	register(ctx, Define("iter_insert", sig("collection", "position", "value"), func(a args, _ kwargs) Value {
		pos := bigInt(a[1])
		return generator(func(yield func(Value) bool) {
			i := int64(-1)
			for t := range Seq(a[0]) {
				i++
				if pos.Cmp(big.NewInt(i)) == 0 && !yield(a[2]) {
					return
				}
				if !yield(t) {
					return
				}
			}
			if pos.Cmp(big.NewInt(i)) > 0 {
				yield(a[2])
			}
		})
	}, Method, Param("collection", Iterable(false, func(v Value) bool { return !IsSet(v) })),
		ParamNullable("value", true), ParamPy("position", PyInt), Name("insert")))
	register(ctx, Define("list_insert", sig("collection", "position", "value"), func(a args, _ kwargs) Value {
		items := append([]Value{}, SequenceItems(a[0])...)
		pos := bigInt(a[1])
		n := int64(len(items))
		var p int64
		switch {
		case !pos.IsInt64():
			if pos.Sign() > 0 {
				p = n
			}
		default:
			p = pos.Int64()
			if p < 0 {
				p += n
				if p < 0 {
					p = 0
				}
			}
			if p > n {
				p = n
			}
		}
		items = append(items[:p], append([]Value{a[2]}, items[p:]...)...)
		return &List{Items: items}
	}, Method, Param("collection", Sequence(false)), ParamNullable("value", true), ParamPy("position", PyInt), Name("insert")))
	register(ctx, Define("insert_many", sig("collection", "position", "values"), func(a args, _ kwargs) Value {
		pos := bigInt(a[1])
		return generator(func(yield func(Value) bool) {
			emit := func() bool {
				for v := range Seq(a[2]) {
					if !yield(v) {
						return false
					}
				}
				return true
			}
			i := int64(-1)
			if pos.Sign() < 0 && !emit() {
				return
			}
			for t := range Seq(a[0]) {
				i++
				if pos.Cmp(big.NewInt(i)) == 0 && !emit() {
					return
				}
				if !yield(t) {
					return
				}
			}
			if pos.Cmp(big.NewInt(i)) > 0 {
				emit()
			}
		})
	}, Method, Param("collection", Iterable(false)), Param("values", Iterable(false)), ParamPy("position", PyInt)))
	if noSets {
		return
	}
	register(ctx, Define("is_set", sig("arg"), func(a args, _ kwargs) Value { return IsSet(a[0]) }))
	register(ctx, Define("set_", varargs(sig("delegate"), "args"), func(a args, _ kwargs) Value {
		return call(a[0], flattenIterators(a[1:]))
	}, ParamNullable("args", true), Inject("delegate", Delegate("to_set", true))))
	register(ctx, Define("to_set", sig("collection"), func(a args, _ kwargs) Value {
		return NewSet(ToSlice(a[0]))
	}, Method, Param("collection", Iterable(false))))
	register(ctx, Define("set_len", sig("s"), func(a args, _ kwargs) Value {
		return Int(int64(Len(a[0])))
	}, ParamPyAlias("s", PySet, "set"), ExtensionMethod, Name("len")))
	asSet := func(v Value, method string) *Set {
		s, ok := v.(*Set)
		if !ok {
			Raise("AttributeError", "'%s' object has no attribute '%s'", viewClassName(v), method)
		}
		return s
	}
	toSet := func(v Value) *Set {
		if s, ok := v.(*Set); ok {
			return s
		}
		return NewSet(ToSlice(v))
	}
	setCompare := func(name, op string, fn func(l, r *Set) bool) *FunctionDefinition {
		return Define(name, sig("left", "right"), func(a args, _ kwargs) Value {
			return fn(toSet(a[0]), toSet(a[1]))
		}, ParamPy("left", PySet), ParamPy("right", PySet), Name("#operator_"+op))
	}
	register(ctx, setCompare("set_lt", "<", func(l, r *Set) bool { return l.Len() < r.Len() && l.IsSubset(r) }))
	register(ctx, setCompare("set_lte", "<=", func(l, r *Set) bool { return l.IsSubset(r) }))
	register(ctx, setCompare("set_gt", ">", func(l, r *Set) bool { return r.Len() < l.Len() && r.IsSubset(l) }))
	register(ctx, setCompare("set_gte", ">=", func(l, r *Set) bool { return r.IsSubset(l) }))
	register(ctx, Define("set_add", varargs(sig("s"), "values"), func(a args, _ kwargs) Value {
		return asSet(a[0], "union").Union(NewSet(a[1:]))
	}, ParamPyAlias("s", PySet, "set"), Method, Name("add")))
	register(ctx, Define("set_remove", varargs(sig("s"), "values"), func(a args, _ kwargs) Value {
		return asSet(a[0], "difference").Difference(NewSet(a[1:]))
	}, ParamPyAlias("s", PySet, "set"), Method, Name("remove")))
	setOp := func(name, method string, fn func(l, r *Set) *Set) *FunctionDefinition {
		return Define(name, sig("left", "right"), func(a args, _ kwargs) Value {
			return fn(asSet(a[0], method), toSet(a[1]))
		}, ParamPy("left", PySet), ParamPy("right", PySet), Method)
	}
	register(ctx, setOp("union", "union", (*Set).Union))
	register(ctx, setOp("intersect", "intersection", (*Set).Intersection))
	difference := setOp("difference", "difference", (*Set).Difference)
	register(ctx, difference)
	register(ctx, difference, RegisterOption{Name: "#operator_-", Function: boolPtr(true), Method: boolPtr(false)})
	register(ctx, setOp("symmetric_difference", "symmetric_difference", (*Set).SymmetricDifference))
}

func viewClassName(v Value) string {
	if dv, ok := v.(*DictView); ok && dv.D.Frozen {
		return [...]string{"KeysView", "ValuesView", "ItemsView"}[dv.Kind]
	}
	return TypeName(v)
}

func chain(collections ...Value) Iterator {
	return generator(func(yield func(Value) bool) {
		for _, c := range collections {
			for x := range Seq(c) {
				if !yield(x) {
					return
				}
			}
		}
	})
}
