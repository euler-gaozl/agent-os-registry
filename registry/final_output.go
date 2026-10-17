package registry

import (
	"context"
	"errors"

	"github.com/euler-gaozl/agent-os-sdk/contracts"
	"github.com/euler-gaozl/agent-os-sdk/toolcontract"
)

// Bind to immutable resource revisions, not a mutable discovered MCP catalog.
// Runtime repeats this check on the frozen execution snapshot before admission.
func (s *Store) validateFinalOutputBinding(ctx context.Context, a Agent) error {
	if a.FinalOutput == nil {
		return nil
	}
	if e := a.FinalOutput.Validate(); e != nil {
		return e
	}
	snap := contracts.Snapshot{Agent: a}
	for _, ref := range a.Connectors {
		var c Connector
		if e := s.Resource(ctx, "connector", ref, &c); e != nil {
			return e
		}
		// Runtime snapshots namespace MCP tools by the immutable binding ID,
		// not the editable display name (which may contain spaces or Chinese).
		c.Name = ref.ID
		snap.Connectors = append(snap.Connectors, c)
	}
	for _, ref := range a.Skills {
		var skill Skill
		if e := s.Resource(ctx, "skill", ref, &skill); e != nil {
			return e
		}
		snap.Skills = append(snap.Skills, skill)
	}
	// ToolDefinitions only uses presence and frozen Agent knowledge references
	// to construct kb_search; it does not need to query the Knowledge service.
	snap.Knowledge = make(map[string]contracts.KnowledgeBase, len(a.Knowledge))
	for _, ref := range a.Knowledge {
		snap.Knowledge[ref.ID] = contracts.KnowledgeBase{}
	}
	tools, e := toolcontract.ToolDefinitions(snap)
	if e != nil {
		return e
	}
	found := false
	for _, tool := range tools {
		if tool.Name == contracts.FinalOutputToolName || tool.Name == "working_state_update" {
			return errors.New("reserved final engine tool collision")
		}
		if tool.Name == a.FinalOutput.Tool {
			found = true
		}
	}
	if !found {
		return errors.New("final output candidate tool must be bound to this Agent version")
	}
	return nil
}
