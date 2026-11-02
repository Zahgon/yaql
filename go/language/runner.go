package language

// Call resolves and invokes the function or method name, as yaql's
// runner.call does. args may hold unevaluated expressions.
func Call(name string, ctx Context, args []Value, kwargs map[string]Value, engine *Engine, receiver Value,
	dataCtx Context, useConvention bool, filter func(*FunctionDefinition, Context) bool) Value {
	if dataCtx == nil {
		dataCtx = ctx
	}
	if filter == nil {
		filter = func(*FunctionDefinition, Context) bool { return true }
	}
	var predicate func(*FunctionDefinition, Context) bool
	if receiver == NoValue {
		predicate = func(fd *FunctionDefinition, c Context) bool { return fd.IsFunction && filter(fd, c) }
	} else {
		predicate = func(fd *FunctionDefinition, c Context) bool { return fd.IsMethod && filter(fd, c) }
	}
	layers := CollectFunctions(ctx, name, predicate, useConvention)
	if len(layers) == 0 {
		if receiver == NoValue {
			Throw(noFunctionRegistered(name))
		}
		Throw(noMethodRegistered(name, receiver))
	}
	delegate := chooseOverload(name, layers, engine, receiver, dataCtx, args, kwargs)
	var result Value
	err := Catch(func() { result = delegate() })
	if err != nil {
		if err.Class == "StopIteration" {
			Throw(&Error{Class: "WrappedException", Message: err.Message, Wrapped: err})
		}
		Throw(err)
	}
	return result
}

type candidate struct {
	fd         *FunctionDefinition
	positional []*ParameterDefinition
	keyword    map[string]*ParameterDefinition
}

func lazySet(c candidate) map[any]bool {
	out := map[any]bool{}
	for i, p := range c.positional {
		if isLazy(p.Type) {
			out[i] = true
		}
	}
	for k, p := range c.keyword {
		if isLazy(p.Type) {
			out[k] = true
		}
	}
	return out
}

func sameSet(a, b map[any]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

func chooseOverload(name string, layers [][]*FunctionDefinition, engine *Engine, receiver Value, ctx Context,
	args []Value, kwargs map[string]Value) func() Value {
	raiseAmbiguous := func() {
		if receiver == NoValue {
			Throw(ambiguousFunction(name))
		}
		Throw(ambiguousMethod(name, receiver))
	}
	raiseNotFound := func() {
		if receiver == NoValue {
			Throw(noMatchingFunction(name))
		}
		Throw(noMatchingMethod(name, receiver))
	}
	if receiver != NoValue {
		args = append([]Value{receiver}, args...)
	}
	var levels [][]candidate
	var lazy map[any]bool
	noKwargsSet := false
	noKwargs := false
	for _, layer := range layers {
		var level []candidate
		for _, fd := range layer {
			if !noKwargsSet {
				noKwargsSet = true
				noKwargs = fd.NoKwargs
				args, kwargs = translateArgs(noKwargs, args, kwargs)
			} else if noKwargs != fd.NoKwargs {
				raiseAmbiguous()
			}
			pos, kw, ok := fd.mapArgs(args, kwargs, ctx, engine)
			if !ok {
				continue
			}
			c := candidate{fd, pos, kw}
			l := lazySet(c)
			if lazy == nil {
				lazy = l
			} else if !sameSet(lazy, l) {
				raiseAmbiguous()
			}
			level = append(level, c)
		}
		if len(level) > 0 {
			levels = append(levels, level)
		}
	}
	if len(levels) == 0 {
		raiseNotFound()
	}
	evaluate := func(key any, arg Value) Value {
		if lazy[key] {
			return arg
		}
		if e, ok := arg.(Expression); ok {
			if _, isConst := e.(*ConstantExpr); !isConst {
				return e.Eval(NoValue, ctx, engine)
			}
		}
		return arg
	}
	evaluated := make([]Value, len(args))
	for i, a := range args {
		evaluated[i] = evaluate(i, a)
	}
	evaluatedKw := map[string]Value{}
	for _, k := range SortedKeys(kwargs) {
		evaluatedKw[k] = evaluate(k, kwargs[k])
	}
	var delegate func() Value
	var winner candidate
	for _, level := range levels {
		for _, c := range level {
			var d func() Value
			err := Catch(func() { d = c.fd.getDelegate(receiver, engine, ctx, evaluated, evaluatedKw) })
			if err != nil {
				if err.Class == "ArgumentException" {
					continue
				}
				Throw(err)
			}
			if delegate != nil {
				if isSpecializationOf(winner, c) {
					continue
				} else if !isSpecializationOf(c, winner) {
					raiseAmbiguous()
				}
			}
			delegate = d
			winner = c
		}
		if delegate != nil {
			break
		}
	}
	if delegate == nil {
		raiseNotFound()
	}
	return delegate
}

func translateArgs(withoutKwargs bool, args []Value, kwargs map[string]Value) ([]Value, map[string]Value) {
	if withoutKwargs {
		if len(kwargs) > 0 {
			Throw(ArgumentError(SortedKeys(kwargs)[0]))
		}
		return args, map[string]Value{}
	}
	var pos []Value
	kw := map[string]Value{}
	for _, a := range args {
		if m, ok := a.(*MappingRuleExpr); ok {
			c, isConst := m.Source.(*ConstantExpr)
			if !isConst || !c.Keyword {
				Throw(mappingTranslationError())
			}
			kw[c.Value.(string)] = m.Destination
		} else {
			pos = append(pos, a)
		}
	}
	for k, v := range kwargs {
		if _, dup := kw[k]; dup {
			Throw(mappingTranslationError())
		}
		kw[k] = v
	}
	return pos, kw
}

func isSpecializationOf(m1, m2 candidate) bool {
	res := false
	n := len(m1.positional)
	if len(m2.positional) < n {
		n = len(m2.positional)
	}
	for i := 0; i < n; i++ {
		a1, a2 := m1.positional[i], m2.positional[i]
		if a2.Type.IsSpecializationOf(a1.Type) {
			return false
		} else if a1.Type.IsSpecializationOf(a2.Type) {
			res = true
		}
	}
	for k, a1 := range m1.keyword {
		a2 := m2.keyword[k]
		if a2 == nil {
			continue
		}
		if a2.Type.IsSpecializationOf(a1.Type) {
			return false
		} else if a1.Type.IsSpecializationOf(a2.Type) {
			res = true
		}
	}
	return res
}
