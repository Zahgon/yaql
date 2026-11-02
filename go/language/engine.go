package language

import (
	"math/big"
)

// Engine parses expressions for one grammar and carries engine options.
type Engine struct {
	grammar *grammar
	options map[string]Value
	// AllowDelegates reports whether "$f(args)" calls are part of the grammar.
	AllowDelegates bool
	// Legacy reports whether the legacy (yaql 0.2 compatible) grammar is used.
	Legacy bool
}

// NewEngine returns an engine for the default or legacy grammar.
func NewEngine(legacy, allowDelegates bool, options map[string]Value) *Engine {
	var g *grammar
	switch {
	case legacy && allowDelegates:
		g = grammarLegacyDelegates
	case legacy:
		g = grammarLegacy
	case allowDelegates:
		g = grammarDelegates
	default:
		g = grammarDefault
	}
	opts := map[string]Value{}
	for k, v := range options {
		opts[k] = v
	}
	return &Engine{grammar: g, options: opts, AllowDelegates: allowDelegates, Legacy: legacy}
}

// Option returns an engine option.
func (e *Engine) Option(name string) (Value, bool) {
	v, ok := e.options[name]
	return v, ok
}

// BoolOption returns a boolean option with its default.
func (e *Engine) BoolOption(name string, def bool) bool {
	if v, ok := e.options[name]; ok {
		return Truthy(v)
	}
	return def
}

// IntOption returns an integer option with its default.
func (e *Engine) IntOption(name string, def int64) int64 {
	if v, ok := e.options[name].(*big.Int); ok {
		return v.Int64()
	}
	return def
}

// Copy returns an engine with options overridden.
func (e *Engine) Copy(options map[string]Value) *Engine {
	c := *e
	c.options = map[string]Value{}
	for k, v := range e.options {
		c.options[k] = v
	}
	for k, v := range options {
		c.options[k] = v
	}
	return &c
}

// Parse parses an expression into a Statement, returning a parse error as *Error.
func (e *Engine) Parse(expression string) (stmt *Statement, err *Error) {
	err = Catch(func() { stmt = &Statement{Expression: parse(e.grammar, expression), Engine: e} })
	return stmt, err
}
