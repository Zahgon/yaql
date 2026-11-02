package stdlib

import (
	"math/big"

	. "yaql/language"
)

// toExtensionMethod clones the function-only or method-only overloads of
// name so that they can be called both ways (utils.to_extension_method).
func toExtensionMethod(name string, ctx Context) []*FunctionDefinition {
	layers := CollectFunctions(ctx, name, func(fd *FunctionDefinition, _ Context) bool {
		return !fd.IsFunction || !fd.IsMethod
	}, true)
	if len(layers) > 1 {
		Raise("ValueError", "Multi layer functions are not supported by this helper method")
	}
	var out []*FunctionDefinition
	if len(layers) > 0 {
		for _, fd := range layers[0] {
			c := fd.Clone()
			c.IsFunction, c.IsMethod = true, true
			out = append(out, c)
		}
	}
	return out
}

// RegisterLegacy registers the yaql 0.2 compatibility layer.
func RegisterLegacy(ctx Context, tuples bool) {
	if tuples {
		register(ctx, Define("build_tuple", sig("left", "right", "context", "engine"), func(a args, _ kwargs) Value {
			left := a[0].(Expression)
			c, engine := a[2].(Context), a[3].(*Engine)
			if f, ok := left.(*FunctionExpr); ok && f.Kind == KindBinaryOperator && f.Operator == "=>" {
				prefix := left.Eval(NoValue, c, engine)
				return append(append(Tuple{}, SequenceItems(prefix)...), a[1])
			}
			return Tuple{left.Eval(NoValue, c, engine), a[1]}
		}, Param("left", YaqlExpressionType{}), Name("#operator_=>")))
		register(ctx, Define("to_list", sig("collection"), func(a args, _ kwargs) Value {
			return &List{Items: ToSlice(a[0])}
		}, Method, Param("collection", Iterable(false))))
		register(ctx, Define("tuple_", varargs(sig(), "args"), func(a args, _ kwargs) Value {
			return Tuple(append([]Value{}, a...))
		}))
	}
	legacyDict := Define("dict_", varargs(sig("delegate"), "tuples"), func(a args, _ kwargs) Value {
		return call(a[0], "dict", Tuple(append([]Value{}, a[1:]...)))
	}, ParamPy("tuples", PyTuple), Inject("delegate", SuperType{WithName: true}), NoKwargs, ExtensionMethod)
	register(ctx, legacyDict)
	registerAs(ctx, legacyDict, "#map")
	indexer := Define("indexer", sig("collection", "index_expression"), func(a args, _ kwargs) Value {
		if PySequence.IsInstance(a[0]) {
			index := call(a[1])
			if _, ok := index.(*big.Int); ok {
				return pyGetItem(a[0], index)
			}
		}
		fn := a[1]
		return generator(func(yield func(Value) bool) {
			for t := range Seq(a[0]) {
				if Truthy(call(fn, t)) && !yield(t) {
					return
				}
			}
		})
	}, Param("collection", Iterable(false)), Param("index_expression", LambdaType{}))
	ctx.RegisterExclusive(Bind(indexer, ctx.Convention(), RegisterOption{Name: "#indexer"}))
	register(ctx, Define("range_", withDefaults(sig("start", "stop"), "stop", nil), func(a args, _ kwargs) Value {
		if a[1] == nil {
			return Count(bigInt(a[0]), big.NewInt(1))
		}
		return &Range{Start: bigInt(a[0]), Stop: bigInt(a[1]), Step: big.NewInt(1)}
	}, ParamPy("start", PyInt), ParamPy("stop", PyInt, true), ExtensionMethod))
	sw := Define("switch", varargs(sig("value", "context"), "conditions"), func(a args, _ kwargs) Value {
		c := a[1].(Context).CreateChildContext()
		c.Set("", a[0])
		for _, cond := range a[2:] {
			res := call(cond, c)
			switch r := res.(type) {
			case Tuple:
				if len(r) != 2 {
					Raise("ValueError", "switch() tuples must be of size 2")
				}
				if Truthy(r[0]) {
					return r[1]
				}
			case *MappingRule:
				if Truthy(r.Source) {
					return r.Destination
				}
			default:
				Raise("ValueError", "switch() must have tuple or mapping parameters")
			}
		}
		return nil
	}, Param("conditions", LambdaType{WithContext: true}), NoKwargs, ExtensionMethod)
	ctx.RegisterExclusive(Bind(sw, ctx.Convention(), RegisterOption{}))
	register(ctx, Define("as_", varargs(sig("context", "receiver"), "mappings"), func(a args, _ kwargs) Value {
		c, receiver := a[0].(Context), a[1]
		for _, m := range a[2:] {
			tt := call(m, receiver)
			switch r := tt.(type) {
			case Tuple:
				if len(r) != 2 {
					Raise("ValueError", "as() tuples must be of size 2")
				}
				c.Set(nameOf(r[1]), r[0])
			case *MappingRule:
				c.Set(nameOf(r.Destination), r.Source)
			default:
				Raise("ValueError", "as() must have tuple parameters")
			}
		}
		c.Set("$0", receiver)
		return c
	}, Param("mappings", LambdaType{}), Method, NoKwargs))
	register(ctx, Define("op_dot_context", sig("receiver", "expr"), func(a args, _ kwargs) Value {
		c := a[0].(Context)
		v, _ := c.Get("$0")
		return call(a[1], v, c)
	}, ParamPy("receiver", PyContext), Param("expr", LambdaType{WithContext: true, IsMethod: true}), Name("#operator_.")))
	register(ctx, Define("dict_keyword_access", sig("d", "key"), func(a args, _ kwargs) Value {
		v, _ := a[0].(*Dict).Get(a[1])
		return v
	}, ParamPyAlias("d", PyMapping, "dict"), Param("key", KeywordType(true)), Name("#operator_.")))
	for _, name := range []string{"get", "list", "bool", "int", "float", "select", "where", "join", "sum", "take_while"} {
		for _, fd := range toExtensionMethod(name, ctx) {
			ctx.RegisterFunction(fd)
		}
	}
}

func nameOf(v Value) string {
	s, ok := v.(string)
	if !ok {
		Raise("TypeError", "can only concatenate str (not \"%s\") to str", TypeName(v))
	}
	return s
}
