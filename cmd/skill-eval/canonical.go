package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// CanonicalizationName is the frozen canonical-JSON algorithm for every
// structured 048 digest (data-model.md "Canonical serialization").
const CanonicalizationName = "agent-memory-trigger-canonical-json-v1"

// LFNormalizedSHA256 implements `lf-normalized-sha256-v1`: require valid UTF-8
// without NUL, convert CRLF and lone CR to LF, SHA-256 the bytes, emit
// lowercase hex.
func LFNormalizedSHA256(data []byte) (string, error) {
	if !utf8.Valid(data) {
		return "", errors.New("lf-normalized-sha256-v1: input is not valid UTF-8")
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return "", errors.New("lf-normalized-sha256-v1: input contains NUL")
	}
	norm := normalizeLF(data)
	sum := sha256.Sum256(norm)
	return hexLower(sum[:]), nil
}

// LFNormalizedSHA256Bytes is the convenience variant for inputs already known
// to be valid (panics on invalid input — callers validate first).
func LFNormalizedSHA256Bytes(data []byte) string {
	h, err := LFNormalizedSHA256(data)
	if err != nil {
		panic(err)
	}
	return h
}

func normalizeLF(data []byte) []byte {
	if !bytes.ContainsRune(data, '\r') {
		return data
	}
	out := make([]byte, 0, len(data))
	for i := 0; i < len(data); i++ {
		if data[i] == '\r' {
			if i+1 < len(data) && data[i+1] == '\n' {
				continue // CRLF → LF: the LF is appended by the next iteration
			}
			out = append(out, '\n') // lone CR → LF
			continue
		}
		out = append(out, data[i])
	}
	return out
}

func hexLower(b []byte) string {
	const hexdigits = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, v := range b {
		out[i*2] = hexdigits[v>>4]
		out[i*2+1] = hexdigits[v&0x0f]
	}
	return string(out)
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hexLower(sum[:])
}

// CanonicalJSON serializes v per agent-memory-trigger-canonical-json-v1:
// object keys sorted by raw UTF-8 bytes, schema-defined array order kept, no
// insignificant whitespace, strings escaping only quote/backslash/control
// characters (short escapes for \b \t \n \f \r, lowercase \u00xx otherwise),
// shortest-decimal integers, lowercase boolean/null literals.
func CanonicalJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	if err := canonicalWrite(&buf, reflect.ValueOf(v), 0); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// CanonicalSHA256 returns CanonicalJSON's lowercase hex digest.
func CanonicalSHA256(v any) (string, error) {
	b, err := CanonicalJSON(v)
	if err != nil {
		return "", err
	}
	return sha256Hex(b), nil
}

var errFloat = errors.New("canonical-json: floating-point values are not part of the closed schema")

