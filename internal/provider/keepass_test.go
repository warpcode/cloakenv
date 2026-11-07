package provider

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tobischo/gokeepasslib/v3"
	"github.com/zalando/go-keyring"

	"github.com/warpcode/cloakenv/internal/utils"
)

func TestKeePassProvider(t *testing.T) {
	keyring.MockInit()
	ctx := context.Background()

	t.Run("Scheme", func(t *testing.T) {
		kp := NewKeePassProvider()
		if kp.Scheme() != "keepass" {
			t.Errorf("expected scheme 'keepass', got %q", kp.Scheme())
		}
	})

	t.Run("Validate", func(t *testing.T) {
		kp := NewKeePassProvider()

		tests := []struct {
			name     string
			settings map[string]string
			wantErr  bool
		}{
			{
				name: "valid vault_path",
				settings: map[string]string{
					"vault_path": "/path/to/vault.kdbx",
				},
				wantErr: false,
			},
			{
				name: "empty vault_path",
				settings: map[string]string{
					"vault_path": "",
				},
				wantErr: true,
			},
			{
				name:     "missing vault_path",
				settings: map[string]string{},
				wantErr:  true,
			},
			{
				name:     "nil settings",
				settings: nil,
				wantErr:  true,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				err := kp.Validate(tt.settings)
				if (err != nil) != tt.wantErr {
					t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
				}
			})
		}
	})

	t.Run("Initialize_NoCredentials", func(t *testing.T) {
		kp := NewKeePassProvider()
		cfg := ProviderConfig{
			Settings: map[string]string{
				"vault_path":  "../../testdata/testDB.kdbx",
				"remote_name": "testdb",
			},
		}
		if err := kp.Initialize(ctx, cfg); err == nil {
			t.Errorf("expected Initialize to fail without stored keyring credentials")
		}
	})

	// Setup keyring credentials and initialize the shared provider for subsequent tests
	// prefix: cloakenv, account: provider/testdb, password: password123
	if err := keyring.Set("cloakenv", "provider/testdb", "password123"); err != nil {
		t.Fatalf("failed to set mock credentials: %v", err)
	}

	kp := NewKeePassProvider()
	cfg := ProviderConfig{
		Settings: map[string]string{
			"vault_path":  "../../testdata/testDB.kdbx",
			"remote_name": "testdb",
		},
	}
	if err := kp.Initialize(ctx, cfg); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	t.Run("Initialize_CredentialsCleared", func(t *testing.T) {
		// Test credentials deletion in memory
		if kp.db.Credentials != nil {
			t.Errorf("expected kp.db.Credentials to be nil after unlock")
		}
	})

	t.Run("GetEntryTitle", func(t *testing.T) {
		title, err := kp.GetEntryTitle(ctx, "website/Test Website")
		if err != nil {
			t.Fatalf("GetEntryTitle failed: %v", err)
		}
		if title != "Test Website" {
			t.Errorf("GetEntryTitle = %q, want %q", title, "Test Website")
		}

		_, err = kp.GetEntryTitle(ctx, "nonexistent/entry")
		if err == nil {
			t.Errorf("expected error for nonexistent entry, got nil")
		}

		uninitKP := NewKeePassProvider()
		_, err = uninitKP.GetEntryTitle(ctx, "website/Test Website")
		if err == nil {
			t.Errorf("expected error on uninitialized KeePassProvider, got nil")
		}
	})

	t.Run("GetSecret", func(t *testing.T) {
		tests := []struct {
			name     string
			location string
			expected string
		}{
			{"DefaultPassword", "website/Test Website", "testPassword123!"},
			{"UserNameAttribute", "website/Test Website:UserName", "user@email.com"},
			{"FileAttachment", "website/Test Website:hello.txt", "Hello, World! Extraction Test Successful."},
			{"RootPrefix", "Root/website/Test Website", "testPassword123!"},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				val, err := kp.GetSecret(ctx, tt.location)
				if err != nil {
					t.Errorf("GetSecret failed: %v", err)
				}
				if val != tt.expected {
					t.Errorf("expected %q, got %q", tt.expected, val)
				}
			})
		}

		// Virtual metadata attribute tests
		entry, err := kp.GetEntry(ctx, "website/Test Website")
		if err != nil {
			t.Fatalf("failed to retrieve entry for GetSecret test: %v", err)
		}
		uuidVal, err := kp.GetSecret(ctx, "website/Test Website:UUID")
		if err != nil || uuidVal != entry.UUID {
			t.Errorf("GetSecret :UUID = %q, want %q, err: %v", uuidVal, entry.UUID, err)
		}
		uuidLowerVal, err := kp.GetSecret(ctx, "website/Test Website:uuid")
		if err != nil || uuidLowerVal != entry.UUID {
			t.Errorf("GetSecret :uuid = %q, want %q, err: %v", uuidLowerVal, entry.UUID, err)
		}
		grpVal, err := kp.GetSecret(ctx, "website/Test Website:group")
		if err != nil || grpVal != "website" {
			t.Errorf("GetSecret :group = %q, want 'website', err: %v", grpVal, err)
		}
		grpNameVal, err := kp.GetSecret(ctx, "website/Test Website:group_name")
		if err != nil || grpNameVal != "website" {
			t.Errorf("GetSecret :group_name = %q, want 'website', err: %v", grpNameVal, err)
		}
		grpUUIDVal, err := kp.GetSecret(ctx, "website/Test Website:group_uuid")
		if err != nil || grpUUIDVal != entry.Group.UUID {
			t.Errorf("GetSecret :group_uuid = %q, want %q, err: %v", grpUUIDVal, entry.Group.UUID, err)
		}
		grpPathVal, err := kp.GetSecret(ctx, "website/Test Website:group_path")
		if err != nil || grpPathVal != "website" {
			t.Errorf("GetSecret :group_path = %q, want 'website', err: %v", grpPathVal, err)
		}
	})

	t.Run("GetEntry", func(t *testing.T) {
		entry, err := kp.GetEntry(ctx, "website/Test Website")
		if err != nil {
			t.Fatalf("GetEntry failed: %v", err)
		}
		if entry.Title != "Test Website" {
			t.Errorf("expected title 'Test Website', got %q", entry.Title)
		}
		if len(entry.UUID) != 32 {
			t.Errorf("expected 32-character hex UUID, got %q", entry.UUID)
		}
		if entry.Group == nil {
			t.Fatalf("expected non-nil Group")
		}
		if entry.Group.Name != "website" {
			t.Errorf("expected group name 'website', got %q", entry.Group.Name)
		}
		if entry.Group.Path != "website" {
			t.Errorf("expected group path 'website', got %q", entry.Group.Path)
		}
		if len(entry.Group.UUID) != 32 {
			t.Errorf("expected 32-character hex Group UUID, got %q", entry.Group.UUID)
		}
	})

	t.Run("Search", func(t *testing.T) {
		tests := []struct {
			name          string
			query         SearchQuery
			wantCount     int
			wantFirstPath string
		}{
			{
				name:          "ByTitle",
				query:         SearchQuery{Title: "Test Website"},
				wantCount:     1,
				wantFirstPath: "website/Test Website",
			},
			{
				name:      "ByPath",
				query:     SearchQuery{Path: "website/Test"},
				wantCount: 1,
			},
			{
				name:      "NoMatch",
				query:     SearchQuery{Title: "NonExistentTitleItem12345"},
				wantCount: 0,
			},
			{
				name:      "NonExistentTag",
				query:     SearchQuery{Tags: []string{"nonexistenttag"}},
				wantCount: 0,
			},
			{
				name:          "EmptyTagFilter",
				query:         SearchQuery{Title: "Test Website", Tags: nil},
				wantCount:     1,
				wantFirstPath: "website/Test Website",
			},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				results, err := kp.Search(ctx, tc.query)
				if err != nil {
					t.Fatalf("Search failed: %v", err)
				}
				if len(results) != tc.wantCount {
					t.Fatalf("expected %d result(s), got %d", tc.wantCount, len(results))
				}
				if tc.wantFirstPath != "" && results[0].Path != tc.wantFirstPath {
					t.Errorf("expected path %q, got %q", tc.wantFirstPath, results[0].Path)
				}
				if tc.wantCount > 0 {
					if len(results[0].Entry.UUID) != 32 {
						t.Errorf("expected 32-character hex UUID in search result, got %q", results[0].Entry.UUID)
					}
					if results[0].Entry.Group == nil || results[0].Entry.Group.Name == "" {
						t.Errorf("expected populated Group in search result, got %+v", results[0].Entry.Group)
					}
				}
			})
		}
	})

	t.Run("ReadOnly", func(t *testing.T) {
		if err := kp.SetSecret(ctx, "a", "b"); err == nil {
			t.Errorf("expected SetSecret to fail")
		} else if !strings.Contains(err.Error(), "read-only") {
			t.Errorf("expected error to contain 'read-only', got %q", err.Error())
		}
		if err := kp.DeleteSecret(ctx, "a"); err == nil {
			t.Errorf("expected DeleteSecret to fail")
		} else if !strings.Contains(err.Error(), "read-only") {
			t.Errorf("expected error to contain 'read-only', got %q", err.Error())
		}
	})

	t.Run("VerifyDatabaseEntriesAndTags", func(t *testing.T) {
		// Validates fixture database entries and tags across all groups,
		// converting diagnostic checks from testdata/test_tags_check.go into an automated test.
		if len(kp.db.Content.Root.Groups) == 0 {
			t.Fatalf("expected at least one root group in test database fixture")
		}

		type entryInfo struct {
			title string
			tags  string
		}

		var collected []entryInfo
		var traverse func(g *gokeepasslib.Group)
		traverse = func(g *gokeepasslib.Group) {
			for _, e := range g.Entries {
				var title string
				for _, v := range e.Values {
					if v.Key == "Title" {
						title = v.Value.Content
						break
					}
				}
				collected = append(collected, entryInfo{
					title: title,
					tags:  e.Tags,
				})
			}
			for i := range g.Groups {
				traverse(&g.Groups[i])
			}
		}

		for i := range kp.db.Content.Root.Groups {
			traverse(&kp.db.Content.Root.Groups[i])
		}

		if len(collected) == 0 {
			t.Fatalf("expected at least one entry in test database fixture, found none")
		}

		foundTestWebsite := false
		for _, info := range collected {
			if info.title == "Test Website" {
				foundTestWebsite = true
				if info.tags != "" {
					t.Errorf("expected entry %q tags to be empty string, got %q", info.title, info.tags)
				}
			}
		}

		if !foundTestWebsite {
			t.Errorf("expected to find entry with title 'Test Website' in test database fixture")
		}
	})
}

