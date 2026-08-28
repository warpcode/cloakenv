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
			expanded = expandTemplate(re, template, parsed.fullCmd, matchIndices)
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

func escapeSubmatch(s string) string {
	var sb strings.Builder
	for i := range s {
		ch := s[i]
		switch ch {
		case '\\', '"', '\'':
			sb.WriteByte('\\')
			sb.WriteByte(ch)
		default:
			sb.WriteByte(ch)
		}
	}
	return sb.String()
}

func expandTemplate(re *regexp.Regexp, template string, src string, matchIndices []int) string {
	names := re.SubexpNames()
	var sb strings.Builder

	for i := 0; i < len(template); i++ {
		ch := template[i]
		if ch != '$' {
			sb.WriteByte(ch)
			continue
		}

		if i+1 >= len(template) {
			sb.WriteByte('$')
			break
		}

		next := template[i+1]
		if next == '$' {
			sb.WriteByte('$')
			i++
			continue
		}

		if next == '{' {
			closeIdx := strings.IndexByte(template[i+2:], '}')
			if closeIdx != -1 {
				nameOrNum := template[i+2 : i+2+closeIdx]
				if isValidGroupNameOrNum(nameOrNum) {
					group := findGroupIndex(names, nameOrNum)
					if group >= 0 && group*2+1 < len(matchIndices) {
						gStart := matchIndices[2*group]
						gEnd := matchIndices[2*group+1]
						if gStart >= 0 && gEnd >= gStart && gEnd <= len(src) {
							val := src[gStart:gEnd]
							val = escapeSubmatch(val)
							sb.WriteString(val)
						}
						i += 2 + closeIdx
						continue
					}
				}
			}
		}

		_, group, consumed := parseSubmatchRef(names, template[i+1:])
		if group >= 0 && group*2+1 < len(matchIndices) {
			gStart := matchIndices[2*group]
			gEnd := matchIndices[2*group+1]
			if gStart >= 0 && gEnd >= gStart && gEnd <= len(src) {
				val := src[gStart:gEnd]
				val = escapeSubmatch(val)
				sb.WriteString(val)
			}
			i += consumed
			continue
		}

		sb.WriteByte('$')
	}

	return sb.String()
}

func isValidGroupNameOrNum(s string) bool {
	if len(s) == 0 {
		return false
	}
	for i := range s {
		c := s[i]
		if !isAlphaNum(c) && c != '_' {
			return false
		}
	}
	return true
}

func findGroupIndex(names []string, nameOrNum string) int {
	var num int
	if _, err := fmt.Sscanf(nameOrNum, "%d", &num); err == nil && num >= 0 && num < len(names) {
		return num
	}
	for i, name := range names {
		if name == nameOrNum && name != "" {
			return i
		}
	}
	return -1
}

func parseSubmatchRef(names []string, s string) (string, int, int) {
	if len(s) == 0 {
		return "", -1, 0
	}
	var digits int
	for digits < len(s) && s[digits] >= '0' && s[digits] <= '9' {
		digits++
	}
	if digits > 0 {
		for d := digits; d > 0; d-- {
			var num int
			if _, err := fmt.Sscanf(s[:d], "%d", &num); err == nil && num < len(names) {
				return "", num, d
			}
		}
	}
	var end int
	for end < len(s) && (isAlphaNum(s[end]) || s[end] == '_') {
		end++
	}
	if end > 0 {
		name := s[0:end]
		for i, n := range names {
			if n == name && n != "" {
				return name, i, end
			}
		}
	}
	return "", -1, 0
}

func isAlphaNum(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
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
