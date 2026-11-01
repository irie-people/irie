package raphael

import (
	"bytes"
	"context"
	"encoding/xml"
	"os"
	"strconv"
	"strings"
)

// readPlistFile reads a property list file, XML or binary: a binary one is
// turned into XML by plutil(1) first.
func readPlistFile(ctx context.Context, r Runner, path string) (map[string]any, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if !looksXML(b) {
		out, _, err := r.Run(ctx, "/usr/bin/plutil", "-convert", "xml1", "-o", "-", path)
		if err != nil {
			return nil, err
		}
		b = out
	}
	v, err := parsePlist(b)
	if err != nil {
		return nil, err
	}
	m, _ := v.(map[string]any)
	if m == nil {
		m = map[string]any{}
	}
	return m, nil
}

func looksXML(b []byte) bool {
	b = bytes.TrimLeft(b, " \t\r\n\ufeff")
	return bytes.HasPrefix(b, []byte("<?xml")) || bytes.HasPrefix(b, []byte("<plist")) || bytes.HasPrefix(b, []byte("<!DOCTYPE"))
}

// parsePlist decodes an XML property list, as plutil -convert xml1 writes
// one, into Go values: a dict is a map[string]any, an array a []any, a
// string, date or data a string, true and false a bool, an integer an int64
// and a real a float64.
func parsePlist(b []byte) (any, error) {
	d := xml.NewDecoder(bytes.NewReader(b))
	for {
		tok, err := d.Token()
		if err != nil {
			return nil, err
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local != "plist" {
			return plistValue(d, se)
		}
	}
}

func plistValue(d *xml.Decoder, se xml.StartElement) (any, error) {
	switch se.Name.Local {
	case "dict":
		m := map[string]any{}
		key := ""
		for {
			tok, err := d.Token()
			if err != nil {
				return nil, err
			}
			switch t := tok.(type) {
			case xml.StartElement:
				if t.Name.Local == "key" {
					if key, err = plistText(d); err != nil {
						return nil, err
					}
					continue
				}
				v, err := plistValue(d, t)
				if err != nil {
					return nil, err
				}
				m[key] = v
			case xml.EndElement:
				return m, nil
			}
		}
	case "array":
		a := []any{}
		for {
			tok, err := d.Token()
			if err != nil {
				return nil, err
			}
			switch t := tok.(type) {
			case xml.StartElement:
				v, err := plistValue(d, t)
				if err != nil {
					return nil, err
				}
				a = append(a, v)
			case xml.EndElement:
				return a, nil
			}
		}
	case "true", "false":
		return se.Name.Local == "true", d.Skip()
	case "integer":
		s, err := plistText(d)
		if err != nil {
			return nil, err
		}
		n, _ := strconv.ParseInt(strings.TrimSpace(s), 0, 64)
		return n, nil
	case "real":
		s, err := plistText(d)
		if err != nil {
			return nil, err
		}
		f, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
		return f, nil
	case "string", "date", "data":
		return plistText(d)
	}
	return nil, d.Skip()
}

// plistText reads the text of the element just started, up to its end.
func plistText(d *xml.Decoder) (string, error) {
	var b strings.Builder
	for {
		tok, err := d.Token()
		if err != nil {
			return "", err
		}
		switch t := tok.(type) {
		case xml.CharData:
			b.Write(t)
		case xml.StartElement:
			if err := d.Skip(); err != nil {
				return "", err
			}
		case xml.EndElement:
			return b.String(), nil
		}
	}
}

// plistString is m[key] when it is a string.
func plistString(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

// plistBool is m[key] when it is true, or a dict of conditions, as
// KeepAlive may be.
func plistBool(m map[string]any, key string) bool {
	switch v := m[key].(type) {
	case bool:
		return v
	case map[string]any:
		return len(v) > 0
	}
	return false
}

// plistStrings is m[key] when it is an array, its strings only.
func plistStrings(m map[string]any, key string) []string {
	a, _ := m[key].([]any)
	var out []string
	for _, v := range a {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