func TestKeePassProvider_SetSecret(t *testing.T) {
	kp := NewKeePassProvider()
	err := kp.SetSecret(context.Background(), "KEY", "VAL")
	if err == nil {
		t.Error("expected error for SetSecret, got nil")
	}
	if err != nil && !strings.Contains(err.Error(), "read-only") {
		t.Errorf("expected error to contain 'read-only', got %q", err.Error())
	}
}

func TestKeePassProvider_DeleteSecret(t *testing.T) {
	kp := NewKeePassProvider()
	err := kp.DeleteSecret(context.Background(), "KEY")
	if err == nil {
		t.Error("expected error for DeleteSecret, got nil")
	}
	if err != nil && !strings.Contains(err.Error(), "read-only") {
		t.Errorf("expected error to contain 'read-only', got %q", err.Error())
	}
}

func TestMatchEntryTags(t *testing.T) {
	tests := []struct {
		name      string
		tagString string
		queryTags []string
		want      bool
	}{
		{
			name:      "empty query tags matches anything",
			tagString: "tag1, tag2",
			queryTags: nil,
			want:      true,
		},
		{
			name:      "matching single tag case-insensitive",
			tagString: "Production, Database",
			queryTags: []string{"production"},
			want:      true,
		},
		{
			name:      "matching all query tags",
			tagString: "Production, Database, Web",
			queryTags: []string{"production", "database"},
			want:      true,
		},
		{
			name:      "missing query tag",
			tagString: "Production, Database",
			queryTags: []string{"production", "redis"},
			want:      false,
		},
		{
			name:      "empty entry tags with non-empty query",
			tagString: "",
			queryTags: []string{"production"},
			want:      false,
		},
		{
			name:      "leading non-breaking space is trimmed",
			tagString: " foo",
			queryTags: []string{"foo"},
			want:      true,
		},
		{
			name:      "trailing non-breaking space is trimmed",
			tagString: "foo ",
			queryTags: []string{"foo"},
			want:      true,
		},
		{
			name:      "ideographic space is trimmed",
			tagString: "　foo",
			queryTags: []string{"foo"},
			want:      true,
		},
		{
			name:      "narrow no-break space is trimmed",
			tagString: " foo",
			queryTags: []string{"foo"},
			want:      true,
		},
		{
			name:      "non-breaking space inside tag is not removed",
			tagString: "foo bar",
			queryTags: []string{"foo"},
			want:      false,
		},
		{
			name:      "empty comma segments are ignored",
			tagString: ",foo,,bar,",
			queryTags: []string{"foo", "bar"},
			want:      true,
		},
		{
			name:      "semicolon is not a delimiter",
			tagString: "work;home",
			queryTags: []string{"work;home"},
			want:      true,
		},
		{
			name:      "semicolon is not a delimiter and does not match a fragment",
			tagString: "work;home",
			queryTags: []string{"work"},
			want:      false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := matchEntryTags(tc.tagString, tc.queryTags)
			if got != tc.want {
				t.Errorf("matchEntryTags(%q, %v) = %v, want %v", tc.tagString, tc.queryTags, got, tc.want)
			}
		})
	}
}

