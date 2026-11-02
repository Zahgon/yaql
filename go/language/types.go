package language

import (
	"math/big"
)

// PyType models the Python classes yaql uses as parameter types.
type PyType struct {
	Name     string
	instance func(Value) bool
	bases    []*PyType
	tuple    bool // a tuple of types, like (int, float)
}

// IsInstance is Python isinstance(v, t).
func (t *PyType) IsInstance(v Value) bool { return t.instance(v) }

// IsSubclass is Python issubclass(t, other).
func (t *PyType) IsSubclass(other *PyType) bool {
	if t == other || other == PyObject {
		return true
	}
	for _, b := range t.bases {
		if b.IsSubclass(other) {
			return true
		}
	}
	return false
}

func newPyType(name string, instance func(Value) bool, bases ...*PyType) *PyType {
	return &PyType{Name: name, instance: instance, bases: bases}
}

// Python classes.
var (
	PyObject   = newPyType("object", func(Value) bool { return true })
	PyNoneType = newPyType("NoneType", func(v Value) bool { return v == nil }, PyObject)
	PyInt      = newPyType("int", func(v Value) bool {
		switch v.(type) {
		case bool, *big.Int:
			return true
		}
		return false
	}, PyObject)
	PyBool  = newPyType("bool", func(v Value) bool { _, ok := v.(bool); return ok }, PyInt)
	PyFloat = newPyType("float", func(v Value) bool { _, ok := v.(float64); return ok }, PyObject)
	// PyIterable is collections.abc.Iterable.
	PyIterable = newPyType("Iterable", func(v Value) bool {
		switch v.(type) {
		case string, Tuple, *List, *Dict, *Set, *DictView, *Range, *OrderingIterable, Iterator:
			return true
		}
		return false
	}, PyObject)
	PyIterator = newPyType("Iterator", func(v Value) bool { return IsIterator(v) }, PyIterable)
	PySequence = newPyType("Sequence", func(v Value) bool {
		switch v.(type) {
		case string, Tuple, *List, *Range:
			return true
		}
		return false
	}, PyIterable)
	PyStr              = newPyType("str", func(v Value) bool { _, ok := v.(string); return ok }, PySequence)
	PyTuple            = newPyType("tuple", func(v Value) bool { _, ok := v.(Tuple); return ok }, PySequence)
	PyMapping          = newPyType("Mapping", func(v Value) bool { return IsMapping(v) }, PyIterable)
	PySet              = newPyType("Set", func(v Value) bool { return IsSet(v) }, PyIterable)
	PyDateTime         = newPyType("datetime", func(v Value) bool { _, ok := v.(*DateTime); return ok }, PyObject)
	PyTimedelta        = newPyType("timedelta", func(v Value) bool { _, ok := v.(Timedelta); return ok }, PyObject)
	PyPattern          = newPyType("Pattern", func(v Value) bool { _, ok := v.(*Regex); return ok }, PyObject)
	PyContext          = newPyType("ContextBase", func(v Value) bool { _, ok := v.(Context); return ok }, PyObject)
	PyOrderingIterable = newPyType("OrderingIterable", func(v Value) bool {
		_, ok := v.(*OrderingIterable)
		return ok
	}, PyIterable)
	PyMappingRule = newPyType("MappingRule", func(v Value) bool { _, ok := v.(*MappingRule); return ok }, PyObject)
	PyNumber      = &PyType{Name: "(int, float)", instance: func(v Value) bool {
		return PyInt.IsInstance(v) || PyFloat.IsInstance(v)
	}, tuple: true}
)

func pyTypeOf(v Value) *PyType {
	switch v.(type) {
	case bool:
		return PyBool
	case *big.Int:
		return PyInt
	case float64:
		return PyFloat
	case string:
		return PyStr
	case Tuple:
		return PyTuple
	case Timedelta:
		return PyTimedelta
	case *DateTime:
		return PyDateTime
	}
	return PyObject
}

// SmartType checks and converts arguments bound to a parameter.
type SmartType interface {
	Check(v Value, ctx Context, engine *Engine) bool
	Convert(v Value, receiver Value, ctx Context, fd *FunctionDefinition, engine *Engine) Value
	IsSpecializationOf(other SmartType) bool
}

// HiddenType marks parameters supplied by the runtime, not the caller.
type HiddenType interface {
	SmartType
	hidden()
}

// LazyType marks parameters that receive unevaluated expressions.
type LazyType interface {
	SmartType
	lazy()
}

type hiddenBase struct{}

func (hiddenBase) hidden()                                 {}
func (hiddenBase) Check(Value, Context, *Engine) bool      { return true }
func (hiddenBase) IsSpecializationOf(other SmartType) bool { return false }

