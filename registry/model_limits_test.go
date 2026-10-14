package registry

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/euler-gaozl/agent-os-sdk/contracts"
)

const nativeFixture = `{"object":"list","data":[{"id":"deepseek-flash","context_window":1048576,"max_output_tokens":393216,"name":"Synthetic provider metadata"}]}`

type modelRoundTrip func(*http.Request) (*http.Response, error)

func (f modelRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func nativeFixtureModel() contracts.ModelConfig {
	return contracts.ModelConfig{Model: "deepseek-flash", BaseURL: "https://api.deepseek.com", KeyEnv: "DEEPSEEK_API_KEY"}
}

func TestNativeMetadataSelectionAndValidation(t *testing.T) {
	model := nativeFixtureModel()
	observed := time.Date(2026, 10, 14, 10, 41, 17, 123, time.UTC)
	for _, tc := range []struct{ name, body string }{
		{"missing output", `{"object":"list","data":[{"id":"deepseek-flash","context_window":1048576}]}`},
		{"unavailable model", strings.ReplaceAll(nativeFixture, "deepseek-flash", "other-model")},
		{"duplicate model", strings.ReplaceAll(nativeFixture, `}]}`, `},{"id":"deepseek-flash","context_window":8192,"max_output_tokens":4096}]}`)},
		{"negative", strings.ReplaceAll(nativeFixture, "1048576", "-1")},
		{"fractional", strings.ReplaceAll(nativeFixture, "1048576", "1048576.5")},
		{"string limit", strings.ReplaceAll(nativeFixture, "1048576", `"1048576"`)},
		{"output over window", strings.ReplaceAll(nativeFixture, "393216", "1048577")},
		{"overflow", strings.ReplaceAll(nativeFixture, "1048576", "99999999999999999999999")},
		{"unsafe JS integer", strings.ReplaceAll(nativeFixture, "1048576", "9007199254740992")},
		{"trailing value", nativeFixture + `{}`},
		{"wrong envelope", strings.ReplaceAll(nativeFixture, `"list"`, `"error"`)},
		{"duplicate envelope", strings.ReplaceAll(nativeFixture, `"object":"list"`, `"object":"error","object":"list"`)},
		{"duplicate data", strings.ReplaceAll(nativeFixture, `"data":`, `"data":[],"data":`)},
		{"duplicate id", strings.ReplaceAll(nativeFixture, `"id":`, `"id":"other","id":`)},
		{"duplicate context", strings.ReplaceAll(nativeFixture, `"context_window":`, `"context_window":8192,"context_window":`)},
		{"duplicate output", strings.ReplaceAll(nativeFixture, `"max_output_tokens":`, `"max_output_tokens":4096,"max_output_tokens":`)},
		{"escaped duplicate", strings.ReplaceAll(nativeFixture, `"id":`, `"\u0069d":"other","id":`)},
		{"excessive nesting", strings.ReplaceAll(nativeFixture, `"name":`, `"unrelated":`+strings.Repeat("[", 70)+`0`+strings.Repeat("]", 70)+`,"name":`)},
		{"wrong case only", strings.ReplaceAll(nativeFixture, `"context_window"`, `"CONTEXT_WINDOW"`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			limits, err := parseModelLimits([]byte(tc.body), model, model.BaseURL+"/models", observed)
			if err == nil || limits != nil {
				t.Fatal("invalid metadata accepted")
			}
		})
	}
	limits, err := parseModelLimits([]byte(nativeFixture), model, model.BaseURL+"/models", observed)
	if err != nil || limits.ContextWindow != 1048576 || limits.MaxOutputTokens != 393216 || limits.ObservedAt != observed.Format(time.RFC3339Nano) || len(limits.MetadataSHA256) != 64 {
		t.Fatal("official-size synthetic capabilities not preserved", err)
	}
	changed, err := parseModelLimits([]byte(strings.ReplaceAll(nativeFixture, "393216", "393215")), model, model.BaseURL+"/models", observed)
	if err != nil || changed.MetadataSHA256 == limits.MetadataSHA256 {
		t.Fatal("metadata change not bound to digest", err)
	}
	// Different extension names must never override protocol fields through
	// Go's case-insensitive struct decoding, including Unicode fold aliases.
	aliases := strings.ReplaceAll(nativeFixture, `"name":`, `"CONTEXT_WINDOW":8192,"MAX_OUTPUT_TOKENS":4096,"max_output_token\u017f":1024,"ID":"other","name":`)
	aliases = strings.ReplaceAll(aliases, `"object":"list"`, `"object":"list","OBJECT":"error","DATA":[]`)
	exact, err := parseModelLimits([]byte(aliases), model, model.BaseURL+"/models", observed)
	if err != nil || exact.Model != model.Model || exact.ContextWindow != 1048576 || exact.MaxOutputTokens != 393216 {
		t.Fatal("case-folded extension overrode an exact protocol field", err)
	}
}

func TestNativeDiscoveryEndpointCredentialAndRedirectBoundary(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "synthetic-discovery-credential")
	var count int
	s := &Server{Store: &Store{}, modelClient: &http.Client{Transport: modelRoundTrip(func(r *http.Request) (*http.Response, error) {
		count++
		if r.URL.Host != "api.deepseek.com" || r.Header.Get("Authorization") != "Bearer synthetic-discovery-credential" {
			t.Fatal("unexpected credential destination")
		}
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": {"https://untrusted.invalid/models"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})}}
	for _, endpoint := range []string{"http://api.deepseek.com", "https://127.0.0.1", "https://api.deepseek.com.evil.invalid", "https://api.deepseek.com:443", "https://api.deepseek.com@untrusted.invalid", "https://api.deepseek.com?key=secret", "https://api.deepseek.com/#fragment", "https://api.deepseek.com/v1/../models"} {
		model := nativeFixtureModel()
		model.BaseURL = endpoint
		if _, err := s.resolveModelLimits(context.Background(), model); err == nil {
			t.Fatal("untrusted endpoint accepted")
		}
	}
	if count != 0 {
		t.Fatal("endpoint validation happened after sending credential")
	}
	if _, err := s.resolveModelLimits(context.Background(), nativeFixtureModel()); err == nil || count != 1 {
		t.Fatal("redirect followed or treated as capabilities")
	}
	for _, credential := range []string{"", "MASTER_KEY", "DATABASE_URL"} {
		model := nativeFixtureModel()
		model.KeyEnv = credential
		if _, err := s.resolveModelLimits(context.Background(), model); err == nil {
			t.Fatal("invalid credential reference accepted")
		}
	}
	if count != 1 {
		t.Fatal("invalid credential reference sent request")
	}
}

func TestNativeDiscoveryFailureDoesNotLeakOrDefault(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "synthetic-discovery-credential")
	for _, tc := range []struct {
		name, body string
		status     int
		err        error
	}{
		{"provider error", "synthetic-discovery-credential", 401, nil},
		{"transport error", "", 0, errors.New("synthetic-discovery-credential")},
		{"malformed", "synthetic-discovery-credential", 200, nil},
		{"oversized", strings.Repeat("x", modelMetadataBytes+1), 200, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &Server{Store: &Store{}, modelClient: &http.Client{Transport: modelRoundTrip(func(r *http.Request) (*http.Response, error) {
				if tc.err != nil {
					return nil, tc.err
				}
				return &http.Response{StatusCode: tc.status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(tc.body)), Request: r}, nil
			})}}
			limits, err := s.resolveModelLimits(context.Background(), nativeFixtureModel())
			if err == nil || limits != nil || strings.Contains(err.Error(), "synthetic-discovery-credential") {
				t.Fatal("capability failure leaked credentials or substituted defaults")
			}
		})
	}
}

func TestNativePreviewAndCancellationDoNotPublish(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "synthetic-discovery-credential")
	s := &Server{Store: &Store{}, modelClient: &http.Client{Transport: modelRoundTrip(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(nativeFixture)), Request: r}, nil
	})}}
	mux := http.NewServeMux()
	s.registryRoutes(mux)
	r := httptest.NewRequest("POST", "/v1/models/resolve", strings.NewReader(string(JSON(nativeFixtureModel()))))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	var limits contracts.ModelLimits
	if err := json.Unmarshal(w.Body.Bytes(), &limits); err != nil || w.Code != 200 || limits.MaxOutputTokens != 393216 || strings.Contains(w.Body.String(), "synthetic-discovery-credential") {
		t.Fatal("preview failed or exposed credential", err, w.Code)
	}
	// There is no database in this fixture: preview cannot create a resource.
	s.modelClient.Transport = modelRoundTrip(func(r *http.Request) (*http.Response, error) {
		<-r.Context().Done()
		return nil, r.Context().Err()
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	if value, err := s.resolveModelLimits(ctx, nativeFixtureModel()); err == nil || value != nil || time.Since(start) > time.Second {
		t.Fatal("cancelled discovery did not stop promptly")
	}
}

func TestNativeDefaultAttemptsRemainIndependentOfTokenLimits(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "synthetic-discovery-credential")
	s := &Server{Store: &Store{}, modelClient: &http.Client{Transport: modelRoundTrip(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(nativeFixture)), Request: r}, nil
	})}}
	for _, count := range []int{0, 12} {
		a := Agent{Model: nativeFixtureModel(), MaxModelAttempts: count, ModelTokenBudget: 250000}
		a.Model.Limits = &contracts.ModelLimits{}
		if err := s.resolveAgentModel(context.Background(), &a); err != nil {
			t.Fatal(err)
		}
		want := count
		if want == 0 {
			want = 36
		}
		if a.MaxModelAttempts != want || a.ModelTokenBudget != 0 || a.Model.MaxTokens != 393216 {
			t.Fatal("attempt policy or token capabilities changed")
		}
	}
}