func canonicalWrite(buf *bytes.Buffer, v reflect.Value, depth int) error {
	if depth > 64 {
		return errors.New("canonical-json: nesting too deep")
	}
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			buf.WriteString("null")
			return nil
		}
		v = v.Elem()
	}
	switch v.Kind() {
	case reflect.Bool:
		if v.Bool() {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
	case reflect.String:
		canonicalWriteString(buf, v.String())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		buf.WriteString(strconv.FormatInt(v.Int(), 10))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		buf.WriteString(strconv.FormatUint(v.Uint(), 10))
	case reflect.Float32, reflect.Float64:
		f := v.Float()
		if math.IsNaN(f) || math.IsInf(f, 0) || f != math.Trunc(f) {
			return errFloat
		}
		buf.WriteString(strconv.FormatInt(int64(f), 10))
	case reflect.Slice:
		if v.IsNil() {
			buf.WriteString("null")
			return nil
		}
		buf.WriteByte('[')
		for i := 0; i < v.Len(); i++ {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := canonicalWrite(buf, v.Index(i), depth+1); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
	case reflect.Array:
		buf.WriteByte('[')
		for i := 0; i < v.Len(); i++ {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := canonicalWrite(buf, v.Index(i), depth+1); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
	case reflect.Map:
		if v.IsNil() {
			buf.WriteString("null")
			return nil
		}
		if v.Type().Key().Kind() != reflect.String {
			return errors.New("canonical-json: map keys must be strings")
		}
		keys := make([]string, 0, v.Len())
		for _, k := range v.MapKeys() {
			keys = append(keys, k.String())
		}
		sort.Strings(keys) // byte-wise lexicographic == raw UTF-8 byte order
		buf.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			canonicalWriteString(buf, k)
			buf.WriteByte(':')
			if err := canonicalWrite(buf, v.MapIndex(reflect.ValueOf(k)), depth+1); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
	case reflect.Struct:
		fields := canonicalFields(v.Type())
		buf.WriteByte('{')
		first := true
		for _, f := range fields {
			fv := v.FieldByIndex(f.index)
			if f.omitEmpty && canonicalIsEmpty(fv) {
				continue
			}
			if !first {
				buf.WriteByte(',')
			}
			first = false
			canonicalWriteString(buf, f.name)
			buf.WriteByte(':')
			if err := canonicalWrite(buf, fv, depth+1); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
	default:
		return fmt.Errorf("canonical-json: unsupported kind %s", v.Kind())
	}
	return nil
}

type canonField struct {
	name      string
	index     []int
	omitEmpty bool
}

func canonicalFields(t reflect.Type) []canonField {
	var out []canonField
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		if sf.PkgPath != "" && !sf.Anonymous {
			continue // unexported
		}
		tag := sf.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name := sf.Name
		omit := false
		if tag != "" {
			parts := strings.Split(tag, ",")
			if parts[0] != "" {
				name = parts[0]
			}
			for _, o := range parts[1:] {
				if o == "omitempty" {
					omit = true
				}
			}
		}
		out = append(out, canonField{name: name, index: sf.Index, omitEmpty: omit})
	}
	// Canonical output requires sorted keys regardless of declaration order.
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

func canonicalIsEmpty(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice:
		return v.IsNil()
	case reflect.String:
		return v.Len() == 0
	case reflect.Bool:
		return !v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return v.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return v.Float() == 0
	case reflect.Struct:
		return v.IsZero()
	}
	return false
}

const lowerHex = "0123456789abcdef"

func canonicalWriteString(buf *bytes.Buffer, s string) {
	buf.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			buf.WriteString(`\"`)
		case '\\':
			buf.WriteString(`\\`)
		case '\b':
			buf.WriteString(`\b`)
		case '\t':
			buf.WriteString(`\t`)
		case '\n':
			buf.WriteString(`\n`)
		case '\f':
			buf.WriteString(`\f`)
		case '\r':
			buf.WriteString(`\r`)
		default:
			if r < 0x20 {
				buf.WriteString(`\u00`)
				buf.WriteByte(lowerHex[r>>4])
				buf.WriteByte(lowerHex[r&0xf])
			} else {
				buf.WriteRune(r)
			}
		}
	}
	buf.WriteByte('"')
}

// ---------- strict closed-schema parsing ----------

// StrictParseClosed parses data into target enforcing the closed-schema rules:
// valid UTF-8, no NUL, no duplicate JSON keys at any level, no unknown fields
// (recursively, via DisallowUnknownFields), no trailing input, and no floats
// where the target type is integral (encoding/json rejects those natively).
func StrictParseClosed(data []byte, target any) error {
	if !utf8.Valid(data) {
		return errors.New("closed schema: input is not valid UTF-8")
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return errors.New("closed schema: input contains NUL")
	}
	if err := rejectDuplicateKeys(data); err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		return fmt.Errorf("closed schema: %w", err)
	}
	if dec.More() {
		return errors.New("closed schema: trailing content after JSON value")
	}
	return nil
}

// rejectDuplicateKeys walks the JSON token stream and fails on any object
// containing the same key twice (encoding/json silently keeps the last).
func rejectDuplicateKeys(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()

	var walk func() error
	walk = func() error {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		delim, ok := tok.(json.Delim)
		if !ok {
			return nil // scalar
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return err
				}
				key, ok := kt.(string)
				if !ok {
					return errors.New("closed schema: non-string object key")
				}
				if seen[key] {
					return fmt.Errorf("closed schema: duplicate JSON key %q", key)
				}
				seen[key] = true
				if err := walk(); err != nil {
					return err
				}
			}
			_, err = dec.Token() // closing '}'
			return err
		case '[':
			for dec.More() {
				if err := walk(); err != nil {
					return err
				}
			}
			_, err = dec.Token() // closing ']'
			return err
		default:
			return errors.New("closed schema: unexpected delimiter")
		}
	}
	if err := walk(); err != nil {
		return err
	}
	if _, err := dec.Token(); err == nil {
		return errors.New("closed schema: trailing content after JSON value")
	}
	return nil
}
