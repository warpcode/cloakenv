## 2026-10-05 - Zero-allocation URI parsing and string expansion in secret resolution

**Learning:** `ParseURI` and `ExpandString` are core utility functions invoked during secret resolution and env building. `strings.SplitN` allocated a 2-element slice on every `ParseURI` invocation, which is eliminated entirely using Go's stdlib `strings.Cut`. Additionally, `ExpandString` allocated a `strings.Builder` even for strings without `${...}` placeholders; checking `!strings.Contains(s, "$")` avoids string builder creation and string copies for plain strings, and pre-allocating buffer growth with `sb.Grow(len(s))` eliminates slice re-allocations on large string inputs. Reusing `ParseURI` in `cloakenv set` inherits null-byte injection prevention and empty scheme validation as security hardening.

**Action:** Use `strings.Cut` instead of `strings.SplitN` for 2-part string splits, and short-circuit string interpolation helpers when no trigger character is present.
