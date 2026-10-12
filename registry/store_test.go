package registry

import (
	"context"
	"strings"
	"testing"

	"github.com/euler-gaozl/agent-os-sdk/catalog"
)

func TestImmutableResourceAndEncryptedCredentialRoundTrip(t *testing.T) {
	db, role := fixtureDatabase(t)
	ctx := context.Background()
	if err := Migrate(ctx, db, role); err != nil {
		t.Fatal(err)
	}
	store := &Store{DB: db}
	t.Setenv("MASTER_KEY", strings.Repeat("synthetic-fixture-key-", 3))
	t.Setenv("CUSTOM_PROVIDER_KEY", "environment-must-not-win")
	if err := store.PutSecret(ctx, "CUSTOM_PROVIDER_KEY", "stored-fixture-credential"); err != nil {
		t.Fatal(err)
	}
	value, err := store.Secret(ctx, "CUSTOM_PROVIDER_KEY")
	if err != nil || value != "stored-fixture-credential" {
		t.Fatal("credential round trip failed", err)
	}
	var sealed []byte
	if err = db.QueryRow(ctx, "SELECT sealed FROM secrets WHERE name='CUSTOM_PROVIDER_KEY'").Scan(&sealed); err != nil || strings.Contains(string(sealed), value) {
		t.Fatal("credential not sealed", err)
	}
	for _, name := range []string{"first", "second"} {
		if _, err = store.PutResource(ctx, "prompt", "fixture", JSON(map[string]string{"name": name, "content": strings.Repeat("x", 10000)})); err != nil {
			t.Fatal(err)
		}
	}
	var old map[string]string
	if err = store.Resource(ctx, "prompt", Ref{ID: "fixture", Revision: 1}, &old); err != nil || old["name"] != "first" {
		t.Fatal("old revision changed", err)
	}
	page, err := store.Catalog(ctx, catalog.Query{Limit: 1})
	if err != nil || len(page.Items) != 1 || page.Items[0].Revision != 2 || !page.Items[0].Summary || len(page.Items[0].Spec) > 2000 {
		t.Fatal("catalog failed to return bounded latest summary", err)
	}
	if err = Migrate(ctx, db, role); err != nil {
		t.Fatal(err)
	}
	if err = store.Resource(ctx, "prompt", Ref{ID: "fixture", Revision: 1}, &old); err != nil || old["name"] != "first" {
		t.Fatal("repeat migration lost old data", err)
	}
}
