package registry

import "github.com/euler-gaozl/agent-os-sdk/contracts"

type Ref = contracts.Ref

type Connector = contracts.Connector

type Skill = contracts.Skill

type Agent = contracts.Agent

type Resource = contracts.Resource

type KnowledgeBase = contracts.KnowledgeBase

var (
	ErrFence   = contracts.ErrFence
	ErrUnknown = contracts.ErrUnknown
	ID         = contracts.ID
	JSON       = contracts.JSON
	Hash       = contracts.Hash
	Has        = contracts.Has
)