func unwrapConstant(v Value) Value {
	if c, ok := v.(*ConstantExpr); ok {
		return c.Value
	}
	return v
}

// GenericType is a nullable check plus an optional checker and converter.
type GenericType struct {
	Nullable  bool
	Checker   func(v Value, ctx Context, engine *Engine) bool
	Converter func(v Value, receiver Value, ctx Context, fd *FunctionDefinition, engine *Engine) Value
}

func (g *GenericType) Check(v Value, ctx Context, engine *Engine) bool {
	v = unwrapConstant(v)
	if v == nil {
		return g.Nullable
	}
	if _, ok := v.(Expression); ok {
		return true
	}
	if g.Checker == nil {
		return true
	}
	return g.Checker(v, ctx, engine)
}

func (g *GenericType) Convert(v Value, receiver Value, ctx Context, fd *FunctionDefinition, engine *Engine) Value {
	v = unwrapConstant(v)
	if !g.Check(v, ctx, engine) {
		Throw(argumentValueError())
	}
	if v == nil || g.Converter == nil {
		return v
	}
	return g.Converter(v, receiver, ctx, fd, engine)
}

func (g *GenericType) IsSpecializationOf(SmartType) bool { return false }

// PythonType accepts instances of a Python class satisfying all validators.
type PythonType struct {
	GenericType
	Type       *PyType
	Validators []func(Value) bool
	kind       string
}

// NewPythonType is yaqltypes.PythonType(t, nullable).
func NewPythonType(t *PyType, nullable bool, validators ...func(Value) bool) *PythonType {
	p := &PythonType{Type: t, Validators: validators}
	p.Nullable = nullable
	p.Checker = func(v Value, _ Context, _ *Engine) bool {
		if !t.IsInstance(v) {
			return false
		}
		for _, f := range p.Validators {
			if !f(v) {
				return false
			}
		}
		return true
	}
	return p
}

func (p *PythonType) IsSpecializationOf(other SmartType) bool {
	o := asPythonType(other)
	if o == nil || p.Type.tuple || o.Type.tuple {
		return false
	}
	return p.Type.IsSubclass(o.Type) && !o.Type.IsSubclass(p.Type)
}

func asPythonType(t SmartType) *PythonType {
	switch x := t.(type) {
	case *PythonType:
		return x
	case *IterableType:
		return x.PythonType
	case *DateTimeType:
		return x.PythonType
	case *StringType:
		return x.PythonType
	}
	return nil
}

func notStrOrMapping(v Value) bool {
	switch v.(type) {
	case string, *Dict:
		return false
	}
	return true
}

func notBool(v Value) bool { _, ok := v.(bool); return !ok }

// StringType is yaqltypes.String.
type StringType struct{ *PythonType }

// String returns the yaqltypes.String smart type.
func String(nullable bool) *StringType { return &StringType{NewPythonType(PyStr, nullable)} }

// Integer returns yaqltypes.Integer: an int that is not a bool.
func Integer(nullable bool) *PythonType { return NewPythonType(PyInt, nullable, notBool) }

// Number returns yaqltypes.Number: int or float, not bool.
func Number(nullable bool) *PythonType { return NewPythonType(PyNumber, nullable, notBool) }

// DateTimeType is yaqltypes.DateTime: naive values gain the UTC zone.
type DateTimeType struct{ *PythonType }

// DateTimeArg returns yaqltypes.DateTime.
func DateTimeArg(nullable bool) *DateTimeType {
	return &DateTimeType{NewPythonType(PyDateTime, nullable)}
}

func (d *DateTimeType) Convert(v Value, receiver Value, ctx Context, fd *FunctionDefinition, engine *Engine) Value {
	if dt, ok := v.(*DateTime); ok {
		if dt.Zone == nil {
			c := *dt
			c.Zone = UTCZone
			return &c
		}
		return dt
	}
	return d.PythonType.Convert(v, receiver, ctx, fd, engine)
}

// IterableType is yaqltypes.Iterable (and Iterator with an extra validator).
type IterableType struct{ *PythonType }

// Iterable returns yaqltypes.Iterable.
func Iterable(nullable bool, validators ...func(Value) bool) *IterableType {
	return &IterableType{NewPythonType(PyIterable, nullable, append([]func(Value) bool{notStrOrMapping}, validators...)...)}
}

// IteratorArg returns yaqltypes.Iterator.
func IteratorArg(nullable bool) *IterableType { return Iterable(nullable, IsIterator) }

