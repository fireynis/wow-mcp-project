// Package tools builds the wow MCP server and registers its tools.
package tools

import (
	"context"
	"encoding/json"
	"net/url"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"wowapp/internal/blizzard"
	"wowapp/internal/config"
)

const (
	serverName    = "wow"
	serverVersion = "0.1.0"
)

// GameSource answers read-only game-data queries (ForeverDB).
type GameSource interface {
	Get(ctx context.Context, path string, q url.Values) (json.RawMessage, error)
}

// CharacterSource answers read-only character queries (Blizzard profile API).
type CharacterSource interface {
	Account(ctx context.Context) ([]blizzard.AccountCharacter, error)
	Summary(ctx context.Context, c config.Character) (any, error)
	Equipment(ctx context.Context, c config.Character) (any, error)
	Talents(ctx context.Context, c config.Character) (any, error)
	Stats(ctx context.Context, c config.Character) (any, error)
	Reputations(ctx context.Context, c config.Character) (any, error)
	Professions(ctx context.Context, c config.Character) (any, error)
}

// Deps are the data sources behind the tools. A nil source means that group of
// tools is not registered.
type Deps struct {
	Game  GameSource
	Chars CharacterSource
	// Companion is Forever Companion, a second, keyless game-data source.
	Companion CompanionSource
	// Characters are the configured characters, the main first. The
	// character tools need at least one.
	Characters []config.Character
	// Now reports the current time; nil means time.Now.
	Now func() time.Time
}

func (d Deps) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

// New builds the MCP server and registers the tools its deps support.
func New(deps Deps) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{Name: serverName, Version: serverVersion}, nil)
	if deps.Game != nil {
		registerGame(srv, deps.Game)
	}
	if deps.Companion != nil {
		registerCompanion(srv, deps.Companion)
	}
	if deps.Chars != nil && len(deps.Characters) > 0 {
		registerCharacters(srv, deps.Chars, deps.Characters)
	}
	return srv
}

// errorResult is a tool result reporting a failure to the model.
func errorResult(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: msg}},
	}
}
