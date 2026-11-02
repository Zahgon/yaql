package language

import (
	"strings"
)

// Expression is a node of a parsed yaql expression.
type Expression interface {
	Eval(receiver Value, ctx Context, engine *Engine) Value
	String() string
	UsesReceiver() bool
}

// FunctionExpr is a function call "name(args)". Operators, indexers, list
// and map literals and context references are function calls with special
// names; Kind distinguishes them where library functions care.
type FunctionExpr struct {
	Name         string
	Args         []Value
	Kind         ExprKind
	Operator     string
	usesReceiver bool
}

// ExprKind is the syntactic origin of a FunctionExpr.
type ExprKind int

// Function expression kinds.
const (
	KindFunction ExprKind = iota
	KindBinaryOperator
	KindUnaryOperator
	KindIndex
	KindList
	KindMap
	KindGetContextValue
	KindStatement
)

func newFunction(name string, args ...Value) *FunctionExpr {
	return &FunctionExpr{Name: name, Args: args, usesReceiver: true}
}

func newBinaryOperator(op string, a, b Value, alias string) *FunctionExpr {
	name := "#operator_" + op
	if alias != "" {
		name = "*" + alias
	}
	return &FunctionExpr{Name: name, Args: []Value{a, b}, Kind: KindBinaryOperator, Operator: op}
}

func newUnaryOperator(op string, a Value, alias string) *FunctionExpr {
	name := "#unary_operator_" + op
	if alias != "" {
		name = "*" + alias
	}
	return &FunctionExpr{Name: name, Args: []Value{a}, Kind: KindUnaryOperator, Operator: op}
}

func newIndexExpression(value Value, args ...Value) *FunctionExpr {
	return &FunctionExpr{Name: "#indexer", Args: append([]Value{value}, args...), Kind: KindIndex}
}

func newListExpression(args ...Value) *FunctionExpr {
	return &FunctionExpr{Name: "#list", Args: args, Kind: KindList}
}

func newMapExpression(args ...Value) *FunctionExpr {
	return &FunctionExpr{Name: "#map", Args: args, Kind: KindMap}
}

func newGetContextValue(path *ConstantExpr) *FunctionExpr {
	return &FunctionExpr{Name: "#get_context_data", Args: []Value{path}, Kind: KindGetContextValue}
}

func (f *FunctionExpr) Eval(receiver Value, ctx Context, engine *Engine) Value {
	return Call(f.Name, ctx, f.Args, nil, engine, receiver, ctx, false, nil)
}

func (f *FunctionExpr) UsesReceiver() bool { return f.usesReceiver }

func (f *FunctionExpr) String() string {
	if f.Kind == KindGetContextValue {
		return Str(f.Args[0].(*ConstantExpr).Value)
	}
	parts := make([]string, len(f.Args))
	for i, a := range f.Args {
		parts[i] = exprString(a)
	}
	return f.Name + "(" + strings.Join(parts, ", ") + ")"
}

func exprString(v Value) string {
	if e, ok := v.(Expression); ok {
		return e.String()
	}
	return Str(v)
}

// ConstantExpr is a literal; Keyword marks a bare identifier.
type ConstantExpr struct {
	Value   Value
	Keyword bool
}

func (c *ConstantExpr) Eval(Value, Context, *Engine) Value { return c.Value }
func (c *ConstantExpr) UsesReceiver() bool                 { return false }
func (c *ConstantExpr) String() string {
	if s, ok := c.Value.(string); ok {
		return "'" + s + "'"
	}
	return Str(c.Value)
}

// WrapExpr is a parenthesized expression.
type WrapExpr struct {
	Expr Expression
}

func (w *WrapExpr) Eval(receiver Value, ctx Context, engine *Engine) Value {
	return w.Expr.Eval(receiver, ctx, engine)
}
func (w *WrapExpr) UsesReceiver() bool { return false }
func (w *WrapExpr) String() string     { return w.Expr.String() }

// MappingRuleExpr is a "source => destination" argument.
type MappingRuleExpr struct {
	Source, Destination Expression
}

func (m *MappingRuleExpr) Eval(receiver Value, ctx Context, engine *Engine) Value {
	return &MappingRule{Source: m.Source.Eval(receiver, ctx, engine), Destination: m.Destination.Eval(receiver, ctx, engine)}
}
func (m *MappingRuleExpr) UsesReceiver() bool { return false }
func (m *MappingRuleExpr) String() string {
	return m.Source.String() + " => " + m.Destination.String()
}

// Statement is a parsed top-level expression bound to its engine.
type Statement struct {
	Expression Expression
	Engine     *Engine
}

// Evaluate runs the statement with data published as "$" (unless data is
// NoValue) in a child of ctx.
func (s *Statement) Evaluate(data Value, ctx Context) Value {
	if data != NoValue {
		if s.Engine.BoolOption("yaql.convertInputData", true) {
			ctx.Set("$", ConvertInputData(data))
		} else {
			ctx.Set("$", data)
		}
	}
	return s.call(ctx)
}

func (s *Statement) call(ctx Context) (result Value) {
	if len(CollectFunctions(ctx, "#finalize", nil, false)) == 0 {
		ctx = ctx.CreateChildContext()
		ctx.RegisterFunction(identityFinalizer())
	}
	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(*Error); ok && e.Class == "WrappedException" {
				panic(e.Wrapped)
			}
			panic(r)
		}
	}()
	return Call("#finalize", ctx, []Value{s.Expression}, nil, s.Engine, NoValue, ctx, false, nil)
}

func (s *Statement) String() string { return s.Expression.String() }

func identityFinalizer() *FunctionDefinition {
	fd := Define("<lambda>", Signature{Args: []string{"x"}}, func(a []Value, _ map[string]Value) Value { return a[0] })
	return Bind(fd, nil, RegisterOption{Name: "#finalize"})
}