func (it *IterableType) Check(v Value, ctx Context, engine *Engine) bool {
	if IsMapping(v) && engine.BoolOption("yaql.iterableDicts", false) {
		return true
	}
	return it.PythonType.Check(v, ctx, engine)
}

func (it *IterableType) Convert(v Value, receiver Value, ctx Context, fd *FunctionDefinition, engine *Engine) Value {
	v = unwrapConstant(v)
	if !it.Check(v, ctx, engine) {
		Throw(argumentValueError())
	}
	if v == nil {
		return nil
	}
	return LimitIterable(v, engine)
}

// Sequence returns yaqltypes.Sequence.
func Sequence(nullable bool) *PythonType {
	return NewPythonType(PySequence, nullable, func(v Value) bool {
		switch t := v.(type) {
		case string:
			return false
		case *Dict:
			return t.Frozen
		}
		return true
	})
}

// LambdaType is yaqltypes.Lambda.
type LambdaType struct {
	WithContext bool
	IsMethod    bool
}

func (LambdaType) lazy() {}

func (l LambdaType) Check(v Value, ctx Context, engine *Engine) bool {
	if l.IsMethod {
		if e, ok := v.(Expression); ok && !e.UsesReceiver() {
			return false
		}
	}
	return true
}

func (LambdaType) IsSpecializationOf(SmartType) bool { return false }

func publishParams(ctx Context, args []Value, kwargs map[string]Value) {
	for i, a := range args {
		ctx.Set("$"+itoa(i+1), a)
	}
	for _, k := range SortedKeys(kwargs) {
		ctx.Set("$"+k, kwargs[k])
	}
}

func (l LambdaType) Convert(v Value, receiver Value, ctx Context, fd *FunctionDefinition, engine *Engine) Value {
	if !l.Check(v, ctx, engine) {
		Throw(argumentValueError())
	}
	if v == nil {
		return nil
	}
	if cf, ok := v.(*CallableFunc); ok && cf.Unwrapped != nil {
		v = cf.Unwrapped
	}
	value := v
	return NewCallable(func(args []Value, kwargs map[string]Value) Value {
		var newReceiver Value = NoValue
		var newCtx Context
		switch {
		case l.IsMethod && l.WithContext:
			newReceiver, newCtx = args[0], args[1].(Context)
			args = args[2:]
		case l.IsMethod:
			newReceiver, newCtx = args[0], ctx.CreateChildContext()
			args = args[1:]
		case l.WithContext:
			newCtx = args[0].(Context)
			args = args[1:]
		default:
			newCtx = ctx.CreateChildContext()
		}
		publishParams(newCtx, args, kwargs)
		switch x := value.(type) {
		case Expression:
			return x.Eval(newReceiver, newCtx, engine)
		case Callable:
			return x.Call(args, kwargs)
		}
		return value
	}, value)
}

// SuperType is yaqltypes.Super: calls the same function one layer up.
type SuperType struct {
	hiddenBase
	WithContext bool
	Method      *bool
	WithName    bool
}

func (s SuperType) Convert(v Value, receiver Value, ctx Context, fd *FunctionDefinition, engine *Engine) Value {
	return NewCallable(func(args []Value, kwargs map[string]Value) Value {
		var fnCtx Context
		for c := ctx; c != nil; c = c.Parent() {
			if c.HasFunction(fd) {
				fnCtx = c
				break
			}
		}
		if fnCtx == nil {
			Throw(noFunctionRegistered(fd.Name))
		}
		parent := fnCtx.Parent()
		if parent == nil {
			Throw(noFunctionRegistered(fd.Name))
		}
		name := fd.Name
		if s.WithName {
			name = args[0].(string)
			args = args[1:]
		}
		newReceiver := receiver
		if s.Method != nil {
			if *s.Method {
				newReceiver = args[0]
				args = args[1:]
			} else {
				newReceiver = NoValue
			}
		}
		var newCtx Context
		if s.WithContext {
			newCtx = args[0].(Context)
			args = args[1:]
		} else {
			newCtx = ctx.CreateChildContext()
		}
		return Call(name, parent, args, kwargs, engine, newReceiver, newCtx, false, nil)
	}, nil)
}

// ContextType injects the call's context.
type ContextType struct{ hiddenBase }

func (ContextType) Convert(v Value, receiver Value, ctx Context, fd *FunctionDefinition, engine *Engine) Value {
	return ctx
}

// DelegateType injects a callable resolving a function by name at call time.
type DelegateType struct {
	hiddenBase
	FuncName      string
	WithContext   bool
	IsMethod      bool
	UseConvention bool
}

// Delegate returns yaqltypes.Delegate(name) with use_convention=True.
func Delegate(name string, method bool) DelegateType {
	return DelegateType{FuncName: name, IsMethod: method, UseConvention: true}
}

