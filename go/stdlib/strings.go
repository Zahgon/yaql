package stdlib

import (
	"math"
	"math/big"
	"strings"

	. "yaql/language"
)

func stringCompare(name, op string, accept func(c int) bool) *FunctionDefinition {
	return Define(name, sig("left", "right"), func(a args, _ kwargs) Value {
		return accept(strings.Compare(a[0].(string), a[1].(string)))
	}, Param("left", String(false)), Param("right", String(false)), Name("#operator_"+op))
}

func repeatString(s string, n *big.Int) string {
	if n.Sign() <= 0 {
		return ""
	}
	if !n.IsInt64() || int64(len(s))*n.Int64() > 1<<28 {
		Raise("MemoryError", "")
	}
	return strings.Repeat(s, int(n.Int64()))
}

// yaqlStr is the yaql str() function: null/true/false spelled as in yaql.
func yaqlStr(v Value) string {
	switch v {
	case nil:
		return "null"
	case true:
		return "true"
	case false:
		return "false"
	}
	return Str(v)
}

var stringModule = map[string]string{
	"digits":          "0123456789",
	"hexdigits":       "0123456789abcdefABCDEF",
	"ascii_lowercase": "abcdefghijklmnopqrstuvwxyz",
	"ascii_uppercase": "ABCDEFGHIJKLMNOPQRSTUVWXYZ",
	"ascii_letters":   "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ",
	"octdigits":       "01234567",
	"punctuation":     "!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~",
	"printable":       "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~ \t\n\r\x0b\x0c",
	"whitespace":      " \t\n\r\x0b\x0c",
}

// findInRange searches a[0] for a[1] within the a[3] characters from a[2].
func findInRange(a args, reverse bool) Value {
	s := a[0].(string)
	n := len([]rune(s))
	start, length := smallInt(a[2]), smallInt(a[3])
	if start < 0 {
		start += n
	}
	if length < 0 {
		length = n - start
	}
	return Int(int64(pyFind(s, a[1].(string), start, satAdd(start, length), reverse)))
}

