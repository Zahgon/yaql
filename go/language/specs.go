package language

import (
	"strings"
)

// noDefault marks a parameter without a default value.
type noDefaultMarker struct{}

func (noDefaultMarker) String() string { return "<NoValue>" }

// NoDefault marks a parameter that must be supplied.
var NoDefault Value = &noDefaultMarker{}

// ParameterDefinition describes one formal parameter.
type ParameterDefinition struct {
	Name     string
	Type     SmartType
	Position int // -1 for keyword-only and **kwargs parameters
	Alias    string
	Default  Value
}

func (p *ParameterDefinition) clone() *ParameterDefinition {
	c := *p
	return &c
}

// FunctionDefinition is a registered function or method overload.
type FunctionDefinition struct {
	Name       string
	Payload    func(args []Value, kwargs map[string]Value) Value
	Parameters []*ParameterDefinition // ordered as Python's parameter dict
	keys       []string               // dict keys: name, "*" or "**"
	IsFunction bool
	IsMethod   bool
	NoKwargs   bool
}

func (fd *FunctionDefinition) param(key string) *ParameterDefinition {
	for i, k := range fd.keys {
		if k == key {
			return fd.Parameters[i]
		}
	}
	return nil
}

// Clone copies the definition and its parameters.
func (fd *FunctionDefinition) Clone() *FunctionDefinition {
	c := *fd
	c.Parameters = make([]*ParameterDefinition, len(fd.Parameters))
	for i, p := range fd.Parameters {
		c.Parameters[i] = p.clone()
	}
	c.keys = append([]string(nil), fd.keys...)
	return &c
}

func isHidden(t SmartType) bool {
	_, ok := t.(HiddenType)
	return ok
}

func isLazy(t SmartType) bool {
	_, ok := t.(LazyType)
	return ok
}

// mapArgs is FunctionDefinition.map_args: a cheap applicability check on
// unevaluated arguments, returning the parameter bound to each argument.
func (fd *FunctionDefinition) mapArgs(args []Value, kwargs map[string]Value, ctx Context, engine *Engine) ([]*ParameterDefinition, map[string]*ParameterDefinition, bool) {
	kw := map[string]Value{}
	for k, v := range kwargs {
		kw[k] = v
	}
	star := fd.param("*")
	positional := make([]*ParameterDefinition, len(args))
	for i := range positional {
		positional[i] = star
	}
	fix := make([]int, len(args)+len(fd.Parameters))
	for _, p := range fd.Parameters {
		if p.Position >= 0 && isHidden(p.Type) {
			for i := p.Position + 1; i < len(fix); i++ {
				fix[i]++
			}
		}
	}
	keyword := map[string]*ParameterDefinition{}
	for i, p := range fd.Parameters {
		key := fd.keys[i]
		argName := p.Alias
		if argName == "" {
			argName = p.Name
		}
		if p.Position >= 0 && key != "*" {
			pos := p.Position - fix[p.Position]
			_, inKw := kw[argName]
			switch {
			case isHidden(p.Type):
				continue
			case pos < len(args) && args[pos] != NoValue:
				if inKw {
					return nil, nil, false
				}
				positional[pos] = p
			case inKw:
				keyword[argName] = p
				delete(kw, argName)
			case p.Default == NoDefault:
				return nil, nil, false
			case pos < len(args) && truthyArg(args[pos]):
				positional[pos] = p
			}
		} else if p.Position < 0 && key != "**" {
			_, inKw := kw[argName]
			switch {
			case isHidden(p.Type):
				continue
			case inKw:
				keyword[argName] = p
				delete(kw, argName)
			case p.Default == NoDefault:
				return nil, nil, false
			}
		}
	}
	if len(kw) > 0 {
		if starstar := fd.param("**"); starstar != nil {
			for k := range kw {
				keyword[k] = starstar
			}
		} else {
			return nil, nil, false
		}
	}
	for i, p := range positional {
		if p == nil {
			return nil, nil, false
		}
		value := args[i]
		if value == NoValue {
			value = p.Default
		}
		if !p.Type.Check(value, ctx, engine) {
			return nil, nil, false
		}
	}
	for k, v := range kw {
		if !keyword[k].Type.Check(v, ctx, engine) {
			return nil, nil, false
		}
	}
	return positional, keyword, true
}

