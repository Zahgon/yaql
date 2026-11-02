package stdlib

import (
	"math"
	"math/big"
	"strconv"
	"strings"
	"unicode"

	. "yaql/language"
)

// toFloat converts an int or float to float64 like Python's float(int).
func toFloat(v Value) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case bool:
		if t {
			return 1
		}
		return 0
	case *big.Int:
		if t.IsInt64() {
			return float64(t.Int64())
		}
		f, acc := new(big.Float).SetInt(t).Float64()
		if math.IsInf(f, 0) || (acc != big.Exact && math.Abs(f) == math.MaxFloat64 && t.BitLen() > 1024) {
			Raise("OverflowError", "int too large to convert to float")
		}
		return correctlyRoundedFloat(t)
	}
	panic("not a number")
}

// correctlyRoundedFloat rounds a big integer to the nearest float64, ties to even.
func correctlyRoundedFloat(t *big.Int) float64 {
	f, _ := new(big.Float).SetPrec(53).SetMode(big.ToNearestEven).SetInt(t).Float64()
	if math.IsInf(f, 0) {
		Raise("OverflowError", "int too large to convert to float")
	}
	return f
}

func isFloat(v Value) bool { _, ok := v.(float64); return ok }

func numAdd(a, b Value) Value {
	if isFloat(a) || isFloat(b) {
		return toFloat(a) + toFloat(b)
	}
	return new(big.Int).Add(bigInt(a), bigInt(b))
}

func numSub(a, b Value) Value {
	if isFloat(a) || isFloat(b) {
		return toFloat(a) - toFloat(b)
	}
	return new(big.Int).Sub(bigInt(a), bigInt(b))
}

func numMul(a, b Value) Value {
	if isFloat(a) || isFloat(b) {
		return toFloat(a) * toFloat(b)
	}
	return new(big.Int).Mul(bigInt(a), bigInt(b))
}

// floorDivMod is Python's divmod for integers.
func floorDivMod(a, b *big.Int) (*big.Int, *big.Int) {
	q, m := new(big.Int).QuoRem(a, b, new(big.Int))
	if m.Sign() != 0 && (m.Sign() < 0) != (b.Sign() < 0) {
		q.Sub(q, big.NewInt(1))
		m.Add(m, b)
	}
	return q, m
}

func numDiv(a, b Value) Value {
	if !isFloat(a) && !isFloat(b) {
		if bigInt(b).Sign() == 0 {
			Raise("ZeroDivisionError", "integer division or modulo by zero")
		}
		q, _ := floorDivMod(bigInt(a), bigInt(b))
		return q
	}
	x, y := toFloat(a), toFloat(b)
	if y == 0 {
		Raise("ZeroDivisionError", "float division by zero")
	}
	return x / y
}

func numMod(a, b Value) Value {
	if !isFloat(a) && !isFloat(b) {
		if bigInt(b).Sign() == 0 {
			Raise("ZeroDivisionError", "integer modulo by zero")
		}
		_, m := floorDivMod(bigInt(a), bigInt(b))
		return m
	}
	x, y := toFloat(a), toFloat(b)
	if y == 0 {
		Raise("ZeroDivisionError", "float modulo by zero")
	}
	m := math.Mod(x, y)
	if m != 0 {
		if (y < 0) != (m < 0) {
			m += y
		}
	} else {
		m = math.Copysign(0, y)
	}
	return m
}

func numNeg(a Value) Value {
	if f, ok := a.(float64); ok {
		return -f
	}
	return new(big.Int).Neg(bigInt(a))
}

func numCmp(a, b Value) (int, bool) { return NumCompare(a, b) }

// pyRound is Python's round(number, ndigits) with ndigits given.
func pyRound(number Value, ndigits *big.Int) Value {
	if !isFloat(number) {
		n := bigInt(number)
		if ndigits.Sign() >= 0 {
			return new(big.Int).Set(n)
		}
		pow := new(big.Int).Exp(big.NewInt(10), new(big.Int).Neg(ndigits), nil)
		return new(big.Int).Mul(roundHalfEvenRat(new(big.Rat).SetFrac(n, pow)), pow)
	}
	x := number.(float64)
	if math.IsInf(x, 0) || math.IsNaN(x) || x == 0 {
		return x
	}
	if ndigits.Cmp(big.NewInt(400)) > 0 {
		return x
	}
	if ndigits.Cmp(big.NewInt(-400)) < 0 {
		return math.Copysign(0, x)
	}
	nd := ndigits.Int64()
	exact := new(big.Rat).SetFloat64(x)
	pow := new(big.Rat).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(abs64(nd)), nil))
	var scaled *big.Rat
	if nd >= 0 {
		scaled = new(big.Rat).Mul(exact, pow)
	} else {
		scaled = new(big.Rat).Quo(exact, pow)
	}
	r := new(big.Rat).SetInt(roundHalfEvenRat(scaled))
	if nd >= 0 {
		r.Quo(r, pow)
	} else {
		r.Mul(r, pow)
	}
	f, _ := r.Float64()
	if math.IsInf(f, 0) {
		Raise("OverflowError", "rounded value too large to represent")
	}
	if f == 0 {
		return math.Copysign(0, x)
	}
	return f
}

