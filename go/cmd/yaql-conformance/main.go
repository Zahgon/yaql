// Command yaql-conformance evaluates yaql expressions read as JSON lines on
// stdin and writes one typed JSON result per line on stdout.
//
// Input:  {"id": "...", "expr": "...", "data": <json>, "engine": {"delegates": bool, "legacy": bool}}
// Output: {"id": "...", "result": <typed>} or {"id": "...", "error": {"class": "...", "message": "..."}}
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"os"
	"strings"

	"yaql"
	"yaql/language"
)

type engineKey struct{ delegates, legacy bool }

type runtime struct {
	engine *yaql.Engine
	ctx    yaql.Context
}

var runtimes = map[engineKey]*runtime{}

func runtimeFor(k engineKey) *runtime {
	if rt, ok := runtimes[k]; ok {
		return rt
	}
	var rt runtime
	opts := yaql.ContextOptions{Delegates: k.delegates}
	if k.legacy {
		rt.engine = yaql.NewLegacyFactory(k.delegates).Create(nil)
		rt.ctx = yaql.CreateLegacyContext(opts, true)
	} else {
		rt.engine = yaql.NewFactory(k.delegates).Create(nil)
		rt.ctx = yaql.CreateContext(opts)
	}
	runtimes[k] = &rt
	return &rt
}

// decodeValue reads one JSON value keeping object key order, integer
// precision and the integer/float distinction.
func decodeValue(dec *json.Decoder) (language.Value, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			d := language.NewDict(false)
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return nil, err
				}
				v, err := decodeValue(dec)
				if err != nil {
					return nil, err
				}
				d.Set(kt.(string), v)
			}
			_, err := dec.Token()
			return d, err
		case '[':
			items := []language.Value{}
			for dec.More() {
				v, err := decodeValue(dec)
				if err != nil {
					return nil, err
				}
				items = append(items, v)
			}
			_, err := dec.Token()
			return &language.List{Items: items}, err
		}
	case json.Number:
		s := string(t)
		if strings.ContainsAny(s, ".eE") {
			f, err := t.Float64()
			return f, err
		}
		v, ok := new(big.Int).SetString(s, 10)
		if !ok {
			return nil, fmt.Errorf("bad integer %q", s)
		}
		return v, nil
	case string, bool, nil:
		return t, nil
	}
	return nil, fmt.Errorf("unexpected token %v", tok)
}

type request struct {
	id      string
	expr    string
	data    language.Value
	hasData bool
	key     engineKey
}

func parseRequest(line []byte) (*request, error) {
	dec := json.NewDecoder(bytes.NewReader(line))
	dec.UseNumber()
	v, err := decodeValue(dec)
	if err != nil {
		return nil, err
	}
	obj, ok := v.(*language.Dict)
	if !ok {
		return nil, fmt.Errorf("request is not an object")
	}
	req := &request{}
	if id, ok := obj.Get("id"); ok {
		req.id, _ = id.(string)
	}
	if e, ok := obj.Get("expr"); ok {
		req.expr, _ = e.(string)
	}
	req.data, req.hasData = obj.Get("data")
	if eng, ok := obj.Get("engine"); ok {
		if ed, ok := eng.(*language.Dict); ok {
			if v, ok := ed.Get("delegates"); ok {
				req.key.delegates = v == true
			}
			if v, ok := ed.Get("legacy"); ok {
				req.key.legacy = v == true
			}
		}
	}
	return req, nil
}

func writeString(b *bytes.Buffer, s string) {
	// Marshalling a string cannot fail.
	enc, _ := json.Marshal(s)
	b.Write(enc)
}

func encode(b *bytes.Buffer, v language.Value) {
	switch t := v.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		if t {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case *big.Int:
		b.WriteString(t.String())
	case float64:
		b.WriteString(`{"float": `)
		writeString(b, language.FloatRepr(t))
		b.WriteString("}")
	case string:
		writeString(b, t)
	case language.Tuple:
		encodeList(b, t)
	case *language.List:
		encodeList(b, t.Items)
	case *language.Dict:
		b.WriteString(`{"dict": [`)
		first := true
		t.Items(func(k, val language.Value) {
			if !first {
				b.WriteString(", ")
			}
			first = false
			b.WriteString("[")
			encode(b, k)
			b.WriteString(", ")
			encode(b, val)
			b.WriteString("]")
		})
		b.WriteString("]}")
	case *language.OutputSet:
		b.WriteString(`{"set": `)
		encodeList(b, t.Items)
		b.WriteString("}")
	case *language.Set:
		b.WriteString(`{"set": `)
		encodeList(b, t.Items())
		b.WriteString("}")
	case *language.DateTime:
		b.WriteString(`{"datetime": `)
		writeString(b, t.ISOFormat("T"))
		b.WriteString("}")
	case language.Timedelta:
		fmt.Fprintf(b, `{"timespan": %d}`, t.Micros())
	case *language.Regex:
		b.WriteString(`{"regex": [`)
		writeString(b, t.Pattern)
		fmt.Fprintf(b, ", %d]}", t.Flags)
	default:
		b.WriteString(`{"object": `)
		writeString(b, language.TypeName(v))
		b.WriteString("}")
	}
}

func encodeList(b *bytes.Buffer, items []language.Value) {
	b.WriteString("[")
	for i, x := range items {
		if i > 0 {
			b.WriteString(", ")
		}
		encode(b, x)
	}
	b.WriteString("]")
}

func evaluate(req *request) (result language.Value, failure *language.Error) {
	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(*language.Error); ok {
				failure = e
				return
			}
			failure = &language.Error{Class: "InternalError", Message: fmt.Sprint(r)}
		}
	}()
	rt := runtimeFor(req.key)
	data := language.NoValue
	if req.hasData {
		data = req.data
	}
	result, failure = yaql.Evaluate(rt.engine, req.expr, data, rt.ctx)
	return result, failure
}

func writeFailure(b *bytes.Buffer, failure *language.Error) {
	b.WriteString(`, "error": {"class": `)
	writeString(b, failure.Class)
	b.WriteString(`, "message": `)
	writeString(b, failure.Message)
	b.WriteString("}}\n")
}

func process(line []byte, out *bufio.Writer) {
	var b bytes.Buffer
	req, err := parseRequest(line)
	if err != nil {
		b.WriteString(`{"id": null, "error": {"class": "ProtocolError", "message": `)
		writeString(&b, err.Error())
		b.WriteString("}}\n")
		out.Write(b.Bytes())
		return
	}
	// Only encode below reads this request's iterators, so closing the scope on
	// return releases every generator the expression left suspended, on the
	// success and error paths alike.
	scope := language.OpenGeneratorScope()
	defer scope.Close()
	result, failure := evaluate(req)
	b.WriteString(`{"id": `)
	writeString(&b, req.id)
	if failure != nil {
		writeFailure(&b, failure)
	} else {
		var enc bytes.Buffer
		if e := language.Catch(func() { encode(&enc, result) }); e != nil {
			writeFailure(&b, e)
		} else {
			b.WriteString(`, "result": `)
			b.Write(enc.Bytes())
			b.WriteString("}\n")
		}
	}
	out.Write(b.Bytes())
}

func main() {
	in := bufio.NewReaderSize(os.Stdin, 1<<20)
	out := bufio.NewWriter(os.Stdout)
	for {
		line, err := in.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			process(line, out)
			// Write errors stick to the buffered writer and surface here.
			if err := out.Flush(); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
		}
		if err == io.EOF {
			return
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
}
