package language

import "fmt"

// parse runs the LR automaton for g over src, building the expression tree.
func parse(g *grammar, src string) Expression {
	lex := newLexer(g, src)
	aliases := map[string]string{}
	symbols := map[string]bool{}
	for _, op := range g.operators {
		aliases[op.name] = op.alias
		symbols[op.symbol] = true
	}
	type entry struct {
		typ   string
		value any
	}
	states := []int{0}
	stack := []entry{{typ: "$end"}}
	var lookahead *token
	state := 0
	for {
		var act int
		var ok bool
		if t, isDefault := g.defaulted[state]; isDefault {
			act, ok = t, true
		} else {
			if lookahead == nil {
				lookahead = lex.next()
				if lookahead == nil {
					lookahead = &token{typ: "$end"}
				}
			}
			act, ok = g.actions[state][lookahead.typ]
		}
		if !ok {
			if lookahead.typ == "$end" {
				panic(&Error{Class: "YaqlGrammarException", Message: "Parse error: unexpected end of statement"})
			}
			panic(&Error{Class: "YaqlGrammarException", Message: fmt.Sprintf(
				"Parse error: unexpected '%s' at position %d of expression '%s'", Str(lookahead.value), lookahead.pos, src)})
		}
		switch {
		case act > 0:
			states = append(states, act)
			state = act
			stack = append(stack, entry{typ: lookahead.typ, value: lookahead.value})
			lookahead = nil
		case act < 0:
			p := g.productions[-act]
			args := make([]entry, p.length)
			copy(args, stack[len(stack)-p.length:])
			stack = stack[:len(stack)-p.length]
			states = states[:len(states)-p.length]
			vals := make([]any, p.length+1)
			types := make([]string, p.length+1)
			for i, a := range args {
				vals[i+1] = a.value
				types[i+1] = a.typ
			}
			result := reduce(p.action, vals, types, aliases, symbols)
			stack = append(stack, entry{typ: p.lhs, value: result})
			state = g.gotos[states[len(states)-1]][p.lhs]
			states = append(states, state)
		default:
			return stack[len(stack)-1].value.(Expression)
		}
	}
}

func argList(v any) []Value {
	if v == nil {
		return nil
	}
	return v.([]Value)
}

func concatArgs(a, b []Value) []Value {
	out := make([]Value, 0, len(a)+len(b))
	return append(append(out, a...), b...)
}

func reduce(action string, p []any, types []string, aliases map[string]string, symbols map[string]bool) any {
	n := len(p) - 1
	switch action {
	case "p_binary":
		return newBinaryOperator(p[2].(string), p[1], p[3], aliases[types[2]])
	case "p_unary":
		if s, ok := p[1].(string); ok && symbols[s] && types[1] != "value" {
			return newUnaryOperator(s, p[2], aliases[types[1]])
		}
		return newUnaryOperator(p[2].(string), p[1], aliases[types[2]])
	case "p_value_call":
		var args []Value
		if n > 3 {
			args = argList(p[3])
		}
		return newFunction("#call", append([]Value{p[1]}, args...)...)
	case "p_value_to_const":
		return &ConstantExpr{Value: p[1]}
	case "p_keyword_constant":
		return &ConstantExpr{Value: p[1], Keyword: true}
	case "p_value_to_dollar":
		return newGetContextValue(&ConstantExpr{Value: p[1]})
	case "p_val_in_parenthesis":
		return &WrapExpr{Expr: p[2].(Expression)}
	case "p_args":
		switch n {
		case 0:
			return []Value{}
		case 1:
			return p[1]
		}
		return concatArgs(argList(p[1]), argList(p[3]))
	case "p_indexer":
		return newIndexExpression(p[1], argList(p[3])...)
	case "p_list":
		return newListExpression(argList(p[2])...)
	case "p_map":
		return newMapExpression(argList(p[2])...)
	case "p_val_to_function":
		return p[1]
	case "p_named_arg_definition":
		return &MappingRuleExpr{Source: p[1].(Expression), Destination: p[3].(Expression)}
	case "p_arg_list":
		switch n {
		case 1:
			return []Value{p[1]}
		case 2:
			return concatArgs([]Value{NoValue}, argList(p[2]))
		}
		return concatArgs(argList(p[1]), argList(p[3]))
	case "p_incomplete_arg_list":
		return concatArgs(argList(p[1]), []Value{NoValue})
	case "p_named_arg_list":
		if n == 1 {
			return []Value{p[1]}
		}
		return concatArgs(argList(p[1]), []Value{p[3]})
	case "p_function":
		var args []Value
		if n > 2 {
			args = argList(p[2])
		}
		return newFunction(p[1].(string), args...)
	}
	panic("unknown grammar action " + action)
}
