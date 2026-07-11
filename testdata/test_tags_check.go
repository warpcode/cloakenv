package main

import (
	"fmt"
	"os"

	"github.com/tobischo/gokeepasslib/v3"
)

func main() {
	file, err := os.Open("testdata/testDB.kdbx")
	if err != nil {
		panic(err)
	}
	defer file.Close()

	db := gokeepasslib.NewDatabase()
	db.Credentials = gokeepasslib.NewPasswordCredentials("password123")
	if err := gokeepasslib.NewDecoder(file).Decode(db); err != nil {
		panic(err)
	}

	if err := db.UnlockProtectedEntries(); err != nil {
		panic(err)
	}

	var traverse func(g *gokeepasslib.Group)
	traverse = func(g *gokeepasslib.Group) {
		for _, e := range g.Entries {
			var title string
			for _, v := range e.Values {
				if v.Key == "Title" {
					title = v.Value.Content
				}
			}
			fmt.Printf("Entry: %q | Tags: %q\n", title, e.Tags)
		}
		for i := range g.Groups {
			traverse(&g.Groups[i])
		}
	}

	traverse(&db.Content.Root.Groups[0])
}