func abs64(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

func roundHalfEvenRat(r *big.Rat) *big.Int {
	num, den := r.Num(), r.Denom()
	q, m := floorDivMod(num, den)
	twice := new(big.Int).Mul(m, big.NewInt(2))
	switch twice.Cmp(den) {
	case 1:
		q.Add(q, big.NewInt(1))
	case 0:
		if q.Bit(0) == 1 {
			q.Add(q, big.NewInt(1))
		}
	}
	return q
}

// pyIntFromString parses like Python's int(str).
func pyIntFromString(s string) *big.Int {
	t := strings.TrimFunc(s, unicode.IsSpace)
	body := t
	if strings.HasPrefix(body, "+") || strings.HasPrefix(body, "-") {
		body = body[1:]
	}
	ok := body != "" && !strings.HasPrefix(body, "_") && !strings.HasSuffix(body, "_") && !strings.Contains(body, "__")
	var digits strings.Builder
	for _, r := range body {
		switch {
		case r == '_':
		case unicode.Is(unicode.Nd, r):
			digits.WriteRune('0' + rune(digitOf(r)))
		default:
			ok = false
		}
	}
	if ok {
		v, good := new(big.Int).SetString(digits.String(), 10)
		if good {
			if strings.HasPrefix(t, "-") {
				v.Neg(v)
			}
			return v
		}
	}
	Raise("ValueError", "invalid literal for int() with base 10: %s", StringRepr(s))
	return nil
}

func digitOf(r rune) int {
	if r >= '0' && r <= '9' {
		return int(r - '0')
	}
	return int(r) % 16 % 10
}

// pyFloatFromString parses like Python's float(str).
func pyFloatFromString(s string) float64 {
	t := strings.TrimFunc(s, unicode.IsSpace)
	lower := strings.ToLower(t)
	sign := 1.0
	body := lower
	if strings.HasPrefix(body, "+") {
		body = body[1:]
	} else if strings.HasPrefix(body, "-") {
		body = body[1:]
		sign = -1
	}
	switch body {
	case "inf", "infinity":
		return math.Inf(int(sign))
	case "nan":
		return math.NaN()
	}
	valid := body != ""
	clean := strings.Builder{}
	prevDigit := false
	for i, r := range body {
		switch {
		case r >= '0' && r <= '9':
			prevDigit = true
			clean.WriteRune(r)
		case r == '_':
			if !prevDigit || i+1 >= len(body) || body[i+1] < '0' || body[i+1] > '9' {
				valid = false
			}
		case r == '.' || r == 'e' || r == '+' || r == '-':
			prevDigit = false
			clean.WriteRune(r)
		default:
			valid = false
		}
	}
	if valid {
		cs := clean.String()
		if strings.ContainsAny(cs, "0123456789") && !strings.HasPrefix(cs, "e") {
			if f, err := strconv.ParseFloat(cs, 64); err == nil || strings.Contains(err.Error(), "range") {
				return sign * f
			}
		}
	}
	Raise("ValueError", "could not convert string to float: %s", StringRepr(s))
	return 0
}

// pyPow is Python's pow(a, b, c).
func pyPow(a, b, c Value) Value {
	if c != nil {
		if isFloat(a) || isFloat(b) || isFloat(c) {
			Raise("TypeError", "pow() 3rd argument not allowed unless all arguments are integers")
		}
		m := bigInt(c)
		if m.Sign() == 0 {
			Raise("ValueError", "pow() 3rd argument cannot be 0")
		}
		base, exp := bigInt(a), bigInt(b)
		mod := new(big.Int).Abs(m)
		var r *big.Int
		if exp.Sign() < 0 {
			inv := new(big.Int).ModInverse(new(big.Int).Mod(base, mod), mod)
			if inv == nil {
				Raise("ValueError", "base is not invertible for the given modulus")
			}
			r = new(big.Int).Exp(inv, new(big.Int).Neg(exp), mod)
		} else {
			r = new(big.Int).Exp(new(big.Int).Mod(base, mod), exp, mod)
		}
		if m.Sign() < 0 && r.Sign() != 0 {
			r.Add(r, m)
		}
		return r
	}
	if !isFloat(a) && !isFloat(b) {
		base, exp := bigInt(a), bigInt(b)
		if exp.Sign() >= 0 {
			if exp.BitLen() > 24 && base.CmpAbs(big.NewInt(1)) > 0 {
				Raise("MemoryError", "")
			}
			return new(big.Int).Exp(base, exp, nil)
		}
		if base.Sign() == 0 {
			Raise("ZeroDivisionError", "0.0 cannot be raised to a negative power")
		}
		return floatPow(toFloat(base), toFloat(exp))
	}
	return floatPow(toFloat(a), toFloat(b))
}

func floatPow(x, y float64) Value {
	if x == 0 && y < 0 {
		Raise("ZeroDivisionError", "0.0 cannot be raised to a negative power")
	}
	if x < 0 && y != math.Trunc(y) && !math.IsInf(y, 0) {
		return &Complex{}
	}
	r := math.Pow(x, y)
	if math.IsInf(r, 0) && !math.IsInf(x, 0) && !math.IsInf(y, 0) {
		Raise("OverflowError", "(34, 'Numerical result out of range')")
	}
	return r
}

// Complex stands for a complex result, which yaql data cannot express.
type Complex struct{}

func (*Complex) String() string { return "complex" }