// truthyArg evaluates Python truthiness of an unevaluated argument object:
// expressions and markers are objects and therefore true.
func truthyArg(v Value) bool {
	switch v.(type) {
	case Expression:
		return true
	}
	return Truthy(v)
}

type argThunk func(ctx Context) Value

// getDelegate binds evaluated arguments, raising ArgumentException when they
// do not fit, and returns the call to perform.
func (fd *FunctionDefinition) getDelegate(receiver Value, engine *Engine, ctx Context, args []Value, kwargs map[string]Value) func() Value {
	checked := func(val Value, p *ParameterDefinition) argThunk {
		if !p.Type.Check(val, ctx, engine) {
			Throw(ArgumentError(p.Name))
		}
		return func(ctx2 Context) Value {
			var out Value
			if err := Catch(func() { out = p.Type.Convert(val, receiver, ctx2, fd, engine) }); err != nil {
				if err.Class == "ArgumentValueException" {
					Throw(ArgumentError(p.Name))
				}
				Throw(err)
			}
			return out
		}
	}
	kw := map[string]Value{}
	for k, v := range kwargs {
		kw[k] = v
	}
	positionalCount := 0
	for i, p := range fd.Parameters {
		if p.Position >= 0 && fd.keys[i] != "*" {
			positionalCount++
		}
	}
	positional := make([]argThunk, positionalCount)
	fix := make([]int, positionalCount)
	for _, p := range fd.Parameters {
		if p.Position >= 0 && isHidden(p.Type) {
			for i := p.Position + 1; i < positionalCount; i++ {
				fix[i]++
			}
		}
	}
	type kwThunk struct {
		key   string
		thunk argThunk
	}
	var keyword []kwThunk
	effective := positionalCount
	for i, p := range fd.Parameters {
		key := fd.keys[i]
		argName := p.Alias
		if argName == "" {
			argName = p.Name
		}
		if p.Position >= 0 && key != "*" {
			pos := p.Position - fix[p.Position]
			switch {
			case isHidden(p.Type):
				positional[p.Position] = checked(nil, p)
				effective--
			case pos < len(args) && args[pos] != NoValue:
				if _, ok := kw[argName]; ok {
					Throw(ArgumentError(p.Name))
				}
				positional[p.Position] = checked(args[pos], p)
			default:
				if v, ok := kw[argName]; ok {
					delete(kw, argName)
					positional[p.Position] = checked(v, p)
				} else if p.Default != NoDefault {
					positional[p.Position] = checked(p.Default, p)
				} else {
					Throw(ArgumentError(p.Name))
				}
			}
		} else if p.Position < 0 && key != "**" {
			if isHidden(p.Type) {
				keyword = append(keyword, kwThunk{key, checked(nil, p)})
			} else if v, ok := kw[argName]; ok {
				delete(kw, argName)
				keyword = append(keyword, kwThunk{key, checked(v, p)})
			} else if p.Default != NoDefault {
				keyword = append(keyword, kwThunk{key, checked(p.Default, p)})
			} else {
				Throw(ArgumentError(p.Name))
			}
		}
	}
	if len(args) > effective {
		star := fd.param("*")
		if star == nil {
			Throw(ArgumentError("*"))
		}
		for _, a := range args[effective:] {
			positional = append(positional, checked(a, star))
		}
	}
	if len(kw) > 0 {
		starstar := fd.param("**")
		if starstar == nil {
			Throw(ArgumentError("**"))
		}
		for _, k := range SortedKeys(kw) {
			keyword = append(keyword, kwThunk{k, checked(kw[k], starstar)})
		}
	}
	return func() Value {
		newCtx := ctx.CreateChildContext()
		pargs := make([]Value, len(positional))
		for i, t := range positional {
			pargs[i] = t(newCtx)
		}
		kargs := map[string]Value{}
		for _, t := range keyword {
			kargs[t.key] = t.thunk(newCtx)
		}
		return fd.Payload(pargs, kargs)
	}
}

// SortedKeys returns the keys of m in alphabetical order.
func SortedKeys(m map[string]Value) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}

