package provider

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"time"

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

	// 2b. URLEntry (Full database URI with userinfo, port, path, query, fragment)
	urlEntry := makeEntry("", "URLEntry", "admin", "secret123", "postgres://admin:secret123@10.0.0.1:5432/mydb?key=val&flag=1#section2", "", map[string]string{
		"Scm":           "{URL:SCM}",
		"Scheme":        "{URL:SCHEME}",
		"Host":          "{URL:HOST}",
		"Port":          "{URL:PORT}",
		"Path":          "{URL:PATH}",
		"Query":         "{URL:QUERY}",
		"Fragment":      "{URL:FRAGMENT}",
		"UserInfo":      "{URL:USERINFO}",
		"UserNameURL":   "{URL:USERNAME}",
		"PasswordURL":   "{URL:PASSWORD}",
		"RmvScm":        "{URL:RMVSCM}",
		"WithoutScheme": "{URL:WITHOUTSCHEME}",
		"TConvScheme":   "{T-CONV:/{URL:SCHEME}/Upper/}",
		"TConvHost":     "{T-CONV:/{URL:HOST}/Base64/}",
	})
	rootGroup.Entries = append(rootGroup.Entries, urlEntry)

	// 2c. IPv6URLEntry
	ipv6Entry := makeEntry("", "IPv6URLEntry", "user", "pass", "http://[2001:db8::1]:8080/path", "", map[string]string{
		"Host": "{URL:HOST}",
		"Port": "{URL:PORT}",
	})
	rootGroup.Entries = append(rootGroup.Entries, ipv6Entry)

	// 2d. EmptyURLEntry
	emptyURLEntry := makeEntry("", "EmptyURLEntry", "user", "pass", "", "", map[string]string{
		"Host": "{URL:HOST}",
		"Port": "{URL:PORT}",
	})
	rootGroup.Entries = append(rootGroup.Entries, emptyURLEntry)

	// 2e. InvalidURLEntry
	invalidURLEntry := makeEntry("", "InvalidURLEntry", "user", "pass", "http://invalid url: %%%", "", map[string]string{
		"Host": "{URL:HOST}",
	})
	rootGroup.Entries = append(rootGroup.Entries, invalidURLEntry)

	// 2f. DynamicURLEntry (URL composed of other placeholders)
	dynamicURLEntry := makeEntry("", "DynamicURLEntry", "dbuser", "dbpass", "postgres://{USERNAME}:{PASSWORD}@{S:ServerHost}:5432/appdb", "", map[string]string{
		"ServerHost":    "db.internal",
		"ExtractedHost": "{URL:HOST}",
		"ExtractedUser": "{URL:USERNAME}",
		"ExtractedPass": "{URL:PASSWORD}",
		"ExtractedPort": "{URL:PORT}",
		"ExtractedPath": "{URL:PATH}",
	})
	rootGroup.Entries = append(rootGroup.Entries, dynamicURLEntry)

	// 2g. SchemeLessURLEntry (URL without scheme and unrecognized component fixtures)
	schemeLessURLEntry := makeEntry("", "SchemeLessURLEntry", "user", "pass", "example.com/path", "", map[string]string{
		"Host":          "{URL:HOST}",
		"Unknown":       "{URL:BOGUS}",
		"EmptyComp":     "{URL:}",
		"RmvScm":        "{URL:RMVSCM}",
		"WithoutScheme": "{URL:WITHOUTSCHEME}",
	})
	rootGroup.Entries = append(rootGroup.Entries, schemeLessURLEntry)

	// 2h. MixedCaseURLEntry (URL with mixed-case scheme to verify EqualFold)
	mixedCaseURLEntry := makeEntry("", "MixedCaseURLEntry", "user", "pass", "HTTPS://example.com/path", "", map[string]string{
		"RmvScm": "{URL:RMVSCM}",
	})
	rootGroup.Entries = append(rootGroup.Entries, mixedCaseURLEntry)

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
		"CrossURLHost":         "{REF:A:HOST@T:URLEntry}",
		"CrossURLPort":         "{REF:URL:PORT@T:URLEntry}",
		"CrossURLScheme":       "{REF:URL:SCHEME@T:URLEntry}",
		"CrossURLEmptyComp":    "{REF:URL:@T:URLEntry}",
		"CrossAEmptyComp":      "{REF:A:@T:URLEntry}",
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

	// 6. TextConversions
	tconvEntry := makeEntry("", "TextConversions", "alice_user", "SecretP@ss123!", "", "", map[string]string{
		"Upper":           "{T-CONV:/{USERNAME}/UPPER/}",
		"UpperShort":      "{T-CONV:/{USERNAME}/U/}",
		"Lower":           "{T-CONV:/SECRET_VALUE/LOWER/}",
		"LowerShort":      "{T-CONV:/SECRET_VALUE/L/}",
		"Base64":          "{T-CONV:/{USERNAME}:{PASSWORD}/BASE64/}",
		"Hex":             "{T-CONV:/hello world/HEX/}",
		"URI":             "{T-CONV:/hello world & foo=bar/URI/}",
		"URIDec":          "{T-CONV:/hello+world+%26+foo%3Dbar/URI-DEC/}",
		"Raw":             "{T-CONV:/{USERNAME}/RAW/}",
		"Nested":          "{T-CONV:/{T-CONV:/{USERNAME}/UPPER/}/BASE64/}",
		"CrossEntryTConv": "{T-CONV:/{REF:U@T:TargetEntry}:{REF:P@T:TargetEntry}/BASE64/}",
		"CustomSep":       "{T-CONV:!{USERNAME}!UPPER!}",
		"EmptyString":     "{T-CONV://UPPER/}",
		"SpecialChars":    "{T-CONV:/Special!@#$%^*()_+/BASE64/}",
		"UnknownMethod":   "{T-CONV:/text/UNKNOWN/}",
		"InvalidURIDec":   "{T-CONV:/%zz/URI-DEC/}",
		"ChainedTConv1":   "{T-CONV:/{USERNAME}/UPPER/}",
		"ChainedTConv2":   "{T-CONV:/{S:ChainedTConv1}/LOWER/}",
	})
	rootGroup.Entries = append(rootGroup.Entries, tconvEntry)

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

