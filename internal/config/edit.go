package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// Edit is one change to the config file: Key takes Value, or with a nil
// Value goes back to its default.
type Edit struct {
	Key   string // dotted, such as spawn.default_profile or profiles.claude.model
	Value any    // a string, bool, int, float64, time.Duration, []string or map[string]string
}

// Apply makes the edits to a config file's contents in place, so its
// comments, order and layout stay: a key the file sets takes its new value
// on its own line, a new key goes after the last key of its table, and a new
// table goes at the end. It checks that the result decodes to the file's
// values with exactly the edits made, and refuses the edits otherwise.
func Apply(data []byte, edits []Edit) ([]byte, error) {
	want := map[string]any{}
	if _, err := toml.Decode(string(data), &want); err != nil {
		return nil, err
	}
	out := slices.Clone(data)
	for _, e := range edits {
		key, err := splitKey(e.Key)
		if err != nil {
			return nil, err
		}
		if e.Value == nil {
			if out, err = unset(out, key); err != nil {
				return nil, fmt.Errorf("%s: %w", e.Key, err)
			}
			deleteIn(want, key)
			continue
		}
		v, err := plain(e.Value)
		if err == nil {
			out, err = set(out, key, v)
		}
		if err == nil {
			err = setIn(want, key, v)
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", e.Key, err)
		}
	}
	got := map[string]any{}
	if _, err := toml.Decode(string(out), &got); err != nil || !reflect.DeepEqual(got, want) {
		return nil, errors.New("the file is laid out in a way Irie does not edit in place; make the change by hand")
	}
	return out, nil
}

// ParseValue reads a value for key from JSON, as the type the key takes; a
// duration is a string such as "30s". JSON null, or nothing, gives nil,
// which puts the key back to its default.
func ParseValue(key string, raw []byte) (any, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	path, err := splitKey(key)
	if err != nil {
		return nil, err
	}
	t, err := keyType(path)
	if err != nil {
		return nil, err
	}
	if t == reflect.TypeFor[time.Duration]() {
		var s string
		var d time.Duration
		err := json.Unmarshal(raw, &s)
		if err == nil {
			d, err = time.ParseDuration(s)
		}
		if err != nil {
			return nil, fmt.Errorf("%s takes a duration, such as \"30s\"", key)
		}
		return d, nil
	}
	switch t.Kind() {
	case reflect.String, reflect.Bool, reflect.Int, reflect.Float64:
	case reflect.Slice, reflect.Map:
		if t.Elem().Kind() != reflect.String {
			return nil, fmt.Errorf("%s is set in the file by hand", key)
		}
	default:
		return nil, fmt.Errorf("%s is set in the file by hand", key)
	}
	p := reflect.New(t)
	if err := json.Unmarshal(raw, p.Interface()); err != nil {
		return nil, fmt.Errorf("%s takes %s, not %s", key, kindName(t), raw)
	}
	return p.Elem().Interface(), nil
}

// keyType is the Go type of the config key: a struct field by its toml
// name, or a map's value for any name.
func keyType(key []string) (reflect.Type, error) {
	t := reflect.TypeFor[Config]()
	for i, k := range key {
		for t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		switch t.Kind() {
		case reflect.Struct:
			f, ok := field(t, k)
			if !ok {
				return nil, fmt.Errorf("unknown key %q", strings.Join(key[:i+1], "."))
			}
			t = f.Type
		case reflect.Map:
			t = t.Elem()
		default:
			return nil, fmt.Errorf("%s is not a table", strings.Join(key[:i], "."))
		}
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t, nil
}

func field(t reflect.Type, name string) (reflect.StructField, bool) {
	for i := range t.NumField() {
		if f := t.Field(i); tomlName(f) == name {
			return f, true
		}
	}
	return reflect.StructField{}, false
}

func tomlName(f reflect.StructField) string {
	name, _, _ := strings.Cut(f.Tag.Get("toml"), ",")
	return name
}

func kindName(t reflect.Type) string {
	switch t.Kind() {
	case reflect.Bool:
		return "true or false"
	case reflect.Int:
		return "a whole number"
	case reflect.Float64:
		return "a number"
	case reflect.Slice:
		return "a list of strings"
	case reflect.Map:
		return "a table of strings"
	}
	return "a string"
}

// Tree is the config as a tree of its TOML keys, for JSON: durations are
// strings such as "30s", and a profile has a local table only when it runs
// on a local model.
func Tree(c Config) map[string]any {
	return tree(reflect.ValueOf(c)).(map[string]any)
}

func tree(v reflect.Value) any {
	if d, ok := v.Interface().(time.Duration); ok {
		return shortDuration(d)
	}
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			return nil
		}
		return tree(v.Elem())
	case reflect.Struct:
		out := map[string]any{}
		for i := range v.NumField() {
			if name := tomlName(v.Type().Field(i)); name != "" && name != "-" {
				if x := tree(v.Field(i)); x != nil {
					out[name] = x
				}
			}
		}
		return out
	case reflect.Map:
		out := map[string]any{}
		for _, k := range v.MapKeys() {
			out[k.String()] = tree(v.MapIndex(k))
		}
		return out
	case reflect.Slice:
		out := make([]any, v.Len())
		for i := range out {
			out[i] = tree(v.Index(i))
		}
		return out
	}
	return v.Interface()
}

