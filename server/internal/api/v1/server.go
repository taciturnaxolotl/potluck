// Package v1 implements the public, OpenAI-compatible HTTP surface.
//
// Authentication is bearer-token only (see internal/auth/apikeys.go).
// All error bodies use the OpenAI envelope; map internal codes via
// errors.go on the way out.
//
// Streaming on this surface is intentionally simple: pass-through, bound
// to the request context. A client disconnect cancels the upstream call.
// No buffer, no resume — see AGENTS.md "Don't try to make /v1/* resumable".
package v1

import (
	"github.com/go-chi/chi/v5"

	"github.com/taciturnaxolotl/potluck/internal/auth"
	"github.com/taciturnaxolotl/potluck/internal/ledger"
	"github.com/taciturnaxolotl/potluck/internal/pool"
	"github.com/taciturnaxolotl/potluck/internal/provider"
	"github.com/taciturnaxolotl/potluck/internal/provider/registry"
	"github.com/taciturnaxolotl/potluck/internal/store"
)

// Server bundles the deps the v1 handlers need.
type Server struct {
	Q            *store.Queries
	Auth         *auth.Service
	Ledger       *ledger.Service
	Provider     *provider.Client // legacy; being replaced by Registry
	Pool         *pool.Manager
	FreeProvider *provider.Client // nil when free provider is not configured
	Registry     *registry.Registry // multi-provider registry (new)
}

// Mount installs the v1 routes onto r. The caller chains the bearer-auth
// middleware in front of this; the balance gate and rate limit are wired
// per-route since /v1/models is a free read.
//
// /chat/completions is registered without the PoolGate middleware so that
// requests to free/ models can bypass pool checks. The handler performs the
// gate check itself for non-free models.
func (s *Server) Mount(r chi.Router) {
	r.Get("/models", s.handleListModels)
	r.Post("/chat/completions", s.handleChatCompletions)
}
