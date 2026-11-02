// Package language implements the yaql expression language: values with
// Python semantics, the lexer and parser, contexts, function specifications
// and overload resolution.
package language

import (
	"math/big"
	"sync/atomic"
)

// Value is any yaql value. The dynamic types used are:
//
//	nil            null (Python None)
//	bool           boolean
//	*big.Int       integer (arbitrary precision, never mutated once shared)
//	float64        floating point number
//	string         string
//	Tuple          immutable sequence (the type yaql lists evaluate to)
//	*List          mutable sequence produced by some library functions
//	*Dict          mapping, frozen or mutable, insertion ordered
//	*Set           immutable set
//	*DictView      keys(), values() or items() view of a mapping
//	*Range         integer range sequence
//	*DateTime      date and time
//	Timedelta      time span
//	*Regex         compiled regular expression
//	Iterator       single-pass lazy sequence
//	Context        evaluation context
//	Callable       function value
//	*MappingRule   evaluated "a => b" pair
type Value = any

// Tuple is an immutable sequence.
type Tuple []Value

// List is a mutable sequence.
type List struct {
	Items []Value
}

// MappingRule is the evaluated form of a "source => destination" argument.
type MappingRule struct {
	Source      Value
	Destination Value
}

type noValue struct{}

func (noValue) String() string { return "<NoValue>" }

// NoValue marks an absent value (an omitted argument, a missing default).
var NoValue Value = &noValue{}

// Callable is a function value, such as a lambda or a delegate.
type Callable interface {
	Call(args []Value, kwargs map[string]Value) Value
}

// CallableFunc adapts a Go function to Callable.
type CallableFunc struct {
	id int64
	Fn func(args []Value, kwargs map[string]Value) Value
	// Unwrapped is the value a lambda was built from.
	Unwrapped Value
}

var identityCounter atomic.Int64

func nextIdentity() int64 { return identityCounter.Add(1) }

// NewCallable wraps fn as a Callable value.
func NewCallable(fn func(args []Value, kwargs map[string]Value) Value, unwrapped Value) *CallableFunc {
	return &CallableFunc{id: nextIdentity(), Fn: fn, Unwrapped: unwrapped}
}

func (c *CallableFunc) Call(args []Value, kwargs map[string]Value) Value {
	return c.Fn(args, kwargs)
}

// Int returns n as a yaql integer.
func Int(n int64) *big.Int { return big.NewInt(n) }

var (
	bigZero = big.NewInt(0)
	bigOne  = big.NewInt(1)
)

// TypeName returns the Python type name of v, as used in error messages.
func TypeName(v Value) string {
	switch t := v.(type) {
	case nil:
		return "NoneType"
	case bool:
		return "bool"
	case *big.Int:
		return "int"
	case float64:
		return "float"
	case string:
		return "str"
	case Tuple:
		return "tuple"
	case *List:
		return "list"
	case *Dict:
		if t.Frozen {
			return "FrozenDict"
		}
		return "dict"
	case *Set:
		return "frozenset"
	case *DictView:
		return [...]string{"dict_keys", "dict_values", "dict_items"}[t.Kind]
	case *Range:
		return "range"
	case *DateTime:
		return "datetime"
	case Timedelta:
		return "timedelta"
	case *Regex:
		return "re.Pattern"
	case *OrderingIterable:
		return "OrderingIterable"
	case Iterator:
		return "generator"
	case Context:
		return "Context"
	case Callable:
		return "function"
	case *MappingRule:
		return "MappingRule"
	case Expression:
		return "Expression"
	}
	return "object"
}

// IsIterator reports whether v is a single-pass iterator.
func IsIterator(v Value) bool {
	_, ok := v.(Iterator)
	return ok
}

// IsIterable mirrors yaql's utils.is_iterable: iterable but not a string or mapping.
func IsIterable(v Value) bool {
	switch v.(type) {
	case Tuple, *List, *Set, *DictView, *Range, *OrderingIterable, Iterator:
		return true
	}
	return false
}

// IsSequence mirrors utils.is_sequence: a sequence other than a string.
func IsSequence(v Value) bool {
	switch v.(type) {
	case Tuple, *List, *Range:
		return true
	}
	return false
}

// IsMapping reports whether v is a mapping.
func IsMapping(v Value) bool {
	_, ok := v.(*Dict)
	return ok
}

// IsSet reports whether v is a set in the collections.abc.Set sense.
func IsSet(v Value) bool {
	switch t := v.(type) {
	case *Set:
		return true
	case *DictView:
		return t.Kind != ViewValues
	}
	return false
}

// SequenceItems returns the items of a tuple, list or range.
func SequenceItems(v Value) []Value {
	switch t := v.(type) {
	case Tuple:
		return t
	case *List:
		return t.Items
	case *Range:
		return t.Items()
	}
	return nil
}
