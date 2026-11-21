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

// Instructions tells the client what the server is for and how to keep WoW
// Forever apart from the other versions of the game.
const Instructions = `These tools answer questions about World of Warcraft: Forever (WoW Forever), Blizzard's own version of WoW. It started from Classic Era but has its own zones, dungeons, talents, items, quests, XP and patch changes, so Classic Era facts (and Season of Discovery, Hardcore, Anniversary, retail or private-server facts) are often wrong for it. Answer from these tools first: the character tools for the player's own character, then ForeverDB (search_items, get_bis, get_recent_changes and the rest) and Forever Companion (companion_*) for game data. Use perplexity_search for anything they do not cover, and kagi_search to find specific pages or when Perplexity comes up thin; put "WoW Forever" in every web query. Treat any web source that is not about Forever as describing another version, and say so if you rely on it.`

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
	// Search is the broad web search (Perplexity).
	Search WebSearcher
	// Kagi is the second web search, returning result pages.
	Kagi KagiSearcher
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
	srv := mcp.NewServer(&mcp.Implementation{Name: serverName, Version: serverVersion}, &mcp.ServerOptions{Instructions: Instructions})
	if deps.Game != nil {
		registerGame(srv, deps.Game)
	}
	if deps.Companion != nil {
		registerCompanion(srv, deps.Companion)
	}
	if deps.Search != nil {
		registerSearch(srv, deps.Search)
	}
	if deps.Kagi != nil {
		registerKagi(srv, deps.Kagi)
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
