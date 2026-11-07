package engine

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"text/template"
)

func TestTemplateLRUCache_GetPut(t *testing.T) {
	cache := newTemplateLRUCache(5)

	tmplStr := "hello {{ .name }}"

	// Miss on empty cache
	item, ok := cache.Get(tmplStr)
	if ok || item != nil {
		t.Fatalf("expected cache miss, got item: %v", item)
	}
	hits, misses := cache.Stats()
	if hits != 0 || misses != 1 {
		t.Errorf("expected 0 hits, 1 miss; got %d hits, %d misses", hits, misses)
	}

	// Put item
	tmpl, err := template.New("test").Parse(tmplStr)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	cached := &cachedTemplate{tmpl: tmpl, dynamic: false}
	cache.Put(tmplStr, cached)

	if cache.Len() != 1 {
		t.Errorf("expected cache len 1, got %d", cache.Len())
	}

	// Hit on subsequent Get
	gotItem, ok := cache.Get(tmplStr)
	if !ok || gotItem == nil {
		t.Fatalf("expected cache hit, got nil")
	}
	if gotItem.tmpl != tmpl {
		t.Errorf("got template %v, want %v", gotItem.tmpl, tmpl)
	}
	hits, misses = cache.Stats()
	if hits != 1 || misses != 1 {
		t.Errorf("expected 1 hit, 1 miss; got %d hits, %d misses", hits, misses)
	}

	// Put duplicate item updates and does not increase length
	cache.Put(tmplStr, cached)
	if cache.Len() != 1 {
		t.Errorf("expected cache len 1 after duplicate Put, got %d", cache.Len())
	}
}

func TestTemplateLRUCache_Eviction(t *testing.T) {
	// Cache with capacity of 3
	cache := newTemplateLRUCache(3)

	k1 := "tmpl1"
	k2 := "tmpl2"
	k3 := "tmpl3"
	k4 := "tmpl4"

	t1, _ := template.New("t1").Parse(k1)
	t2, _ := template.New("t2").Parse(k2)
	t3, _ := template.New("t3").Parse(k3)
	t4, _ := template.New("t4").Parse(k4)

	cache.Put(k1, &cachedTemplate{tmpl: t1})
	cache.Put(k2, &cachedTemplate{tmpl: t2})
	cache.Put(k3, &cachedTemplate{tmpl: t3})

	if cache.Len() != 3 {
		t.Fatalf("expected len 3, got %d", cache.Len())
	}

	// Access k1 so order is k1 (newest), k3, k2 (oldest)
	_, ok := cache.Get(k1)
	if !ok {
		t.Fatalf("expected hit on k1")
	}

	// Insert k4; should evict k2
	cache.Put(k4, &cachedTemplate{tmpl: t4})

	if cache.Len() != 3 {
		t.Fatalf("expected len 3 after eviction, got %d", cache.Len())
	}

	// k2 must be evicted
	if _, ok := cache.Get(k2); ok {
		t.Errorf("expected k2 to be evicted, but it was found in cache")
	}

	// k1, k3, k4 must still be present
	for _, k := range []string{k1, k3, k4} {
		if _, ok := cache.Get(k); !ok {
			t.Errorf("expected key %q to be present in cache", k)
		}
	}
}

func TestTemplateLRUCache_Purge(t *testing.T) {
	cache := newTemplateLRUCache(10)
	k := "purge_test"
	t1, _ := template.New("t1").Parse("test")
	cache.Put(k, &cachedTemplate{tmpl: t1})

	cache.Get(k) // 1 hit

	cache.Purge()
	if cache.Len() != 0 {
		t.Errorf("expected len 0 after purge, got %d", cache.Len())
	}
	hits, misses := cache.Stats()
	if hits != 0 || misses != 0 {
		t.Errorf("expected 0 hits, 0 misses after purge; got %d hits, %d misses", hits, misses)
	}

	if _, ok := cache.Get(k); ok {
		t.Errorf("expected key to be gone after purge")
	}
}

func TestTemplateLRUCache_DefaultCapacity(t *testing.T) {
	cache := newTemplateLRUCache(0)
	if cache.capacity != defaultTemplateCacheCapacity {
		t.Errorf("expected default capacity %d, got %d", defaultTemplateCacheCapacity, cache.capacity)
	}
}