// TestMatchEntryTagsAgreesWithParseTagString verifies that search filtering and
// serialization agree on what a tag is. matchEntryTags is used by Search, while
// toEntry serializes tags via utils.ParseTagString. If these disagree, a tag can
// be discoverable via one path and unresolvable via the other.
func TestMatchEntryTagsAgreesWithParseTagString(t *testing.T) {
	tagStrings := []string{
		"Production, Database",
		"  Production  ,  Database  ",
		" Production ",
		"　Production　",
		"Production,　Database",
		"work;home",
		",foo,,bar,",
		"\tProduction\n",
	}
	queries := []string{"production", "database", "foo", "bar", "work", "work;home", "home"}

	for _, ts := range tagStrings {
		parsed := utils.ParseTagString(ts)
		for _, q := range queries {
			t.Run(ts+"/"+q, func(t *testing.T) {
				searchHit := matchEntryTags(ts, []string{q})
				serialized := false
				for _, p := range parsed {
					if strings.EqualFold(p, q) {
						serialized = true
						break
					}
				}
				if searchHit != serialized {
					t.Errorf("disagreement for tagString=%q query=%q: Search matchEntryTags=%v but GetEntry/ParseTagString tags %q contain it=%v",
						ts, q, searchHit, parsed, serialized)
				}
			})
		}
	}
}