// shortDuration writes d as Go does, without zero minutes or seconds: 1m,
// not 1m0s, and 24h, not 24h0m0s.
func shortDuration(d time.Duration) string {
	s := d.String()
	if strings.HasSuffix(s, "m0s") {
		s = strings.TrimSuffix(s, "0s")
	}
	if strings.HasSuffix(s, "h0m") {
		s = strings.TrimSuffix(s, "0m")
	}
	return s
}

// plain is a value as TOML decodes it into an interface: int64 for a whole
// number, a duration's string, []any for a list and map[string]any for a
// table.
func plain(v any) (any, error) {
	switch v := v.(type) {
	case string, bool, int64:
		return v, nil
	case int:
		return int64(v), nil
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, fmt.Errorf("%v is not a number a setting takes", v)
		}
		return v, nil
	case time.Duration:
		return shortDuration(v), nil
	case []string:
		out := make([]any, len(v))
		for i, s := range v {
			out[i] = s
		}
		return out, nil
	case map[string]string:
		out := make(map[string]any, len(v))
		for k, s := range v {
			out[k] = s
		}
		return out, nil
	}
	return nil, fmt.Errorf("a %T is not a value Irie writes", v)
}

// literal writes a plain value as TOML.
func literal(v any) (string, error) {
	switch v := v.(type) {
	case string:
		return quote(v), nil
	case bool:
		return strconv.FormatBool(v), nil
	case int64:
		return strconv.FormatInt(v, 10), nil
	case float64:
		s := strconv.FormatFloat(v, 'f', -1, 64)
		if !strings.Contains(s, ".") {
			s += ".0"
		}
		return s, nil
	case []any:
		parts := make([]string, len(v))
		for i, x := range v {
			s, err := literal(x)
			if err != nil {
				return "", err
			}
			parts[i] = s
		}
		return "[" + strings.Join(parts, ", ") + "]", nil
	case map[string]any:
		if len(v) == 0 {
			return "{}", nil
		}
		parts := make([]string, 0, len(v))
		for _, k := range slices.Sorted(maps.Keys(v)) {
			s, err := literal(v[k])
			if err != nil {
				return "", err
			}
			parts = append(parts, keyText([]string{k})+" = "+s)
		}
		return "{ " + strings.Join(parts, ", ") + " }", nil
	}
	return "", fmt.Errorf("a %T is not a value Irie writes", v)
}

// quote writes s as a TOML basic string.
func quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\t':
			b.WriteString(`\t`)
		case '\n':
			b.WriteString(`\n`)
		case '\f':
			b.WriteString(`\f`)
		case '\r':
			b.WriteString(`\r`)
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&b, `\u%04X`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

var bareKey = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// keyText writes a dotted key, quoting the parts that need it.
func keyText(key []string) string {
	parts := make([]string, len(key))
	for i, k := range key {
		parts[i] = k
		if !bareKey.MatchString(k) {
			parts[i] = quote(k)
		}
	}
	return strings.Join(parts, ".")
}

