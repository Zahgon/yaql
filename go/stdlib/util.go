// Package stdlib implements the yaql standard library.
package stdlib

import (
	"math/big"

	. "yaql/language"
)

type (
	args   = []Value
	kwargs = map[string]Value
)

// sig builds a signature from positional parameter names; a name may carry a
// default through def().
func sig(names ...string) Signature {
	return Signature{Args: names, Defaults: map[string]Value{}}
}

func withDefaults(s Signature, kv ...Value) Signature {
	for i := 0; i < len(kv); i += 2 {
		s.Defaults[kv[i].(string)] = kv[i+1]
	}
	return s
}

func varargs(s Signature, name string) Signature {
	s.VarArgs = name
	return s
}

func varkw(s Signature, name string) Signature {
	s.VarKw = name
	return s
}

func register(ctx Context, fd *FunctionDefinition, opt ...RegisterOption) {
	o := RegisterOption{}
	if len(opt) > 0 {
		o = opt[0]
	}
	ctx.RegisterFunction(Bind(fd, ctx.Convention(), o))
}

func registerAs(ctx Context, fd *FunctionDefinition, name string) {
	register(ctx, fd, RegisterOption{Name: name})
}

func call(fn Value, a ...Value) Value {
	return fn.(Callable).Call(a, nil)
}

func callKw(fn Value, a []Value, kw map[string]Value) Value {
	return fn.(Callable).Call(a, kw)
}

func bigInt(v Value) *big.Int {
	switch t := v.(type) {
	case *big.Int:
		return t
	case bool:
		if t {
			return big.NewInt(1)
		}
		return big.NewInt(0)
	}
	panic("not an int")
}

func smallInt(v Value) int {
	b := bigInt(v)
	if !b.IsInt64() {
		if b.Sign() > 0 {
			return int(^uint(0) >> 1)
		}
		return -int(^uint(0)>>1) - 1
	}
	return int(b.Int64())
}

func boolPtr(b bool) *bool { return &b }

func generator(body func(yield func(Value) bool)) Iterator { return Generator(body) }