func TestKeePassProvider_CustomUUIDAttributePrecedence(t *testing.T) {
	keyring.MockInit()
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("LocalAppData", t.TempDir())

	ctx := context.Background()
	if err := keyring.Set("cloakenv", "provider/testdb_custom_uuid", "password123"); err != nil {
		t.Fatalf("failed to set mock credentials: %v", err)
	}

	kp := NewKeePassProvider()
	cfg := ProviderConfig{
		Settings: map[string]string{
			"vault_path":  "../../testdata/testDB.kdbx",
			"remote_name": "testdb_custom_uuid",
		},
	}
	if err := kp.Initialize(ctx, cfg); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	entry, _, _, _, err := kp.findEntry("website/Test Website")
	if err != nil {
		t.Fatalf("failed to find entry: %v", err)
	}
	origValues := entry.Values
	defer func() {
		entry.Values = origValues
	}()

	entry.Values = append(entry.Values, gokeepasslib.ValueData{
		Key:   "UUID",
		Value: gokeepasslib.V{Content: "custom-override-uuid"},
	})

	// GetSecret for both :UUID and :uuid returns "custom-override-uuid" rather than entry XML UUID
	valUpper, err := kp.GetSecret(ctx, "website/Test Website:UUID")
	if err != nil {
		t.Fatalf("GetSecret :UUID failed: %v", err)
	}
	if valUpper != "custom-override-uuid" {
		t.Errorf("GetSecret :UUID = %q, want 'custom-override-uuid'", valUpper)
	}

	valLower, err := kp.GetSecret(ctx, "website/Test Website:uuid")
	if err != nil {
		t.Fatalf("GetSecret :uuid failed: %v", err)
	}
	if valLower != "custom-override-uuid" {
		t.Errorf("GetSecret :uuid = %q, want 'custom-override-uuid'", valLower)
	}

	// GetEntry still populates entry.UUID with 32-char hex XML UUID and entry.Attributes["UUID"] with "custom-override-uuid"
	e, err := kp.GetEntry(ctx, "website/Test Website")
	if err != nil {
		t.Fatalf("GetEntry failed: %v", err)
	}
	if len(e.UUID) != 32 {
		t.Errorf("expected 32-char hex XML UUID in entry.UUID, got %q", e.UUID)
	}
	if e.UUID == "custom-override-uuid" {
		t.Errorf("e.UUID should be XML UUID, not custom attribute")
	}
	if e.Attributes["UUID"] != "custom-override-uuid" {
		t.Errorf("e.Attributes[\"UUID\"] = %v, want 'custom-override-uuid'", e.Attributes["UUID"])
	}
}