// splitKey reads a dotted key, such as profiles.claude.model or
// dog.land."~/code/web".
func splitKey(s string) ([]string, error) {
	key, i, err := parseKey([]byte(s), 0)
	if err == nil && skipBlank([]byte(s), i) != len(s) {
		err = errors.New("a key has nothing after it")
	}
	if err != nil {
		return nil, fmt.Errorf("key %q: %w", s, err)
	}
	return key, nil
}

func setIn(m map[string]any, key []string, v any) error {
	for i, k := range key[:len(key)-1] {
		next, ok := m[k]
		if !ok {
			next = map[string]any{}
			m[k] = next
		}
		t, ok := next.(map[string]any)
		if !ok {
			return fmt.Errorf("%s is not a table", keyText(key[:i+1]))
		}
		m = t
	}
	m[key[len(key)-1]] = v
	return nil
}

func deleteIn(m map[string]any, key []string) {
	for _, k := range key[:len(key)-1] {
		t, ok := m[k].(map[string]any)
		if !ok {
			return
		}
		m = t
	}
	delete(m, key[len(key)-1])
}

// set gives key the plain value v in data.
func set(data []byte, key []string, v any) ([]byte, error) {
	lit, err := literal(v)
	if err != nil {
		return nil, err
	}
	entries, err := scan(data)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.pair() && slices.Equal(e.key, key) {
			return splice(data, e.vstart, e.vend, lit), nil
		}
	}
	if e, ok := inlineOver(data, entries, key); ok {
		return editInline(data, e, func(t map[string]any) error { return setIn(t, key[len(e.key):], v) })
	}
	parent := key[:len(key)-1]
	// After the last key of its table, written as that key is: under the
	// table's header, or dotted from a table above.
	last := -1
	for i, e := range entries {
		if e.pair() && slices.Equal(e.key[:len(e.key)-1], parent) {
			last = i
		}
	}
	if last >= 0 {
		e := entries[last]
		return insert(data, e.end, keyText(key[len(e.table):])+" = "+lit+"\n"), nil
	}
	// Under its table's header, after whatever else the table holds.
	for i, e := range entries {
		if e.header && !e.array && slices.Equal(e.key, parent) {
			at := e.end
			for _, f := range entries[i+1:] {
				if f.header {
					break
				}
				at = f.end
			}
			return insert(data, at, keyText(key[len(parent):])+" = "+lit+"\n"), nil
		}
	}
	if len(parent) == 0 {
		return nil, errors.New("a setting is in a table, such as spawn.default_profile")
	}
	table := "[" + keyText(parent) + "]\n" + keyText(key[len(parent):]) + " = " + lit + "\n"
	if len(bytes.TrimSpace(data)) > 0 {
		table = "\n" + table
	}
	return insert(data, len(data), table), nil
}

// unset takes key out of data, so its default applies.
func unset(data []byte, key []string) ([]byte, error) {
	entries, err := scan(data)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.pair() && slices.Equal(e.key, key) {
			return splice(data, e.start, e.end, ""), nil
		}
	}
	if e, ok := inlineOver(data, entries, key); ok {
		return editInline(data, e, func(t map[string]any) error {
			deleteIn(t, key[len(e.key):])
			return nil
		})
	}
	return data, nil
}

// inlineOver finds the inline table key is in, such as dog.land's in
// `land = { irie = "live" }`.
func inlineOver(data []byte, entries []entry, key []string) (entry, bool) {
	for _, e := range entries {
		if e.pair() && len(e.key) < len(key) && slices.Equal(e.key, key[:len(e.key)]) && data[e.vstart] == '{' {
			return e, true
		}
	}
	return entry{}, false
}

// editInline writes e's inline table again after change.
func editInline(data []byte, e entry, change func(map[string]any) error) ([]byte, error) {
	var doc struct {
		V map[string]any `toml:"v"`
	}
	if _, err := toml.Decode("v = "+string(data[e.vstart:e.vend]), &doc); err != nil {
		return nil, err
	}
	if err := change(doc.V); err != nil {
		return nil, err
	}
	lit, err := literal(doc.V)
	if err != nil {
		return nil, err
	}
	return splice(data, e.vstart, e.vend, lit), nil
}

