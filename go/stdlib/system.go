package stdlib

import (
	"fmt"
	"regexp"

	. "yaql/language"
)

func systemFunctions(delegates bool) []*FunctionDefinition {
	getContextData := Define("get_context_data", sig("name", "context"), func(a args, _ kwargs) Value {
		v, _ := a[1].(Context).Get(a[0].(string))
		return v
	}, Param("name", StringConstant(false)), Name("#get_context_data"))

	opDot := Define("op_dot", sig("receiver", "expr"), func(a args, _ kwargs) Value {
		return call(a[1], a[0])
	}, Param("expr", LambdaType{IsMethod: true}), Name("#operator_."))

	elvis := Define("elvis_operator", sig("operator", "receiver", "expr"), func(a args, _ kwargs) Value {
		if a[1] == nil {
			return nil
		}
		return call(a[0], a[1], a[2])
	}, Param("expr", YaqlExpressionType{}), Inject("operator", Delegate("#operator_.", false)), Name("#operator_?."))

	unpack := Define("unpack", varargs(sig("sequence", "context"), "args"), func(a args, _ kwargs) Value {
		seq, ctx, names := a[0], a[1].(Context), a[2:]
		it := Iter(seq)
		var lst []Value
		for len(lst) < len(names)+1 {
			v, ok := it.Next()
			if !ok {
				break
			}
			lst = append(lst, v)
		}
		if len(names) > 0 && len(names) != len(lst) {
			Raise("ValueError", "Cannot unpack %d elements into %d", len(lst), len(names))
		}
		if len(names) > 0 {
			for i := range lst {
				ctx.Set(names[i].(string), lst[i])
			}
		} else {
			i := 1
			for v := range Seq(seq) {
				ctx.Set(fmt.Sprint(i), v)
				i++
			}
		}
		return ctx
	}, Param("sequence", Iterable(false)), Param("args", String(false)), Method)

	with := Define("with_", varargs(sig("context"), "args"), func(a args, _ kwargs) Value {
		ctx := a[0].(Context)
		for i, v := range a[1:] {
			ctx.Set(fmt.Sprint(i+1), v)
		}
		return ctx
	})

	let := Define("let", varkw(varargs(sig("__context__"), "args"), "kwargs"), func(a args, kw kwargs) Value {
		ctx := a[0].(Context)
		for i, v := range a[1:] {
			ctx.Set(fmt.Sprint(i+1), v)
		}
		// Python preserves call order; the runner normalizes it to alphabetical.
		for _, k := range SortedKeys(kw) {
			ctx.Set(k, kw[k])
		}
		return ctx
	}, Inject("__context__", ContextType{}))

	def := Define("def_", sig("name", "func", "context"), func(a args, _ kwargs) Value {
		name, fn, ctx := a[0].(string), a[1], a[2].(Context)
		wrapper := Define("wrapper", varkw(varargs(sig(), "args"), "kwargs"), func(wa args, wk kwargs) Value {
			return callKw(fn, wa, wk)
		}, Name(name))
		register(ctx, wrapper)
		return ctx
	}, Param("name", String(false)), Param("func", LambdaType{}))

	sendContext := Define("send_context", sig("left", "right"), func(a args, _ kwargs) Value {
		return call(a[1], a[0])
	}, ParamPy("left", PyContext), Param("right", LambdaType{WithContext: true}), Name("#operator_->"))

	assert := Define("assert__", withDefaults(sig("engine", "obj", "condition", "message"), "message", "Assertion failed"),
		func(a args, _ kwargs) Value {
			obj := a[1]
			if IsIterator(obj) {
				obj = Memorize(obj)
			}
			if !Truthy(call(a[2], obj)) {
				Raise("AssertionError", "%s", a[3].(string))
			}
			return obj
		}, Method, Param("condition", LambdaType{}), Param("message", String(false)))

	callFn := Define("call", varkw(varargs(sig("callable_"), "args"), "kwargs"), func(a args, kw kwargs) Value {
		return callKw(a[0], a[1:], kw)
	}, Name("#call"), Param("callable_", NewPythonType(PyObject, false, func(v Value) bool {
		_, ok := v.(Callable)
		return ok
	})))

	lambda := Define("lambda_", sig("func"), func(a args, _ kwargs) Value { return a[0] },
		Param("func", LambdaType{}))

	callFunc := Define("call_func", withDefaults(sig("context", "engine", "name", "args", "kwargs", "receiver"), "receiver", NoValue),
		func(a args, _ kwargs) Value {
			ctx, engine, name := a[0].(Context), a[1].(*Engine), a[2].(string)
			kw := filterParametersDict(a[4].(*Dict))
			return Call(name, ctx, SequenceItems(a[3]), kw, engine, a[5], nil, false, nil)
		}, Name("call"), Param("name", String(false)), Param("args", Sequence(false)), ParamPy("kwargs", PyMapping))

	out := []*FunctionDefinition{getContextData, opDot, unpack, with, sendContext, let, def, elvis, assert, callFunc}
	if delegates {
		out = append(out, callFn, lambda)
	}
	return out
}

var keywordRegex = regexp.MustCompile(`^[\p{L}_][\p{L}\p{N}_]*`)

func filterParametersDict(d *Dict) map[string]Value {
	out := map[string]Value{}
	removed := false
	d.Items(func(k, v Value) {
		s, ok := k.(string)
		if !ok {
			Raise("TypeError", "expected string or bytes-like object, got '%s'", TypeName(k))
		}
		if len(s) >= 2 && s[:2] == "__" || !keywordRegex.MatchString(s) {
			removed = true
			return
		}
		out[s] = v
	})
	if removed {
		Raise("RuntimeError", "dictionary changed size during iteration")
	}
	return out
}

func getPropertyFunction() *FunctionDefinition {
	return Define("get_property", sig("func", "obj", "name"), func(a args, _ kwargs) Value {
		return call(a[0], "#property#"+a[2].(string), a[1])
	}, Name("#operator_."), Param("name", KeywordType(true)),
		Inject("func", DelegateType{UseConvention: false}))
}

// RegisterSystem registers the system functions.
func RegisterSystem(ctx Context, delegates bool) {
	for _, fd := range systemFunctions(delegates) {
		register(ctx, fd)
	}
}

// RegisterSystemFallbacks registers the fallback property accessor.
func RegisterSystemFallbacks(ctx Context) {
	register(ctx, getPropertyFunction())
}
