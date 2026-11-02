package language

import (
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"time"
)

const (
	microsPerSecond = 1000000
	microsPerDay    = 86400 * microsPerSecond
	maxDeltaDays    = 999999999
)

// Timedelta is a duration with microsecond resolution (Python timedelta).
type Timedelta struct {
	micros int64
}

// NewTimedelta builds a timedelta from a total number of microseconds.
func NewTimedelta(micros int64) Timedelta { return Timedelta{micros} }

// TimedeltaFromBig builds a timedelta, raising OverflowError when out of range.
func TimedeltaFromBig(micros *big.Int) Timedelta {
	days := new(big.Int).Div(micros, big.NewInt(microsPerDay))
	if days.CmpAbs(big.NewInt(maxDeltaDays)) > 0 || !micros.IsInt64() {
		Raise("OverflowError", "days=%s; must have magnitude <= 999999999", days)
	}
	return Timedelta{micros.Int64()}
}

// TimedeltaFromFloat rounds a float number of microseconds half to even.
func TimedeltaFromFloat(micros float64) Timedelta {
	if math.IsNaN(micros) {
		Raise("ValueError", "cannot convert float NaN to integer")
	}
	if math.IsInf(micros, 0) || math.Abs(micros) > 9e18 {
		Raise("OverflowError", "days=%d; must have magnitude <= 999999999", int64(micros/microsPerDay))
	}
	return TimedeltaFromBig(big.NewInt(int64(math.RoundToEven(micros))))
}

// Micros returns the total microseconds.
func (t Timedelta) Micros() int64 { return t.micros }

// Parts returns the normalized (days, seconds, microseconds).
func (t Timedelta) Parts() (days, seconds, micros int64) {
	days = floorDiv(t.micros, microsPerDay)
	rest := t.micros - days*microsPerDay
	return days, rest / microsPerSecond, rest % microsPerSecond
}

func floorDiv(a, b int64) int64 {
	q := a / b
	if (a%b != 0) && ((a < 0) != (b < 0)) {
		q--
	}
	return q
}

func (t Timedelta) String() string {
	days, secs, us := t.Parts()
	mm, ss := secs/60, secs%60
	hh, mm := mm/60, mm%60
	s := fmt.Sprintf("%d:%02d:%02d", hh, mm, ss)
	if days != 0 {
		plural := "s"
		if days == 1 || days == -1 {
			plural = ""
		}
		s = fmt.Sprintf("%d day%s, ", days, plural) + s
	}
	if us != 0 {
		s += fmt.Sprintf(".%06d", us)
	}
	return s
}

// Repr formats like Python's repr(timedelta).
func (t Timedelta) Repr() string {
	days, secs, us := t.Parts()
	var parts []string
	if days != 0 {
		parts = append(parts, fmt.Sprintf("days=%d", days))
	}
	if secs != 0 {
		parts = append(parts, fmt.Sprintf("seconds=%d", secs))
	}
	if us != 0 {
		parts = append(parts, fmt.Sprintf("microseconds=%d", us))
	}
	if len(parts) == 0 {
		return "datetime.timedelta(0)"
	}
	return "datetime.timedelta(" + strings.Join(parts, ", ") + ")"
}

// TotalSeconds is timedelta.total_seconds().
func (t Timedelta) TotalSeconds() float64 {
	return float64(t.micros) / microsPerSecond
}

// TZ is a fixed-offset time zone. UTC marks dateutil's tzutc().
type TZ struct {
	Offset int64 // seconds east of UTC
	UTC    bool
}

// UTCZone is the zone yaql attaches to naive datetimes.
var UTCZone = &TZ{UTC: true}

func (z *TZ) repr() string {
	if z.UTC {
		return "tzutc()"
	}
	return fmt.Sprintf("tzoffset(None, %d)", z.Offset)
}

// DateTime is a Python datetime; Zone nil means naive.
type DateTime struct {
	Year, Month, Day, Hour, Minute, Second, Microsecond int
	Zone                                                *TZ
}

var daysInMonth = [13]int{0, 31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}

