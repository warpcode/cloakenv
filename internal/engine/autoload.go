package engine

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/warpcode/cloakenv/internal/config"
)

// regexCache stores compiled regular expressions to avoid recompilation on
// every autoload rule evaluation.
var regexCache sync.Map

type parsedCmd struct {
	fullCmd       string
	execPath      string
	execBase      string
	execPathLower string
	execBaseLower string
	fullCmdLower  string
}

func parseCommandArgs(cmdArgs []string) parsedCmd {
	if len(cmdArgs) == 0 {
		return parsedCmd{}
	}
	fullCmd := strings.Join(cmdArgs, " ")
	execPath := cmdArgs[0]
	execBase := filepath.Base(execPath)
	return parsedCmd{
		fullCmd:       fullCmd,
		execPath:      execPath,
		execBase:      execBase,
		execPathLower: strings.ToLower(execPath),
		execBaseLower: strings.ToLower(execBase),
		fullCmdLower:  strings.ToLower(fullCmd),
	}
}

// MatchRunAlias evaluates configured autoload/run alias rules against command arguments
// and returns the first matching AutoloadRule, a match indicator, and any
// command-substitution error encountered for the matched rule.
func MatchRunAlias(cfg *config.Config, cmdArgs []string) (config.AutoloadRule, bool, error) {
	if cfg == nil || len(cfg.Autoload) == 0 || len(cmdArgs) == 0 {
		return config.AutoloadRule{}, false, nil
	}
	parsed := parseCommandArgs(cmdArgs)
	for _, rule := range cfg.Autoload {
		matched, _, err := matchPreparedCommandRule(rule, cmdArgs, parsed)
		if matched {
			return rule, true, err
		}
	}
	return config.AutoloadRule{}, false, nil
}

// IsRunAlias reports whether a command argument slice matches any configured autoload/run alias rule.
// As a boolean predicate, substitution errors are intentionally not surfaced.
func IsRunAlias(cfg *config.Config, cmdArgs []string) bool {
	_, matched, _ := MatchRunAlias(cfg, cmdArgs)
	return matched
}

// MatchCommand reports whether a command argument slice matches an autoload rule match pattern.
// As a boolean predicate, substitution errors are intentionally not surfaced.
func MatchCommand(ruleMatch string, cmdArgs []string) bool {
	matched, _, _ := MatchCommandRule(config.AutoloadRule{Match: ruleMatch}, cmdArgs)
	return matched
}

// MatchCommandRule evaluates an autoload rule against command arguments.
// If it matches and specifies a command replacement template, it returns the substituted command args.
func MatchCommandRule(rule config.AutoloadRule, cmdArgs []string) (bool, []string, error) {
	if len(cmdArgs) == 0 {
		return false, cmdArgs, nil
	}
	return matchPreparedCommandRule(rule, cmdArgs, parseCommandArgs(cmdArgs))
}

func matchPreparedCommandRule(rule config.AutoloadRule, cmdArgs []string, parsed parsedCmd) (bool, []string, error) {
	if len(cmdArgs) == 0 {
		return false, cmdArgs, nil
	}

	pattern := strings.TrimSpace(rule.Match)
	if pattern == "" {
		return false, cmdArgs, nil
	}

	ruleLower := strings.ToLower(pattern)

	var re *regexp.Regexp
	var matchIndices []int

	// 1. Attempt Regex compilation & match (cached)
	if cached, ok := regexCache.Load(pattern); ok {
		compiled := cached.(*regexp.Regexp)
		if indices := compiled.FindStringSubmatchIndex(parsed.fullCmd); indices != nil {
			re = compiled
			matchIndices = indices
		}
	} else if compiled, err := regexp.Compile(pattern); err == nil {
		regexCache.Store(pattern, compiled)
		if indices := compiled.FindStringSubmatchIndex(parsed.fullCmd); indices != nil {
			re = compiled
			matchIndices = indices
		}
	}
	// Note: invalid regex patterns are not retried with a case-insensitive
	// flag — prefixing (?i) cannot repair a syntax error. Non-regex patterns
	// fall through to the basename/glob/prefix matching below, which is
	// case-insensitive by construction.

	isMatched := matchIndices != nil

	// 2. Fallback to basename / glob / prefix matching if regex didn't match
	if !isMatched {
		if parsed.execBaseLower == ruleLower || parsed.execPathLower == ruleLower || parsed.fullCmdLower == ruleLower ||
			strings.HasPrefix(parsed.fullCmdLower, ruleLower+" ") || strings.HasPrefix(parsed.fullCmdLower, ruleLower+"\t") ||
			matchWildcard(ruleLower, parsed.execBaseLower) || matchWildcard(ruleLower, parsed.execPathLower) || matchWildcard(ruleLower, parsed.fullCmdLower) {
			isMatched = true
		}
	}

	if !isMatched {
		return false, cmdArgs, nil
	}

	// 3. Command Substitution: If rule.Command provides a replacement template
	if strings.TrimSpace(rule.Command) != "" {
		template := convertBackslashGroups(rule.Command)
		var expanded string
		if re != nil && matchIndices != nil {
			expanded = string(re.ExpandString(nil, template, parsed.fullCmd, matchIndices))
		} else {
			expanded = template
		}
		newArgs, err := splitCommand(expanded)
		if err != nil {
			return true, cmdArgs, fmt.Errorf("failed to parse substituted command %q: %w", expanded, err)
		}
		return true, newArgs, nil
	}

	return true, cmdArgs, nil
}

func convertBackslashGroups(template string) string {
	var sb strings.Builder
	for i := 0; i < len(template); i++ {
		if template[i] == '\\' && i+1 < len(template) && template[i+1] >= '1' && template[i+1] <= '9' {
			sb.WriteByte('$')
			sb.WriteByte(template[i+1])
			i++
			continue
		}
		sb.WriteByte(template[i])
	}
	return sb.String()
}

func splitCommand(s string) ([]string, error) {
	var tokens []string
	var current strings.Builder
	inSingle := false
	inDouble := false
	escaped := false

	for i := range len(s) {
		ch := s[i]

		if escaped {
			current.WriteByte(ch)
			escaped = false
			continue
		}

		if ch == '\\' && !inSingle {
			if i+1 < len(s) {
				next := s[i+1]
				if inDouble {
					if next == '"' || next == '\\' || next == '$' || next == '`' {
						escaped = true
						continue
					}
				} else {
					if next == ' ' || next == '\t' || next == '\n' || next == '"' || next == '\'' || next == '\\' {
						escaped = true
						continue
					}
				}
			}
		}

		if ch == '\'' && !inDouble {
			inSingle = !inSingle
			continue
		}

		if ch == '"' && !inSingle {
			inDouble = !inDouble
			continue
		}

		if (ch == ' ' || ch == '\t' || ch == '\n') && !inSingle && !inDouble {
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
			continue
		}

		current.WriteByte(ch)
	}

	if inSingle || inDouble {
		return nil, fmt.Errorf("unclosed quote in command string: %s", s)
	}

	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}

	return tokens, nil
}

func matchWildcard(pattern, text string) bool {
	if pattern == "*" {
		return true
	}
	parts := strings.Split(pattern, "*")
	if len(parts) == 1 {
		return pattern == text
	}
	if !strings.HasPrefix(text, parts[0]) {
		return false
	}
	text = text[len(parts[0]):]
	for i := 1; i < len(parts)-1; i++ {
		p := parts[i]
		if p == "" {
			continue
		}
		idx := strings.Index(text, p)
		if idx == -1 {
			return false
		}
		text = text[idx+len(p):]
	}
	return strings.HasSuffix(text, parts[len(parts)-1])
}
