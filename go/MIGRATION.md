# Migration map

| Python module | Go package or file |
|---|---|
| yaql/__init__.py | go/yaql.go |
| yaql/legacy.py | go/yaql.go (CreateLegacyContext) |
| yaql/yaql_interface.py | go/yaql.go (public API surface) |
| yaql/language/contexts.py | go/language/contexts.go |
| yaql/language/conventions.py | go/language/contexts.go (naming conventions) |
| yaql/language/exceptions.py | go/language/exceptions.go |
| yaql/language/expressions.py | go/language/expressions.go |
| yaql/language/factory.py | go/language/engine.go |
| yaql/language/lexer.py | go/language/lexer.go |
| yaql/language/parser.py | go/language/parser.go |
| yaql/language/runner.py | go/language/runner.go |
| yaql/language/specs.py | go/language/specs.go |
| yaql/language/utils.py | go/language/utils.go |
| yaql/language/yaqltypes.py | go/language/types.go |
| yaql/standard_library/boolean.py | go/stdlib/common.go (boolean operators) |
| yaql/standard_library/branching.py | go/stdlib/common.go (branching functions) |
| yaql/standard_library/collections.py | go/stdlib/collections.go |
| yaql/standard_library/common.py | go/stdlib/common.go |
| yaql/standard_library/date_time.py | go/stdlib/datetime.go |
| yaql/standard_library/legacy.py | go/stdlib/legacy.go |
| yaql/standard_library/math.py | go/stdlib/math.go |
| yaql/standard_library/queries.py | go/stdlib/queries.go |
| yaql/standard_library/regex.py | go/stdlib/regex.go |
| yaql/standard_library/strings.py | go/stdlib/strings.go |
| yaql/standard_library/system.py | go/stdlib/system.go |

Not ported: yaql/cli (command-line shell), yaql/yaqlization.py (host-object binding) and yaql/standard_library/yaqlized.py (its accessors); JSON input data is never yaqlized.
