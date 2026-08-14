package yaml

import (
	"fmt"
	"io"

	goyaml "gopkg.in/yaml.v3"
)

// Unmarshal wraps yaml.Unmarshal with centralized error handling.
func Unmarshal(in []byte, out any) error {
	if err := goyaml.Unmarshal(in, out); err != nil {
		return fmt.Errorf("yaml unmarshal failed: %w", err)
	}
	return nil
}

// Marshal wraps yaml.Marshal with centralized error handling.
func Marshal(in any) ([]byte, error) {
	data, err := goyaml.Marshal(in)
	if err != nil {
		return nil, fmt.Errorf("yaml marshal failed: %w", err)
	}
	return data, nil
}

// Encoder is a type alias to the underlying yaml Encoder.
type Encoder = goyaml.Encoder

// NewEncoder returns a new yaml Encoder.
func NewEncoder(w io.Writer) *Encoder {
	return goyaml.NewEncoder(w)
}