func splice(data []byte, from, to int, s string) []byte {
	return slices.Concat(data[:from], []byte(s), data[to:])
}

// insert puts the line s at offset at, which starts a line, or ends the
// file.
func insert(data []byte, at int, s string) []byte {
	if at > 0 && data[at-1] != '\n' {
		s = "\n" + s
	}
	return splice(data, at, at, s)
}

// entry is a key-value pair or a table header of a TOML file, and where it
// lies.
type entry struct {
	table      []string // the table it is in; a header's own
	key        []string // the full key, from the root
	header     bool
	array      bool // a [[table]] header, or a pair under one
	start, end int  // its line or lines, with the last newline
	vstart     int  // a pair's value
	vend       int
}

// pair reports whether e is a key-value pair Irie may edit.
func (e entry) pair() bool { return !e.header && !e.array }

// scan finds the pairs and table headers of a TOML file, reading only as
// much of the syntax as tells where each starts and ends.
func scan(data []byte) ([]entry, error) {
	var out []entry
	var table []string
	array := false
	for i := 0; i < len(data); {
		start := i
		i = skipBlank(data, i)
		if i == len(data) {
			break
		}
		switch data[i] {
		case '\r', '\n', '#':
			i = lineEnd(data, i)
			continue
		case '[':
			e := entry{header: true, start: start}
			j, closing := i+1, "]"
			if j < len(data) && data[j] == '[' {
				e.array, j, closing = true, j+1, "]]"
			}
			key, j, err := parseKey(data, j)
			if err != nil {
				return nil, err
			}
			j = skipBlank(data, j)
			if !bytes.HasPrefix(data[j:], []byte(closing)) {
				return nil, errAt(data, j, "a table's name ends with "+closing)
			}
			if e.end, err = endOfLine(data, j+len(closing)); err != nil {
				return nil, err
			}
			e.table, e.key = key, key
			table, array = key, e.array
			out = append(out, e)
			i = e.end
		default:
			key, j, err := parseKey(data, i)
			if err != nil {
				return nil, err
			}
			if j = skipBlank(data, j); j == len(data) || data[j] != '=' {
				return nil, errAt(data, j, "a key is followed by =")
			}
			e := entry{table: table, key: slices.Concat(table, key), array: array, start: start, vstart: skipBlank(data, j+1)}
			if e.vend, err = scanValue(data, e.vstart); err != nil {
				return nil, err
			}
			if e.end, err = endOfLine(data, e.vend); err != nil {
				return nil, err
			}
			out = append(out, e)
			i = e.end
		}
	}
	return out, nil
}

// parseKey reads a dotted key at i: bare, "quoted" or 'literal' parts.
func parseKey(data []byte, i int) ([]string, int, error) {
	var key []string
	for {
		i = skipBlank(data, i)
		if i == len(data) {
			return nil, i, errAt(data, i, "a key is missing")
		}
		switch data[i] {
		case '"':
			end, err := scanString(data, i)
			if err != nil {
				return nil, i, err
			}
			k, err := strconv.Unquote(string(data[i:end]))
			if err != nil {
				return nil, i, errAt(data, i, "a quoted key Irie cannot read")
			}
			key, i = append(key, k), end
		case '\'':
			end, err := scanString(data, i)
			if err != nil {
				return nil, i, err
			}
			key, i = append(key, string(data[i+1:end-1])), end
		default:
			j := i
			for j < len(data) && (isAlnum(data[j]) || data[j] == '_' || data[j] == '-') {
				j++
			}
			if j == i {
				return nil, i, errAt(data, i, "a key is missing")
			}
			key, i = append(key, string(data[i:j])), j
		}
		if j := skipBlank(data, i); j < len(data) && data[j] == '.' {
			i = j + 1
			continue
		}
		return key, i, nil
	}
}

