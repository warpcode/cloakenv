package provider

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/tobischo/gokeepasslib/v3"
)

func createTestDBWithReferences() *gokeepasslib.Database {
	db := gokeepasslib.NewDatabase()
	rootGroup := gokeepasslib.NewGroup()
	rootGroup.Name = "Root"

	// Helper to create an entry
	makeEntry := func(uuidHex, title, username, password, url, notes string, custom map[string]string) gokeepasslib.Entry {
		entry := gokeepasslib.NewEntry()
		if uuidHex != "" {
			b, _ := hex.DecodeString(strings.ReplaceAll(uuidHex, "-", ""))
			copy(entry.UUID[:], b)
		}
		entry.Values = append(entry.Values,
			gokeepasslib.ValueData{Key: "Title", Value: gokeepasslib.V{Content: title}},
			gokeepasslib.ValueData{Key: "UserName", Value: gokeepasslib.V{Content: username}},
			gokeepasslib.ValueData{Key: "Password", Value: gokeepasslib.V{Content: password}},
			gokeepasslib.ValueData{Key: "URL", Value: gokeepasslib.V{Content: url}},
			gokeepasslib.ValueData{Key: "Notes", Value: gokeepasslib.V{Content: notes}},
		)
		for k, v := range custom {
			entry.Values = append(entry.Values, gokeepasslib.ValueData{Key: k, Value: gokeepasslib.V{Content: v}})
		}
		return entry
	}

	// 1. TargetEntry
	targetUUID := "102030405060708090a0b0c0d0e0f001"
	targetEntry := makeEntry(targetUUID, "TargetEntry", "targetuser", "targetpassword!", "https://target.com", "Target notes", map[string]string{
		"TargetCustom": "targetCustomValue",
		"OtherField":   "unique_other_value",
	})
	rootGroup.Entries = append(rootGroup.Entries, targetEntry)

	// 2. LocalPlaceholders
	localEntry := makeEntry("", "LocalPlaceholders", "{TITLE}_user", "pass_{USERNAME}", "http://{S:CustomAttr}.example.com", "User:{USERNAME},Pass:{PASSWORD}", map[string]string{
		"CustomAttr":  "localCustom",
		"RefCustom":   "{S:CustomAttr}_extended",
		"LowerCustom": "{s:CustomAttr}_extended",
	})
	rootGroup.Entries = append(rootGroup.Entries, localEntry)

	// 3. CrossReferences
	crossEntry := makeEntry("", "CrossReferences", "crossuser", "crosspass", "", "", map[string]string{
		"PasswordByTitle":      "{REF:P@T:TargetEntry}",
		"UsernameByTitle":      "{REF:U@T:TargetEntry}",
		"TitleByTitle":         "{REF:T@T:TargetEntry}",
		"URLByTitle":           "{REF:A@T:TargetEntry}",
		"NotesByTitle":         "{REF:N@T:TargetEntry}",
		"CustomByTitle":        "{REF:S:TargetCustom@T:TargetEntry}",
		"ByUUID":               "{REF:P@I:102030405060708090a0b0c0d0e0f001}",
		"ByUUIDWithHyphens":    "{REF:P@I:10203040-5060-7080-90a0-b0c0d0e0f001}",
		"SubstringMatch":       "{REF:P@T:Target}",
		"MatchByUsername":      "{REF:P@U:targetuser}",
		"MatchByPassword":      "{REF:U@P:targetpassword!}",
		"MatchByURL":           "{REF:P@A:https://target.com}",
		"MatchByNotes":         "{REF:P@N:Target notes}",
		"MatchByOther":         "{REF:P@O:unique_other_value}",
		"LowerCaseRef":         "{ref:p@t:TargetEntry}",
		"LowerCasePlaceholder": "{s:TargetCustom}",
		"NonExistentEntry":     "{REF:P@T:DoesNotExist}",
		"NonExistentField":     "{REF:S:DoesNotExist@T:TargetEntry}",
		"Compound":             "https://{REF:U@T:TargetEntry}:{REF:P@T:TargetEntry}@{REF:S:TargetCustom@T:TargetEntry}",
	})
	rootGroup.Entries = append(rootGroup.Entries, crossEntry)

	// 4. Circular references
	circA := makeEntry("a0000000000000000000000000000001", "CircularA", "usera", "{REF:P@T:CircularB}", "", "", nil)
	circB := makeEntry("b0000000000000000000000000000002", "CircularB", "userb", "{REF:P@T:CircularA}", "", "", nil)
	selfCirc := makeEntry("c0000000000000000000000000000003", "SelfCircular", "userc", "{PASSWORD}", "", "", nil)
	rootGroup.Entries = append(rootGroup.Entries, circA, circB, selfCirc)

	// 5. Deep recursion chain (11 steps: Deep0 -> Deep1 -> ... -> Deep11)
	for i := range 12 {
		nextVal := "final_secret"
		if i < 11 {
			nextVal = fmt.Sprintf("{REF:P@T:Deep%d}", i+1)
		}
		e := makeEntry(fmt.Sprintf("d00000000000000000000000000000%02d", i), fmt.Sprintf("Deep%d", i), "user", nextVal, "", "", nil)
		rootGroup.Entries = append(rootGroup.Entries, e)
	}

	db.Content.Root.Groups = []gokeepasslib.Group{rootGroup}
	return db
}