func TestKeePassReferences_DateTimePlaceholders(t *testing.T) {
	ctx := context.Background()

	// Fixed time: Oct 13, 2026, 16:04:05 in UTC+5 timezone (single-digit month 10? no, 10 is 2 digits.
	// Let's use May 3, 2026, 04:05:06 to test zero-padding for single-digit month, day, hour, min, sec!)
	// Local: 2026-05-03 04:05:06 +0500 UTC+5
	// UTC:   2026-05-02 23:05:06 +0000 UTC
	fixedLocation := time.FixedZone("UTC+5", 5*3600)
	fixedLocal := time.Date(2026, 5, 3, 4, 5, 6, 0, fixedLocation)

	origNowFunc := nowFunc
	nowFunc = func() time.Time { return fixedLocal }
	t.Cleanup(func() { nowFunc = origNowFunc })

	db := gokeepasslib.NewDatabase()
	rootGroup := gokeepasslib.NewGroup()
	rootGroup.Name = "Root"

	entry := gokeepasslib.NewEntry()
	entry.Values = append(entry.Values,
		gokeepasslib.ValueData{Key: "Title", Value: gokeepasslib.V{Content: "DateTimeTest"}},
		gokeepasslib.ValueData{Key: "DT_SIMPLE", Value: gokeepasslib.V{Content: "{DT_SIMPLE}"}},
		gokeepasslib.ValueData{Key: "DT_YEAR", Value: gokeepasslib.V{Content: "{DT_YEAR}"}},
		gokeepasslib.ValueData{Key: "DT_MONTH", Value: gokeepasslib.V{Content: "{DT_MONTH}"}},
		gokeepasslib.ValueData{Key: "DT_DAY", Value: gokeepasslib.V{Content: "{DT_DAY}"}},
		gokeepasslib.ValueData{Key: "DT_HOUR", Value: gokeepasslib.V{Content: "{DT_HOUR}"}},
		gokeepasslib.ValueData{Key: "DT_MINUTE", Value: gokeepasslib.V{Content: "{DT_MINUTE}"}},
		gokeepasslib.ValueData{Key: "DT_SECOND", Value: gokeepasslib.V{Content: "{DT_SECOND}"}},
		gokeepasslib.ValueData{Key: "DT_UTC_SIMPLE", Value: gokeepasslib.V{Content: "{DT_UTC_SIMPLE}"}},
		gokeepasslib.ValueData{Key: "DT_UTC_YEAR", Value: gokeepasslib.V{Content: "{DT_UTC_YEAR}"}},
		gokeepasslib.ValueData{Key: "DT_UTC_MONTH", Value: gokeepasslib.V{Content: "{DT_UTC_MONTH}"}},
		gokeepasslib.ValueData{Key: "DT_UTC_DAY", Value: gokeepasslib.V{Content: "{DT_UTC_DAY}"}},
		gokeepasslib.ValueData{Key: "DT_UTC_HOUR", Value: gokeepasslib.V{Content: "{DT_UTC_HOUR}"}},
		gokeepasslib.ValueData{Key: "DT_UTC_MINUTE", Value: gokeepasslib.V{Content: "{DT_UTC_MINUTE}"}},
		gokeepasslib.ValueData{Key: "DT_UTC_SECOND", Value: gokeepasslib.V{Content: "{DT_UTC_SECOND}"}},
		gokeepasslib.ValueData{Key: "MixedCase", Value: gokeepasslib.V{Content: "{dt_simple}_{dt_utc_year}"}},
		gokeepasslib.ValueData{Key: "ComposedPath", Value: gokeepasslib.V{Content: "/logs/{DT_YEAR}/{DT_MONTH}/app.log"}},
		gokeepasslib.ValueData{Key: "ComposedSession", Value: gokeepasslib.V{Content: "backup-{DT_SIMPLE}"}},
	)
	rootGroup.Entries = append(rootGroup.Entries, entry)
	db.Content.Root.Groups = []gokeepasslib.Group{rootGroup}

	kp := NewKeePassProvider()
	kp.db = db
	kp.resolveReferences = true

	tests := []struct {
		field string
		want  string
	}{
		{"DT_SIMPLE", "20260503040506"},
		{"DT_YEAR", "2026"},
		{"DT_MONTH", "05"},
		{"DT_DAY", "03"},
		{"DT_HOUR", "04"},
		{"DT_MINUTE", "05"},
		{"DT_SECOND", "06"},
		{"DT_UTC_SIMPLE", "20260502230506"},
		{"DT_UTC_YEAR", "2026"},
		{"DT_UTC_MONTH", "05"},
		{"DT_UTC_DAY", "02"},
		{"DT_UTC_HOUR", "23"},
		{"DT_UTC_MINUTE", "05"},
		{"DT_UTC_SECOND", "06"},
		{"MixedCase", "20260503040506_2026"},
		{"ComposedPath", "/logs/2026/05/app.log"},
		{"ComposedSession", "backup-20260503040506"},
	}

	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			val, err := kp.GetSecret(ctx, "Root/DateTimeTest:"+tt.field)
			if err != nil {
				t.Fatalf("GetSecret(%q) failed: %v", tt.field, err)
			}
			if val != tt.want {
				t.Errorf("GetSecret(%q) = %q, want %q", tt.field, val, tt.want)
			}
		})
	}
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

