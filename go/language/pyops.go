package language

import (
	"fmt"
	"hash/fnv"
	"math"
	"math/big"
	"strconv"
	"strings"
	"unicode"
)

// Truthy reports the boolean value of v under Python rules.
func Truthy(v Value) bool {
	switch t := v.(type) {
	case nil:
		return false
	case bool:
		return t
	case *big.Int:
		return t.Sign() != 0
	case float64:
		return t != 0
	case string:
		return t != ""
	case Tuple:
		return len(t) > 0
	case *List:
		return len(t.Items) > 0
	case *Dict:
		return t.Len() > 0
	case *Set:
		return t.Len() > 0
	case *DictView:
		return t.D.Len() > 0
	case *Range:
		return t.Len() > 0
	case Timedelta:
		return t.Micros() != 0
	}
	return true
}

// numeric returns v as an exact rational if it is a bool, int or finite float.
func numeric(v Value) (*big.Rat, bool) {
	switch t := v.(type) {
	case bool:
		if t {
			return big.NewRat(1, 1), true
		}
		return big.NewRat(0, 1), true
	case *big.Int:
		return new(big.Rat).SetInt(t), true
	case float64:
		if math.IsInf(t, 0) || math.IsNaN(t) {
			return nil, false
		}
		return new(big.Rat).SetFloat64(t), true
	}
	return nil, false
}

func isNumber(v Value) bool {
	switch v.(type) {
	case bool, *big.Int, float64:
		return true
	}
	return false
}

// NumCompare compares two numbers exactly, returning -1, 0 or 1, and false if
// the comparison is unordered (NaN).
func NumCompare(a, b Value) (int, bool) {
	fa, aFloat := a.(float64)
	fb, bFloat := b.(float64)
	if (aFloat && math.IsNaN(fa)) || (bFloat && math.IsNaN(fb)) {
		return 0, false
	}
	if aFloat && math.IsInf(fa, 0) || bFloat && math.IsInf(fb, 0) {
		x, y := toFloatLoose(a), toFloatLoose(b)
		switch {
		case x < y:
			return -1, true
		case x > y:
			return 1, true
		}
		return 0, true
	}
	ra, _ := numeric(a)
	rb, _ := numeric(b)
	return ra.Cmp(rb), true
}

func toFloatLoose(v Value) float64 {
	switch t := v.(type) {
	case bool:
		if t {
			return 1
		}
		return 0
	case *big.Int:
		f, _ := new(big.Float).SetInt(t).Float64()
		return f
	case float64:
		return t
	}
	return 0
}

// Equal implements Python ==.
func Equal(a, b Value) bool {
	if isNumber(a) && isNumber(b) {
		c, ok := NumCompare(a, b)
		return ok && c == 0
	}
	switch x := a.(type) {
	case nil:
		return b == nil
	case string:
		y, ok := b.(string)
		return ok && x == y
	case Tuple:
		y, ok := b.(Tuple)
		return ok && equalSlices(x, y)
	case *List:
		y, ok := b.(*List)
		return ok && equalSlices(x.Items, y.Items)
	case *Range:
		y, ok := b.(*Range)
		return ok && equalSlices(x.Items(), y.Items())
	case *Dict:
		y, ok := b.(*Dict)
		return ok && equalDicts(x, y)
	case *Set:
		return equalSetLike(x, b)
	case *DictView:
		if x.Kind == ViewValues {
			return a == b
		}
		return equalSetLike(x.asSet(), b)
	case *DateTime:
		y, ok := b.(*DateTime)
		return ok && x.equal(y)
	case Timedelta:
		y, ok := b.(Timedelta)
		return ok && x == y
	case *Regex:
		y, ok := b.(*Regex)
		return ok && x.Pattern == y.Pattern && x.Flags == y.Flags
	}
	return a == b
}

func equalSetLike(x *Set, b Value) bool {
	var y *Set
	switch t := b.(type) {
	case *Set:
		y = t
	case *DictView:
		if t.Kind == ViewValues {
			return false
		}
		y = t.asSet()
	default:
		return false
	}
	return x.Len() == y.Len() && x.IsSubset(y)
}

