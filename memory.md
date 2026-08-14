* Created a wrapper package around an external dependency to centralize error handling, and updated all imports.
* When replacing an external package like `gopkg.in/yaml.v3` with an internal wrapper, make sure to use `type` aliases for exported structs/interfaces (e.g., `type Encoder = goyaml.Encoder`) to preserve compatibility.
* Added `fmt.Errorf("...: %w", err)` to properly wrap errors from the underlying library while preserving the original error types for `errors.Is`/`errors.As` checks.
