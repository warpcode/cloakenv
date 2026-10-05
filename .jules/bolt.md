## 2026-10-05 - Zero-allocation URI parsing and string expansion in secret resolution hot paths

**Learning:** `ParseURI` and `ExpandString` are called heavily during secret resolution loops and env building. `strings.SplitN` allocated a 2-element slice on every `ParseURI` invocation, which can be eliminated entirely using Go's stdlib `strings.Cut`. Additionally, `ExpandString` allocated a `strings.Builder` even for strings without `${...}` placeholders; checking `!strings.Contains(s, "$")` avoids string builder creation and string copies for plain strings.

**Action:** Use `strings.Cut` instead of `strings.SplitN` for 2-part string splits, and short-circuit string interpolation helpers when no trigger character is present.
