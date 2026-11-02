package stdlib

import (
	"math/big"
	"time"

	. "yaql/language"
)

var zeroTimespan = NewTimedelta(0)

func getTZ(offset Value) *TZ {
	if offset == nil {
		return nil
	}
	td := offset.(Timedelta)
	if td.Micros() == 0 {
		return UTCZone
	}
	return &TZ{Offset: int64(float64(td.Micros()) / 1e6)}
}

func asDT(v Value) *DateTime { return v.(*DateTime) }
func asTD(v Value) Timedelta { return v.(Timedelta) }
func int64Of(v Value) int64 {
	b := bigInt(v)
	if !b.IsInt64() {
		Raise("OverflowError", "Python int too large to convert to C int")
	}
	return b.Int64()
}

func timespanTimesNumber(ts Timedelta, n Value) Value {
	if f, ok := n.(float64); ok {
		return TimedeltaFromFloat(float64(ts.Micros()) * f)
	}
	return TimedeltaFromBig(new(big.Int).Mul(big.NewInt(ts.Micros()), bigInt(n)))
}

func dtProperty(name string, t SmartType, fn func(Value) Value) *FunctionDefinition {
	return Define(name, sig("obj"), func(a args, _ kwargs) Value { return fn(a[0]) },
		Name("#property#"+name), Param("obj", t))
}

func utcOf(dt *DateTime) *DateTime {
	off := dt.UTCOffset()
	if off == nil {
		Raise("TypeError", "unsupported operand type(s) for -: 'datetime.datetime' and 'NoneType'")
	}
	return dt.Add(NewTimedelta(-off.Micros()))
}

func dateOf(dt *DateTime) *DateTime {
	return NewDateTime(int64(dt.Year), int64(dt.Month), int64(dt.Day), 0, 0, 0, 0, dt.Zone)
}

