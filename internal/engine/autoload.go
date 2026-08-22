package engine

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/warpcode/cloakenv/internal/config"
)

// MatchRunAlias evaluates configured autoload/run alias rules against command arguments
// and returns the first matching AutoloadRule and true, or an empty rule and false if no rule matched.
func MatchRunAlias(cfg *config.Config, cmdArgs []string) (config.AutoloadRule, bool) {
	if cfg == nil || len(cfg.Autoload) == 0 || len(cmdArgs) == 0 {
		return config.AutoloadRule{}, false
	}
	for _, rule := range cfg.Autoload {
		matched, _, _ := MatchCommandRule(rule, cmdArgs)
		if matched {
			return rule, true
		}
	}
	return config.AutoloadRule{}, false
}

// IsRunAlias reports whether a command argument slice matches any configured autoload/run alias rule.
func IsRunAlias(cfg *config.Config, cmdArgs []string) bool {
	_, matched := MatchRunAlias(cfg, cmdArgs)
	return matched
}

// MatchCommand reports whether a command argument slice matches an autoload rule match pattern.
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

	pattern := strings.TrimSpace(rule.Match)
	if pattern == "" {
		return false, cmdArgs, nil
	}

	fullCmd := strings.Join(cmdArgs, " ")
	execPath := cmdArgs[0]
	execBase := filepath.Base(execPath)

	ruleLower := strings.ToLower(pattern)
	execPathLower := strings.ToLower(execPath)
	execBaseLower := strings.ToLower(execBase)
	fullCmdLower := strings.ToLower(fullCmd)

	var re *regexp.Regexp
	var matchIndices []int

	// 1. Attempt Regex compilation & match
	compiled, err := regexp.Compile(pattern)
	if err == nil {
		indices := compiled.FindStringSubmatchIndex(fullCmd)
		if indices != nil {
			re = compiled
			matchIndices = indices
		}
	} else {
		if compiledCi, errCi := regexp.Compile("(?i)" + pattern); errCi == nil {
			indices := compiledCi.FindStringSubmatchIndex(fullCmd)
			if indices != nil {
				re = compiledCi
				matchIndices = indices
			}
		}
	}

	isMatched := matchIndices != nil

	// 2. Fallback to basename / glob / prefix matching if regex didn't match
	if !isMatched {
		if execBaseLower == ruleLower || execPathLower == ruleLower || fullCmdLower == ruleLower ||
			strings.HasPrefix(fullCmdLower, ruleLower+" ") || strings.HasPrefix(fullCmdLower, ruleLower+"\t") ||
			matchWildcard(ruleLower, execBaseLower) || matchWildcard(ruleLower, execPathLower) || matchWildcard(ruleLower, fullCmdLower) {
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
			expanded = string(re.ExpandString(nil, template, fullCmd, matchIndices))
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

	for i := 0; i < len(s); i++ {
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
