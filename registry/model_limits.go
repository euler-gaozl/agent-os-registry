package registry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/euler-gaozl/agent-os-sdk/contracts"
	"github.com/euler-gaozl/agent-os-sdk/servicehttp"
)

const modelMetadataBytes = 1 << 20

// Discovery is deliberately restricted to provider endpoints whose capability
// protocol is implemented here. Never send a named credential to a URL supplied
// in the observation, follow redirects, or infer limits from a model's name.
func modelMetadataSource(base string) (string, error) {
	switch strings.TrimRight(base, "/") {
	case "https://api.deepseek.com", "https://api.deepseek.com/v1":
		return strings.TrimRight(base, "/") + "/models", nil
	default:
		return "", errors.New("native model capabilities are not supported for this provider endpoint")
	}
}

func (s *Server) resolveModelHandler(w http.ResponseWriter, r *http.Request) error {
	var model contracts.ModelConfig
	if err := decode(w, r, &model); err != nil {
		return err
	}
	limits, err := s.resolveModelLimits(r.Context(), model)
	if err != nil {
		return err
	}
	jsonResponse(w, http.StatusOK, limits)
	return nil
}

// A non-nil limits object opts a NEW revision into native model capabilities.
// Any client-supplied values are discarded. Reads of historical revisions and
// task recovery never call this method and retain the original observation.
func (s *Server) resolveAgentModel(ctx context.Context, agent *Agent) error {
	if agent.Model.Limits == nil {
		return nil
	}
	limits, err := s.resolveModelLimits(ctx, agent.Model)
	if err != nil {
		return err
	}
	agent.Model.Limits = limits
	agent.ContextTokens = limits.ContextWindow
	agent.Model.MaxTokens = limits.MaxOutputTokens
	// Native mode has no inherited family token allowance. Attempts, usage and
	// unknown-call accounting remain independent execution responsibilities.
	agent.ModelTokenBudget = 0
	return nil
}

func (s *Server) resolveModelLimits(ctx context.Context, model contracts.ModelConfig) (*contracts.ModelLimits, error) {
	source, err := modelMetadataSource(model.BaseURL)
	if err != nil {
		return nil, err
	}
	if model.Model == "" || !secretName.MatchString(model.KeyEnv) || reservedCredentialName(model.KeyEnv) {
		return nil, errors.New("model and provider credential reference are required")
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	key, err := s.Store.Secret(ctx, model.KeyEnv)
	if err != nil || key == "" {
		return nil, errors.New("provider credential is not available")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return nil, errors.New("invalid model capability endpoint")
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "application/json")
	client := servicehttp.Client
	if s.modelClient != nil {
		client = s.modelClient
	}
	bounded := *client
	bounded.Timeout = 15 * time.Second
	bounded.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := bounded.Do(req)
	if err != nil {
		// Transport errors may contain a URL or echoed headers; never return them.
		return nil, &servicehttp.StatusError{Status: 502, Message: "model capability request failed"}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &servicehttp.StatusError{Status: 502, Message: "provider did not return model capabilities"}
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, modelMetadataBytes+1))
	if err != nil || len(raw) > modelMetadataBytes {
		return nil, &servicehttp.StatusError{Status: 502, Message: "invalid or oversized model capability response"}
	}
	return parseModelLimits(raw, model, source, time.Now().UTC())
}

func parseModelLimits(raw []byte, model contracts.ModelConfig, source string, observed time.Time) (*contracts.ModelLimits, error) {
	invalid := &servicehttp.StatusError{Status: 502, Message: "provider model capabilities are missing, ambiguous or invalid"}
	if !uniqueModelMetadata(raw) {
		return nil, invalid
	}
	// Exact map keys avoid encoding/json's case-insensitive struct aliases.
	var list map[string]json.RawMessage
	var object string
	var data []json.RawMessage
	if json.Unmarshal(raw, &list) != nil || json.Unmarshal(list["object"], &object) != nil || object != "list" || json.Unmarshal(list["data"], &data) != nil {
		return nil, invalid
	}
	var match *contracts.ModelLimits
	for _, item := range data {
		var entry map[string]json.RawMessage
		var id string
		var window, output int
		if json.Unmarshal(item, &entry) != nil || json.Unmarshal(entry["id"], &id) != nil {
			return nil, invalid
		}
		if id != model.Model {
			continue
		}
		if match != nil || json.Unmarshal(entry["context_window"], &window) != nil || json.Unmarshal(entry["max_output_tokens"], &output) != nil {
			return nil, invalid
		}
		match = &contracts.ModelLimits{
			Model: id, BaseURL: strings.TrimRight(model.BaseURL, "/"), Protocol: "openai-chat-completions",
			ContextWindow: window, MaxOutputTokens: output,
			Source: source, ObservedAt: observed.Format(time.RFC3339Nano), MetadataSHA256: Hash(item),
		}
		model.MaxTokens = match.MaxOutputTokens
		if err := match.Validate(match.ContextWindow, model); err != nil {
			return nil, invalid
		}
	}
	if match == nil {
		return nil, invalid
	}
	return match, nil
}

// encoding/json otherwise accepts repeated members with last-value-wins
// semantics. A capability observation must have one unambiguous interpretation,
// including escaped spellings of the same name. This parser is metadata-only.
func uniqueModelMetadata(raw []byte) bool {
	if len(raw) > modelMetadataBytes {
		return false
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var value func(int) bool
	value = func(depth int) bool {
		if depth > 64 {
			return false
		}
		token, err := d.Token()
		if err != nil {
			return false
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return true
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				key, err := d.Token()
				name, ok := key.(string)
				if err != nil || !ok || seen[name] {
					return false
				}
				seen[name] = true
				if !value(depth + 1) {
					return false
				}
			}
			end, err := d.Token()
			return err == nil && end == json.Delim('}')
		case '[':
			for d.More() {
				if !value(depth + 1) {
					return false
				}
			}
			end, err := d.Token()
			return err == nil && end == json.Delim(']')
		default:
			return false
		}
	}
	if !value(0) {
		return false
	}
	_, err := d.Token()
	return err == io.EOF
}