func equalSlices(x, y []Value) bool {
	if len(x) != len(y) {
		return false
	}
	for i := range x {
		if !Equal(x[i], y[i]) {
			return false
		}
	}
	return true
}

func equalDicts(x, y *Dict) bool {
	if x.Len() != y.Len() {
		return false
	}
	same := true
	x.Items(func(k, v Value) {
		if !same {
			return
		}
		w, ok := y.Get(k)
		if !ok || !Equal(v, w) {
			same = false
		}
	})
	return same
}

func hashString(s string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(s))
	return h.Sum64()
}

// Hash returns a hash consistent with Equal; unhashable values raise TypeError.
func Hash(v Value) uint64 {
	switch t := v.(type) {
	case nil:
		return 0x9e3779b97f4a7c15
	case bool:
		if t {
			return hashString("i1")
		}
		return hashString("i0")
	case *big.Int:
		return hashString("i" + t.String())
	case float64:
		if t == math.Trunc(t) && !math.IsInf(t, 0) {
			bi, _ := new(big.Float).SetFloat64(t).Int(nil)
			return hashString("i" + bi.String())
		}
		return hashString("f" + strconv.FormatFloat(t, 'g', -1, 64))
	case string:
		return hashString("s" + t)
	case Tuple:
		h := uint64(0x345678)
		for _, x := range t {
			h = h*1000003 ^ Hash(x)
		}
		return h ^ uint64(len(t))
	case *Dict:
		if !t.Frozen {
			Raise("TypeError", "unhashable type: 'dict'")
		}
		var h uint64
		t.Items(func(k, val Value) { h ^= Hash(Tuple{k, val}) })
		return h
	case *Set:
		var h uint64 = 0x1f
		for _, x := range t.items {
			h ^= Hash(x) * 0x100000001b3
		}
		return h
	case *List:
		Raise("TypeError", "unhashable type: 'list'")
	case *DictView:
		return uint64(identityOf(v))
	case *DateTime:
		return hashString("dt" + t.hashKey())
	case Timedelta:
		return hashString(fmt.Sprintf("td%d", t.Micros()))
	case *Regex:
		return hashString("re" + t.Pattern)
	}
	return uint64(identityOf(v))
}

func identityOf(v Value) int64 {
	switch t := v.(type) {
	case *sliceIterator:
		return t.id
	case *generator:
		return t.id
	case *countIterator:
		return t.id
	case *CallableFunc:
		return t.id
	}
	return int64(hashString(fmt.Sprintf("%p", v)))
}

// Str implements yaql's view of Python str() for values met inside messages
// and containers: strings unquoted, everything else as repr().
func Str(v Value) string {
	switch t := v.(type) {
	case string:
		return t
	case *DateTime:
		return t.String()
	case Timedelta:
		return t.String()
	}
	return Repr(v)
}

// Repr implements Python repr().
func Repr(v Value) string {
	var b strings.Builder
	writeRepr(&b, v)
	return b.String()
}

