package runner

import (
	"io"
	"strings"
	"sync"
)

// RedactedPlaceholder is the replacement text emitted when a registered secret
// is detected in a child process's stdout or stderr stream.
const RedactedPlaceholder = "<REDACTED>"

// minMaskSecretLen avoids masking trivial single-character or short flag values
// (such as "0", "1", "yes") that would corrupt ordinary process output.
const minMaskSecretLen = 4

type trieNode struct {
	children map[byte]int
	fail     int
	depth    int
	matchLen int
}

// MaskTrie is an immutable Aho-Corasick prefix trie compiled from secret strings
// for linear-time streaming redaction of child stdout/stderr output.
type MaskTrie struct {
	nodes  []trieNode
	maxLen int
}

// NewMaskTrie compiles a MaskTrie from the provided secret values.
// Returns nil if no secrets qualify for masking.
func NewMaskTrie(secrets []string) *MaskTrie {
	seen := make(map[string]struct{}, len(secrets))
	filtered := make([]string, 0, len(secrets))
	for _, s := range secrets {
		if len(s) < minMaskSecretLen || strings.TrimSpace(s) == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		filtered = append(filtered, s)
	}
	if len(filtered) == 0 {
		return nil
	}

	nodes := make([]trieNode, 1, 64)
	maxLen := 0
	for _, s := range filtered {
		if len(s) > maxLen {
			maxLen = len(s)
		}
		cur := 0
		for i := range len(s) {
			b := s[i]
			if nodes[cur].children == nil {
				nodes[cur].children = make(map[byte]int, 4)
			}
			next, ok := nodes[cur].children[b]
			if !ok {
				next = len(nodes)
				nodes[cur].children[b] = next
				nodes = append(nodes, trieNode{depth: nodes[cur].depth + 1})
			}
			cur = next
		}
		if len(s) > nodes[cur].matchLen {
			nodes[cur].matchLen = len(s)
		}
	}

	// Build Aho-Corasick failure links via BFS.
	queue := make([]int, 0, len(nodes))
	for _, next := range nodes[0].children {
		nodes[next].fail = 0
		queue = append(queue, next)
	}
	for head := 0; head < len(queue); head++ {
		u := queue[head]
		for b, v := range nodes[u].children {
			f := nodes[u].fail
			for f != 0 {
				if _, ok := nodes[f].children[b]; ok {
					break
				}
				f = nodes[f].fail
			}
			if next, ok := nodes[f].children[b]; ok && next != v {
				nodes[v].fail = next
			} else {
				nodes[v].fail = 0
			}
			if nodes[nodes[v].fail].matchLen > nodes[v].matchLen {
				nodes[v].matchLen = nodes[nodes[v].fail].matchLen
			}
			queue = append(queue, v)
		}
	}

	return &MaskTrie{
		nodes:  nodes,
		maxLen: maxLen,
	}
}

func (t *MaskTrie) step(state int, b byte) int {
	for state != 0 {
		if next, ok := t.nodes[state].children[b]; ok {
			return next
		}
		state = t.nodes[state].fail
	}
	if next, ok := t.nodes[0].children[b]; ok {
		return next
	}
	return 0
}

// extendableDepth returns the length of the longest suffix represented by state
// that still has outgoing transitions in the trie (and can therefore match a
// longer pattern if more bytes arrive).
func (t *MaskTrie) extendableDepth(state int) int {
	for state != 0 && len(t.nodes[state].children) == 0 {
		state = t.nodes[state].fail
	}
	return t.nodes[state].depth
}

// MaskingWriter wraps an io.Writer and redacts all occurrences of secrets in
// the compiled MaskTrie across arbitrary streaming Write chunk boundaries.
type MaskingWriter struct {
	mu   sync.Mutex
	dst  io.Writer
	trie *MaskTrie
	buf  []byte
}

// NewMaskingWriter returns a streaming writer that masks secrets matched by trie.
func NewMaskingWriter(dst io.Writer, trie *MaskTrie) *MaskingWriter {
	return &MaskingWriter{
		dst:  dst,
		trie: trie,
	}
}

// Write scans p for secret matches and writes safe/redacted bytes to dst.
func (w *MaskingWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.trie == nil || len(w.trie.nodes) <= 1 {
		return w.dst.Write(p)
	}

	w.buf = append(w.buf, p...)
	if err := w.process(false); err != nil {
		return 0, err
	}
	return len(p), nil
}

// Flush processes any buffered trailing prefix bytes and writes them to dst.
// Callers must invoke Flush after the producer closes/exits.
func (w *MaskingWriter) Flush() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.trie == nil || len(w.trie.nodes) <= 1 {
		return nil
	}
	return w.process(true)
}

func (w *MaskingWriter) process(flush bool) error {
	placeholder := []byte(RedactedPlaceholder)

	for len(w.buf) > 0 {
		state := 0
		firstStart := -1
		matchEnd := -1

		for i := range len(w.buf) {
			state = w.trie.step(state, w.buf[i])
			if mLen := w.trie.nodes[state].matchLen; mLen > 0 {
				s := i + 1 - mLen
				e := i + 1
				if firstStart == -1 {
					firstStart = s
					matchEnd = e
				} else if s < matchEnd {
					if s < firstStart {
						firstStart = s
					}
					if e > matchEnd {
						matchEnd = e
					}
				}
			}
			// Once we have scanned at least maxLen-1 bytes beyond matchEnd, no later
			// pattern can start before matchEnd and overlap/extend it.
			if firstStart != -1 && i+1 >= matchEnd+w.trie.maxLen-1 {
				break
			}
		}

		if firstStart == -1 {
			if flush {
				if err := writeAll(w.dst, w.buf); err != nil {
					return err
				}
				w.buf = w.buf[:0]
				return nil
			}
			keep := w.trie.extendableDepth(state)
			if keep > len(w.buf) {
				keep = len(w.buf)
			}
			safe := len(w.buf) - keep
			if safe > 0 {
				if err := writeAll(w.dst, w.buf[:safe]); err != nil {
					return err
				}
				w.buf = append(w.buf[:0], w.buf[safe:]...)
			}
			return nil
		}

		// A match [firstStart, matchEnd) was found. Determine if trailing bytes in
		// w.buf could still extend or overlap this match when more bytes arrive.
		if !flush {
			activeDepth := w.trie.extendableDepth(state)
			prefixStart := len(w.buf) - activeDepth
			if activeDepth > 0 && prefixStart < matchEnd {
				safe := firstStart
				if prefixStart < safe {
					safe = prefixStart
				}
				if safe > 0 {
					if err := writeAll(w.dst, w.buf[:safe]); err != nil {
						return err
					}
					w.buf = append(w.buf[:0], w.buf[safe:]...)
				}
				return nil
			}
		}

		if firstStart > 0 {
			if err := writeAll(w.dst, w.buf[:firstStart]); err != nil {
				return err
			}
		}
		if err := writeAll(w.dst, placeholder); err != nil {
			return err
		}
		w.buf = append(w.buf[:0], w.buf[matchEnd:]...)
	}

	return nil
}

func writeAll(dst io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := dst.Write(data)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}
