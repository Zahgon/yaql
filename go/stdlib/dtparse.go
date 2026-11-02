package stdlib

import (
	"regexp"
	"strconv"
	"strings"

	. "yaql/language"
)

var isoDateTime = regexp.MustCompile(`^\s*(\d{4})-(\d{1,2})-(\d{1,2})(?:[T ](\d{1,2}):(\d{2})(?::(\d{2})(?:\.(\d+))?)?)?\s*(Z|[+-]\d{2}(?::?\d{2})?)?\s*$`)

// parseDateString accepts the ISO 8601 forms, as dateutil.parser.parse reads
// them.
func parseDateString(s string) *DateTime {
	m := isoDateTime.FindStringSubmatch(s)
	if m == nil {
		Raise("ParserError", "Unknown string format: %s", s)
	}
	num := func(x string) int64 {
		if x == "" {
			return 0
		}
		v, _ := strconv.ParseInt(x, 10, 64)
		return v
	}
	var micro int64
	if m[7] != "" {
		f := m[7]
		if len(f) > 6 {
			f = f[:6]
		}
		micro = num(f + strings.Repeat("0", 6-len(f)))
	}
	var zone *TZ
	switch tz := m[8]; {
	case tz == "":
	case tz == "Z":
		zone = UTCZone
	default:
		sign := int64(1)
		if tz[0] == '-' {
			sign = -1
		}
		digits := strings.ReplaceAll(tz[1:], ":", "")
		hours := num(digits[:2])
		var minutes int64
		if len(digits) > 2 {
			minutes = num(digits[2:])
		}
		offset := sign * (hours*3600 + minutes*60)
		if offset == 0 {
			zone = UTCZone
		} else {
			zone = &TZ{Offset: offset}
		}
	}
	var dt *DateTime
	err := Catch(func() {
		dt = NewDateTime(num(m[1]), num(m[2]), num(m[3]), num(m[4]), num(m[5]), num(m[6]), micro, zone)
	})
	if err != nil {
		Raise("ParserError", "%s: %s", err.Message, s)
	}
	return dt
}

var strptimeDirectives = map[byte]string{
	'Y': `(?P<Y>\d\d\d\d)`,
	'm': `(?P<m>1[0-2]|0[1-9]|[1-9])`,
	'd': `(?P<d>3[01]|[12]\d|0[1-9]|[1-9]| [1-9])`,
	'H': `(?P<H>2[0-3]|[0-1]\d|\d)`,
	'M': `(?P<M>[0-5]\d|\d)`,
	'S': `(?P<S>6[0-1]|[0-5]\d|\d)`,
	'f': `(?P<f>[0-9]{1,6})`,
	'y': `(?P<y>\d\d)`,
	'I': `(?P<I>1[0-2]|0[1-9]|[1-9])`,
	'p': `(?P<p>am|pm)`,
	'b': `(?P<b>jan|feb|mar|apr|may|jun|jul|aug|sep|oct|nov|dec)`,
	'B': `(?P<B>january|february|march|april|may|june|july|august|september|october|november|december)`,
	'a': `(?P<a>mon|tue|wed|thu|fri|sat|sun)`,
	'A': `(?P<A>monday|tuesday|wednesday|thursday|friday|saturday|sunday)`,
	'j': `(?P<j>36[0-6]|3[0-5]\d|[12]\d\d|0[1-9]\d|00[1-9]|[1-9]\d|0[1-9]|[1-9])`,
	'z': `(?P<z>[+-]\d\d:?[0-5]\d(?::?[0-5]\d(?:\.\d{1,6})?)?|Z)`,
}

// strptime is datetime.strptime for the directives in strptimeDirectives.
func strptime(data, format string) *DateTime {
	var b strings.Builder
	used := map[byte]bool{}
	for i := 0; i < len(format); i++ {
		c := format[i]
		switch {
		case c == '%' && i+1 < len(format):
			i++
			d := format[i]
			if d == '%' {
				b.WriteString("%")
				continue
			}
			re, ok := strptimeDirectives[d]
			if !ok {
				Raise("ValueError", "'%c' is a bad directive in format '%s'", d, format)
			}
			if used[d] {
				re = regexp.MustCompile(`\(\?P<\w>`).ReplaceAllString(re, "(?:")
			}
			used[d] = true
			b.WriteString(re)
		case c == ' ' || c == '\t' || c == '\n':
			for i+1 < len(format) && strings.ContainsRune(" \t\n", rune(format[i+1])) {
				i++
			}
			b.WriteString(`\s+`)
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	re := regexp.MustCompile(`(?i)^(?:` + b.String() + `)`)
	m := re.FindStringSubmatchIndex(data)
	if m == nil {
		Raise("ValueError", "time data %s does not match format %s", StringRepr(data), StringRepr(format))
	}
	if m[1] != len(data) {
		Raise("ValueError", "unconverted data remains: %s", data[m[1]:])
	}
	group := func(name string) string {
		i := re.SubexpIndex(name)
		if i < 0 || m[2*i] < 0 {
			return ""
		}
		return strings.TrimSpace(data[m[2*i]:m[2*i+1]])
	}
	num := func(name string, def int64) int64 {
		s := group(name)
		if s == "" {
			return def
		}
		v, _ := strconv.ParseInt(s, 10, 64)
		return v
	}
	year := num("Y", 1900)
	if y := group("y"); y != "" {
		v, _ := strconv.ParseInt(y, 10, 64)
		if v <= 68 {
			year = 2000 + v
		} else {
			year = 1900 + v
		}
	}
	month := num("m", 1)
	if s := strings.ToLower(group("b")); s != "" {
		month = int64(indexPrefix(s, 3) + 1)
	}
	if s := strings.ToLower(group("B")); s != "" {
		month = int64(indexPrefix(s, 0) + 1)
	}
	day := num("d", 1)
	hour := num("H", 0)
	if group("I") != "" {
		hour = num("I", 12) % 12
		if strings.ToLower(group("p")) == "pm" {
			hour += 12
		}
	}
	var micro int64
	if f := group("f"); f != "" {
		micro, _ = strconv.ParseInt(f+strings.Repeat("0", 6-len(f)), 10, 64)
	}
	var zone *TZ
	if z := group("z"); z != "" {
		if strings.EqualFold(z, "Z") {
			zone = &TZ{}
		} else {
			sign := int64(1)
			if z[0] == '-' {
				sign = -1
			}
			digits := strings.ReplaceAll(z[1:], ":", "")
			h, _ := strconv.ParseInt(digits[:2], 10, 64)
			mi, _ := strconv.ParseInt(digits[2:4], 10, 64)
			var sec int64
			if len(digits) >= 6 {
				sec, _ = strconv.ParseInt(digits[4:6], 10, 64)
			}
			zone = &TZ{Offset: sign * (h*3600 + mi*60 + sec)}
		}
	}
	if j := group("j"); j != "" && group("m") == "" && group("d") == "" && group("b") == "" && group("B") == "" {
		jd, _ := strconv.ParseInt(j, 10, 64)
		base := NewDateTime(year, 1, 1, 0, 0, 0, 0, nil)
		d := base.Add(NewTimedelta((jd - 1) * 86400 * 1000000))
		month, day = int64(d.Month), int64(d.Day)
	}
	return NewDateTime(year, month, day, hour, num("M", 0), num("S", 0), micro, zone)
}

func indexPrefix(s string, n int) int {
	months := []string{"january", "february", "march", "april", "may", "june", "july", "august",
		"september", "october", "november", "december"}
	for i, m := range months {
		if n > 0 && m[:n] == s || m == s {
			return i
		}
	}
	return 0
}