func writeRepr(b *strings.Builder, v Value) {
	switch t := v.(type) {
	case nil:
		b.WriteString("None")
	case bool:
		if t {
			b.WriteString("True")
		} else {
			b.WriteString("False")
		}
	case *big.Int:
		b.WriteString(t.String())
	case float64:
		b.WriteString(FloatRepr(t))
	case string:
		b.WriteString(StringRepr(t))
	case Tuple:
		b.WriteByte('(')
		for i, x := range t {
			if i > 0 {
				b.WriteString(", ")
			}
			writeRepr(b, x)
		}
		if len(t) == 1 {
			b.WriteByte(',')
		}
		b.WriteByte(')')
	case *List:
		writeList(b, t.Items)
	case *Dict:
		b.WriteByte('{')
		first := true
		t.Items(func(k, val Value) {
			if !first {
				b.WriteString(", ")
			}
			first = false
			writeRepr(b, k)
			b.WriteString(": ")
			writeRepr(b, val)
		})
		b.WriteByte('}')
	case *Set:
		if t.Len() == 0 {
			b.WriteString("frozenset()")
			return
		}
		b.WriteString("frozenset({")
		for i, x := range t.items {
			if i > 0 {
				b.WriteString(", ")
			}
			writeRepr(b, x)
		}
		b.WriteString("})")
	case *DictView:
		b.WriteString(TypeName(t))
		b.WriteByte('(')
		writeList(b, t.Items())
		b.WriteByte(')')
	case *Range:
		if t.Step.Cmp(bigOne) == 0 {
			fmt.Fprintf(b, "range(%s, %s)", t.Start, t.Stop)
		} else {
			fmt.Fprintf(b, "range(%s, %s, %s)", t.Start, t.Stop, t.Step)
		}
	case *DateTime:
		b.WriteString(t.Repr())
	case Timedelta:
		b.WriteString(t.Repr())
	case *Regex:
		b.WriteString(t.Repr())
	case *MappingRule:
		fmt.Fprintf(b, "<yaql.language.utils.MappingRule object at 0x%x>", identityOf(v))
	case fmt.Stringer:
		b.WriteString(t.String())
	default:
		fmt.Fprintf(b, "<%s object at 0x%x>", TypeName(v), identityOf(v))
	}
}

func writeList(b *strings.Builder, items []Value) {
	b.WriteByte('[')
	for i, x := range items {
		if i > 0 {
			b.WriteString(", ")
		}
		writeRepr(b, x)
	}
	b.WriteByte(']')
}

// FloatRepr formats f as Python's repr(float) does.
func FloatRepr(f float64) string {
	switch {
	case math.IsNaN(f):
		return "nan"
	case math.IsInf(f, 1):
		return "inf"
	case math.IsInf(f, -1):
		return "-inf"
	}
	s := strconv.FormatFloat(f, 'e', -1, 64)
	sign := ""
	if s[0] == '-' {
		sign = "-"
		s = s[1:]
	}
	mant, expPart, _ := strings.Cut(s, "e")
	exp, _ := strconv.Atoi(expPart)
	digits := strings.Replace(mant, ".", "", 1)
	if exp < -4 || exp >= 16 {
		out := digits[:1]
		if len(digits) > 1 {
			out += "." + digits[1:]
		}
		es := "+"
		if exp < 0 {
			es = "-"
			exp = -exp
		}
		return fmt.Sprintf("%s%se%s%02d", sign, out, es, exp)
	}
	if exp < 0 {
		return sign + "0." + strings.Repeat("0", -exp-1) + digits
	}
	if len(digits) <= exp+1 {
		return sign + digits + strings.Repeat("0", exp+1-len(digits)) + ".0"
	}
	return sign + digits[:exp+1] + "." + digits[exp+1:]
}

// StringRepr formats s as Python's repr(str) does.
func StringRepr(s string) string {
	quote := byte('\'')
	if strings.ContainsRune(s, '\'') && !strings.ContainsRune(s, '"') {
		quote = '"'
	}
	var b strings.Builder
	b.WriteByte(quote)
	for _, r := range s {
		switch {
		case r == rune(quote) || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\t':
			b.WriteString(`\t`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\x%02x`, r)
		case r < 0x7f:
			b.WriteRune(r)
		case isPrintable(r):
			b.WriteRune(r)
		case r <= 0xff:
			fmt.Fprintf(&b, `\x%02x`, r)
		case r <= 0xffff:
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			fmt.Fprintf(&b, `\U%08x`, r)
		}
	}
	b.WriteByte(quote)
	return b.String()
}

// isPrintable approximates Python's str.isprintable for one character.
func isPrintable(r rune) bool {
	if r == ' ' {
		return true
	}
	if unicode.Is(unicode.Zs, r) || unicode.In(r, unicode.Cc, unicode.Cf, unicode.Cs, unicode.Co, unicode.Zl, unicode.Zp) {
		return false
	}
	return unicode.IsPrint(r) || unicode.IsGraphic(r) && !unicode.IsSpace(r)
}