func (d DelegateType) Convert(v Value, receiver Value, ctx Context, fd *FunctionDefinition, engine *Engine) Value {
	return NewCallable(func(args []Value, kwargs map[string]Value) Value {
		name := d.FuncName
		if name == "" {
			name = args[0].(string)
			args = args[1:]
		}
		var newReceiver Value = NoValue
		if d.IsMethod {
			newReceiver = args[0]
			args = args[1:]
		}
		var newCtx Context
		if d.WithContext {
			newCtx = args[0].(Context)
			args = args[1:]
		} else {
			newCtx = ctx.CreateChildContext()
		}
		return Call(name, newCtx, args, kwargs, engine, newReceiver, nil, d.UseConvention, nil)
	}, nil)
}

// ReceiverType injects the method receiver.
type ReceiverType struct{ hiddenBase }

func (ReceiverType) Convert(v Value, receiver Value, ctx Context, fd *FunctionDefinition, engine *Engine) Value {
	return receiver
}

// EngineType injects the engine.
type EngineType struct{ hiddenBase }

func (EngineType) Convert(v Value, receiver Value, ctx Context, fd *FunctionDefinition, engine *Engine) Value {
	return engine
}

// ConstantType is yaqltypes.Constant and its refinements.
type ConstantType struct {
	Nullable bool
	Expand   bool
	accept   func(c *ConstantExpr) bool
}

func (c ConstantType) Check(v Value, ctx Context, engine *Engine) bool {
	if v == nil {
		return c.Nullable
	}
	ce, ok := v.(*ConstantExpr)
	if !ok {
		return false
	}
	return c.accept == nil || c.accept(ce)
}

func (c ConstantType) Convert(v Value, receiver Value, ctx Context, fd *FunctionDefinition, engine *Engine) Value {
	if !c.Check(v, ctx, engine) {
		Throw(argumentValueError())
	}
	if v == nil {
		Raise("AttributeError", "'NoneType' object has no attribute 'value'")
	}
	if c.Expand {
		return v.(*ConstantExpr).Value
	}
	return v
}

func (ConstantType) IsSpecializationOf(SmartType) bool { return false }

// StringConstant returns yaqltypes.StringConstant.
func StringConstant(nullable bool) ConstantType {
	return ConstantType{Nullable: nullable, Expand: true, accept: func(c *ConstantExpr) bool {
		_, ok := c.Value.(string)
		return ok
	}}
}

// KeywordType returns yaqltypes.Keyword.
func KeywordType(expand bool) ConstantType {
	return ConstantType{Expand: expand, accept: func(c *ConstantExpr) bool { return c.Keyword }}
}

// YaqlExpressionType is yaqltypes.YaqlExpression.
type YaqlExpressionType struct {
	FunctionOnly bool // restrict to plain function calls
}

func (YaqlExpressionType) lazy() {}

func (y YaqlExpressionType) Check(v Value, ctx Context, engine *Engine) bool {
	if y.FunctionOnly {
		f, ok := v.(*FunctionExpr)
		return ok && f.Kind == KindFunction
	}
	_, ok := v.(Expression)
	return ok
}

func (y YaqlExpressionType) Convert(v Value, receiver Value, ctx Context, fd *FunctionDefinition, engine *Engine) Value {
	if !y.Check(v, ctx, engine) {
		Throw(argumentValueError())
	}
	return v
}

func (YaqlExpressionType) IsSpecializationOf(SmartType) bool { return false }

// LazyMappingRule carries unevaluated source and destination thunks.
type LazyMappingRule struct {
	Source, Destination func() Value
}

// MappingRuleType is yaqltypes.MappingRule (lazy "a => b" arguments).
type MappingRuleType struct{}

func (MappingRuleType) lazy() {}

func (MappingRuleType) Check(v Value, ctx Context, engine *Engine) bool {
	_, ok := v.(*MappingRuleExpr)
	return ok
}

func (m MappingRuleType) Convert(v Value, receiver Value, ctx Context, fd *FunctionDefinition, engine *Engine) Value {
	if !m.Check(v, ctx, engine) {
		Throw(argumentValueError())
	}
	rule := v.(*MappingRuleExpr)
	return &LazyMappingRule{
		Source:      func() Value { return rule.Source.Eval(receiver, ctx, engine) },
		Destination: func() Value { return rule.Destination.Eval(receiver, ctx, engine) },
	}
}

func (MappingRuleType) IsSpecializationOf(SmartType) bool { return false }

func itoa(i int) string { return big.NewInt(int64(i)).String() }
