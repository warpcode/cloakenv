package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tobischo/gokeepasslib/v3"
	"github.com/zalando/go-keyring"
)

type testKDBXEntry struct {
	Title      string
	Tags       string
	Attributes map[string]string
}

func writeTestKDBX(t *testing.T, vaultName, password string, entries ...testKDBXEntry) string {
	t.Helper()
	keyring.MockInit()
	if err := keyring.Set("cloakenv", "provider/"+vaultName, password); err != nil {
		t.Fatalf("failed to set keyring password for %q: %v", vaultName, err)
	}

	rootGroup := gokeepasslib.NewGroup()
	rootGroup.Name = "Root"

	for _, e := range entries {
		entry := gokeepasslib.NewEntry()
		entry.Tags = e.Tags
		entry.Values = append(entry.Values, gokeepasslib.ValueData{
			Key:   "Title",
			Value: gokeepasslib.V{Content: e.Title},
		})
		for k, v := range e.Attributes {
			entry.Values = append(entry.Values, gokeepasslib.ValueData{
				Key:   k,
				Value: gokeepasslib.V{Content: v},
			})
		}
		rootGroup.Entries = append(rootGroup.Entries, entry)
	}

	db := gokeepasslib.NewDatabase()
	db.Credentials = gokeepasslib.NewPasswordCredentials(password)
	db.Content.Root = &gokeepasslib.RootData{
		Groups: []gokeepasslib.Group{rootGroup},
	}

	dbPath := filepath.Join(t.TempDir(), vaultName+".kdbx")
	f, err := os.Create(dbPath)
	if err != nil {
		t.Fatalf("failed to create temp kdbx %q: %v", dbPath, err)
	}
	defer func() { _ = f.Close() }()

	enc := gokeepasslib.NewEncoder(f)
	if err := enc.Encode(db); err != nil {
		t.Fatalf("failed to encode temp kdbx %q: %v", dbPath, err)
	}

	return dbPath
}
