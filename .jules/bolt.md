## 2026-07-03 - KeePass Tag Matching Zero-Allocation Scanning
**Learning:** KeePass tag strings can use both commas and semicolons as delimiters. Scanning directly via `strings.IndexAny(s, ",;")` avoids string splitting and map allocations during search while maintaining complete compatibility with KeePass tag formats.
**Action:** When searching/matching delimited tags in Go, scan using `strings.IndexAny` + reslicing + `strings.TrimSpace` + `strings.EqualFold` to achieve zero allocations per match.
