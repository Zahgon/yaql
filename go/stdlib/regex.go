package stdlib

import (
	"fmt"

	. "yaql/language"
)

func matchRecord(m *Match, i int) *Dict {
	rec := NewDict(false)
	rec.Set("value", m.Group(i))
	rec.Set("start", Int(int64(m.Start(i))))
	rec.Set("end", Int(int64(m.End(i))))
	return rec
}

// publishMatch mirrors regex._publish_match, including how it iterates the
// values of named groups.
func publishMatch(ctx Context, re *Regex, m *Match) {
	ctx.Set("$1", matchRecord(m, 0))
	for i := 1; i <= m.NumGroups(); i++ {
		ctx.Set(fmt.Sprintf("$%d", i+1), matchRecord(m, i))
	}
	for _, name := range re.GroupNames() {
		v := m.Group(re.GroupIndex(name))
		s, ok := v.(string)
		if !ok {
			Raise("TypeError", "cannot unpack non-iterable NoneType object")
		}
		rs := []rune(s)
		if len(rs) < 2 {
			Raise("ValueError", "not enough values to unpack (expected 2, got %d)", len(rs))
		}
		if len(rs) > 2 {
			Raise("ValueError", "too many values to unpack (expected 2)")
		}
		idx := re.GroupIndex(string(rs[1]))
		if idx < 0 {
			Raise("IndexError", "no such group")
		}
		rec := NewDict(false)
		rec.Set("value", string(rs[1]))
		rec.Set("start", Int(int64(m.Start(idx))))
		rec.Set("end", Int(int64(m.End(idx))))
		ctx.Set("$"+string(rs[0]), rec)
	}
}

func compileString(pattern string) *Regex { return CompileRegex(pattern, 0) }

