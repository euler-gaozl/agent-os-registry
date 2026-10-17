package registry

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/euler-gaozl/agent-os-sdk/contracts"
	"github.com/euler-gaozl/agent-os-sdk/serviceclient"
)

func TestFinalOutputPublicationRetainsPolicyAndExactBinding(t *testing.T) {
	db, role := fixtureDatabase(t)
	ctx := context.Background()
	if e := Migrate(ctx, db, role); e != nil {
		t.Fatal(e)
	}
	store := &Store{DB: db}
	t.Setenv("DEEPSEEK_API_KEY", "synthetic-discovery-credential")
	s := &Server{Store: store, modelClient: &http.Client{Transport: modelRoundTrip(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(nativeFixture)), Request: r}, nil
	})}}
	mux := http.NewServeMux()
	s.registryRoutes(mux)
	c := Connector{Name: "fixture", Tools: []contracts.Tool{{Name: "check", InputSchema: JSON(map[string]any{"type": "object", "properties": map[string]any{"draft": map[string]string{"type": "string"}}, "required": []string{"draft"}})}}}
	if _, e := store.PutResource(ctx, "connector", "checker", JSON(c)); e != nil {
		t.Fatal(e)
	}
	a := Agent{Name: "Final fixture", Prompt: "Check the retained original.", Model: nativeFixtureModel(), Connectors: []Ref{{ID: "checker", Revision: 1}}, FinalOutput: &contracts.FinalOutputPolicy{Protocol: contracts.FinalOutputProtocol, Tool: "mcp_fixture_check", InputField: "draft", ReadyPath: []string{"structuredContent", "ready"}}}
	a.Model.Limits = &contracts.ModelLimits{}
	post := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("POST", "/v1/resources", strings.NewReader(string(JSON(map[string]any{"kind": "agent", "id": "final-fixture", "spec": a})))))
		return w
	}
	w := post()
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var resource Resource
	if e := json.Unmarshal(w.Body.Bytes(), &resource); e != nil {
		t.Fatal(e)
	}
	var returned, stored Agent
	if e := json.Unmarshal(resource.Spec, &returned); e != nil {
		t.Fatal(e)
	}
	if e := store.Resource(ctx, "agent", Ref{ID: "final-fixture", Revision: 1}, &stored); e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(returned.FinalOutput, a.FinalOutput) || !reflect.DeepEqual(stored.FinalOutput, a.FinalOutput) {
		t.Fatal("policy lost during typed publication/read")
	}
	if stored.Model.Limits == nil || stored.Validate() != nil {
		t.Fatal("native capabilities not frozen")
	}
	// A new catalog revision cannot change the old Agent's bound checker.
	c.Tools[0].Name = "different"
	if _, e := store.PutResource(ctx, "connector", "checker", JSON(c)); e != nil {
		t.Fatal(e)
	}
	if w = post(); w.Code != 201 {
		t.Fatal("pinned old checker changed", w.Code, w.Body.String())
	}
	a.Connectors[0].Revision = 2
	if w = post(); w.Code == 201 {
		t.Fatal("unbound candidate published")
	}
	a.Connectors = nil
	if w = post(); w.Code == 201 {
		t.Fatal("missing candidate published")
	}
	a.FinalOutput.Tool = contracts.FinalOutputToolName
	if w = post(); w.Code == 201 {
		t.Fatal("reserved tool published")
	}
	var count int
	if e := db.QueryRow(ctx, "SELECT count(*) FROM resources WHERE kind='agent' AND id='final-fixture'").Scan(&count); e != nil || count != 2 {
		t.Fatal("rejected policy created version", count, e)
	}
	// Ordinary Agent edits preserve the old absent-field JSON contract.
	a.FinalOutput = nil
	if w = post(); w.Code != 201 || strings.Contains(w.Body.String(), `"final_output"`) {
		t.Fatal("ordinary Agent acquired policy", w.Code, w.Body.String())
	}
	if e := store.Resource(ctx, "agent", Ref{ID: "final-fixture", Revision: 1}, &stored); e != nil || stored.FinalOutput == nil {
		t.Fatal("historical final policy changed", e)
	}
}

func TestFinalOutputBindingUsesImmutableToolHubHTTP(t *testing.T) {
	available := true
	calls := 0
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "GET" || r.URL.Path != "/v1/resolve/connector/fixed-checker/3" || r.Header.Get("Authorization") != "Bearer fixture-registry" {
			t.Error("wrong immutable ToolHub lookup")
			w.WriteHeader(400)
			return
		}
		if !available {
			w.WriteHeader(503)
			return
		}
		jsonResponse(w, 200, Resource{Kind: "connector", ID: "fixed-checker", Revision: 3, Spec: JSON(Connector{Name: "fixture", Tools: []contracts.Tool{{Name: "check", InputSchema: json.RawMessage(`{"type":"object"}`)}}})})
	}))
	defer hub.Close()
	s := &Store{CatalogURL: hub.URL, Remote: &serviceclient.Client{CatalogURL: hub.URL, Token: "fixture-registry"}}
	a := Agent{Connectors: []Ref{{ID: "fixed-checker", Revision: 3}}, FinalOutput: &contracts.FinalOutputPolicy{Protocol: contracts.FinalOutputProtocol, Tool: "mcp_fixture_check", InputField: "draft", ReadyPath: []string{"ready"}}}
	if e := s.validateFinalOutputBinding(context.Background(), a); e != nil {
		t.Fatal(e)
	}
	available = false
	if e := s.validateFinalOutputBinding(context.Background(), a); e == nil {
		t.Fatal("missing ToolHub version accepted")
	}
	a.FinalOutput = nil
	if e := s.validateFinalOutputBinding(context.Background(), a); e != nil || calls != 2 {
		t.Fatal("legacy publish acquired new catalog dependency", e, calls)
	}
}