func TestKeePassReferences_Disabled(t *testing.T) {
	ctx := context.Background()
	kp := NewKeePassProvider()
	kp.db = createTestDBWithReferences()
	kp.resolveReferences = false

	// When disabled, raw strings should be returned unmodified
	val, err := kp.GetSecret(ctx, "Root/CrossReferences:PasswordByTitle")
	if err != nil {
		t.Fatalf("GetSecret failed: %v", err)
	}
	want := "{REF:P@T:TargetEntry}"
	if val != want {
		t.Errorf("GetSecret when disabled = %q, want %q", val, want)
	}

	entry, err := kp.GetEntry(ctx, "Root/LocalPlaceholders")
	if err != nil {
		t.Fatalf("GetEntry failed: %v", err)
	}
	if entry.Attributes["UserName"] != "{TITLE}_user" {
		t.Errorf("GetEntry attribute when disabled = %q, want %q", entry.Attributes["UserName"], "{TITLE}_user")
	}
}

func TestKeePassReferences_SameEntryPlaceholders(t *testing.T) {
	ctx := context.Background()
	kp := NewKeePassProvider()
	kp.db = createTestDBWithReferences()
	kp.resolveReferences = true

	tests := []struct {
		name     string
		location string
		want     string
	}{
		{"TitleInUsername", "Root/LocalPlaceholders:UserName", "LocalPlaceholders_user"},
		{"NestedUsernameInPassword", "Root/LocalPlaceholders:Password", "pass_LocalPlaceholders_user"},
		{"CustomAttrInURL", "Root/LocalPlaceholders:URL", "http://localCustom.example.com"},
		{"MultiplePlaceholdersInNotes", "Root/LocalPlaceholders:Notes", "User:LocalPlaceholders_user,Pass:pass_LocalPlaceholders_user"},
		{"CustomAttrInCustomAttr", "Root/LocalPlaceholders:RefCustom", "localCustom_extended"},
		{"LowerCustomPlaceholder", "Root/LocalPlaceholders:LowerCustom", "localCustom_extended"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, err := kp.GetSecret(ctx, tt.location)
			if err != nil {
				t.Fatalf("GetSecret(%q) failed: %v", tt.location, err)
			}
			if val != tt.want {
				t.Errorf("GetSecret(%q) = %q, want %q", tt.location, val, tt.want)
			}
		})
	}
}

func TestKeePassReferences_CrossEntryReferences(t *testing.T) {
	ctx := context.Background()
	kp := NewKeePassProvider()
	kp.db = createTestDBWithReferences()
	kp.resolveReferences = true

	tests := []struct {
		name     string
		location string
		want     string
	}{
		{"PasswordByTitle", "Root/CrossReferences:PasswordByTitle", "targetpassword!"},
		{"UsernameByTitle", "Root/CrossReferences:UsernameByTitle", "targetuser"},
		{"TitleByTitle", "Root/CrossReferences:TitleByTitle", "TargetEntry"},
		{"URLByTitle", "Root/CrossReferences:URLByTitle", "https://target.com"},
		{"NotesByTitle", "Root/CrossReferences:NotesByTitle", "Target notes"},
		{"CustomByTitle", "Root/CrossReferences:CustomByTitle", "targetCustomValue"},
		{"ByUUID", "Root/CrossReferences:ByUUID", "targetpassword!"},
		{"ByUUIDWithHyphens", "Root/CrossReferences:ByUUIDWithHyphens", "targetpassword!"},
		{"SubstringMatch", "Root/CrossReferences:SubstringMatch", "targetpassword!"},
		{"MatchByUsername", "Root/CrossReferences:MatchByUsername", "targetpassword!"},
		{"MatchByPassword", "Root/CrossReferences:MatchByPassword", "targetuser"},
		{"MatchByURL", "Root/CrossReferences:MatchByURL", "targetpassword!"},
		{"MatchByNotes", "Root/CrossReferences:MatchByNotes", "targetpassword!"},
		{"MatchByOther", "Root/CrossReferences:MatchByOther", "targetpassword!"},
		{"LowerCaseRef", "Root/CrossReferences:LowerCaseRef", "targetpassword!"},
		{"LowerCasePlaceholder", "Root/CrossReferences:LowerCasePlaceholder", ""},
		{"CompoundReference", "Root/CrossReferences:Compound", "https://targetuser:targetpassword!@targetCustomValue"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, err := kp.GetSecret(ctx, tt.location)
			if err != nil {
				t.Fatalf("GetSecret(%q) failed: %v", tt.location, err)
			}
			if val != tt.want {
				t.Errorf("GetSecret(%q) = %q, want %q", tt.location, val, tt.want)
			}
		})
	}
}

