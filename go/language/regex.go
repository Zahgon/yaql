package language

import (
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Python re flag values.
const (
	ReIgnoreCase = 2
	ReMultiline  = 8
	ReDotAll     = 16
	ReUnicode    = 32
)

// Regex is a compiled regular expression with Python re semantics for the
// constructs Python and Go share.
type Regex struct {
	Pattern string
	Flags   int
	re      *regexp.Regexp
}

// CompileRegex compiles pattern with Python flags.
func CompileRegex(pattern string, flags int) *Regex {
	prefix := ""
	if flags&ReIgnoreCase != 0 {
		prefix += "i"
	}
	if flags&ReMultiline != 0 {
		prefix += "m"
	}
	if flags&ReDotAll != 0 {
		prefix += "s"
	}
	src := translatePattern(pattern)
	if prefix != "" {
		src = "(?" + prefix + ")" + src
	}
	re, err := regexp.Compile(src)
	if err != nil {
		Raise("error", "%s", err.Error())
	}
	return &Regex{Pattern: pattern, Flags: flags | ReUnicode, re: re}
}

func translatePattern(p string) string {
	var b strings.Builder
	for i := 0; i < len(p); i++ {
		if p[i] == '\\' && i+1 < len(p) {
			if p[i+1] == 'Z' {
				b.WriteString(`\z`)
			} else {
				b.WriteByte(p[i])
				b.WriteByte(p[i+1])
			}
			i++
			continue
		}
		b.WriteByte(p[i])
	}
	return b.String()
}

// Repr formats like Python's repr of a compiled pattern.
func (r *Regex) Repr() string {
	var names []string
	if r.Flags&ReIgnoreCase != 0 {
		names = append(names, "re.IGNORECASE")
	}
	if r.Flags&ReMultiline != 0 {
		names = append(names, "re.MULTILINE")
	}
	if r.Flags&ReDotAll != 0 {
		names = append(names, "re.DOTALL")
	}
	if len(names) == 0 {
		return "re.compile(" + StringRepr(r.Pattern) + ")"
	}
	return "re.compile(" + StringRepr(r.Pattern) + ", " + strings.Join(names, "|") + ")"
}

// Match is one regex match with code point offsets.
type Match struct {
	subject string
	idx     []int // byte offsets
	re      *Regex
}

func runeOffset(s string, byteOff int) int {
	if byteOff < 0 {
		return -1
	}
	return utf8.RuneCountInString(s[:byteOff])
}

// Group returns group i, or nil when it did not participate.
func (m *Match) Group(i int) Value {
	if m.idx[2*i] < 0 {
		return nil
	}
	return m.subject[m.idx[2*i]:m.idx[2*i+1]]
}

// Start returns the code point start of group i, -1 if unmatched.
func (m *Match) Start(i int) int { return runeOffset(m.subject, m.idx[2*i]) }

// End returns the code point end of group i, -1 if unmatched.
func (m *Match) End(i int) int { return runeOffset(m.subject, m.idx[2*i+1]) }

// NumGroups returns the number of capturing groups.
func (m *Match) NumGroups() int { return len(m.idx)/2 - 1 }

// GroupNames returns the names of named groups in definition order.
func (r *Regex) GroupNames() []string {
	var out []string
	for _, n := range r.re.SubexpNames()[1:] {
		if n != "" {
			out = append(out, n)
		}
	}
	return out
}

// GroupIndex returns the index of a named group.
func (r *Regex) GroupIndex(name string) int { return r.re.SubexpIndex(name) }

// Search returns the first match or nil.
func (r *Regex) Search(s string) *Match {
	idx := r.re.FindStringSubmatchIndex(s)
	if idx == nil {
		return nil
	}
	return &Match{s, idx, r}
}

// FindAll returns all non-overlapping matches.
func (r *Regex) FindAll(s string) []*Match {
	var out []*Match
	for _, idx := range r.re.FindAllStringSubmatchIndex(s, -1) {
		out = append(out, &Match{s, idx, r})
	}
	return out
}

// Split is pattern.split(string, maxsplit).
func (r *Regex) Split(s string, maxSplit int) *List {
	var out []Value
	last := 0
	n := 0
	for _, m := range r.FindAll(s) {
		if maxSplit < 0 || maxSplit > 0 && n >= maxSplit {
			break
		}
		out = append(out, s[last:m.idx[0]])
		for g := 1; g <= m.NumGroups(); g++ {
			out = append(out, m.Group(g))
		}
		last = m.idx[1]
		n++
	}
	out = append(out, s[last:])
	return &List{Items: out}
}

// Sub is pattern.sub(repl, string, count) with repl computed per match.
func (r *Regex) Sub(s string, count int, repl func(*Match) string) string {
	var b strings.Builder
	last := 0
	n := 0
	for _, m := range r.FindAll(s) {
		if count < 0 || count > 0 && n >= count {
			break
		}
		b.WriteString(s[last:m.idx[0]])
		b.WriteString(repl(m))
		last = m.idx[1]
		n++
	}
	b.WriteString(s[last:])
	return b.String()
}

// ExpandTemplate expands a Python replacement template for m.
func (r *Regex) ExpandTemplate(template string, m *Match) string {
	var b strings.Builder
	for i := 0; i < len(template); i++ {
		c := template[i]
		if c != '\\' || i+1 >= len(template) {
			if c == '\\' {
				Raise("error", "bad escape (end of pattern) at position %d", i)
			}
			b.WriteByte(c)
			continue
		}
		i++
		c = template[i]
		switch {
		case c == 'g':
			end := strings.IndexByte(template[i:], '>')
			if i+1 >= len(template) || template[i+1] != '<' || end < 0 {
				Raise("error", "missing <")
			}
			name := template[i+2 : i+end]
			idx, err := strconv.Atoi(name)
			if err != nil {
				idx = r.GroupIndex(name)
				if idx < 0 {
					Raise("IndexError", "unknown group name '%s'", name)
				}
			}
			if idx > m.NumGroups() {
				Raise("error", "invalid group reference %d", idx)
			}
			if g := m.Group(idx); g != nil {
				b.WriteString(g.(string))
			}
			i += end
		case c >= '0' && c <= '9':
			j := i + 1
			if j < len(template) && template[j] >= '0' && template[j] <= '9' {
				j++
			}
			idx, _ := strconv.Atoi(template[i:j])
			if idx > m.NumGroups() {
				Raise("error", "invalid group reference %d at position %d", idx, i-1)
			}
			if g := m.Group(idx); g != nil {
				b.WriteString(g.(string))
			}
			i = j - 1
		case c == 'n':
			b.WriteByte('\n')
		case c == 't':
			b.WriteByte('\t')
		case c == 'r':
			b.WriteByte('\r')
		case c == 'f':
			b.WriteByte('\f')
		case c == 'v':
			b.WriteByte('\v')
		case c == 'a':
			b.WriteByte('\a')
		case c == 'b':
			b.WriteByte('\b')
		case c == '\\':
			b.WriteByte('\\')
		case c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z':
			Raise("error", "bad escape \\%c at position %d", c, i-1)
		default:
			b.WriteByte('\\')
			b.WriteByte(c)
		}
	}
	return b.String()
}

// EscapeRegex is Python's re.escape.
func EscapeRegex(s string) string {
	const special = "()[]{}?*+-|^$\\.&~# \t\n\r\v\f"
	var b strings.Builder
	for _, r := range s {
		if r == 0 {
			b.WriteString(`\x00`)
			continue
		}
		if r < 128 && strings.ContainsRune(special, r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}
