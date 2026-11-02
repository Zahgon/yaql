package stdlib

import (
	"strings"
	"unicode"

	. "yaql/language"
)

func isPySpace(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\r', '\v', '\f', 0x1c, 0x1d, 0x1e, 0x1f, 0x85:
		return true
	}
	return r > 0x7f && unicode.IsSpace(r)
}

func strList(items []string) *List {
	out := make([]Value, len(items))
	for i, s := range items {
		out[i] = s
	}
	return &List{Items: out}
}

// pySplit is str.split(sep, maxsplit).
func pySplit(s string, sep Value, maxSplit int) *List {
	if sep == nil {
		var out []string
		rs := []rune(s)
		i := 0
		for maxSplit != 0 {
			for i < len(rs) && isPySpace(rs[i]) {
				i++
			}
			if i >= len(rs) {
				break
			}
			j := i
			for j < len(rs) && !isPySpace(rs[j]) {
				j++
			}
			out = append(out, string(rs[i:j]))
			i = j
			if maxSplit > 0 {
				maxSplit--
			}
		}
		for i < len(rs) && isPySpace(rs[i]) {
			i++
		}
		if i < len(rs) {
			out = append(out, string(rs[i:]))
		}
		return strList(out)
	}
	sp := sep.(string)
	if sp == "" {
		Raise("ValueError", "empty separator")
	}
	return strList(strings.SplitN(s, sp, splitLimit(maxSplit)))
}

func splitLimit(maxSplit int) int {
	if maxSplit < 0 {
		return -1
	}
	return maxSplit + 1
}

// pyRSplit is str.rsplit(sep, maxsplit).
func pyRSplit(s string, sep Value, maxSplit int) *List {
	if sep == nil {
		var out []string
		rs := []rune(s)
		j := len(rs)
		for maxSplit != 0 {
			for j > 0 && isPySpace(rs[j-1]) {
				j--
			}
			if j <= 0 {
				break
			}
			i := j
			for i > 0 && !isPySpace(rs[i-1]) {
				i--
			}
			out = append(out, string(rs[i:j]))
			j = i
			if maxSplit > 0 {
				maxSplit--
			}
		}
		for j > 0 && isPySpace(rs[j-1]) {
			j--
		}
		if j > 0 {
			out = append(out, string(rs[:j]))
		}
		for a, b := 0, len(out)-1; a < b; a, b = a+1, b-1 {
			out[a], out[b] = out[b], out[a]
		}
		return strList(out)
	}
	sp := sep.(string)
	if sp == "" {
		Raise("ValueError", "empty separator")
	}
	if maxSplit < 0 {
		return strList(strings.Split(s, sp))
	}
	var out []string
	rest := s
	for maxSplit > 0 {
		k := strings.LastIndex(rest, sp)
		if k < 0 {
			break
		}
		out = append(out, rest[k+len(sp):])
		rest = rest[:k]
		maxSplit--
	}
	out = append(out, rest)
	for a, b := 0, len(out)-1; a < b; a, b = a+1, b-1 {
		out[a], out[b] = out[b], out[a]
	}
	return strList(out)
}

func stripSet(chars Value) func(rune) bool {
	if chars == nil {
		return isPySpace
	}
	set := chars.(string)
	return func(r rune) bool { return strings.ContainsRune(set, r) }
}

func pyStrip(s string, chars Value) string  { return strings.TrimFunc(s, stripSet(chars)) }
func pyLStrip(s string, chars Value) string { return strings.TrimLeftFunc(s, stripSet(chars)) }
func pyRStrip(s string, chars Value) string { return strings.TrimRightFunc(s, stripSet(chars)) }

// pyReplace is str.replace(old, new, count).
func pyReplace(s, old, repl string, count int) string {
	if count < 0 {
		count = -1
	}
	if old == "" {
		rs := []rune(s)
		var b strings.Builder
		n := 0
		for i := 0; i <= len(rs); i++ {
			if count < 0 || n < count {
				b.WriteString(repl)
				n++
			}
			if i < len(rs) {
				b.WriteRune(rs[i])
			}
		}
		return b.String()
	}
	return strings.Replace(s, old, repl, count)
}

// sliceBounds applies Python slice index semantics to [start, end).
func sliceBounds(n, start, end int) (int, int) {
	adjust := func(i int) int {
		if i < 0 {
			i += n
			if i < 0 {
				i = 0
			}
		}
		if i > n {
			i = n
		}
		return i
	}
	return adjust(start), adjust(end)
}

// pyFind is str.find(sub, start, end) on code points; end < 0 sentinel
// handled by the caller passing len.
func pyFind(s, sub string, start, end int, reverse bool) int {
	rs, sr := []rune(s), []rune(sub)
	n := len(rs)
	// Python checks start > len before clamping for the empty-substring case.
	st, en := start, end
	if st < 0 {
		st += n
		if st < 0 {
			st = 0
		}
	}
	if en > n {
		en = n
	} else if en < 0 {
		en += n
		if en < 0 {
			en = 0
		}
	}
	if st > n || en-st < len(sr) {
		return -1
	}
	if reverse {
		for i := en - len(sr); i >= st; i-- {
			if runesEqual(rs[i:i+len(sr)], sr) {
				return i
			}
		}
		return -1
	}
	for i := st; i+len(sr) <= en; i++ {
		if runesEqual(rs[i:i+len(sr)], sr) {
			return i
		}
	}
	return -1
}

func runesEqual(a, b []rune) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