func isLeap(y int) bool { return y%4 == 0 && (y%100 != 0 || y%400 == 0) }

func monthDays(y, m int) int {
	if m == 2 && isLeap(y) {
		return 29
	}
	return daysInMonth[m]
}

// NewDateTime validates fields as datetime() does.
func NewDateTime(year, month, day, hour, minute, second, micro int64, zone *TZ) *DateTime {
	if year < 1 || year > 9999 {
		Raise("ValueError", "year %d is out of range", year)
	}
	if month < 1 || month > 12 {
		Raise("ValueError", "month must be in 1..12")
	}
	if day < 1 || day > int64(monthDays(int(year), int(month))) {
		Raise("ValueError", "day is out of range for month")
	}
	if hour < 0 || hour > 23 {
		Raise("ValueError", "hour must be in 0..23")
	}
	if minute < 0 || minute > 59 {
		Raise("ValueError", "minute must be in 0..59")
	}
	if second < 0 || second > 59 {
		Raise("ValueError", "second must be in 0..59")
	}
	if micro < 0 || micro > 999999 {
		Raise("ValueError", "microsecond must be in 0..999999")
	}
	return &DateTime{int(year), int(month), int(day), int(hour), int(minute), int(second), int(micro), zone}
}

// ordinal returns days since 0001-01-01 (day 1).
func ordinal(y, m, d int) int64 {
	y1 := int64(y - 1)
	days := y1*365 + y1/4 - y1/100 + y1/400
	for i := 1; i < m; i++ {
		days += int64(monthDays(y, i))
	}
	return days + int64(d)
}

func fromOrdinal(n int64) (y, m, d int) {
	// Python's algorithm (datetime._ord2ymd).
	n--
	n400, n := n/146097, n%146097
	year := n400*400 + 1
	n100, n := n/36524, n%36524
	n4, n := n/1461, n%1461
	n1, n := n/365, n%365
	year += n100*100 + n4*4 + n1
	if n1 == 4 || n100 == 4 {
		return int(year - 1), 12, 31
	}
	y = int(year)
	m = 1
	for n >= int64(monthDays(y, m)) {
		n -= int64(monthDays(y, m))
		m++
	}
	return y, m, int(n) + 1
}

// wallMicros returns microseconds since 0001-01-01 of the wall time.
func (d *DateTime) wallMicros() int64 {
	days := ordinal(d.Year, d.Month, d.Day) - 1
	return days*microsPerDay + int64(d.Hour)*3600*microsPerSecond + int64(d.Minute)*60*microsPerSecond +
		int64(d.Second)*microsPerSecond + int64(d.Microsecond)
}

func dateTimeFromWall(micros int64, zone *TZ) *DateTime {
	if micros < 0 || micros >= ordinal(10000, 1, 1)*microsPerDay-microsPerDay {
		Raise("OverflowError", "date value out of range")
	}
	days := micros / microsPerDay
	rest := micros % microsPerDay
	y, m, dd := fromOrdinal(days + 1)
	return &DateTime{y, m, dd, int(rest / 3600 / microsPerSecond), int(rest / 60 / microsPerSecond % 60),
		int(rest / microsPerSecond % 60), int(rest % microsPerSecond), zone}
}

// UTCOffset returns the zone offset, or nil for a naive datetime.
func (d *DateTime) UTCOffset() *Timedelta {
	if d.Zone == nil {
		return nil
	}
	t := NewTimedelta(d.Zone.Offset * microsPerSecond)
	return &t
}

func (d *DateTime) utcMicros() int64 {
	m := d.wallMicros()
	if d.Zone != nil {
		m -= d.Zone.Offset * microsPerSecond
	}
	return m
}

func (d *DateTime) equal(o *DateTime) bool {
	if (d.Zone == nil) != (o.Zone == nil) {
		return false
	}
	return d.utcMicros() == o.utcMicros()
}

func (d *DateTime) hashKey() string {
	return strconv.FormatInt(d.utcMicros(), 10)
}

