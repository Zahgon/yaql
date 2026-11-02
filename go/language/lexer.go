package language

import (
	"math/big"
	"strconv"
	"strings"
	"unicode"
)

type operatorRecord struct {
	symbol        string
	unary, binary int
	name, alias   string
}

type production struct {
	lhs    string
	length int
	action string
}

type grammar struct {
	nameValueOp string
	operators   []operatorRecord
	lexRules    []string
	productions []production
	actions     []map[string]int
	gotos       []map[string]int
	defaulted   map[int]int
}

type token struct {
	typ   string
	value Value
	pos   int
}

type lexer struct {
	g        *grammar
	src      string
	runes    []rune
	pos      int
	literal  map[string]string // rule name -> literal symbol
	opByWord map[string]string // operator symbol -> token type
}

func newLexer(g *grammar, src string) *lexer {
	l := &lexer{g: g, src: src, runes: []rune(src), literal: map[string]string{}, opByWord: map[string]string{}}
	for _, op := range g.operators {
		l.opByWord[op.symbol] = op.name
		switch op.symbol {
		case "[]":
			l.literal["INDEXER"] = "["
		case "{}":
			l.literal["MAP"] = "{"
		default:
			l.literal[op.name] = op.symbol
		}
	}
	if g.nameValueOp != "" {
		l.literal["MAPPING"] = g.nameValueOp
	}
	return l
}

func isWordRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsNumber(r)
}

func isDecimalRune(r rune) bool { return unicode.Is(unicode.Nd, r) }

func (l *lexer) at(i int) rune {
	if i < 0 || i >= len(l.runes) {
		return -1
	}
	return l.runes[i]
}

func (l *lexer) boundary(i int) bool {
	a, b := l.at(i-1), l.at(i)
	return (a >= 0 && isWordRune(a)) != (b >= 0 && isWordRune(b))
}

// matchRule returns the length matched by rule at l.pos, or -1.
func (l *lexer) matchRule(rule string) int {
	p := l.pos
	switch rule {
	case "DOLLAR":
		if l.at(p) != '$' {
			return -1
		}
		i := p + 1
		for i < len(l.runes) && isWordRune(l.runes[i]) {
			i++
		}
		return i - p
	case "NUMBER":
		return l.matchNumber()
	case "FUNC", "KEYWORD_STRING":
		if rule == "KEYWORD_STRING" && l.at(p) == '_' && l.at(p+1) == '_' {
			return -1
		}
		if !l.boundary(p) {
			return -1
		}
		r := l.at(p)
		if r < 0 || !isWordRune(r) || isDecimalRune(r) {
			return -1
		}
		i := p + 1
		for i < len(l.runes) && isWordRune(l.runes[i]) {
			i++
		}
		if rule == "FUNC" {
			if l.at(i) != '(' {
				return -1
			}
			return i + 1 - p
		}
		return i - p
	case "QUOTED_STRING":
		return l.matchQuoted('\'')
	case "DOUBLE_QUOTED_STRING":
		return l.matchQuoted('"')
	case "QUOTED_VERBATIM_STRING":
		return l.matchQuoted('`')
	}
	lit, ok := l.literal[rule]
	if !ok {
		return -1
	}
	lr := []rune(lit)
	if p+len(lr) > len(l.runes) {
		return -1
	}
	for i, r := range lr {
		if l.runes[p+i] != r {
			return -1
		}
	}
	return len(lr)
}

func (l *lexer) matchQuoted(q rune) int {
	p := l.pos
	if l.at(p) != q {
		return -1
	}
	for i := p + 1; i < len(l.runes); i++ {
		switch l.runes[i] {
		case '\\':
			if i+1 < len(l.runes) && l.runes[i+1] != '\n' {
				i++
				continue
			}
			return -1
		case q:
			return i + 1 - p
		}
	}
	return -1
}

// matchNumber implements \b\d+(\.?\d+)?\b with Python's backtracking order.
func (l *lexer) matchNumber() int {
	p := l.pos
	if !l.boundary(p) {
		return -1
	}
	maxD := p
	for maxD < len(l.runes) && isDecimalRune(l.runes[maxD]) {
		maxD++
	}
	for i := maxD; i > p; i-- {
		// optional group (\.?\d+)
		if l.at(i) == '.' {
			k := i + 1
			for k < len(l.runes) && isDecimalRune(l.runes[k]) {
				k++
			}
			for j := k; j > i+1; j-- {
				if l.boundary(j) {
					return j - p
				}
			}
		}
		for j := maxD; j > i; j-- {
			if l.boundary(j) {
				return j - p
			}
		}
		if l.boundary(i) {
			return i - p
		}
	}
	return -1
}

