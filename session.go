package agentwrapper

import (
	"github.com/smallnest/agent-wrapper/types"
)

// SessionStore 管理 Session 的持久化。
type SessionStore interface {
	Create() (*types.Session, error)
	Get(id string) (*types.Session, error)
	Save(session *types.Session) error
	Delete(id string) error
	List() []*types.SessionSummary
}
