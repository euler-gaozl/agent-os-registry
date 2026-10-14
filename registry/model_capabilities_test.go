package registry

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/euler-gaozl/agent-os-sdk/servicehttp"
)

func TestModelManagementProfileIsAuthenticatedAndReadOnly(t *testing.T) {
	tokens := map[string]string{"control": strings.Repeat("c", 40), "worker": strings.Repeat("w", 40)}
	peers := map[string]string{}
	for role, token := range tokens {
		peers[role] = servicehttp.TokenHash(token)
	}
	// No Store or provider client: this endpoint must not touch data or credentials.
	s := &Server{}
	h := servicehttp.New(servicehttp.Config{Role: "registry", Peers: peers, Authorize: Authorize}, nil, s.registryRoutes)
	for _, tc := range []struct {
		caller string
		want   int
	}{{"", 401}, {"worker", 403}, {"control", 200}} {
		r := httptest.NewRequest(http.MethodGet, "/v1/models/capabilities", nil)
		r.Header.Set("Authorization", "Bearer "+tokens[tc.caller])
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatal("unexpected capability access", tc.caller, w.Code)
		}
		if tc.want == 200 {
			var profile struct {
				Format   int    `json:"format"`
				Provides string `json:"provides"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &profile); err != nil || profile.Format != 1 || profile.Provides != "agent-model-native-v1" {
				t.Fatal("incorrect model-management contract", err)
			}
		}
	}
}
