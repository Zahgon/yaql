package stdlib

import (
	"math"
	"math/big"
	"math/rand/v2"

	. "yaql/language"
)

func numericOp(name, op string, fn func(a, b Value) Value) *FunctionDefinition {
	return Define(name, sig("left", "right"), func(a args, _ kwargs) Value { return fn(a[0], a[1]) },
		Param("left", Number(false)), Param("right", Number(false)), Name("#operator_"+op))
}

func compareOp(name, op string, accept func(c int) bool) *FunctionDefinition {
	return numericOp(name, op, func(a, b Value) Value {
		c, ok := numCmp(a, b)
		return ok && accept(c)
	})
}

func intOp(name string, fn func(a, b Value) Value) *FunctionDefinition {
	return Define(name, sig("left", "right"), func(a args, _ kwargs) Value { return fn(a[0], a[1]) },
		ParamPy("left", PyInt), ParamPy("right", PyInt))
}

// bitwise keeps Python's bool & bool -> bool rule.
func bitwise(op func(z, x, y *big.Int) *big.Int, boolOp func(x, y bool) bool) func(a, b Value) Value {
	return func(a, b Value) Value {
		x, xb := a.(bool)
		y, yb := b.(bool)
		if xb && yb {
			return boolOp(x, y)
		}
		return op(new(big.Int), bigInt(a), bigInt(b))
	}
}