func TestNativePublishedRevisionFreezesServerObservation(t *testing.T) {
	db, role := fixtureDatabase(t)
	ctx := context.Background()
	if err := Migrate(ctx, db, role); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MASTER_KEY", strings.Repeat("synthetic-native-fixture-", 3))
	store := &Store{DB: db}
	if err := store.PutSecret(ctx, "NATIVE_FIXTURE_KEY", "synthetic-encrypted-provider-credential"); err != nil {
		t.Fatal(err)
	}
	var count int
	body := nativeFixture
	s := &Server{Store: store, modelClient: &http.Client{Transport: modelRoundTrip(func(r *http.Request) (*http.Response, error) {
		count++
		if r.Method != "GET" || r.URL.String() != "https://api.deepseek.com/v1/models" || r.Header.Get("Authorization") != "Bearer synthetic-encrypted-provider-credential" {
			t.Fatal("wrong server-side credential or metadata request")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}}
	mux := http.NewServeMux()
	s.registryRoutes(mux)
	model := nativeFixtureModel()
	model.BaseURL += "/v1/"
	model.KeyEnv = "NATIVE_FIXTURE_KEY"
	agent := Agent{Name: "Native fixture", Prompt: strings.Repeat("long pinned prompt ", 5000), Model: model, ContextTokens: 64000, ModelTokenBudget: 250000, MaxModelAttempts: 12}
	agent.Model.MaxTokens = 6144
	// Deliberately forged metadata cannot become an authoritative observation.
	agent.Model.Limits = &contracts.ModelLimits{ContextWindow: 8, MaxOutputTokens: 2, Source: "https://untrusted.invalid/models"}
	post := func() Resource {
		t.Helper()
		r := httptest.NewRequest("POST", "/v1/resources", strings.NewReader(string(JSON(map[string]any{"kind": "agent", "id": "native-fixture", "spec": agent}))))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != 201 || strings.Contains(w.Body.String(), "synthetic-encrypted-provider-credential") {
			t.Fatal("resource publication failed", w.Code, w.Body.String())
		}
		var result Resource
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	first := post()
	body = strings.ReplaceAll(nativeFixture, "393216", "262144")
	second := post()
	for _, tc := range []struct{ revision, max int }{{first.Revision, 393216}, {second.Revision, 262144}} {
		var stored Agent
		if err := store.Resource(ctx, "agent", Ref{ID: "native-fixture", Revision: tc.revision}, &stored); err != nil {
			t.Fatal(err)
		}
		if stored.Model.MaxTokens != tc.max || stored.ContextTokens != 1048576 || stored.ModelTokenBudget != 0 || stored.MaxModelAttempts != 12 || stored.Prompt != agent.Prompt || stored.Model.Limits == nil || stored.Validate() != nil {
			t.Fatal("published capability or unrelated policy changed")
		}
	}
	if count != 2 {
		t.Fatal("historical read rediscovered capabilities")
	}
	// A provider outage must not publish a silently downgraded revision.
	body = `{}`
	r := httptest.NewRequest("POST", "/v1/resources", strings.NewReader(string(JSON(map[string]any{"kind": "agent", "id": "native-fixture", "spec": agent}))))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	var revisions int
	if err := db.QueryRow(ctx, "SELECT count(*) FROM resources WHERE id='native-fixture'").Scan(&revisions); err != nil || w.Code != 502 || revisions != 2 {
		t.Fatal("failed discovery published a revision", err, w.Code, revisions)
	}
	// Legacy configurations are not silently upgraded or sent to the provider.
	agent.Model.Limits = nil
	agent.Prompt = "Legacy fixture"
	legacy := post()
	var old Agent
	if err := json.Unmarshal(legacy.Spec, &old); err != nil || old.Model.Limits != nil || old.Model.MaxTokens != 6144 || old.ContextTokens != 64000 || old.ModelTokenBudget != 250000 || count != 3 {
		t.Fatal("legacy publication changed semantics", err)
	}
}