// RegisterDateTime registers the date and time functions.
func RegisterDateTime(ctx Context) {
	tdType := NewPythonType(PyTimedelta, false)
	dtType := NewPythonType(PyDateTime, false)
	defs := []*FunctionDefinition{
		Define("build_datetime", withDefaults(sig("year", "month", "day", "hour", "minute", "second", "microsecond", "offset"),
			"hour", Int(0), "minute", Int(0), "second", Int(0), "microsecond", Int(0), "offset", zeroTimespan),
			func(a args, _ kwargs) Value {
				return NewDateTime(int64Of(a[0]), int64Of(a[1]), int64Of(a[2]), int64Of(a[3]), int64Of(a[4]), int64Of(a[5]),
					int64Of(a[6]), getTZ(a[7]))
			}, Name("datetime"), ParamPy("year", PyInt), ParamPy("month", PyInt), ParamPy("day", PyInt), ParamPy("hour", PyInt),
			ParamPy("minute", PyInt), ParamPy("second", PyInt), ParamPy("microsecond", PyInt), ParamPy("offset", PyTimedelta)),
		Define("datetime_from_timestamp", withDefaults(sig("timestamp", "offset"), "offset", zeroTimespan), func(a args, _ kwargs) Value {
			return FromTimestamp(a[0], getTZ(a[1]))
		}, Name("datetime"), Param("timestamp", Number(false)), ParamPy("offset", PyTimedelta)),
		Define("datetime_from_string", withDefaults(sig("string", "format__"), "format__", nil), func(a args, _ kwargs) Value {
			var result *DateTime
			if !Truthy(a[1]) {
				result = parseDateString(a[0].(string))
			} else {
				result = strptime(a[0].(string), a[1].(string))
			}
			if result.Zone == nil {
				c := *result
				c.Zone = UTCZone
				return &c
			}
			return result
		}, Name("datetime"), Param("string", String(false)), Param("format__", String(true))),
		Define("build_timespan", withDefaults(sig("days", "hours", "minutes", "seconds", "milliseconds", "microseconds"),
			"days", Int(0), "hours", Int(0), "minutes", Int(0), "seconds", Int(0), "milliseconds", Int(0), "microseconds", Int(0)),
			func(a args, _ kwargs) Value {
				total := new(big.Int)
				mul := []int64{86400000000, 3600000000, 60000000, 1000000, 1000, 1}
				for i, m := range mul {
					total.Add(total, new(big.Int).Mul(bigInt(a[i]), big.NewInt(m)))
				}
				return TimedeltaFromBig(total)
			}, Name("timespan"), ParamPy("days", PyInt), ParamPy("hours", PyInt), ParamPy("minutes", PyInt),
			Param("seconds", Integer(false)), Param("milliseconds", Integer(false)), Param("microseconds", Integer(false))),
		Define("now", withDefaults(sig("offset"), "offset", zeroTimespan), func(a args, _ kwargs) Value {
			return Now(getTZ(a[0]))
		}),
		Define("localtz", sig(), func(args, kwargs) Value {
			_, off := time.Now().Zone()
			return NewTimedelta(int64(off) * 1000000)
		}),
		Define("utctz", sig(), func(args, kwargs) Value { return zeroTimespan }),
		dtProperty("utc", DateTimeArg(false), func(v Value) Value { return utcOf(asDT(v)) }),
		dtProperty("days", tdType, func(v Value) Value { return float64(asTD(v).Micros()) / 86400000000.0 }),
		dtProperty("hours", tdType, func(v Value) Value { return float64(asTD(v).Micros()) / 3600000000.0 }),
		dtProperty("minutes", tdType, func(v Value) Value { return float64(asTD(v).Micros()) / 60000000.0 }),
		dtProperty("seconds", tdType, func(v Value) Value { return float64(asTD(v).Micros()) / 1000000.0 }),
		dtProperty("milliseconds", tdType, func(v Value) Value { return float64(asTD(v).Micros()) / 1000.0 }),
		dtProperty("microseconds", tdType, func(v Value) Value { return Int(asTD(v).Micros()) }),
		Define("datetime_plus_timespan", sig("dt", "ts"), func(a args, _ kwargs) Value {
			return asDT(a[0]).Add(asTD(a[1]))
		}, Name("#operator_+"), Param("dt", DateTimeArg(false)), ParamPy("ts", PyTimedelta)),
		Define("timespan_plus_datetime", sig("ts", "dt"), func(a args, _ kwargs) Value {
			return asDT(a[1]).Add(asTD(a[0]))
		}, Name("#operator_+"), ParamPy("ts", PyTimedelta), Param("dt", DateTimeArg(false))),
		Define("datetime_minus_timespan", sig("dt", "ts"), func(a args, _ kwargs) Value {
			return asDT(a[0]).Add(NewTimedelta(-asTD(a[1]).Micros()))
		}, Name("#operator_-"), Param("dt", DateTimeArg(false)), ParamPy("ts", PyTimedelta)),
		Define("datetime_minus_datetime", sig("dt1", "dt2"), func(a args, _ kwargs) Value {
			return asDT(a[0]).Sub(asDT(a[1]))
		}, Name("#operator_-"), Param("dt1", DateTimeArg(false)), Param("dt2", DateTimeArg(false))),
		Define("timespan_plus_timespan", sig("ts1", "ts2"), func(a args, _ kwargs) Value {
			return TimedeltaFromBig(new(big.Int).Add(big.NewInt(asTD(a[0]).Micros()), big.NewInt(asTD(a[1]).Micros())))
		}, Name("#operator_+"), ParamPy("ts1", PyTimedelta), ParamPy("ts2", PyTimedelta)),
		Define("timespan_minus_timespan", sig("ts1", "ts2"), func(a args, _ kwargs) Value {
			return TimedeltaFromBig(new(big.Int).Sub(big.NewInt(asTD(a[0]).Micros()), big.NewInt(asTD(a[1]).Micros())))
		}, Name("#operator_-"), ParamPy("ts1", PyTimedelta), ParamPy("ts2", PyTimedelta)),
	}
	dtCmp := func(name, op string, accept func(int) bool) *FunctionDefinition {
		return Define(name, sig("dt1", "dt2"), func(a args, _ kwargs) Value {
			return accept(asDT(a[0]).Compare(asDT(a[1])))
		}, Name("#operator_"+op), Param("dt1", DateTimeArg(false)), Param("dt2", DateTimeArg(false)))
	}
	tsCmp := func(name, op string, accept func(int) bool) *FunctionDefinition {
		return Define(name, sig("ts1", "ts2"), func(a args, _ kwargs) Value {
			x, y := asTD(a[0]).Micros(), asTD(a[1]).Micros()
			c := 0
			if x < y {
				c = -1
			} else if x > y {
				c = 1
			}
			return accept(c)
		}, Name("#operator_"+op), ParamPy("ts1", PyTimedelta), ParamPy("ts2", PyTimedelta))
	}
	gt := func(c int) bool { return c > 0 }
	ge := func(c int) bool { return c >= 0 }
	lt := func(c int) bool { return c < 0 }
	le := func(c int) bool { return c <= 0 }
	defs = append(defs,
		dtCmp("datetime_gt_datetime", ">", gt), dtCmp("datetime_gte_datetime", ">=", ge),
		dtCmp("datetime_lt_datetime", "<", lt), dtCmp("datetime_lte_datetime", "<=", le),
		tsCmp("timespan_gt_timespan", ">", gt), tsCmp("timespan_gte_timespan", ">=", ge),
		tsCmp("timespan_lt_timespan", "<", lt), tsCmp("timespan_lte_timespan", "<=", le),
		Define("negative_timespan", sig("ts"), func(a args, _ kwargs) Value {
			return NewTimedelta(-asTD(a[0]).Micros())
		}, Name("#unary_operator_-"), ParamPy("ts", PyTimedelta)),
		Define("positive_timespan", sig("ts"), func(a args, _ kwargs) Value { return a[0] },
			Name("#unary_operator_+"), ParamPy("ts", PyTimedelta)),
		Define("timespan_by_num", sig("ts", "n"), func(a args, _ kwargs) Value {
			return timespanTimesNumber(asTD(a[0]), a[1])
		}, Name("#operator_*"), ParamPy("ts", PyTimedelta), Param("n", Number(false))),
		Define("num_by_timespan", sig("n", "ts"), func(a args, _ kwargs) Value {
			return timespanTimesNumber(asTD(a[1]), a[0])
		}, Name("#operator_*"), Param("n", Number(false)), ParamPy("ts", PyTimedelta)),
		Define("div_timespans", sig("ts1", "ts2"), func(a args, _ kwargs) Value {
			y := asTD(a[1]).Micros()
			if y == 0 {
				Raise("ZeroDivisionError", "float division by zero")
			}
			return float64(asTD(a[0]).Micros()) / float64(y)
		}, Name("#operator_/"), ParamPy("ts1", PyTimedelta), ParamPy("ts2", PyTimedelta)),
		Define("div_timespan_by_num", sig("ts", "n"), func(a args, _ kwargs) Value {
			us := asTD(a[0]).Micros()
			if f, ok := a[1].(float64); ok {
				if f == 0 {
					Raise("ZeroDivisionError", "float division by zero")
				}
				return TimedeltaFromFloat(float64(us) / f)
			}
			n := bigInt(a[1])
			if n.Sign() == 0 {
				Raise("ZeroDivisionError", "division by zero")
			}
			q, _ := new(big.Rat).SetFrac(big.NewInt(us), n).Float64()
			return TimedeltaFromFloat(q)
		}, Name("#operator_/"), ParamPy("ts", PyTimedelta), Param("n", Number(false))),
		dtProperty("year", dtType, func(v Value) Value { return Int(int64(asDT(v).Year)) }),
		dtProperty("month", dtType, func(v Value) Value { return Int(int64(asDT(v).Month)) }),
		dtProperty("day", dtType, func(v Value) Value { return Int(int64(asDT(v).Day)) }),
		dtProperty("hour", dtType, func(v Value) Value { return Int(int64(asDT(v).Hour)) }),
		dtProperty("minute", dtType, func(v Value) Value { return Int(int64(asDT(v).Minute)) }),
		dtProperty("second", dtType, func(v Value) Value { return Int(int64(asDT(v).Second)) }),
		dtProperty("microsecond", dtType, func(v Value) Value { return Int(int64(asDT(v).Microsecond)) }),
		dtProperty("weekday", dtType, func(v Value) Value { return Int(int64(asDT(v).Weekday())) }),
		dtProperty("offset", dtType, func(v Value) Value {
			if off := asDT(v).UTCOffset(); off != nil && off.Micros() != 0 {
				return *off
			}
			return zeroTimespan
		}),
		dtProperty("timestamp", dtType, func(v Value) Value {
			epoch := NewDateTime(1970, 1, 1, 0, 0, 0, 0, UTCZone)
			return utcOf(asDT(v)).Sub(epoch).TotalSeconds()
		}),
		dtProperty("date", DateTimeArg(false), func(v Value) Value { return dateOf(asDT(v)) }),
		dtProperty("time", DateTimeArg(false), func(v Value) Value {
			dt := asDT(v)
			return dt.Sub(dateOf(dt))
		}),
		Define("replace", withDefaults(sig("dt", "year", "month", "day", "hour", "minute", "second", "microsecond", "offset"),
			"year", nil, "month", nil, "day", nil, "hour", nil, "minute", nil, "second", nil, "microsecond", nil, "offset", nil),
			func(a args, _ kwargs) Value {
				fields := map[string]int64{}
				names := []string{"year", "month", "day", "hour", "minute", "second", "microsecond"}
				for i, n := range names {
					if a[i+1] != nil {
						fields[n] = int64Of(a[i+1])
					}
				}
				var zone *TZ
				setZone := a[8] != nil
				if setZone {
					zone = getTZ(a[8])
				}
				return asDT(a[0]).Replace(fields, zone, setZone)
			}, Method, Param("dt", DateTimeArg(false)), ParamPy("year", PyInt), ParamPy("month", PyInt), ParamPy("day", PyInt),
			ParamPy("hour", PyInt), ParamPy("minute", PyInt), ParamPy("second", PyInt), ParamPy("microsecond", PyInt),
			ParamPy("offset", PyTimedelta)),
		Define("format_", sig("dt", "format__"), func(a args, _ kwargs) Value {
			return asDT(a[0]).Strftime(a[1].(string))
		}, Method, Param("dt", DateTimeArg(false)), Param("format__", String(false))),
		Define("is_datetime", sig("value"), func(a args, _ kwargs) Value { _, ok := a[0].(*DateTime); return ok }),
		Define("is_timespan", sig("value"), func(a args, _ kwargs) Value { _, ok := a[0].(Timedelta); return ok }),
	)
	for _, fd := range defs {
		register(ctx, fd)
	}
}