// RegisterStrings registers the string functions.
func RegisterStrings(ctx Context) {
	register(ctx, stringCompare("gt", ">", func(c int) bool { return c > 0 }))
	register(ctx, stringCompare("lt", "<", func(c int) bool { return c < 0 }))
	register(ctx, stringCompare("gte", ">=", func(c int) bool { return c >= 0 }))
	register(ctx, stringCompare("lte", "<=", func(c int) bool { return c <= 0 }))
	register(ctx, Define("len_", sig("string"), func(a args, _ kwargs) Value {
		return Int(int64(len([]rune(a[0].(string)))))
	}, Param("string", String(false)), ExtensionMethod))
	register(ctx, Define("to_lower", sig("string"), func(a args, _ kwargs) Value {
		return strings.ToLower(a[0].(string))
	}, Param("string", String(false)), Method))
	register(ctx, Define("to_upper", sig("string"), func(a args, _ kwargs) Value {
		return strings.ToUpper(a[0].(string))
	}, Param("string", String(false)), Method))
	register(ctx, Define("split", withDefaults(sig("string", "separator", "max_splits"), "separator", nil, "max_splits", Int(-1)),
		func(a args, _ kwargs) Value {
			return pySplit(a[0].(string), a[1], smallInt(a[2]))
		}, Param("string", String(false)), Param("separator", String(true)), ParamPy("max_splits", PyInt), Method))
	register(ctx, Define("right_split", withDefaults(sig("string", "separator", "max_splits"), "separator", nil, "max_splits", Int(-1)),
		func(a args, _ kwargs) Value {
			return pyRSplit(a[0].(string), a[1], smallInt(a[2]))
		}, Param("string", String(false)), Param("separator", String(true)), ParamPy("max_splits", PyInt), Method))
	join := func(sequence, separator, strDelegate Value) Value {
		var parts []string
		for v := range Seq(sequence) {
			s := call(strDelegate, v)
			str, ok := s.(string)
			if !ok {
				Raise("TypeError", "sequence item %d: expected str instance, %s found", len(parts), TypeName(s))
			}
			parts = append(parts, str)
		}
		return strings.Join(parts, separator.(string))
	}
	register(ctx, Define("join", sig("sequence", "separator", "str_delegate"), func(a args, _ kwargs) Value {
		return join(a[0], a[1], a[2])
	}, Param("sequence", Iterable(false)), Param("separator", String(false)), Inject("str_delegate", Delegate("str", false)), Method))
	register(ctx, Define("join_", sig("separator", "sequence", "str_delegate"), func(a args, _ kwargs) Value {
		return join(a[1], a[0], a[2])
	}, Param("sequence", Iterable(false)), Param("separator", String(false)), Inject("str_delegate", Delegate("str", false)), Method))
	register(ctx, Define("str_", sig("value"), func(a args, _ kwargs) Value {
		return yaqlStr(a[0])
	}, ParamNullable("value", true)))
	concat := Define("concat", varargs(sig(), "args"), func(a args, _ kwargs) Value {
		var b strings.Builder
		for _, s := range a {
			b.WriteString(s.(string))
		}
		return b.String()
	}, Param("args", String(false)))
	register(ctx, concat)
	registerAs(ctx, concat, "#operator_+")
	register(ctx, Define("trim", withDefaults(sig("string", "chars"), "chars", nil), func(a args, _ kwargs) Value {
		return pyStrip(a[0].(string), a[1])
	}, Param("string", String(false)), Param("chars", String(true)), Method))
	register(ctx, Define("trim_left", withDefaults(sig("string", "chars"), "chars", nil), func(a args, _ kwargs) Value {
		return pyLStrip(a[0].(string), a[1])
	}, Param("string", String(false)), Param("chars", String(true)), Method))
	register(ctx, Define("trim_right", withDefaults(sig("string", "chars"), "chars", nil), func(a args, _ kwargs) Value {
		return pyRStrip(a[0].(string), a[1])
	}, Param("string", String(false)), Param("chars", String(true)), Method))
	register(ctx, Define("replace", withDefaults(sig("string", "old", "new", "count"), "count", Int(-1)), func(a args, _ kwargs) Value {
		return pyReplace(a[0].(string), a[1].(string), a[2].(string), smallInt(a[3]))
	}, Param("string", String(false)), Param("old", String(false)), Param("new", String(false)), ParamPy("count", PyInt), Method))
	register(ctx, Define("replace_with_dict", withDefaults(sig("string", "str_func", "replacements", "count"), "count", Int(-1)),
		func(a args, _ kwargs) Value {
			s, strFunc := a[0].(string), a[1]
			a[2].(*Dict).Items(func(k, v Value) {
				s = pyReplace(s, call(strFunc, k).(string), call(strFunc, v).(string), smallInt(a[3]))
			})
			return s
		}, Param("string", String(false)), ParamPy("replacements", PyMapping), ParamPy("count", PyInt),
		Inject("str_func", Delegate("str", false)), Method, Name("replace")))
	register(ctx, Define("is_empty", withDefaults(sig("string", "trim_spaces", "chars"), "trim_spaces", true, "chars", nil),
		func(a args, _ kwargs) Value {
			if a[0] == nil {
				return true
			}
			s := a[0].(string)
			if Truthy(a[1]) {
				s = pyStrip(s, a[2])
			}
			return s == ""
		}, Param("string", String(true)), ParamPyAlias("trim_spaces", PyBool, "trim"), Param("chars", String(true)), ExtensionMethod))
	stringByInt := func(s string, n Value) Value { return repeatString(s, bigInt(n)) }
	register(ctx, Define("string_by_int", sig("left", "right", "engine"), func(a args, _ kwargs) Value {
		return stringByInt(a[0].(string), a[1])
	}, Param("left", String(false)), ParamPy("right", PyInt), Name("#operator_*")))
	register(ctx, Define("int_by_string", sig("left", "right", "engine"), func(a args, _ kwargs) Value {
		return stringByInt(a[1].(string), a[0])
	}, ParamPy("left", PyInt), Param("right", String(false)), Name("#operator_*")))
	register(ctx, Define("in_", sig("left", "right"), func(a args, _ kwargs) Value {
		return strings.Contains(a[1].(string), a[0].(string))
	}, Param("left", String(false)), Param("right", String(false)), Name("#operator_in")))
	register(ctx, Define("substring", withDefaults(sig("string", "start", "length"), "length", Int(-1)), func(a args, _ kwargs) Value {
		rs := []rune(a[0].(string))
		start, length := smallInt(a[1]), smallInt(a[2])
		if length < 0 {
			length = len(rs)
		}
		if start < 0 {
			start += len(rs)
		}
		s, e := sliceBounds(len(rs), start, satAdd(start, length))
		if s >= e {
			return ""
		}
		return string(rs[s:e])
	}, Param("string", String(false)), ParamPy("start", PyInt), ParamPy("length", PyInt), Method))
	register(ctx, Define("index_of", withDefaults(sig("string", "sub", "start"), "start", Int(0)), func(a args, _ kwargs) Value {
		s := a[0].(string)
		return Int(int64(pyFind(s, a[1].(string), smallInt(a[2]), math.MaxInt32, false)))
	}, Param("string", String(false)), Param("sub", String(false)), ParamPy("start", PyInt), Method))
	register(ctx, Define("index_of_", sig("string", "sub", "start", "length"), func(a args, _ kwargs) Value {
		return findInRange(a, false)
	}, Param("string", String(false)), Param("sub", String(false)), ParamPy("start", PyInt), ParamPy("length", PyInt), Method))
	register(ctx, Define("last_index_of", withDefaults(sig("string", "sub", "start"), "start", Int(0)), func(a args, _ kwargs) Value {
		s := a[0].(string)
		return Int(int64(pyFind(s, a[1].(string), smallInt(a[2]), math.MaxInt32, true)))
	}, Param("string", String(false)), Param("sub", String(false)), ParamPy("start", PyInt), Method))
	register(ctx, Define("last_index_of_", sig("string", "sub", "start", "length"), func(a args, _ kwargs) Value {
		return findInRange(a, true)
	}, Param("string", String(false)), Param("sub", String(false)), ParamPy("start", PyInt), ParamPy("length", PyInt), Method))
	register(ctx, Define("to_char_array", sig("string"), func(a args, _ kwargs) Value {
		var out Tuple
		for _, r := range a[0].(string) {
			out = append(out, string(r))
		}
		if out == nil {
			out = Tuple{}
		}
		return out
	}, Param("string", String(false)), Method))
	charFlags := []string{"digits", "hexdigits", "ascii_lowercase", "ascii_uppercase", "ascii_letters", "letters",
		"octdigits", "punctuation", "printable", "lowercase", "uppercase", "whitespace"}
	charSig := sig(charFlags...)
	for _, f := range charFlags {
		charSig.Defaults[f] = false
	}
	register(ctx, Define("characters", charSig, func(a args, _ kwargs) Value {
		var b strings.Builder
		for i, f := range charFlags {
			if !Truthy(a[i]) {
				continue
			}
			s, ok := stringModule[f]
			if !ok {
				Raise("AttributeError", "module 'string' has no attribute '%s'", f)
			}
			b.WriteString(s)
		}
		var items []Value
		for _, r := range b.String() {
			items = append(items, string(r))
		}
		return Tuple(NewSet(items).Items())
	}))
	register(ctx, Define("is_string", sig("arg"), func(a args, _ kwargs) Value {
		_, ok := a[0].(string)
		return ok
	}))
	register(ctx, Define("norm", withDefaults(sig("string", "chars"), "chars", nil), func(a args, _ kwargs) Value {
		if a[0] == nil {
			return nil
		}
		v := pyStrip(a[0].(string), a[1])
		if v == "" {
			return nil
		}
		return v
	}, Param("string", String(true)), Param("chars", String(true)), ExtensionMethod))
	affix := func(name string, test func(s, p string) bool) *FunctionDefinition {
		return Define(name, varargs(sig("string"), "args"), func(a args, _ kwargs) Value {
			s := a[0].(string)
			for _, p := range a[1:] {
				if test(s, p.(string)) {
					return true
				}
			}
			return false
		}, Param("string", String(false)), Param("args", String(false)), Method)
	}
	register(ctx, affix("starts_with", strings.HasPrefix))
	register(ctx, affix("ends_with", strings.HasSuffix))
	register(ctx, Define("hex_", sig("num"), func(a args, _ kwargs) Value {
		switch t := a[0].(type) {
		case *big.Int:
			if t.Sign() < 0 {
				return "-0x" + new(big.Int).Neg(t).Text(16)
			}
			return "0x" + t.Text(16)
		}
		Raise("TypeError", "'%s' object cannot be interpreted as an integer", TypeName(a[0]))
		return nil
	}, Param("num", Number(true))))
}

func satAdd(a, b int) int {
	s := a + b
	if b > 0 && s < a {
		return math.MaxInt
	}
	return s
}