func (fd *FunctionDefinition) isValidMethod() bool {
	minPos := len(fd.Parameters)
	var minArg *ParameterDefinition
	for _, p := range fd.Parameters {
		if p.Position >= 0 && p.Position < minPos && !isHidden(p.Type) {
			minPos = p.Position
			minArg = p
		}
	}
	return minArg != nil && !isLazy(minArg.Type)
}

// Callable returns the definition invoked directly with Python-style
// arguments, as FunctionDefinition.__call__ does.
func (fd *FunctionDefinition) Callable(engine *Engine, ctx Context, receiver Value) func(args []Value, kwargs map[string]Value) Value {
	return func(args []Value, kwargs map[string]Value) Value {
		if receiver != NoValue {
			args = append([]Value{receiver}, args...)
		}
		return fd.getDelegate(receiver, engine, ctx, args, kwargs)()
	}
}

// Signature describes a Python function signature for building definitions.
type Signature struct {
	Args     []string         // positional parameter names in order
	Defaults map[string]Value // defaults by name
	VarArgs  string           // *args name, "" if none
	KwOnly   []string         // keyword-only parameter names
	VarKw    string           // **kwargs name, "" if none
}

// Decorator customizes a definition being built, like yaql's @specs helpers.
type Decorator func(b *defBuilder)

type defBuilder struct {
	fd  *FunctionDefinition
	sig Signature
}

func (b *defBuilder) setParameter(name string, t SmartType, rawType *PyType, nullable *bool, alias string) {
	key := name
	position := -1
	switch {
	case name == b.sig.VarKw && name != "":
		key = "**"
	case name == b.sig.VarArgs && name != "":
		key = "*"
		position = len(b.sig.Args)
	default:
		for i, a := range b.sig.Args {
			if a == name {
				position = i
			}
		}
		if position < 0 {
			found := false
			for _, k := range b.sig.KwOnly {
				found = found || k == name
			}
			if !found {
				Raise("NoParameterFoundException", "Function '%s' has no parameter called '{param_name}'", b.fd.Name)
			}
		}
	}
	def := NoDefault
	if d, ok := b.sig.Defaults[name]; ok && key != "*" && key != "**" {
		def = d
	}
	if b.fd.param(key) != nil {
		Raise("DuplicateParameterDecoratorException", "Function '%s' has multiple decorators for parameter '%s'", b.fd.Name, name)
	}
	st := t
	switch {
	case t == nil && rawType == nil:
		n := true
		if nullable != nil {
			n = *nullable
		}
		base := PyObject
		if def != NoDefault && def != nil && def != NoValue {
			base = pyTypeOf(def)
		}
		st = NewPythonType(base, n)
	case t == nil:
		n := def == nil
		if nullable != nil {
			n = *nullable
		}
		st = NewPythonType(rawType, n)
	}
	b.fd.Parameters = append(b.fd.Parameters, &ParameterDefinition{Name: name, Type: st, Position: position, Alias: alias, Default: def})
	b.fd.keys = append(b.fd.keys, key)
}

// Param is @specs.parameter(name, type) with a smart type.
func Param(name string, t SmartType) Decorator {
	return func(b *defBuilder) { b.setParameter(name, t, nil, nil, "") }
}

// ParamAlias is @specs.parameter with an alias.
func ParamAlias(name string, t SmartType, alias string) Decorator {
	return func(b *defBuilder) { b.setParameter(name, t, nil, nil, alias) }
}

// ParamPy is @specs.parameter(name, python_type) with an optional nullable.
func ParamPy(name string, t *PyType, nullable ...bool) Decorator {
	return func(b *defBuilder) {
		var n *bool
		if len(nullable) > 0 {
			n = &nullable[0]
		}
		b.setParameter(name, nil, t, n, "")
	}
}

// ParamPyAlias is ParamPy with an alias.
func ParamPyAlias(name string, t *PyType, alias string) Decorator {
	return func(b *defBuilder) { b.setParameter(name, nil, t, nil, alias) }
}

// ParamNullable is @specs.parameter(name, nullable=...) without a type.
func ParamNullable(name string, nullable bool) Decorator {
	return func(b *defBuilder) { b.setParameter(name, nil, nil, &nullable, "") }
}