var keywordValues = map[string]Value{"true": true, "false": false, "null": nil}
var keywordTypes = map[string]string{"true": "TRUE", "false": "FALSE", "null": "NULL"}

// next returns the next token, nil at the end of input.
func (l *lexer) next() *token {
	for l.pos < len(l.runes) && strings.ContainsRune(" \t\r\n", l.runes[l.pos]) {
		l.pos++
	}
	if l.pos >= len(l.runes) {
		return nil
	}
	start := l.pos
	for _, rule := range l.g.lexRules {
		n := l.matchRule(rule)
		if n < 0 {
			continue
		}
		text := string(l.runes[start : start+n])
		l.pos += n
		tok := &token{typ: rule, value: text, pos: start}
		switch rule {
		case "NUMBER":
			if strings.Contains(text, ".") {
				// The token is digits around a dot, so the only error is range,
				// where ParseFloat returns Inf as Python's float() does.
				f, _ := strconv.ParseFloat(asciiDigits(text), 64)
				tok.value = f
			} else {
				v, _ := new(big.Int).SetString(asciiDigits(text), 10)
				tok.value = v
			}
		case "FUNC":
			tok.value = text[:len(text)-1]
		case "KEYWORD_STRING":
			if name, ok := l.opByWord[text]; ok {
				tok.typ = name
			} else if kt, ok := keywordTypes[text]; ok {
				tok.typ = kt
				tok.value = keywordValues[text]
			}
		case "QUOTED_STRING", "DOUBLE_QUOTED_STRING":
			tok.typ = "QUOTED_STRING"
			body := string(l.runes[start+1 : start+n-1])
			tok.value = decodeEscapes(body)
		case "QUOTED_VERBATIM_STRING":
			tok.typ = "QUOTED_STRING"
			body := string(l.runes[start+1 : start+n-1])
			tok.value = strings.ReplaceAll(body, "\\`", "`")
		}
		return tok
	}
	c := string(l.runes[start])
	if strings.Contains("()],}", c) {
		l.pos++
		return &token{typ: c, value: c, pos: start}
	}
	panic(&Error{Class: "YaqlLexicalException",
		Message: "Lexical error: illegal character '" + c + "' at position " + strconv.Itoa(start)})
}

func asciiDigits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r == '.' {
			b.WriteRune(r)
		} else {
			b.WriteRune('0' + rune(digitValue(r)))
		}
	}
	return b.String()
}

func digitValue(r rune) int {
	if r >= '0' && r <= '9' {
		return int(r - '0')
	}
	for _, rt := range unicode.Nd.R16 {
		if uint16(r) >= rt.Lo && uint16(r) <= rt.Hi && r <= 0xffff {
			return int(uint16(r)-rt.Lo) % 10
		}
	}
	return 0
}

// decodeEscapes mirrors yaql's lexer.decode_escapes.
func decodeEscapes(s string) string {
	rs := []rune(s)
	var b strings.Builder
	for i := 0; i < len(rs); i++ {
		if rs[i] != '\\' || i+1 >= len(rs) {
			b.WriteRune(rs[i])
			continue
		}
		c := rs[i+1]
		hexEscape := func(n int) bool {
			if i+2+n > len(rs) {
				return false
			}
			chunk := string(rs[i+2 : i+2+n])
			if strings.ContainsRune(chunk, '\n') {
				return false
			}
			v, err := strconv.ParseUint(chunk, 16, 32)
			if err != nil {
				Raise("UnicodeDecodeError", "'unicodeescape' codec can't decode bytes in position 0-%d: truncated \\%c escape", n+1, c)
			}
			if v > 0x10ffff {
				Raise("UnicodeDecodeError", "'unicodeescape' codec can't decode bytes in position 0-%d: illegal Unicode character", n+1)
			}
			b.WriteRune(rune(v))
			i += 1 + n
			return true
		}
		switch {
		case c == 'U':
			if hexEscape(8) {
				continue
			}
		case c == 'u':
			if hexEscape(4) {
				continue
			}
		case c == 'x':
			if hexEscape(2) {
				continue
			}
		case c >= '0' && c <= '7':
			j := i + 1
			for j < len(rs) && j < i+4 && rs[j] >= '0' && rs[j] <= '7' {
				j++
			}
			v, _ := strconv.ParseUint(string(rs[i+1:j]), 8, 32)
			b.WriteRune(rune(v))
			i = j - 1
			continue
		case strings.ContainsRune("\\'\"abfnrtv", c):
			b.WriteRune(map[rune]rune{'\\': '\\', '\'': '\'', '"': '"', 'a': 7, 'b': 8, 'f': 12,
				'n': 10, 'r': 13, 't': 9, 'v': 11}[c])
			i++
			continue
		}
		b.WriteRune(rs[i])
	}
	return b.String()
}
