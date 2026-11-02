package language

import "fmt"

// Error is a raised exception. Class is the Python-compatible class name
// reported by the conformance protocol; Message is its str().
type Error struct {
	Class   string
	Message string
	// Wrapped carries the original exception of a WrappedException.
	Wrapped *Error
	// ParameterName is set on ArgumentException.
	ParameterName string
}

func (e *Error) Error() string { return e.Class + ": " + e.Message }

// exceptionParents encodes the exception class hierarchy used by except clauses.
var exceptionParents = map[string]string{
	"YaqlException":                        "Exception",
	"ResolutionError":                      "YaqlException",
	"FunctionResolutionError":              "ResolutionError",
	"MethodResolutionError":                "ResolutionError",
	"NoFunctionRegisteredException":        "FunctionResolutionError",
	"NoMethodRegisteredException":          "MethodResolutionError",
	"NoMatchingFunctionException":          "FunctionResolutionError",
	"NoMatchingMethodException":            "MethodResolutionError",
	"AmbiguousFunctionException":           "FunctionResolutionError",
	"AmbiguousMethodException":             "MethodResolutionError",
	"ArgumentException":                    "YaqlException",
	"MappingTranslationException":          "YaqlException",
	"ArgumentValueException":               "YaqlException",
	"InvalidMethodException":               "YaqlException",
	"YaqlParsingException":                 "YaqlException",
	"YaqlGrammarException":                 "YaqlParsingException",
	"YaqlLexicalException":                 "YaqlParsingException",
	"WrappedException":                     "YaqlException",
	"CollectionTooLargeException":          "YaqlException",
	"MemoryQuotaExceededException":         "YaqlException",
	"StopIteration":                        "Exception",
	"AssertionError":                       "Exception",
	"ValueError":                           "Exception",
	"TypeError":                            "Exception",
	"LookupError":                          "Exception",
	"KeyError":                             "LookupError",
	"IndexError":                           "LookupError",
	"ArithmeticError":                      "Exception",
	"ZeroDivisionError":                    "ArithmeticError",
	"OverflowError":                        "ArithmeticError",
	"AttributeError":                       "Exception",
	"RuntimeError":                         "Exception",
	"RecursionError":                       "RuntimeError",
	"UnicodeDecodeError":                   "ValueError",
	"ParserError":                          "ValueError",
	"error":                                "Exception",
	"Exception":                            "BaseException",
	"InvalidOperatorTableException":        "YaqlException",
	"DuplicateParameterDecoratorException": "YaqlException",
	"NoParameterFoundException":            "YaqlException",
}

// IsA reports whether the exception is an instance of class.
func (e *Error) IsA(class string) bool {
	for c := e.Class; c != ""; c = exceptionParents[c] {
		if c == class {
			return true
		}
	}
	return false
}

// Raise panics with a new exception.
func Raise(class, format string, args ...any) {
	msg := format
	if len(args) > 0 {
		msg = fmt.Sprintf(format, args...)
	}
	panic(&Error{Class: class, Message: msg})
}

// Throw panics with an existing exception.
func Throw(e *Error) { panic(e) }

// Catch runs fn and returns the exception it raised, if any. Panics that are
// not exceptions propagate.
func Catch(fn func()) (err *Error) {
	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(*Error); ok {
				err = e
				return
			}
			panic(r)
		}
	}()
	fn()
	return nil
}

func noFunctionRegistered(name string) *Error {
	return &Error{Class: "NoFunctionRegisteredException", Message: fmt.Sprintf("Unknown function \"%s\"", name)}
}

func noMethodRegistered(name string, receiver Value) *Error {
	return &Error{Class: "NoMethodRegisteredException",
		Message: fmt.Sprintf("Unknown method \"%s\" for receiver %s", name, Str(receiver))}
}

func noMatchingFunction(name string) *Error {
	return &Error{Class: "NoMatchingFunctionException",
		Message: fmt.Sprintf("No function \"%s\" matches supplied arguments", name)}
}

func noMatchingMethod(name string, receiver Value) *Error {
	return &Error{Class: "NoMatchingMethodException",
		Message: fmt.Sprintf("No method \"%s\" for receiver %s matches supplied arguments", name, Str(receiver))}
}

func ambiguousFunction(name string) *Error {
	return &Error{Class: "AmbiguousFunctionException", Message: fmt.Sprintf("Ambiguous function \"%s\"", name)}
}

func ambiguousMethod(name string, receiver Value) *Error {
	return &Error{Class: "AmbiguousMethodException",
		Message: fmt.Sprintf("Ambiguous method \"%s\" for receiver %s", name, Str(receiver))}
}

// ArgumentError builds an ArgumentException for the named parameter.
func ArgumentError(name string) *Error {
	return &Error{Class: "ArgumentException", Message: "Invalid argument " + name, ParameterName: name}
}

func mappingTranslationError() *Error {
	return &Error{Class: "MappingTranslationException", Message: "Cannot convert mapping to keyword argument"}
}

func argumentValueError() *Error {
	return &Error{Class: "ArgumentValueException", Message: ""}
}

// NoFunctionRegistered is raised when no function of that name exists.
func NoFunctionRegistered(name string) *Error { return noFunctionRegistered(name) }
