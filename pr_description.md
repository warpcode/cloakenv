## Documentation Accuracy Fixes

### Summary
- Documentation artifacts scanned: 3
- Discrepancies fixed: 4 (Stale: 3, Dead: 1)
- Flagged for human review (not auto-fixed): 0 (Unverifiable: 0, Suspected code bugs: 0)

### Fixes Applied

#### Remove non-existent "plain" output format
- **Type:** DEAD
- **File:** `internal/cmd/help.go:37`
- **Before:** `-o, --output    Output format: plain, json, yaml, env (depends on command)`
- **After:** `-o, --output    Output format: keys, json, yaml, env (depends on command)`
- **Reason:** The `show` command does not implement `plain`, it implements `keys`.

#### Clarify `cloakenv run` and `cloakenv show` `-m entry-uri` flag attribute targeting
- **Type:** STALE
- **File:** `internal/cmd/help.go:35`, `internal/cmd/help.go:72`
- **Before:** `-m entry-uri    Merge all attributes from an entry into the environment (repeatable)`
- **After:** `-m entry-uri    Merge attributes from an entry into the environment. Can target a single attribute using :attribute (repeatable)`
- **Reason:** The command's help text was missing the `:attribute` selection functionality documented in README and implemented in the code.

#### Clarify `-i KEY` meaning difference for search command vs run/show commands
- **Type:** STALE
- **File:** `internal/cmd/help.go:36`
- **Before:** `-i KEY          Filter/whitelist keys/variables (repeatable)`
- **After:** `-i KEY          Filter/whitelist keys or select output fields (repeatable)`
- **Reason:** For the `search` command, `-i KEY` does not whitelist merged keys, but selects output fields as seen in `parseSearchArgs`.

#### Update ResolveValues support for inline interpolations
- **Type:** STALE
- **File:** `internal/config/config.go:124`
- **Before:** `// Only whole-value replacement is supported; inline interpolation is not.`
- **After:** `// Inline interpolation is supported using ${scheme://...} syntax.`
- **Reason:** The actual code in `utils.ExpandString` fully parses and resolves substrings with `${...}`, refuting the comment that only whole-value replacement works.
