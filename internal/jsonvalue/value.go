// Package jsonvalue normalizes bounded JSON without silently accepting duplicate keys.
package jsonvalue

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"unicode/utf8"
)

var errInvalid = errors.New("JSON 无效、键重复或嵌套过深")

// Normalize sorts object keys while retaining JSON number spellings. Callers
// impose byte limits before parsing; depth is independently limited here.
func Normalize(raw []byte) ([]byte, error) {
	if !utf8.Valid(raw) {
		return nil, errInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	value, err := read(decoder, 1)
	if err != nil {
		return nil, errInvalid
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, errInvalid
	}
	return json.Marshal(value)
}

// Object inspects an already normalized object. A nonempty field list enforces
// exact, case-sensitive names rather than encoding/json's case folding.
func Object(raw []byte, fields ...string) (map[string]json.RawMessage, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return nil, errInvalid
	}
	if len(fields) != 0 {
		for key := range object {
			found := false
			for _, field := range fields {
				found = found || key == field
			}
			if !found {
				return nil, errInvalid
			}
		}
	}
	return object, nil
}

func read(decoder *json.Decoder, depth int) (any, error) {
	if depth > 32 {
		return nil, errInvalid
	}
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	delim, composite := token.(json.Delim)
	if !composite {
		return token, nil
	}
	switch delim {
	case '{':
		object := make(map[string]any)
		for decoder.More() {
			key, err := decoder.Token()
			if err != nil {
				return nil, err
			}
			name, ok := key.(string)
			if !ok {
				return nil, errInvalid
			}
			if _, exists := object[name]; exists {
				return nil, errInvalid
			}
			value, err := read(decoder, depth+1)
			if err != nil {
				return nil, err
			}
			object[name] = value
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim('}') {
			return nil, errInvalid
		}
		return object, nil
	case '[':
		array := make([]any, 0)
		for decoder.More() {
			value, err := read(decoder, depth+1)
			if err != nil {
				return nil, err
			}
			array = append(array, value)
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim(']') {
			return nil, errInvalid
		}
		return array, nil
	default:
		return nil, errInvalid
	}
}
