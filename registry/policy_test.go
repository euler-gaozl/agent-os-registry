package registry

import (
	"github.com/euler-gaozl/agent-os-sdk/servicehttp"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServiceIdentityCannotCrossAdministrativeBoundary(t *testing.T) {
	tokens := map[string]string{"control": strings.Repeat("c", 40), "futures": strings.Repeat("f", 40), "worker": strings.Repeat("w", 40)}
	peers := map[string]string{}
	for k, v := range tokens {
		peers[k] = servicehttp.TokenHash(v)
	}
	h := servicehttp.New(servicehttp.Config{Role: "registry", Peers: peers, Authorize: Authorize}, nil, func(m *http.ServeMux) {
		m.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	})
	for _, tc := range []struct {
		role, method, path string
		want               int
	}{{"futures", "POST", "/v1/credentials/resolve", 403}, {"futures", "POST", "/v1/resources", 403}, {"control", "POST", "/v1/credentials/resolve", 403}, {"control", "POST", "/v1/resources", 204}, {"worker", "POST", "/v1/credentials/resolve", 204}, {"worker", "POST", "/v1/resources", 403}, {"futures", "GET", "/v1/published/futures", 204}, {"", "GET", "/v1/resources", 401}} {
		r := httptest.NewRequest(tc.method, tc.path, nil)
		r.Header.Set("Authorization", "Bearer "+tokens[tc.role])
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatalf("%+v got %d", tc, w.Code)
		}
	}
}
