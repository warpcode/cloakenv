package utils

import (
	"bytes"
	"fmt"
	"io"
	"strconv"
	"sync"

	goyaml "gopkg.in/yaml.v3"
)

var yamlBufPool = sync.Pool{
	New: func() any {
		return new(bytes.Buffer)
	},
}

// UnmarshalYAML wraps yaml.Unmarshal with centralized error handling.
func UnmarshalYAML(in []byte, out any) error {
	if err := goyaml.Unmarshal(in, out); err != nil {
		return fmt.Errorf("yaml unmarshal failed: %w", err)
	}
	return nil
}

// MarshalYAML wraps yaml.Marshal with centralized error handling and buffer pooling.
func MarshalYAML(in any) ([]byte, error) {
	buf := yamlBufPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer yamlBufPool.Put(buf)

	enc := goyaml.NewEncoder(buf)
	enc.SetIndent(2)
	if err := enc.Encode(in); err != nil {
		if closeErr := enc.Close(); closeErr != nil {
			return nil, fmt.Errorf("yaml marshal failed: %w (close error: %w)", err, closeErr)
		}
		return nil, fmt.Errorf("yaml marshal failed: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("yaml marshal failed: %w", err)
	}

	res := make([]byte, buf.Len())
	copy(res, buf.Bytes())
	return res, nil
}

// MarshalYAMLString encodes the value as YAML into a string, trimming any trailing
// newlines, using a pooled buffer to reduce allocations.
func MarshalYAMLString(in any) (string, error) {
	buf := yamlBufPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer yamlBufPool.Put(buf)

	enc := goyaml.NewEncoder(buf)
	enc.SetIndent(2)
	if err := enc.Encode(in); err != nil {
		if closeErr := enc.Close(); closeErr != nil {
			return "", fmt.Errorf("yaml marshal failed: %w (close error: %w)", err, closeErr)
		}
		return "", fmt.Errorf("yaml marshal failed: %w", err)
	}
	if err := enc.Close(); err != nil {
		return "", fmt.Errorf("yaml marshal failed: %w", err)
	}

	b := buf.Bytes()
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return string(b), nil
}

// SerializeYAMLValue converts structured yaml/json data or scalar values to string format.
// For scalar types, fast-path strconv conversions avoid reflection/formatting allocations.
func SerializeYAMLValue(val any) (string, error) {
	switch v := val.(type) {
	case string:
		return v, nil
	case int:
		return strconv.Itoa(v), nil
	case int64:
		return strconv.FormatInt(v, 10), nil
	case int32:
		return strconv.FormatInt(int64(v), 10), nil
	case uint:
		return strconv.FormatUint(uint64(v), 10), nil
	case uint64:
		return strconv.FormatUint(v, 10), nil
	case float64:
		return strconv.FormatFloat(v, 'g', -1, 64), nil
	case float32:
		return strconv.FormatFloat(float64(v), 'g', -1, 32), nil
	case bool:
		return strconv.FormatBool(v), nil
	case []any, map[string]any, map[any]any, []string:
		return MarshalYAMLString(v)
	case nil:
		return "", nil
	default:
		return fmt.Sprintf("%v", v), nil
	}
}

// YAMLEncoder is a type alias to the underlying yaml Encoder.
type YAMLEncoder = goyaml.Encoder

// NewYAMLEncoder returns a new yaml Encoder.
func NewYAMLEncoder(w io.Writer) *YAMLEncoder {
	return goyaml.NewEncoder(w)
}
