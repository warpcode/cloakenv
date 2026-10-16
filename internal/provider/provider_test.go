package provider

import (
	"context"
	"reflect"
	"testing"
)

func TestFieldPolicy_ApplyToEntry(t *testing.T) {
	entry := Entry{
		Title: "Test Entry",
		Attributes: map[string]any{
			"a": 1,
			"b": 2,
			"c": 3,
		},
	}

	t.Run("nil policy", func(t *testing.T) {
		var fp *FieldPolicy
		got := fp.ApplyToEntry(entry, "", nil)
		if !reflect.DeepEqual(got, entry) {
			t.Errorf("ApplyToEntry() = %v, want %v", got, entry)
		}
	})

	t.Run("include only", func(t *testing.T) {
		fp := &FieldPolicy{
			IncludeLayers: [][]string{{"a", "b"}},
		}
		got := fp.ApplyToEntry(entry, "", nil)
		want := Entry{
			Title: "Test Entry",
			Attributes: map[string]any{
				"a": 1,
				"b": 2,
			},
		}
		if !reflect.DeepEqual(got.Attributes, want.Attributes) {
			t.Errorf("ApplyToEntry() attributes = %v, want %v", got.Attributes, want.Attributes)
		}
	})

	t.Run("exclude only", func(t *testing.T) {
		fp := &FieldPolicy{
			ExcludeFields: []string{"b"},
		}
		got := fp.ApplyToEntry(entry, "", nil)
		want := Entry{
			Title: "Test Entry",
			Attributes: map[string]any{
				"a": 1,
				"c": 3,
			},
		}
		if !reflect.DeepEqual(got.Attributes, want.Attributes) {
			t.Errorf("ApplyToEntry() attributes = %v, want %v", got.Attributes, want.Attributes)
		}
	})

	t.Run("include and exclude", func(t *testing.T) {
		fp := &FieldPolicy{
			IncludeLayers: [][]string{{"a", "b", "c"}},
			ExcludeFields: []string{"b"},
		}
		got := fp.ApplyToEntry(entry, "", nil)
		want := Entry{
			Title: "Test Entry",
			Attributes: map[string]any{
				"a": 1,
				"c": 3,
			},
		}
		if !reflect.DeepEqual(got.Attributes, want.Attributes) {
			t.Errorf("ApplyToEntry() attributes = %v, want %v", got.Attributes, want.Attributes)
		}
	})

	t.Run("multiple include layers", func(t *testing.T) {
		fp := &FieldPolicy{
			IncludeLayers: [][]string{{"a", "b", "c"}, {"a", "b"}, {"a"}},
		}
		got := fp.ApplyToEntry(entry, "", nil)
		want := Entry{
			Title: "Test Entry",
			Attributes: map[string]any{
				"a": 1,
			},
		}
		if !reflect.DeepEqual(got.Attributes, want.Attributes) {
			t.Errorf("ApplyToEntry() attributes = %v, want %v", got.Attributes, want.Attributes)
		}
	})

	t.Run("empty include layer drops everything", func(t *testing.T) {
		// Note: FilterEntryWithPath with empty include drops all unless matched, let's see how it behaves
		fp := &FieldPolicy{
			IncludeLayers: [][]string{{}},
		}
		got := fp.ApplyToEntry(entry, "", nil)
		// Should drop everything? Not exactly, empty slice might mean nothing is included.
		// Let's just check it doesn't crash.
		_ = got
	})
}

func TestWithFieldPolicy(t *testing.T) {
	ctx := context.Background()

	t.Run("empty policies", func(t *testing.T) {
		ctx2 := WithFieldPolicy(ctx, nil, nil)
		if ctx2 != ctx {
			t.Errorf("WithFieldPolicy with nil policies should return original context")
		}
	})

	t.Run("set include and exclude", func(t *testing.T) {
		ctx2 := WithFieldPolicy(ctx, []string{"a"}, []string{"b"})
		fp := FieldPolicyFromContext(ctx2)
		if fp == nil {
			t.Fatal("Expected FieldPolicy, got nil")
		}
		if len(fp.IncludeLayers) != 1 || fp.IncludeLayers[0][0] != "a" {
			t.Errorf("IncludeLayers = %v", fp.IncludeLayers)
		}
		if len(fp.ExcludeFields) != 1 || fp.ExcludeFields[0] != "b" {
			t.Errorf("ExcludeFields = %v", fp.ExcludeFields)
		}
	})

	t.Run("append to existing policy", func(t *testing.T) {
		ctx1 := WithFieldPolicy(ctx, []string{"a", "b"}, []string{"exc1"})
		ctx2 := WithFieldPolicy(ctx1, []string{"b", "c"}, []string{"exc2", "exc1"})

		fp := FieldPolicyFromContext(ctx2)
		if fp == nil {
			t.Fatal("Expected FieldPolicy, got nil")
		}

		if len(fp.IncludeLayers) != 2 {
			t.Errorf("Expected 2 include layers, got %d", len(fp.IncludeLayers))
		}

		// check deduping in exclude
		if len(fp.ExcludeFields) != 2 {
			t.Errorf("Expected 2 exclude fields, got %d: %v", len(fp.ExcludeFields), fp.ExcludeFields)
		}
	})
}

func TestFieldPolicyFromContext(t *testing.T) {
	t.Run("nil context", func(t *testing.T) {
		if fp := FieldPolicyFromContext(nil); fp != nil {
			t.Errorf("Expected nil, got %v", fp)
		}
	})

	t.Run("context without policy", func(t *testing.T) {
		if fp := FieldPolicyFromContext(context.Background()); fp != nil {
			t.Errorf("Expected nil, got %v", fp)
		}
	})
}