// Compare orders two datetimes; mixing naive and aware raises TypeError.
func (d *DateTime) Compare(o *DateTime) int {
	if (d.Zone == nil) != (o.Zone == nil) {
		Raise("TypeError", "can't compare offset-naive and offset-aware datetimes")
	}
	a, b := d.utcMicros(), o.utcMicros()
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// Add returns d + t.
func (d *DateTime) Add(t Timedelta) *DateTime {
	return dateTimeFromWall(d.wallMicros()+t.micros, d.Zone)
}

// Sub returns d - o.
func (d *DateTime) Sub(o *DateTime) Timedelta {
	if (d.Zone == nil) != (o.Zone == nil) {
		Raise("TypeError", "can't subtract offset-naive and offset-aware datetimes")
	}
	if d.Zone == o.Zone {
		return NewTimedelta(d.wallMicros() - o.wallMicros())
	}
	return NewTimedelta(d.utcMicros() - o.utcMicros())
}

// Weekday returns Monday=0 .. Sunday=6.
func (d *DateTime) Weekday() int {
	return int((ordinal(d.Year, d.Month, d.Day) + 6) % 7)
}

func formatOffset(seconds int64, sep string) string {
	sign := "+"
	if seconds < 0 {
		sign = "-"
		seconds = -seconds
	}
	s := fmt.Sprintf("%s%02d%s%02d", sign, seconds/3600, sep, seconds/60%60)
	if seconds%60 != 0 {
		s += fmt.Sprintf("%s%02d", sep, seconds%60)
	}
	return s
}

// ISOFormat is datetime.isoformat(sep).
func (d *DateTime) ISOFormat(sep string) string {
	s := fmt.Sprintf("%04d-%02d-%02d%s%02d:%02d:%02d", d.Year, d.Month, d.Day, sep, d.Hour, d.Minute, d.Second)
	if d.Microsecond != 0 {
		s += fmt.Sprintf(".%06d", d.Microsecond)
	}
	if d.Zone != nil {
		s += formatOffset(d.Zone.Offset, ":")
	}
	return s
}

func (d *DateTime) String() string { return d.ISOFormat(" ") }

// Repr formats like Python's repr(datetime).
func (d *DateTime) Repr() string {
	fields := []int{d.Year, d.Month, d.Day, d.Hour, d.Minute, d.Second, d.Microsecond}
	if fields[len(fields)-1] == 0 {
		fields = fields[:len(fields)-1]
	}
	if fields[len(fields)-1] == 0 {
		fields = fields[:len(fields)-1]
	}
	parts := make([]string, len(fields))
	for i, f := range fields {
		parts[i] = strconv.Itoa(f)
	}
	s := "datetime.datetime(" + strings.Join(parts, ", ")
	if d.Zone != nil {
		s += ", tzinfo=" + d.Zone.repr()
	}
	return s + ")"
}

// Replace returns a copy with the given fields replaced, validated anew.
func (d *DateTime) Replace(fields map[string]int64, zone *TZ, setZone bool) *DateTime {
	get := func(name string, cur int) int64 {
		if v, ok := fields[name]; ok {
			return v
		}
		return int64(cur)
	}
	z := d.Zone
	if setZone {
		z = zone
	}
	return NewDateTime(get("year", d.Year), get("month", d.Month), get("day", d.Day), get("hour", d.Hour),
		get("minute", d.Minute), get("second", d.Second), get("microsecond", d.Microsecond), z)
}

// FromTimestamp is datetime.fromtimestamp(ts, tz).
func FromTimestamp(ts Value, zone *TZ) *DateTime {
	var whole, micro int64
	switch t := ts.(type) {
	case *big.Int:
		if !t.IsInt64() {
			Raise("OverflowError", "timestamp out of range for platform time_t")
		}
		whole = t.Int64()
	case float64:
		if math.IsNaN(t) {
			Raise("ValueError", "Invalid value NaN (not a number)")
		}
		if math.IsInf(t, 0) || math.Abs(t) > 1e17 {
			Raise("OverflowError", "timestamp out of range for platform time_t")
		}
		w, frac := math.Modf(t)
		us := math.RoundToEven(frac * 1e6)
		whole, micro = int64(w), int64(us)
		if micro >= 1000000 {
			whole++
			micro -= 1000000
		} else if micro < 0 {
			whole--
			micro += 1000000
		}
	}
	epoch := (ordinal(1970, 1, 1) - 1) * microsPerDay
	wall := epoch + whole*microsPerSecond + micro
	if zone != nil {
		wall += zone.Offset * microsPerSecond
	} else {
		_, off := time.Unix(whole, 0).Zone()
		wall += int64(off) * microsPerSecond
	}
	if wall < 0 || wall >= (ordinal(10000, 1, 1)-1)*microsPerDay {
		Raise("ValueError", "year is out of range")
	}
	return dateTimeFromWall(wall, zone)
}

// Now returns the current time in zone (or local naive time for nil).
func Now(zone *TZ) *DateTime {
	now := time.Now()
	return FromTimestamp(float64(now.UnixMicro())/1e6, zone)
}

var (
	weekdayNames = []string{"Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday"}
	monthNames   = []string{"", "January", "February", "March", "April", "May", "June", "July",
		"August", "September", "October", "November", "December"}
)

// Strftime formats with the C-locale strftime directives Python passes
// through, plus Python's own %f, %z and %Z.
func (d *DateTime) Strftime(format string) string {
	var b strings.Builder
	rs := []rune(format)
	for i := 0; i < len(rs); i++ {
		if rs[i] != '%' || i+1 >= len(rs) {
			b.WriteRune(rs[i])
			continue
		}
		i++
		switch rs[i] {
		case 'Y':
			fmt.Fprintf(&b, "%d", d.Year)
		case 'y':
			fmt.Fprintf(&b, "%02d", d.Year%100)
		case 'm':
			fmt.Fprintf(&b, "%02d", d.Month)
		case 'd':
			fmt.Fprintf(&b, "%02d", d.Day)
		case 'e':
			fmt.Fprintf(&b, "%2d", d.Day)
		case 'H':
			fmt.Fprintf(&b, "%02d", d.Hour)
		case 'I':
			h := d.Hour % 12
			if h == 0 {
				h = 12
			}
			fmt.Fprintf(&b, "%02d", h)
		case 'M':
			fmt.Fprintf(&b, "%02d", d.Minute)
		case 'S':
			fmt.Fprintf(&b, "%02d", d.Second)
		case 'f':
			fmt.Fprintf(&b, "%06d", d.Microsecond)
		case 'p':
			if d.Hour < 12 {
				b.WriteString("AM")
			} else {
				b.WriteString("PM")
			}
		case 'a':
			b.WriteString(weekdayNames[d.Weekday()][:3])
		case 'A':
			b.WriteString(weekdayNames[d.Weekday()])
		case 'b', 'h':
			b.WriteString(monthNames[d.Month][:3])
		case 'B':
			b.WriteString(monthNames[d.Month])
		case 'j':
			fmt.Fprintf(&b, "%03d", ordinal(d.Year, d.Month, d.Day)-ordinal(d.Year, 1, 1)+1)
		case 'w':
			fmt.Fprintf(&b, "%d", (d.Weekday()+1)%7)
		case 'u':
			fmt.Fprintf(&b, "%d", d.Weekday()+1)
		case 'z':
			if d.Zone != nil {
				b.WriteString(formatOffset(d.Zone.Offset, ""))
			}
		case 'Z':
			if d.Zone != nil && d.Zone.UTC {
				b.WriteString("UTC")
			}
		case 'D':
			fmt.Fprintf(&b, "%02d/%02d/%02d", d.Month, d.Day, d.Year%100)
		case 'F':
			fmt.Fprintf(&b, "%d-%02d-%02d", d.Year, d.Month, d.Day)
		case 'T':
			fmt.Fprintf(&b, "%02d:%02d:%02d", d.Hour, d.Minute, d.Second)
		case 'R':
			fmt.Fprintf(&b, "%02d:%02d", d.Hour, d.Minute)
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case '%':
			b.WriteByte('%')
		default:
			b.WriteByte('%')
			b.WriteRune(rs[i])
		}
	}
	return b.String()
}