func TestKeePassReferences_MissingEntryAndField(t *testing.T) {
	ctx := context.Background()
	kp := NewKeePassProvider()
	kp.db = createTestDBWithReferences()
	kp.resolveReferences = true

	t.Run("NonExistentEntry", func(t *testing.T) {
		_, err := kp.GetSecret(ctx, "Root/CrossReferences:NonExistentEntry")
		if err == nil {
			t.Fatal("expected error for non-existent entry reference, got nil")
		}
		if !strings.Contains(err.Error(), "entry matching T:\"DoesNotExist\" not found") {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("NonExistentField", func(t *testing.T) {
		val, err := kp.GetSecret(ctx, "Root/CrossReferences:NonExistentField")
		if err != nil {
			t.Fatalf("unexpected error for non-existent field on valid entry: %v", err)
		}
		// Unpopulated/missing fields resolve to "" in KeePass
		if val != "" {
			t.Errorf("expected empty string for non-existent field, got %q", val)
		}
	})
}

func TestKeePassReferences_CircularReference(t *testing.T) {
	ctx := context.Background()
	kp := NewKeePassProvider()
	kp.db = createTestDBWithReferences()
	kp.resolveReferences = true

	t.Run("MutualCircular", func(t *testing.T) {
		_, err := kp.GetSecret(ctx, "Root/CircularA:Password")
		if err == nil {
			t.Fatal("expected error for circular reference, got nil")
		}
		if !strings.Contains(err.Error(), "circular reference detected") {
			t.Errorf("expected 'circular reference detected' in error, got: %v", err)
		}
	})

	t.Run("SelfCircular", func(t *testing.T) {
		_, err := kp.GetSecret(ctx, "Root/SelfCircular:Password")
		if err == nil {
			t.Fatal("expected error for self-referential field, got nil")
		}
		if !strings.Contains(err.Error(), "circular reference detected") {
			t.Errorf("expected 'circular reference detected' in error, got: %v", err)
		}
	})
}

func TestKeePassReferences_MaxDepthLimit(t *testing.T) {
	ctx := context.Background()
	kp := NewKeePassProvider()
	kp.db = createTestDBWithReferences()
	kp.resolveReferences = true

	_, err := kp.GetSecret(ctx, "Root/Deep0:Password")
	if err == nil {
		t.Fatal("expected error for exceeding max recursion depth, got nil")
	}
	if !strings.Contains(err.Error(), "maximum recursion depth (10) exceeded") {
		t.Errorf("expected 'maximum recursion depth (10) exceeded' in error, got: %v", err)
	}
}

func TestKeePassReferences_GetEntryAndSearch(t *testing.T) {
	ctx := context.Background()
	kp := NewKeePassProvider()
	kp.db = createTestDBWithReferences()
	kp.resolveReferences = true

	t.Run("GetEntry", func(t *testing.T) {
		entry, err := kp.GetEntry(ctx, "Root/LocalPlaceholders")
		if err != nil {
			t.Fatalf("GetEntry failed: %v", err)
		}
		if entry.Attributes["UserName"] != "LocalPlaceholders_user" {
			t.Errorf("expected resolved UserName 'LocalPlaceholders_user', got %q", entry.Attributes["UserName"])
		}
		if entry.Attributes["Password"] != "pass_LocalPlaceholders_user" {
			t.Errorf("expected resolved Password 'pass_LocalPlaceholders_user', got %q", entry.Attributes["Password"])
		}
	})

	t.Run("Search", func(t *testing.T) {
		results, err := kp.Search(ctx, SearchQuery{Title: "LocalPlaceholders"})
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		if len(results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(results))
		}
		if results[0].Entry.Attributes["UserName"] != "LocalPlaceholders_user" {
			t.Errorf("expected resolved UserName in Search result, got %q", results[0].Entry.Attributes["UserName"])
		}
	})
}