// helper to create a test KeePass database file with given credentials
func createTestKeePassDatabase(t *testing.T, dbPath string, creds *gokeepasslib.DBCredentials) {
	t.Helper()
	f, err := os.Create(dbPath)
	if err != nil {
		t.Fatalf("failed to create db file: %v", err)
	}
	defer func() {
		_ = f.Close()
	}()

	db := gokeepasslib.NewDatabase()
	db.Credentials = creds

	entry := gokeepasslib.NewEntry()
	entry.Values = append(entry.Values, gokeepasslib.ValueData{
		Key:   "Title",
		Value: gokeepasslib.V{Content: "KeyFileEntry"},
	})
	entry.Values = append(entry.Values, gokeepasslib.ValueData{
		Key:   "Password",
		Value: gokeepasslib.V{Content: "KeyFilePass123"},
	})
	entry.Values = append(entry.Values, gokeepasslib.ValueData{
		Key:   "UserName",
		Value: gokeepasslib.V{Content: "keyuser"},
	})

	rootGroup := gokeepasslib.NewGroup()
	rootGroup.Name = "Root"
	rootGroup.Entries = append(rootGroup.Entries, entry)
	db.Content.Root.Groups = []gokeepasslib.Group{rootGroup}

	enc := gokeepasslib.NewEncoder(f)
	if err := enc.Encode(db); err != nil {
		t.Fatalf("failed to encode test db: %v", err)
	}
}

func TestKeePassProvider_KeyFileOnly(t *testing.T) {
	keyring.MockInit()
	ctx := context.Background()
	tempDir := t.TempDir()

	keyFilePath := filepath.Join(tempDir, "vault.key")
	if err := os.WriteFile(keyFilePath, []byte("supersecretrawkeybytes1234567890"), 0600); err != nil {
		t.Fatalf("failed to write key file: %v", err)
	}

	creds, err := gokeepasslib.NewKeyCredentials(keyFilePath)
	if err != nil {
		t.Fatalf("failed to build key credentials: %v", err)
	}

	dbPath := filepath.Join(tempDir, "keyfile_only.kdbx")
	createTestKeePassDatabase(t, dbPath, creds)

	t.Run("UnlockSuccess", func(t *testing.T) {
		kp := NewKeePassProvider()
		cfg := ProviderConfig{
			Settings: map[string]string{
				"vault_path":  dbPath,
				"key_file":    keyFilePath,
				"remote_name": "test_key_only",
			},
		}

		if err := kp.Initialize(ctx, cfg); err != nil {
			t.Fatalf("Initialize failed for key-file only database: %v", err)
		}

		title, err := kp.GetEntryTitle(ctx, "KeyFileEntry")
		if err != nil {
			t.Fatalf("GetEntryTitle failed: %v", err)
		}
		if title != "KeyFileEntry" {
			t.Errorf("expected title 'KeyFileEntry', got %q", title)
		}

		secret, err := kp.GetSecret(ctx, "KeyFileEntry")
		if err != nil {
			t.Fatalf("GetSecret failed: %v", err)
		}
		if secret != "KeyFilePass123" {
			t.Errorf("expected secret 'KeyFilePass123', got %q", secret)
		}
	})

	t.Run("WrongKeyFile", func(t *testing.T) {
		wrongKeyPath := filepath.Join(tempDir, "wrong.key")
		if err := os.WriteFile(wrongKeyPath, []byte("differentkeybytes0987654321"), 0600); err != nil {
			t.Fatalf("failed to write wrong key file: %v", err)
		}

		kp := NewKeePassProvider()
		cfg := ProviderConfig{
			Settings: map[string]string{
				"vault_path":  dbPath,
				"key_file":    wrongKeyPath,
				"remote_name": "test_wrong_key",
			},
		}

		// Since key file unlock fails and no password in keyring, it should return error instructing to log in
		err := kp.Initialize(ctx, cfg)
		if err == nil {
			t.Fatalf("expected Initialize to fail with wrong key file")
		}
	})

	t.Run("MissingKeyFile", func(t *testing.T) {
		kp := NewKeePassProvider()
		cfg := ProviderConfig{
			Settings: map[string]string{
				"vault_path":  dbPath,
				"key_file":    filepath.Join(tempDir, "missing.key"),
				"remote_name": "test_missing_key",
			},
		}

		err := kp.Initialize(ctx, cfg)
		if err == nil {
			t.Fatalf("expected Initialize to fail with missing key file")
		}
	})
}

