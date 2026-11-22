package runner

import (
	"bytes"
	"strings"
	"sync"
	"testing"
)

func TestMaskingWriter_BasicAndEdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		secrets []string
		chunks  []string
		want    string
	}{
		{
			name:    "single secret in middle",
			secrets: []string{"sk-live-99999"},
			chunks:  []string{"Authorization: Bearer sk-live-99999\n"},
			want:    "Authorization: Bearer <REDACTED>\n",
		},
		{
			name:    "secret split byte by byte",
			secrets: []string{"super_secret_token"},
			chunks:  strings.Split("prefix super_secret_token suffix", ""),
			want:    "prefix <REDACTED> suffix",
		},
		{
			name:    "multiple distinct secrets",
			secrets: []string{"alpha_secret", "beta_secret"},
			chunks:  []string{"a=alpha_", "secret b=beta_sec", "ret\n"},
			want:    "a=<REDACTED> b=<REDACTED>\n",
		},
		{
			name:    "prefix-nested secrets choose longest match without tail leak",
			secrets: []string{"secret", "secret_12345"},
			chunks:  []string{"val=", "secret", "_12345 done"},
			want:    "val=<REDACTED> done",
		},
		{
			name:    "prefix-nested shorter secret at EOF",
			secrets: []string{"secret", "secret_12345"},
			chunks:  []string{"val=secret"},
			want:    "val=<REDACTED>",
		},
		{
			name:    "overlapping secrets merged without leaking remainder",
			secrets: []string{"abcdef", "defghi"},
			chunks:  []string{"x abcdefghi y"},
			want:    "x <REDACTED> y",
		},
		{
			name:    "multiline rendered template content masked",
			secrets: []string{"model_list:\n  - api_key: sk-12345\n"},
			chunks:  []string{"DUMP:\nmodel_list:\n  - api_key: ", "sk-12345\nEND"},
			want:    "DUMP:\n<REDACTED>END",
		},
		{
			name:    "short and whitespace secrets ignored",
			secrets: []string{"", "   ", "1", "yes", "real_secret"},
			chunks:  []string{"count=1 yes real_secret"},
			want:    "count=1 yes <REDACTED>",
		},
		{
			name:    "partial prefix that never completes flushes intact",
			secrets: []string{"super_secret_token"},
			chunks:  []string{"super_sec", "ondary value"},
			want:    "super_secondary value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			trie := NewMaskTrie(tt.secrets)
			var out bytes.Buffer
			mw := NewMaskingWriter(&out, trie)
			for _, chunk := range tt.chunks {
				n, err := mw.Write([]byte(chunk))
				if err != nil {
					t.Fatalf("Write(%q) failed: %v", chunk, err)
				}
				if n != len(chunk) {
					t.Fatalf("Write(%q) = %d, want %d", chunk, n, len(chunk))
				}
			}
			if err := mw.Flush(); err != nil {
				t.Fatalf("Flush() failed: %v", err)
			}
			if got := out.String(); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMaskingWriter_StreamsNonMatchingLinesImmediately(t *testing.T) {
	// Even with a large multi-kilobyte template in the trie, ordinary non-matching
	// output must flush immediately without waiting for EOF.
	largeTemplate := strings.Repeat("secret_config_line: sk-abc123456\n", 100)
	trie := NewMaskTrie([]string{largeTemplate})
	var out bytes.Buffer
	mw := NewMaskingWriter(&out, trie)

	line := "Starting server on :8080\n"
	if _, err := mw.Write([]byte(line)); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if out.String() != line {
		t.Errorf("expected immediate streaming output %q before Flush, got %q", line, out.String())
	}
}

func TestMaskingWriter_ConcurrentWrites(t *testing.T) {
	trie := NewMaskTrie([]string{"secret_alpha_99", "secret_beta_88"})
	var out bytes.Buffer
	mw := NewMaskingWriter(&out, trie)

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 20 {
				_, _ = mw.Write([]byte("line secret_alpha_99 and secret_beta_88\n"))
			}
		}()
	}
	wg.Wait()
	if err := mw.Flush(); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}
	if strings.Contains(out.String(), "secret_alpha_99") || strings.Contains(out.String(), "secret_beta_88") {
		t.Errorf("secret leaked under concurrent writes: %s", out.String())
	}
}

func TestNewMaskTrie_NilWhenNoEligibleSecrets(t *testing.T) {
	if trie := NewMaskTrie([]string{"", "   ", "1", "ab", "xyz"}); trie != nil {
		t.Errorf("expected nil MaskTrie when all secrets are shorter than %d chars", minMaskSecretLen)
	}
}
