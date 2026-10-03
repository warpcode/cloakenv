# Bolt's Journal - Critical Performance Learnings

## 2026-10-03 - Avoid Per-Entry Map Allocations in Search Formatting
**Learning:** Lowercasing all attribute keys into a temporary `map[string]string` per result entry during search result flattening causes significant heap allocations (4000+ allocs/1000 items). Direct exact key lookup followed by `strings.EqualFold` linear scanning over the entry's attributes map eliminates per-entry map allocations entirely while maintaining case-insensitivity.
**Action:** Prefer direct map lookups with a fallback `strings.EqualFold` scan instead of allocating lowercased key index maps when querying small map structures.
