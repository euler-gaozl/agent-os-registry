package registry

import (
	"context"
	"github.com/euler-gaozl/agent-os-sdk/servicehttp"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCredentialRPCDoesNotExposeProcessConfiguration(t *testing.T) {
	token := strings.Repeat("fixture-", 5)
	config := servicehttp.Config{Role: "registry", Peers: map[string]string{"telemetry": servicehttp.TokenHash(token)}, Authorize: Authorize}
	handler := (&Server{Store: &Store{}}).RegistryHandler(config)
	for _, name := range []string{"MASTER_KEY", "DATABASE_URL", "SERVICE_TOKEN", "SERVICE_PEERS", "ADMIN_TOKEN", "BUSINESS_TOKEN", "MIGRATION_DATABASE_URL", "PGPASSWORD"} {
		t.Setenv(name, "fixture-value-that-must-not-cross-rpc")
		r := httptest.NewRequest("POST", "/v1/credentials/resolve", strings.NewReader(`{"name":"`+name+`"}`))
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != http.StatusBadRequest || strings.Contains(w.Body.String(), "fixture-value") {
			t.Fatal("process configuration exposed or wrong rejection", name, w.Code)
		}
		if err := (&Store{}).PutSecret(context.Background(), name, "fixture"); err == nil {
			t.Fatal("reserved credential accepted", name)
		}
	}
}
func TestCredentialEnvironmentIsExplicit(t *testing.T) {
	t.Setenv("ARBITRARY_PROCESS_SECRET", "fixture-private")
	if credentialEnvironment("ARBITRARY_PROCESS_SECRET") != "" {
		t.Fatal("arbitrary environment exposed")
	}
	t.Setenv("DEEPSEEK_API_KEY", "fixture-provider")
	value, err := (&Store{}).Secret(context.Background(), "DEEPSEEK_API_KEY")
	if err != nil || value != "fixture-provider" {
		t.Fatal("explicit provider compatibility lost", err)
	}
}