func TestTemplateUsesDynamicFuncs(t *testing.T) {
	tests := []struct {
		name    string
		tmpl    string
		dynamic bool
	}{
		{
			name:    "plain text",
			tmpl:    "hello world",
			dynamic: false,
		},
		{
			name:    "variable interpolation only",
			tmpl:    "user={{ .user }} pass={{ .pass }}",
			dynamic: false,
		},
		{
			name:    "pure functions upper and lower",
			tmpl:    "{{ .val | upper | lower }}",
			dynamic: false,
		},
		{
			name:    "pure function shquote",
			tmpl:    "{{ shquote .val }}",
			dynamic: false,
		},
		{
			name:    "context function get",
			tmpl:    `{{ get "vault:path" }}`,
			dynamic: true,
		},
		{
			name:    "context function tryGet",
			tmpl:    `{{ tryGet "v1:k" "v2:k" }}`,
			dynamic: true,
		},
		{
			name:    "context function entry",
			tmpl:    `{{ (entry "vault:path").attributes.foo }}`,
			dynamic: true,
		},
		{
			name:    "context function search",
			tmpl:    `{{ range (search "tag=prod") }}{{ .attributes.foo }}{{ end }}`,
			dynamic: true,
		},
		{
			name:    "context function include",
			tmpl:    `{{ include "base" . }}`,
			dynamic: true,
		},
		{
			name:    "context function includeFile",
			tmpl:    `{{ includeFile "/path" . }}`,
			dynamic: true,
		},
		{
			name:    "context function exec",
			tmpl:    `{{ exec "echo" "hi" }}`,
			dynamic: true,
		},
		{
			name:    "nested get inside if condition",
			tmpl:    `{{ if (get "vault:cond") }}yes{{ else }}no{{ end }}`,
			dynamic: true,
		},
		{
			name:    "nested exec inside range",
			tmpl:    `{{ range .items }}{{ exec "echo" . }}{{ end }}`,
			dynamic: true,
		},
		{
			name:    "associated template with get",
			tmpl:    `{{ define "sub" }}{{ get "v:k" }}{{ end }}{{ template "sub" . }}`,
			dynamic: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpl, err := template.New("test").Funcs(baseTemplateFuncs).Parse(tt.tmpl)
			if err != nil {
				t.Fatalf("template parse failed: %v", err)
			}
			got := templateUsesDynamicFuncs(tmpl)
			if got != tt.dynamic {
				t.Errorf("templateUsesDynamicFuncs(%q) = %v, want %v", tt.tmpl, got, tt.dynamic)
			}
		})
	}

	if templateUsesDynamicFuncs(nil) {
		t.Errorf("expected false for nil template")
	}
}

func TestTemplateLRUCache_Concurrent(t *testing.T) {
	cache := newTemplateLRUCache(16)
	const numGoroutines = 30
	const numIters = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for g := range numGoroutines {
		go func(gid int) {
			defer wg.Done()
			for i := range numIters {
				keyStr := fmt.Sprintf("template_%d", (gid+i)%20)

				item, ok := cache.Get(keyStr)
				if !ok || item == nil {
					tmpl, err := template.New("tmpl").Parse(keyStr)
					if err == nil {
						cache.Put(keyStr, &cachedTemplate{tmpl: tmpl, dynamic: false})
					}
				}
			}
		}(g)
	}

	wg.Wait()

	if cache.Len() > 16 {
		t.Errorf("cache size %d exceeded capacity 16", cache.Len())
	}
	hits, misses := cache.Stats()
	if hits+misses == 0 {
		t.Errorf("expected non-zero total accesses")
	}
}

func TestTemplateEngine_CacheIntegration(t *testing.T) {
	PurgeTemplateCache()
	defer PurgeTemplateCache()

	te := NewTemplateEngine(nil)
	ctx := context.Background()

	// Initial render: cache miss
	data := map[string]any{"user": "alice"}
	tmplA := "hello {{ .user }}"
	res1, err := te.Render(ctx, tmplA, data, 0, "")
	if err != nil {
		t.Fatalf("first render failed: %v", err)
	}
	if res1 != "hello alice" {
		t.Errorf("unexpected output: %q", res1)
	}

	hits, misses, size := TemplateCacheStats()
	if misses != 1 || hits != 0 || size != 1 {
		t.Fatalf("after first render: expected 0 hits, 1 miss, size 1; got %d hits, %d misses, size %d", hits, misses, size)
	}

	// Second render with identical template string: MUST be cache hit
	res2, err := te.Render(ctx, tmplA, map[string]any{"user": "bob"}, 0, "")
	if err != nil {
		t.Fatalf("second render failed: %v", err)
	}
	if res2 != "hello bob" {
		t.Errorf("unexpected output: %q", res2)
	}

	hits, misses, size = TemplateCacheStats()
	if hits != 1 || misses != 1 || size != 1 {
		t.Fatalf("after second render: expected 1 hit, 1 miss, size 1; got %d hits, %d misses, size %d", hits, misses, size)
	}

	// Different template string: cache miss
	tmplB := "welcome {{ .user }}!"
	res3, err := te.Render(ctx, tmplB, data, 0, "")
	if err != nil {
		t.Fatalf("third render failed: %v", err)
	}
	if res3 != "welcome alice!" {
		t.Errorf("unexpected output: %q", res3)
	}

	hits, misses, size = TemplateCacheStats()
	if hits != 1 || misses != 2 || size != 2 {
		t.Fatalf("after third render: expected 1 hit, 2 misses, size 2; got %d hits, %d misses, size %d", hits, misses, size)
	}

	// Re-render tmplA: cache hit
	_, _ = te.Render(ctx, tmplA, data, 0, "")
	// Re-render tmplB: cache hit
	_, _ = te.Render(ctx, tmplB, data, 0, "")

	hits, misses, size = TemplateCacheStats()
	if hits != 3 || misses != 2 || size != 2 {
		t.Fatalf("after re-renders: expected 3 hits, 2 misses, size 2; got %d hits, %d misses, size %d", hits, misses, size)
	}
}

func TestTemplateEngine_CacheSyntaxErrorNotCached(t *testing.T) {
	PurgeTemplateCache()
	defer PurgeTemplateCache()

	te := NewTemplateEngine(nil)
	ctx := context.Background()

	badTmpl := "hello {{ unclosed"
	_, err := te.Render(ctx, badTmpl, nil, 0, "test_key")
	if err == nil {
		t.Fatalf("expected error on invalid syntax, got nil")
	}

	// Invalid template should not be stored in cache
	hits, misses, size := TemplateCacheStats()
	if size != 0 {
		t.Errorf("expected cache size 0 for failed parse, got %d", size)
	}
	if misses != 1 || hits != 0 {
		t.Errorf("expected 0 hits, 1 miss; got %d hits, %d misses", hits, misses)
	}
}