func TestKeePassReferences_TextConversions(t *testing.T) {
	ctx := context.Background()
	kp := NewKeePassProvider()
	kp.db = createTestDBWithReferences()
	kp.resolveReferences = true

	tests := []struct {
		name     string
		location string
		want     string
	}{
		{"Upper", "Root/TextConversions:Upper", "ALICE_USER"},
		{"UpperShort", "Root/TextConversions:UpperShort", "ALICE_USER"},
		{"Lower", "Root/TextConversions:Lower", "secret_value"},
		{"LowerShort", "Root/TextConversions:LowerShort", "secret_value"},
		{"Base64", "Root/TextConversions:Base64", "YWxpY2VfdXNlcjpTZWNyZXRQQHNzMTIzIQ=="},
		{"Hex", "Root/TextConversions:Hex", "68656c6c6f20776f726c64"},
		{"URI", "Root/TextConversions:URI", "hello+world+%26+foo%3Dbar"},
		{"URIDec", "Root/TextConversions:URIDec", "hello world & foo=bar"},
		{"Raw", "Root/TextConversions:Raw", "alice_user"},
		{"Nested", "Root/TextConversions:Nested", "QUxJQ0VfVVNFUg=="},
		{"CrossEntryTConv", "Root/TextConversions:CrossEntryTConv", "dGFyZ2V0dXNlcjp0YXJnZXRwYXNzd29yZCE="},
		{"CustomSep", "Root/TextConversions:CustomSep", "ALICE_USER"},
		{"EmptyString", "Root/TextConversions:EmptyString", ""},
		{"SpecialChars", "Root/TextConversions:SpecialChars", "U3BlY2lhbCFAIyQlXiooKV8r"},
		{"ChainedTConvFields", "Root/TextConversions:ChainedTConv2", "alice_user"},
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

	t.Run("UnknownMethod", func(t *testing.T) {
		_, err := kp.GetSecret(ctx, "Root/TextConversions:UnknownMethod")
		if err == nil {
			t.Fatal("expected error for unknown T-CONV method, got nil")
		}
		if !strings.Contains(err.Error(), "unknown T-CONV method") {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("InvalidURIDec", func(t *testing.T) {
		_, err := kp.GetSecret(ctx, "Root/TextConversions:InvalidURIDec")
		if err == nil {
			t.Fatal("expected error for invalid URI decode, got nil")
		}
		if !strings.Contains(err.Error(), "failed to URI decode") {
			t.Errorf("unexpected error message: %v", err)
		}
	})
}

func TestKeePassReferences_URLComponents(t *testing.T) {
	ctx := context.Background()
	kp := NewKeePassProvider()
	kp.db = createTestDBWithReferences()
	kp.resolveReferences = true

	tests := []struct {
		name     string
		location string
		want     string
	}{
		{"SCM", "Root/URLEntry:Scm", "postgres"},
		{"SCHEME", "Root/URLEntry:Scheme", "postgres"},
		{"HOST", "Root/URLEntry:Host", "10.0.0.1"},
		{"PORT", "Root/URLEntry:Port", "5432"},
		{"PATH", "Root/URLEntry:Path", "/mydb"},
		{"QUERY", "Root/URLEntry:Query", "key=val&flag=1"},
		{"FRAGMENT", "Root/URLEntry:Fragment", "section2"},
		{"USERINFO", "Root/URLEntry:UserInfo", "admin:secret123"},
		{"USERNAME", "Root/URLEntry:UserNameURL", "admin"},
		{"PASSWORD", "Root/URLEntry:PasswordURL", "secret123"},
		{"RMVSCM", "Root/URLEntry:RmvScm", "//admin:secret123@10.0.0.1:5432/mydb?key=val&flag=1#section2"},
		{"WITHOUTSCHEME", "Root/URLEntry:WithoutScheme", "//admin:secret123@10.0.0.1:5432/mydb?key=val&flag=1#section2"},
		{"TConvScheme", "Root/URLEntry:TConvScheme", "POSTGRES"},
		{"TConvHost", "Root/URLEntry:TConvHost", "MTAuMC4wLjE="},

		// IPv6
		{"IPv6Host", "Root/IPv6URLEntry:Host", "2001:db8::1"},
		{"IPv6Port", "Root/IPv6URLEntry:Port", "8080"},

		// Empty URL
		{"EmptyURLHost", "Root/EmptyURLEntry:Host", ""},
		{"EmptyURLPort", "Root/EmptyURLEntry:Port", ""},

		// Invalid URL
		{"InvalidURLHost", "Root/InvalidURLEntry:Host", ""},

		// Dynamic URL (built from other placeholders)
		{"DynamicHost", "Root/DynamicURLEntry:ExtractedHost", "db.internal"},
		{"DynamicUser", "Root/DynamicURLEntry:ExtractedUser", "dbuser"},
		{"DynamicPass", "Root/DynamicURLEntry:ExtractedPass", "dbpass"},
		{"DynamicPort", "Root/DynamicURLEntry:ExtractedPort", "5432"},
		{"DynamicPath", "Root/DynamicURLEntry:ExtractedPath", "/appdb"},

		// Cross-entry URL component references
		{"CrossURLHost", "Root/CrossReferences:CrossURLHost", "10.0.0.1"},
		{"CrossURLPort", "Root/CrossReferences:CrossURLPort", "5432"},
		{"CrossURLScheme", "Root/CrossReferences:CrossURLScheme", "postgres"},
		{"CrossURLEmptyComp", "Root/CrossReferences:CrossURLEmptyComp", ""},
		{"CrossAEmptyComp", "Root/CrossReferences:CrossAEmptyComp", ""},

		// Scheme-less & unrecognized component fallbacks
		{"SchemeLessHost", "Root/SchemeLessURLEntry:Host", ""},
		{"SchemeLessRmvScm", "Root/SchemeLessURLEntry:RmvScm", "example.com/path"},
		{"SchemeLessWithoutScheme", "Root/SchemeLessURLEntry:WithoutScheme", "example.com/path"},
		{"UnknownComponent", "Root/SchemeLessURLEntry:Unknown", ""},
		{"EmptyComponent", "Root/SchemeLessURLEntry:EmptyComp", ""},
		{"MixedCaseRmvScm", "Root/MixedCaseURLEntry:RmvScm", "//example.com/path"},
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
