// Package yaql is the public API of the yaql query language: engine
// factories, context creation, function registration and evaluation.
package yaql

import (
	"yaql/language"
	"yaql/stdlib"
)

// Value is any yaql value.
type Value = language.Value

// Context is an evaluation context.
type Context = language.Context

// Engine parses expressions.
type Engine = language.Engine

// Error is a yaql exception with a Python-compatible class name.
type Error = language.Error

// Factory builds engines for one grammar.
type Factory struct {
	legacy         bool
	allowDelegates bool
}

// NewFactory returns the standard factory. With allowDelegates,
// "$f(args)" calls a callable held in a context value.
func NewFactory(allowDelegates bool) *Factory {
	return &Factory{allowDelegates: allowDelegates}
}

// NewLegacyFactory returns the yaql 0.2 compatible factory, where "=>" is a
// tuple operator rather than a keyword argument separator.
func NewLegacyFactory(allowDelegates bool) *Factory {
	return &Factory{legacy: true, allowDelegates: allowDelegates}
}

// Create returns an engine with the given options (for example
// "yaql.limitIterators" or "yaql.convertTuplesToLists").
func (f *Factory) Create(options map[string]Value) *Engine {
	opts := map[string]Value{}
	for k, v := range options {
		opts[k] = v
	}
	if f.legacy {
		opts["yaql.convertTuplesToLists"] = false
		opts["yaql.iterableDicts"] = true
	}
	return language.NewEngine(f.legacy, f.allowDelegates, opts)
}

// ContextOptions selects the standard library parts registered by CreateContext.
type ContextOptions struct {
	Delegates bool
	NoSets    bool
	// Data, when not nil, is published as "$".
	Data Value
	// HasData marks Data as present even when it is nil (JSON null).
	HasData bool
	// DisableGroupByAggFallback turns off groupBy's pre-1.1.1 aggregator fallback.
	DisableGroupByAggFallback bool
}

func finalizers(ctx Context) {
	limit := language.Define("limit", language.Signature{Args: []string{"iterator"}},
		func(a []Value, _ map[string]Value) Value { return a[0] },
		language.Param("iterator", language.Iterable(false)), language.Name("#iter"))
	finalize := language.Define("finalize", language.Signature{Args: []string{"obj", "limiter", "engine"}},
		func(a []Value, _ map[string]Value) Value {
			engine := a[2].(*Engine)
			if !engine.BoolOption("yaql.convertOutputData", true) {
				return a[0]
			}
			limiter := a[1].(language.Callable)
			return language.ConvertOutputData(a[0], language.OutputOptions{
				SetsToLists:   engine.BoolOption("yaql.convertSetsToLists", false),
				TuplesToLists: engine.BoolOption("yaql.convertTuplesToLists", true),
				Limit:         func(v Value) Value { return limiter.Call([]Value{v}, nil) },
			})
		},
		language.Inject("limiter", language.Delegate("#iter", false)),
		language.Inject("engine", language.EngineType{}), language.Name("#finalize"))
	ctx.RegisterFunction(language.Bind(limit, ctx.Convention(), language.RegisterOption{}))
	ctx.RegisterFunction(language.Bind(finalize, ctx.Convention(), language.RegisterOption{}))
}

// CreateContext builds a context with the standard library, as
// yaql.create_context does.
func CreateContext(opts ContextOptions) Context {
	var ctx Context = language.NewContext(nil, language.CamelCaseConvention{})
	finalizers(ctx)
	if opts.HasData || opts.Data != nil {
		ctx.Set("$", language.ConvertInputData(opts.Data))
	}
	stdlib.RegisterSystemFallbacks(ctx)
	ctx = ctx.CreateChildContext()
	stdlib.RegisterSystem(ctx, opts.Delegates)
	stdlib.RegisterCommon(ctx)
	stdlib.RegisterBoolean(ctx)
	stdlib.RegisterStrings(ctx)
	stdlib.RegisterMath(ctx)
	stdlib.RegisterCollections(ctx, opts.NoSets)
	stdlib.RegisterQueries(ctx, !opts.DisableGroupByAggFallback)
	stdlib.RegisterRegex(ctx)
	stdlib.RegisterBranching(ctx)
	stdlib.RegisterDateTime(ctx)
	return ctx
}

// CreateLegacyContext builds a standard context plus the legacy layer.
func CreateLegacyContext(opts ContextOptions, tuples bool) Context {
	ctx := CreateContext(opts).CreateChildContext()
	stdlib.RegisterLegacy(ctx, tuples)
	return ctx
}

// RegisterFunction registers a Go function in ctx under the context's
// naming convention.
func RegisterFunction(ctx Context, fd *language.FunctionDefinition) {
	ctx.RegisterFunction(language.Bind(fd, ctx.Convention(), language.RegisterOption{}))
}

// Evaluate parses and evaluates expression against data (language.NoValue
// for none) in a child of ctx.
func Evaluate(engine *Engine, expression string, data Value, ctx Context) (result Value, err *Error) {
	stmt, err := engine.Parse(expression)
	if err != nil {
		return nil, err
	}
	err = language.Catch(func() { result = stmt.Evaluate(data, ctx.CreateChildContext()) })
	return result, err
}