// Inject is @specs.inject for hidden parameters.
func Inject(name string, t SmartType) Decorator { return Param(name, t) }

// Name is @specs.name.
func Name(name string) Decorator { return func(b *defBuilder) { b.fd.Name = name } }

// Method is @specs.method.
func Method(b *defBuilder) { b.fd.IsMethod, b.fd.IsFunction = true, false }

// ExtensionMethod is @specs.extension_method.
func ExtensionMethod(b *defBuilder) { b.fd.IsMethod, b.fd.IsFunction = true, true }

// NoKwargs is @specs.no_kwargs.
func NoKwargs(b *defBuilder) { b.fd.NoKwargs = true }

// RegisterOption customizes registration (name override, function/method flags).
type RegisterOption struct {
	Name     string
	Function *bool
	Method   *bool
}

func inferParameterType(name string) SmartType {
	switch name {
	case "context", "__context":
		return ContextType{}
	case "engine", "__engine":
		return EngineType{}
	}
	return nil
}

// Define builds a function definition from a Python-style signature and its
// decorators listed top to bottom as in the source; they apply bottom-up.
func Define(pyName string, sig Signature, payload func(args []Value, kwargs map[string]Value) Value, decorators ...Decorator) *FunctionDefinition {
	b := &defBuilder{fd: &FunctionDefinition{Payload: payload, IsFunction: true}, sig: sig}
	explicitName := ""
	for i := len(decorators) - 1; i >= 0; i-- {
		decorators[i](b)
		if b.fd.Name != "" && explicitName == "" {
			explicitName = b.fd.Name
		}
	}
	b.fd.Name = explicitName
	all := append(append([]string{}, sig.Args...), sig.KwOnly...)
	for _, arg := range all {
		if b.fd.param(arg) == nil {
			b.setParameter(arg, inferParameterType(arg), nil, nil, "")
		}
	}
	if sig.VarArgs != "" && b.fd.param("*") == nil {
		b.setParameter(sig.VarArgs, inferParameterType(sig.VarArgs), nil, nil, "")
	}
	if sig.VarKw != "" && b.fd.param("**") == nil {
		b.setParameter(sig.VarKw, inferParameterType(sig.VarKw), nil, nil, "")
	}
	if b.fd.Name == "" {
		b.fd.Name = "\x00" + pyName
	}
	return b.fd
}

// Bind finalizes a definition for registration in a context, applying the
// naming convention as get_function_definition does.
func Bind(fd *FunctionDefinition, convention Convention, opt RegisterOption) *FunctionDefinition {
	c := fd.Clone()
	fromPyName := strings.HasPrefix(c.Name, "\x00")
	if fromPyName {
		c.Name = c.Name[1:]
	}
	switch {
	case opt.Name != "":
		c.Name = opt.Name
	case fromPyName:
		c.Name = convertFunctionName(c.Name, convention)
	case convention != nil:
		c.Name = convertFunctionName(c.Name, convention)
	}
	if opt.Function != nil {
		c.IsFunction = *opt.Function
	}
	if opt.Method != nil {
		c.IsMethod = *opt.Method
	}
	if convention != nil {
		for _, p := range c.Parameters {
			if p.Alias == "" {
				p.Alias = convertParameterName(p.Name, convention)
			}
		}
	}
	return c
}

func convertFunctionName(name string, convention Convention) string {
	if name == "" {
		return name
	}
	name = strings.TrimRight(name, "_")
	if convention == nil {
		return name
	}
	first := []rune(name)[0]
	if !isAlpha(first) {
		finish := strings.IndexRune(name[len(string(first)):], first)
		if finish < 0 {
			return name
		}
		finish += len(string(first))
		if finish <= 1 {
			return name
		}
		return name[:finish+1] + convention.ConvertFunctionName(name[finish+1:])
	}
	return convention.ConvertFunctionName(name)
}

func isAlpha(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r > 127
}

func convertParameterName(name string, convention Convention) string {
	if name == "" {
		return name
	}
	name = strings.TrimRight(name, "_")
	if convention == nil {
		return name
	}
	return convention.ConvertParameterName(name)
}