// RegisterRegex registers the regular expression functions.
func RegisterRegex(ctx Context) {
	register(ctx, Define("regex", withDefaults(sig("pattern", "ignore_case", "multi_line", "dot_all"),
		"ignore_case", false, "multi_line", false, "dot_all", false), func(a args, _ kwargs) Value {
		flags := 0
		if Truthy(a[1]) {
			flags |= ReIgnoreCase
		}
		if Truthy(a[2]) {
			flags |= ReMultiline
		}
		if Truthy(a[3]) {
			flags |= ReDotAll
		}
		return CompileRegex(a[0].(string), flags)
	}, Param("pattern", String(false))))
	register(ctx, Define("matches", sig("regexp", "string"), func(a args, _ kwargs) Value {
		return a[0].(*Regex).Search(a[1].(string)) != nil
	}, ParamPy("regexp", PyPattern), Param("string", String(false)), Method))
	register(ctx, Define("matches_", sig("string", "regexp"), func(a args, _ kwargs) Value {
		return compileString(a[1].(string)).Search(a[0].(string)) != nil
	}, Param("string", String(false)), Param("regexp", String(false)), Method))
	register(ctx, Define("matches_operator_string", sig("string", "pattern"), func(a args, _ kwargs) Value {
		return compileString(a[1].(string)).Search(a[0].(string)) != nil
	}, Param("pattern", String(false)), Param("string", String(false)), Name("#operator_=~")))
	register(ctx, Define("matches_operator_regex", sig("string", "regexp"), func(a args, _ kwargs) Value {
		return a[1].(*Regex).Search(a[0].(string)) != nil
	}, ParamPy("regexp", PyPattern), Param("string", String(false)), Name("#operator_=~")))
	register(ctx, Define("not_matches_operator_string", sig("string", "pattern"), func(a args, _ kwargs) Value {
		return compileString(a[1].(string)).Search(a[0].(string)) == nil
	}, Param("pattern", String(false)), Param("string", String(false)), Name("#operator_!~")))
	register(ctx, Define("not_matches_operator_regex", sig("string", "regexp"), func(a args, _ kwargs) Value {
		return a[1].(*Regex).Search(a[0].(string)) == nil
	}, ParamPy("regexp", PyPattern), Param("string", String(false)), Name("#operator_!~")))
	register(ctx, Define("search", withDefaults(sig("context", "regexp", "string", "selector"), "selector", nil), func(a args, _ kwargs) Value {
		ctx, re := a[0].(Context), a[1].(*Regex)
		m := re.Search(a[2].(string))
		newCtx := ctx.CreateChildContext()
		if m == nil {
			return nil
		}
		if a[3] == nil {
			return m.Group(0)
		}
		publishMatch(newCtx, re, m)
		return call(a[3], newCtx)
	}, ParamPy("regexp", PyPattern), Param("string", String(false)), Param("selector", LambdaType{WithContext: true}), Method))
	register(ctx, Define("search_all", withDefaults(sig("context", "regexp", "string", "selector"), "selector", nil), func(a args, _ kwargs) Value {
		ctx, re := a[0].(Context), a[1].(*Regex)
		return generator(func(yield func(Value) bool) {
			for _, m := range re.FindAll(a[2].(string)) {
				newCtx := ctx.CreateChildContext()
				var out Value
				if a[3] == nil {
					out = m.Group(0)
				} else {
					publishMatch(newCtx, re, m)
					out = call(a[3], newCtx)
				}
				if !yield(out) {
					return
				}
			}
		})
	}, ParamPy("regexp", PyPattern), Param("string", String(false)), Param("selector", LambdaType{WithContext: true}), Method))
	register(ctx, Define("split", withDefaults(sig("regexp", "string", "max_split"), "max_split", Int(0)), func(a args, _ kwargs) Value {
		return a[0].(*Regex).Split(a[1].(string), smallInt(a[2]))
	}, ParamPy("regexp", PyPattern), Param("string", String(false)), ParamPy("max_split", PyInt), Method))
	register(ctx, Define("split_string", withDefaults(sig("string", "regexp", "max_split"), "max_split", Int(0)), func(a args, _ kwargs) Value {
		return a[1].(*Regex).Split(a[0].(string), smallInt(a[2]))
	}, ParamPy("regexp", PyPattern), Param("string", String(false)), ParamPy("max_split", PyInt), Method, Name("split")))
	sub := func(re *Regex, s, repl string, count int) Value {
		return re.Sub(s, count, func(m *Match) string { return re.ExpandTemplate(repl, m) })
	}
	register(ctx, Define("replace", withDefaults(sig("regexp", "string", "repl", "count"), "count", Int(0)), func(a args, _ kwargs) Value {
		return sub(a[0].(*Regex), a[1].(string), a[2].(string), smallInt(a[3]))
	}, ParamPy("regexp", PyPattern), Param("string", String(false)), Param("repl", String(false)), ParamPy("count", PyInt), Method))
	register(ctx, Define("replace_string", withDefaults(sig("string", "regexp", "repl", "count"), "count", Int(0)), func(a args, _ kwargs) Value {
		return sub(a[1].(*Regex), a[0].(string), a[2].(string), smallInt(a[3]))
	}, ParamPy("regexp", PyPattern), Param("string", String(false)), Param("repl", String(false)), ParamPy("count", PyInt), Method, Name("replace")))
	replaceBy := func(ctx Context, re *Regex, s string, repl Value, count int) Value {
		return re.Sub(s, count, func(m *Match) string {
			newCtx := ctx.CreateChildContext()
			publishMatch(ctx, re, m)
			r := call(repl, newCtx)
			str, ok := r.(string)
			if !ok {
				Raise("TypeError", "expected str instance, %s found", TypeName(r))
			}
			return str
		})
	}
	register(ctx, Define("replace_by", withDefaults(sig("context", "regexp", "string", "repl", "count"), "count", Int(0)), func(a args, _ kwargs) Value {
		return replaceBy(a[0].(Context), a[1].(*Regex), a[2].(string), a[3], smallInt(a[4]))
	}, ParamPy("regexp", PyPattern), Param("string", String(false)), Param("repl", LambdaType{WithContext: true}), ParamPy("count", PyInt), Method))
	register(ctx, Define("replace_by_string", withDefaults(sig("context", "string", "regexp", "repl", "count"), "count", Int(0)), func(a args, _ kwargs) Value {
		return replaceBy(a[0].(Context), a[2].(*Regex), a[1].(string), a[3], smallInt(a[4]))
	}, ParamPy("regexp", PyPattern), Param("string", String(false)), Param("repl", LambdaType{WithContext: true}), ParamPy("count", PyInt),
		Method, Name("replaceBy")))
	register(ctx, Define("escape_regex", sig("string"), func(a args, _ kwargs) Value {
		return EscapeRegex(a[0].(string))
	}, Param("string", String(false))))
	register(ctx, Define("is_regex", sig("value"), func(a args, _ kwargs) Value {
		_, ok := a[0].(*Regex)
		return ok
	}))
}