func TestKeePassProvider_CompositeAuth(t *testing.T) {
	keyring.MockInit()
	ctx := context.Background()
	tempDir := t.TempDir()

	keyFilePath := filepath.Join(tempDir, "composite.key")
	if err := os.WriteFile(keyFilePath, []byte("compositekeyfilecontent!@#$"), 0600); err != nil {
		t.Fatalf("failed to write key file: %v", err)
	}

	masterPassword := "MasterSecret456!"
	creds, err := gokeepasslib.NewPasswordAndKeyCredentials(masterPassword, keyFilePath)
	if err != nil {
		t.Fatalf("failed to build composite credentials: %v", err)
	}

	dbPath := filepath.Join(tempDir, "composite.kdbx")
	createTestKeePassDatabase(t, dbPath, creds)

	remoteName := "test_composite"
	if err := keyring.Set("cloakenv", "provider/"+remoteName, masterPassword); err != nil {
		t.Fatalf("failed to set keyring password: %v", err)
	}

	t.Run("UnlockSuccess", func(t *testing.T) {
		kp := NewKeePassProvider()
		cfg := ProviderConfig{
			Settings: map[string]string{
				"vault_path":  dbPath,
				"key_file":    keyFilePath,
				"remote_name": remoteName,
			},
		}

		if err := kp.Initialize(ctx, cfg); err != nil {
			t.Fatalf("Initialize failed for composite credentials: %v", err)
		}

		secret, err := kp.GetSecret(ctx, "KeyFileEntry:UserName")
		if err != nil {
			t.Fatalf("GetSecret failed: %v", err)
		}
		if secret != "keyuser" {
			t.Errorf("expected UserName 'keyuser', got %q", secret)
		}
	})

	t.Run("WrongPasswordWithCorrectKeyFile", func(t *testing.T) {
		wrongRemote := "test_composite_wrong_pass"
		if err := keyring.Set("cloakenv", "provider/"+wrongRemote, "wrongpassword"); err != nil {
			t.Fatalf("failed to set keyring password: %v", err)
		}

		kp := NewKeePassProvider()
		cfg := ProviderConfig{
			Settings: map[string]string{
				"vault_path":  dbPath,
				"key_file":    keyFilePath,
				"remote_name": wrongRemote,
			},
		}

		err := kp.Initialize(ctx, cfg)
		if err == nil {
			t.Fatalf("expected Initialize to fail with wrong password")
		}
		if !strings.Contains(err.Error(), "decryption failed using credentials from keyring") {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("CorrectPasswordWithWrongKeyFile", func(t *testing.T) {
		wrongKeyPath := filepath.Join(tempDir, "other.key")
		if err := os.WriteFile(wrongKeyPath, []byte("otherkeydata"), 0600); err != nil {
			t.Fatalf("failed to write wrong key file: %v", err)
		}

		wrongKeyRemote := "test_composite_wrong_key"
		if err := keyring.Set("cloakenv", "provider/"+wrongKeyRemote, masterPassword); err != nil {
			t.Fatalf("failed to set keyring password: %v", err)
		}

		kp := NewKeePassProvider()
		cfg := ProviderConfig{
			Settings: map[string]string{
				"vault_path":  dbPath,
				"key_file":    wrongKeyPath,
				"remote_name": wrongKeyRemote,
			},
		}

		err := kp.Initialize(ctx, cfg)
		if err == nil {
			t.Fatalf("expected Initialize to fail with wrong key file")
		}
		if !strings.Contains(err.Error(), "decryption failed using credentials from keyring") {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("KeyFileWithoutPasswordFailsCompositeUnlock", func(t *testing.T) {
		// Remote has no password stored in keyring
		kp := NewKeePassProvider()
		cfg := ProviderConfig{
			Settings: map[string]string{
				"vault_path":  dbPath,
				"key_file":    keyFilePath,
				"remote_name": "test_composite_no_password",
			},
		}

		err := kp.Initialize(ctx, cfg)
		if err == nil {
			t.Fatalf("expected Initialize to fail without password for composite db")
		}
		if !strings.Contains(err.Error(), "no credentials found for remote") {
			t.Errorf("expected error instructing to login, got: %v", err)
		}
	})
}