var datePart = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// scanValue is just past the value at i.
func scanValue(data []byte, i int) (int, error) {
	if i == len(data) {
		return 0, errAt(data, i, "a value is missing")
	}
	switch {
	case bytes.HasPrefix(data[i:], []byte(`"""`)), bytes.HasPrefix(data[i:], []byte(`'''`)):
		return scanLong(data, i)
	case data[i] == '"' || data[i] == '\'':
		return scanString(data, i)
	case data[i] == '[' || data[i] == '{':
		closing, table := byte(']'), data[i] == '{'
		if table {
			closing = '}'
		}
		j := i + 1
		for {
			if j = skipSpace(data, j); j < len(data) && data[j] == closing {
				return j + 1, nil
			}
			if table {
				_, k, err := parseKey(data, j)
				if err != nil {
					return 0, err
				}
				if k = skipBlank(data, k); k == len(data) || data[k] != '=' {
					return 0, errAt(data, k, "a key is followed by =")
				}
				j = skipBlank(data, k+1)
			}
			var err error
			if j, err = scanValue(data, j); err != nil {
				return 0, err
			}
			switch j = skipSpace(data, j); {
			case j < len(data) && data[j] == ',':
				j++
			case j < len(data) && data[j] == closing:
				return j + 1, nil
			default:
				return 0, errAt(data, j, fmt.Sprintf("a list or table goes on with , or ends with %c", closing))
			}
		}
	}
	// A number, a bool or a date and time, which may be apart by a space.
	end := func(j int) int {
		for j < len(data) && !bytes.ContainsRune([]byte(" \t\r\n#,]}"), rune(data[j])) {
			j++
		}
		return j
	}
	j := end(i)
	if j+1 < len(data) && data[j] == ' ' && isDigit(data[j+1]) && datePart.Match(data[i:j]) {
		j = end(j + 1)
	}
	if j == i {
		return 0, errAt(data, i, "a value is missing")
	}
	return j, nil
}

// scanString is just past the one-line string at i.
func scanString(data []byte, i int) (int, error) {
	q := data[i]
	for j := i + 1; j < len(data) && data[j] != '\n'; j++ {
		switch data[j] {
		case '\\':
			if q == '"' {
				j++
			}
		case q:
			return j + 1, nil
		}
	}
	return 0, errAt(data, i, "a string is not closed on its line")
}

// scanLong is just past the multi-line string at i.
func scanLong(data []byte, i int) (int, error) {
	q := data[i]
	for j := i + 3; j < len(data); j++ {
		if data[j] == '\\' && q == '"' {
			j++
			continue
		}
		if bytes.HasPrefix(data[j:], []byte{q, q, q}) {
			end := j + 3
			// One or two more quotes are the string's own.
			for n := 0; n < 2 && end < len(data) && data[end] == q; n++ {
				end++
			}
			return end, nil
		}
	}
	return 0, errAt(data, i, "a string is not closed")
}

// endOfLine is just past the newline after a value or a header, with any
// comment before it.
func endOfLine(data []byte, i int) (int, error) {
	i = skipBlank(data, i)
	if i < len(data) && data[i] != '#' && data[i] != '\n' && data[i] != '\r' {
		return 0, errAt(data, i, "the line goes on after its value")
	}
	return lineEnd(data, i), nil
}

// lineEnd is just past the newline that ends the line i is in.
func lineEnd(data []byte, i int) int {
	if n := bytes.IndexByte(data[i:], '\n'); n >= 0 {
		return i + n + 1
	}
	return len(data)
}

func skipBlank(data []byte, i int) int {
	for i < len(data) && (data[i] == ' ' || data[i] == '\t') {
		i++
	}
	return i
}

// skipSpace skips blanks, newlines and comments, as between a list's
// items.
func skipSpace(data []byte, i int) int {
	for i < len(data) {
		switch data[i] {
		case ' ', '\t', '\r', '\n':
			i++
		case '#':
			i = lineEnd(data, i)
		default:
			return i
		}
	}
	return i
}

func isDigit(b byte) bool { return '0' <= b && b <= '9' }
func isAlnum(b byte) bool { return isDigit(b) || 'a' <= b && b <= 'z' || 'A' <= b && b <= 'Z' }

func errAt(data []byte, i int, msg string) error {
	return fmt.Errorf("line %d: %s", 1+bytes.Count(data[:min(i, len(data))], []byte("\n")), msg)
}
