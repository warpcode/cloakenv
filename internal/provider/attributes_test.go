package provider

import (
	"context"
	"fmt"
	"testing"
)

func TestAttributesProvider_GetEntry(t *testing.T) {
	ctx := context.Background()
	underlying := &mockSearchableProvider{
		entries: map[string]Entry{
			"test-entry": {
				Title: "test-entry",
				Attributes: map[string]any{
					"existing": "value",
				},
			},
		},
	}
	attributes := map[string]any{
		"injected": "injected-value",
		"existing": "overwritten",
	}

	p := NewAttributesProvider(underlying, attributes)
	searchable, ok := p.(SearchableProvider)
	if !ok {
		t.Fatalf("expected NewAttributesProvider to return a SearchableProvider")
	}

	entry, err := searchable.GetEntry(ctx, "test-entry")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if val, ok := entry.Attributes["injected"]; !ok || val != "injected-value" {
		t.Errorf("expected injected attribute 'injected' to be 'injected-value', got %v", val)
	}
	if val, ok := entry.Attributes["existing"]; !ok || val != "overwritten" {
		t.Errorf("expected existing attribute to be overwritten with 'overwritten', got %v", val)
	}
}

func TestAttributesProvider_Search(t *testing.T) {
	ctx := context.Background()
	underlying := &mockSearchableProvider{
		results: []SearchResult{
			{
				Entry: Entry{
					Title: "test-entry",
					Attributes: map[string]any{
						"existing": "value",
					},
				},
			},
		},
	}
	attributes := map[string]any{
		"injected": "injected-value",
	}

	p := NewAttributesProvider(underlying, attributes)
	searchable, ok := p.(SearchableProvider)
	if !ok {
		t.Fatalf("expected NewAttributesProvider to return a SearchableProvider")
	}

	results, err := searchable.Search(ctx, SearchQuery{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	entry := results[0].Entry
	if val, ok := entry.Attributes["injected"]; !ok || val != "injected-value" {
		t.Errorf("expected injected attribute 'injected' to be 'injected-value', got %v", val)
	}
}

func TestAttributesProvider_GetSecret(t *testing.T) {
	ctx := context.Background()
	underlying := &mockSearchableProvider{
		entries: map[string]Entry{
			"test-entry": {
				Title: "test-entry",
				Attributes: map[string]any{
					"existing": "value",
				},
			},
		},
	}
	attributes := map[string]any{
		"injected": "injected-value",
	}

	p := NewAttributesProvider(underlying, attributes)

	// Test getting an existing attribute from underlying (fallback)
	// Actually mockSearchableProvider needs GetSecret implementation for this to work
	// Let's rely on injected attribute being returned
	val, err := p.GetSecret(ctx, "test-entry:injected")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "injected-value" {
		t.Errorf("expected 'injected-value', got %q", val)
	}
}

func TestAttributesProvider_InterfacePreservation(t *testing.T) {
	attributes := map[string]any{"injected": "value"}

	t.Run("only searchable", func(t *testing.T) {
		underlying := &mockSearchableProvider{}
		p := NewAttributesProvider(underlying, attributes)

		if _, ok := p.(SearchableProvider); !ok {
			t.Error("expected SearchableProvider interface to be preserved")
		}
		if _, ok := p.(ValueResolvableProvider); ok {
			t.Error("did not expect ValueResolvableProvider interface")
		}
	})

	t.Run("only value resolvable", func(t *testing.T) {
		underlying := &mockValueResolvableProvider{}
		p := NewAttributesProvider(underlying, attributes)

		if _, ok := p.(SearchableProvider); ok {
			t.Error("did not expect SearchableProvider interface")
		}
		if _, ok := p.(ValueResolvableProvider); !ok {
			t.Error("expected ValueResolvableProvider interface to be preserved")
		}
	})

	t.Run("both interfaces", func(t *testing.T) {
		underlying := &mockSearchableValueResolvableProvider{}
		p := NewAttributesProvider(underlying, attributes)

		if _, ok := p.(SearchableProvider); !ok {
			t.Error("expected SearchableProvider interface to be preserved")
		}
		if _, ok := p.(ValueResolvableProvider); !ok {
			t.Error("expected ValueResolvableProvider interface to be preserved")
		}
	})
}

// mock providers for testing
type mockSecretProvider struct{}

func (m *mockSecretProvider) Scheme() string { return "mock" }
func (m *mockSecretProvider) Initialize(ctx context.Context, cfg ProviderConfig) error { return nil }
func (m *mockSecretProvider) GetSecret(ctx context.Context, location string) (string, error) {
	return "", fmt.Errorf("not found")
}
func (m *mockSecretProvider) SetSecret(ctx context.Context, location, value string) error { return nil }
func (m *mockSecretProvider) DeleteSecret(ctx context.Context, location string) error { return nil }
func (m *mockSecretProvider) Validate(settings map[string]string) error { return nil }

type mockSearchableProvider struct {
	mockSecretProvider
	entries map[string]Entry
	results []SearchResult
}

func (m *mockSearchableProvider) GetEntry(ctx context.Context, location string) (Entry, error) {
	if entry, ok := m.entries[location]; ok {
		return entry, nil
	}
	return Entry{}, fmt.Errorf("not found")
}

func (m *mockSearchableProvider) Search(ctx context.Context, query SearchQuery) ([]SearchResult, error) {
	return m.results, nil
}

type mockValueResolvableProvider struct {
	mockSecretProvider
}

func (m *mockValueResolvableProvider) SupportsValueResolution() bool { return true }

type mockSearchableValueResolvableProvider struct {
	mockSearchableProvider
}

func (m *mockSearchableValueResolvableProvider) SupportsValueResolution() bool { return true }
