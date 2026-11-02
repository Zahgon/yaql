package stdlib

import (
	. "yaql/language"
)

func constant(v Value) func(args, kwargs) Value {
	return func(args, kwargs) Value { return v }
}

// RegisterCommon registers equality and the null-ordering operators.
func RegisterCommon(ctx Context) {
	register(ctx, Define("eq", sig("left", "right"), func(a args, _ kwargs) Value {
		return Equal(a[0], a[1])
	}, Name("*equal")))
	register(ctx, Define("neq", sig("left", "right"), func(a args, _ kwargs) Value {
		return !Equal(a[0], a[1])
	}, Name("*not_equal")))

	leftNotNull := func(op string, result bool) *FunctionDefinition {
		return Define("left_null", sig("left", "right"), constant(result),
			ParamPy("right", PyNoneType, true), ParamNullable("left", false), Name("#operator_"+op))
	}
	rightNotNull := func(op string, result bool) *FunctionDefinition {
		return Define("null_right", sig("left", "right"), constant(result),
			ParamPy("left", PyNoneType, true), ParamNullable("right", false), Name("#operator_"+op))
	}
	bothNull := func(op string, result bool) *FunctionDefinition {
		return Define("null_null", sig("left", "right"), constant(result),
			ParamPy("left", PyNoneType, true), ParamPy("right", PyNoneType, true), Name("#operator_"+op))
	}
	register(ctx, leftNotNull("<", false))
	register(ctx, leftNotNull("<=", false))
	register(ctx, leftNotNull(">", true))
	register(ctx, leftNotNull(">=", true))
	register(ctx, rightNotNull("<", true))
	register(ctx, rightNotNull("<=", true))
	register(ctx, rightNotNull(">", false))
	register(ctx, rightNotNull(">=", false))
	register(ctx, bothNull("<", false))
	register(ctx, bothNull("<=", true))
	register(ctx, bothNull(">", false))
	register(ctx, bothNull(">=", true))
}

// RegisterBoolean registers the boolean operators and helpers.
func RegisterBoolean(ctx Context) {
	register(ctx, Define("and_", sig("left", "right"), func(a args, _ kwargs) Value {
		l := call(a[0])
		if !Truthy(l) {
			return l
		}
		return call(a[1])
	}, Param("left", LambdaType{}), Param("right", LambdaType{}), Name("#operator_and")))
	register(ctx, Define("or_", sig("left", "right"), func(a args, _ kwargs) Value {
		l := call(a[0])
		if Truthy(l) {
			return l
		}
		return call(a[1])
	}, Param("left", LambdaType{}), Param("right", LambdaType{}), Name("#operator_or")))
	register(ctx, Define("not_", sig("arg"), func(a args, _ kwargs) Value {
		return !Truthy(a[0])
	}, Name("#unary_operator_not")))
	register(ctx, Define("bool_", sig("value"), func(a args, _ kwargs) Value {
		return Truthy(a[0])
	}))
	register(ctx, Define("is_boolean", sig("value"), func(a args, _ kwargs) Value {
		_, ok := a[0].(bool)
		return ok
	}))
}

// RegisterBranching registers switch, selectCase and related functions.
func RegisterBranching(ctx Context) {
	register(ctx, Define("switch", varargs(sig(), "args"), func(a args, _ kwargs) Value {
		for _, m := range a {
			rule := m.(*LazyMappingRule)
			if Truthy(rule.Source()) {
				return rule.Destination()
			}
		}
		return nil
	}, Param("args", MappingRuleType{}), NoKwargs))
	register(ctx, Define("select_case", varargs(sig(), "args"), func(a args, _ kwargs) Value {
		for i, f := range a {
			if Truthy(call(f)) {
				return Int(int64(i))
			}
		}
		return Int(int64(len(a)))
	}, Param("args", LambdaType{})))
	register(ctx, Define("switch_case", varargs(sig("case"), "args"), func(a args, _ kwargs) Value {
		c, fns := bigInt(a[0]), a[1:]
		if c.Sign() >= 0 && c.IsInt64() && c.Int64() < int64(len(fns)) {
			return call(fns[c.Int64()])
		}
		if len(fns) == 0 {
			return nil
		}
		return call(fns[len(fns)-1])
	}, ParamPy("case", PyInt), Param("args", LambdaType{}), Method))
	register(ctx, Define("select_all_cases", varargs(sig(), "args"), func(a args, _ kwargs) Value {
		return generator(func(yield func(Value) bool) {
			for i, f := range a {
				if Truthy(call(f)) {
					if !yield(Int(int64(i))) {
						return
					}
				}
			}
		})
	}, Param("args", LambdaType{})))
	register(ctx, Define("examine", varargs(sig(), "args"), func(a args, _ kwargs) Value {
		return generator(func(yield func(Value) bool) {
			for _, f := range a {
				if !yield(Truthy(call(f))) {
					return
				}
			}
		})
	}, Param("args", LambdaType{})))
	register(ctx, Define("coalesce", varargs(sig(), "args"), func(a args, _ kwargs) Value {
		for _, f := range a {
			if r := call(f); r != nil {
				return r
			}
		}
		return nil
	}, Param("args", LambdaType{})))
}
