package registry

import (
	"encoding/json"
	"errors"

	"github.com/euler-gaozl/agent-os-sdk/catalog"
	"github.com/euler-gaozl/agent-os-sdk/servicehttp"
	"io"

	"net/http"

	"strconv"
)

type Server struct {
	Store *Store
	// Tests can replace the transport, but endpoint and redirect policy still apply.
	modelClient *http.Client
}

type handler func(http.ResponseWriter, *http.Request) error

func jsonResponse(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func decode(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(dst); e != nil {
		return e
	}
	var extra any
	if e := d.Decode(&extra); e != io.EOF {
		return errors.New("expected exactly one JSON value")
	}
	return nil
}

func wrap(h handler) http.HandlerFunc {
	return servicehttp.Wrap(h)
}

func (s *Server) registryRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/models/resolve", wrap(s.resolveModelHandler))
	mux.HandleFunc("GET /v1/catalog", wrap(func(w http.ResponseWriter, r *http.Request) error {
		q, e := catalog.Parse(r.URL.Query())
		if e != nil {
			return e
		}
		page, e := s.Store.Catalog(r.Context(), q)
		if e != nil {
			return e
		}
		jsonResponse(w, 200, page)
		return nil
	}))
	mux.HandleFunc("GET /v1/status", wrap(func(w http.ResponseWriter, r *http.Request) error {
		_, e := s.Store.Secret(r.Context(), "DEEPSEEK_API_KEY")
		jsonResponse(w, 200, map[string]bool{"deepseek_configured": e == nil})
		return nil
	}))
	mux.HandleFunc("GET /v1/resources/{kind}/{id}/{revision}", wrap(func(w http.ResponseWriter, r *http.Request) error {
		rev, e := strconv.Atoi(r.PathValue("revision"))
		if e != nil {
			return e
		}
		var spec json.RawMessage
		if e = s.Store.Resource(r.Context(), r.PathValue("kind"), Ref{ID: r.PathValue("id"), Revision: rev}, &spec); e != nil {
			return e
		}
		jsonResponse(w, 200, Resource{Kind: r.PathValue("kind"), ID: r.PathValue("id"), Revision: rev, Spec: spec})
		return nil
	}))
	mux.HandleFunc("POST /v1/credentials/resolve", wrap(func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			Name string `json:"name"`
		}
		if e := decode(w, r, &in); e != nil {
			return e
		}
		value, e := s.Store.Secret(r.Context(), in.Name)
		if e != nil {
			return e
		}
		jsonResponse(w, 200, map[string]string{"value": value})
		return nil
	}))
	mux.HandleFunc("POST /v1/credentials", wrap(func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		}
		if e := decode(w, r, &in); e != nil {
			return e
		}
		if e := s.Store.PutSecret(r.Context(), in.Name, in.Value); e != nil {
			return e
		}
		jsonResponse(w, 200, map[string]bool{"saved": true})
		return nil
	}))
	mux.HandleFunc("GET /v1/resources", wrap(func(w http.ResponseWriter, r *http.Request) error {
		v, e := s.Store.Resources(r.Context(), r.URL.Query().Get("after"))
		if e != nil {
			return e
		}
		visible := []Resource{}
		for _, item := range v {
			if item.Kind != "action" {
				visible = append(visible, item)
			}
		}
		jsonResponse(w, 200, visible)
		return nil
	}))
	mux.HandleFunc("POST /v1/resources", wrap(func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			Kind, ID string
			Spec     json.RawMessage
		}
		if e := decode(w, r, &in); e != nil {
			return e
		}
		if !safeName.MatchString(in.ID) || !Has([]string{"agent", "skill", "prompt", "connector", "knowledge"}, in.Kind) {
			return errors.New("invalid resource kind or id")
		}
		switch in.Kind {
		case "knowledge":
			var kb KnowledgeBase
			if e := json.Unmarshal(in.Spec, &kb); e != nil {
				return e
			}
			if e := kb.Validate(); e != nil {
				return e
			}
			if kb.Engine == "lightrag" {
				var count int
				if e := s.Store.DB.QueryRow(r.Context(), "SELECT count(*) FROM resources WHERE kind='knowledge' AND spec->>'engine'='lightrag' AND spec->>'endpoint'=$1", kb.Endpoint).Scan(&count); e != nil {
					return e
				}
				if count > 0 {
					return errors.New("each LightRAG knowledge revision requires a distinct workspace endpoint")
				}
			}
			in.Spec = JSON(kb)
		case "agent":
			var a Agent
			if e := json.Unmarshal(in.Spec, &a); e != nil {
				return e
			}
			if e := s.resolveAgentModel(r.Context(), &a); e != nil {
				return e
			}
			if e := a.Validate(); e != nil {
				return e
			}
			if e := validateEndpoint(a.Model.BaseURL); e != nil {
				return e
			}
			in.Spec = JSON(a)
		case "skill", "prompt":
			var v Skill
			if e := json.Unmarshal(in.Spec, &v); e != nil {
				return e
			}
			if v.Name == "" || v.Content == "" || len(v.Content) > 40000 {
				return errors.New("name and content (max 40 KB) required")
			}
		case "connector":
			var c Connector
			if e := json.Unmarshal(in.Spec, &c); e != nil {
				return e
			}
			if e := validateEndpoint(c.Endpoint); e != nil {
				return e
			}
			if c.Protocol != "2025-11-25" {
				return errors.New("select supported MCP protocol 2025-11-25")
			}
			for _, t := range c.Tools {
				if !safeName.MatchString(t.Name) {
					return errors.New("invalid MCP tool name")
				}
				if _, e := compileSchema(t.InputSchema); e != nil {
					return e
				}
			}
		}
		v, e := s.Store.PutResource(r.Context(), in.Kind, in.ID, in.Spec)
		if e != nil {
			return e
		}
		jsonResponse(w, 201, v)
		return nil
	}))
	mux.HandleFunc("POST /v1/connectors/discover", wrap(func(w http.ResponseWriter, r *http.Request) error {
		var c Connector
		if e := decode(w, r, &c); e != nil {
			return e
		}
		var e error
		c.AuthToken, e = s.Store.Secret(r.Context(), c.KeyEnv)
		if e != nil {
			return e
		}
		v, e := DiscoverMCP(r.Context(), c)
		if e != nil {
			return e
		}
		jsonResponse(w, 200, v)
		return nil
	}))

}
