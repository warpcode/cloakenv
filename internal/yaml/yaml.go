package yaml

import (
	"bytes"
	"fmt"
	"io"
	"strconv"
	"sync"

	goyaml "gopkg.in/yaml.v3"
)

var bufPool = sync.Pool{
	New: func() any {
		return new(bytes.Buffer)
	},
}

// Unmarshal wraps yaml.Unmarshal with centralized error handling.
func Unmarshal(in []byte, out any) error {
	if err := goyaml.Unmarshal(in, out); err != nil {
		return fmt.Errorf("yaml unmarshal failed: %w", err)
	}
	return nil
}

// Marshal wraps yaml.Marshal with centralized error handling and buffer pooling.
func Marshal(in any) ([]byte, error) {
	buf := bufPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer bufPool.Put(buf)

	enc := goyaml.NewEncoder(buf)
	if err := enc.Encode(in); err != nil {
		return nil, fmt.Errorf("yaml marshal failed: %w", err)
	}
	_ = enc.Close()

	res := make([]byte, buf.Len())
	copy(res, buf.Bytes())
	return res, nil
}

// MarshalString encodes the value as YAML into a string, trimming any trailing
// newlines, using a pooled buffer to reduce allocations.
func MarshalString(in any) (string, error) {
	buf := bufPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer bufPool.Put(buf)

	enc := goyaml.NewEncoder(buf)
	if err := enc.Encode(in); err != nil {
		return "", fmt.Errorf("yaml marshal failed: %w", err)
	}
	_ = enc.Close()

	b := buf.Bytes()
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return string(b), nil
}

// SerializeValue converts structured yaml/json data or scalar values to string format.
// For scalar types, fast-path strconv conversions avoid reflection/formatting allocations.
func SerializeValue(val any) (string, error) {
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
		return MarshalString(v)
	case nil:
		return "", nil
	default:
		return fmt.Sprintf("%v", v), nil
	}
}

// Encoder is a type alias to the underlying yaml Encoder.
type Encoder = goyaml.Encoder

// NewEncoder returns a new yaml Encoder.
func NewEncoder(w io.Writer) *Encoder {
	return goyaml.NewEncoder(w)
}