// RegisterMath registers arithmetic and numeric functions.
func RegisterMath(ctx Context) {
	register(ctx, numericOp("binary_plus", "+", numAdd))
	register(ctx, numericOp("binary_minus", "-", numSub))
	register(ctx, numericOp("multiplication", "*", numMul))
	register(ctx, numericOp("division", "/", numDiv))
	register(ctx, numericOp("modulo", "mod", numMod))
	register(ctx, Define("unary_plus", sig("op"), func(a args, _ kwargs) Value { return a[0] },
		Param("op", Number(false)), Name("#unary_operator_+")))
	register(ctx, Define("unary_minus", sig("op"), func(a args, _ kwargs) Value { return numNeg(a[0]) },
		Param("op", Number(false)), Name("#unary_operator_-")))
	register(ctx, Define("abs_", sig("op"), func(a args, _ kwargs) Value {
		if f, ok := a[0].(float64); ok {
			return math.Abs(f)
		}
		return new(big.Int).Abs(bigInt(a[0]))
	}, Param("op", Number(false))))
	register(ctx, compareOp("gt", ">", func(c int) bool { return c > 0 }))
	register(ctx, compareOp("gte", ">=", func(c int) bool { return c >= 0 }))
	register(ctx, compareOp("lt", "<", func(c int) bool { return c < 0 }))
	register(ctx, compareOp("lte", "<=", func(c int) bool { return c <= 0 }))
	register(ctx, Define("int_", sig("value"), func(a args, _ kwargs) Value { return pyInt(a[0]) }))
	register(ctx, Define("float_", sig("value"), func(a args, _ kwargs) Value { return pyFloat(a[0]) }))
	register(ctx, Define("random_", sig(), func(args, kwargs) Value { return rand.Float64() }))
	register(ctx, Define("random__", sig("from_", "to_"), func(a args, _ kwargs) Value {
		lo, hi := bigInt(a[0]), bigInt(a[1])
		if lo.Cmp(hi) > 0 {
			Raise("ValueError", "empty range in randrange(%s, %s)", lo, new(big.Int).Add(hi, big.NewInt(1)))
		}
		span := new(big.Int).Sub(hi, lo)
		span.Add(span, big.NewInt(1))
		if span.IsInt64() {
			return new(big.Int).Add(lo, big.NewInt(rand.Int64N(span.Int64())))
		}
		return lo
	}))
	register(ctx, intOp("bitwise_and", bitwise((*big.Int).And, func(x, y bool) bool { return x && y })))
	register(ctx, intOp("bitwise_or", bitwise((*big.Int).Or, func(x, y bool) bool { return x || y })))
	register(ctx, Define("bitwise_not", sig("arg"), func(a args, _ kwargs) Value {
		return new(big.Int).Not(bigInt(a[0]))
	}, ParamPy("arg", PyInt)))
	register(ctx, intOp("bitwise_xor", bitwise((*big.Int).Xor, func(x, y bool) bool { return x != y })))
	register(ctx, Define("shift_bits_left", sig("value", "bits_number"), func(a args, _ kwargs) Value {
		n := bigInt(a[1])
		if n.Sign() < 0 {
			Raise("ValueError", "negative shift count")
		}
		if !n.IsInt64() || n.Int64() > 1<<20 {
			Raise("OverflowError", "too many digits in integer")
		}
		return new(big.Int).Lsh(bigInt(a[0]), uint(n.Int64()))
	}, ParamPy("value", PyInt), ParamPy("bits_number", PyInt)))
	register(ctx, Define("shift_bits_right", sig("value", "bits_number"), func(a args, _ kwargs) Value {
		n := bigInt(a[1])
		if n.Sign() < 0 {
			Raise("ValueError", "negative shift count")
		}
		v := bigInt(a[0])
		if !n.IsInt64() || n.Int64() > int64(v.BitLen())+1 {
			if v.Sign() < 0 {
				return big.NewInt(-1)
			}
			return big.NewInt(0)
		}
		return new(big.Int).Rsh(v, uint(n.Int64()))
	}, ParamPy("value", PyInt), ParamPy("bits_number", PyInt)))
	register(ctx, Define("max_", sig("a", "b", "operator"), func(a args, _ kwargs) Value {
		if Truthy(call(a[2], a[1], a[0])) {
			return a[1]
		}
		return a[0]
	}, ParamNullable("a", true), ParamNullable("b", true), Inject("operator", Delegate("#operator_>", false))))
	register(ctx, Define("min_", sig("a", "b", "operator"), func(a args, _ kwargs) Value {
		if Truthy(call(a[2], a[1], a[0])) {
			return a[0]
		}
		return a[1]
	}, Inject("operator", Delegate("#operator_>", false))))
	register(ctx, Define("pow_", withDefaults(sig("a", "b", "c"), "c", nil), func(a args, _ kwargs) Value {
		return pyPow(a[0], a[1], a[2])
	}, Param("a", Number(false)), Param("b", Number(false)), Param("c", Number(true))))
	register(ctx, Define("sign", sig("num"), func(a args, _ kwargs) Value {
		c, ok := numCmp(a[0], Int(0))
		switch {
		case ok && c > 0:
			return Int(1)
		case ok && c < 0:
			return Int(-1)
		}
		return Int(0)
	}, Param("num", Number(false))))
	register(ctx, Define("round_", withDefaults(sig("number", "ndigits"), "ndigits", Int(0)), func(a args, _ kwargs) Value {
		return pyRound(a[0], bigInt(a[1]))
	}, Param("number", Number(false)), ParamPy("ndigits", PyInt)))
	register(ctx, Define("is_integer", sig("value"), func(a args, _ kwargs) Value {
		_, ok := a[0].(*big.Int)
		return ok
	}))
	register(ctx, Define("is_number", sig("value"), func(a args, _ kwargs) Value {
		switch a[0].(type) {
		case *big.Int, float64:
			return true
		}
		return false
	}))
}

func pyInt(v Value) Value {
	switch t := v.(type) {
	case nil:
		return Int(0)
	case bool, *big.Int:
		return new(big.Int).Set(bigInt(t))
	case float64:
		if math.IsNaN(t) {
			Raise("ValueError", "cannot convert float NaN to integer")
		}
		if math.IsInf(t, 0) {
			Raise("OverflowError", "cannot convert float infinity to integer")
		}
		bi, _ := new(big.Float).SetFloat64(math.Trunc(t)).Int(nil)
		return bi
	case string:
		return pyIntFromString(t)
	}
	Raise("TypeError", "int() argument must be a string, a bytes-like object or a real number, not '%s'", TypeName(v))
	return nil
}

func pyFloat(v Value) Value {
	switch t := v.(type) {
	case nil:
		return 0.0
	case bool, *big.Int, float64:
		return toFloat(t)
	case string:
		return pyFloatFromString(t)
	}
	Raise("TypeError", "float() argument must be a string or a real number, not '%s'", TypeName(v))
	return nil
}
