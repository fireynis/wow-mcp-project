package tools

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"wowapp/internal/foreverdb"
	"wowapp/internal/keychain"
)

const (
	foreverDBSource = "https://foreverdb.net"
	// maxListEntries is how many list entries a tool result carries; more are
	// cut so one answer cannot flood the model's context.
	maxListEntries = 25
)

// gameResult is what every ForeverDB tool returns.
type gameResult struct {
	Source    string `json:"source"`
	Data      any    `json:"data"`
	Truncated bool   `json:"truncated,omitempty"`
}

// gameFetch is one ForeverDB lookup made for a tool call.
type gameFetch func(ctx context.Context, g GameSource) (data any, truncated bool, err error)

// get fetches path and caps any list in the answer.
func get(ctx context.Context, g GameSource, path string, q url.Values) (any, bool, error) {
	body, err := g.Get(ctx, path, q)
	if err != nil {
		return nil, false, gameError(path, err)
	}
	data, truncated, err := capList(body)
	if err != nil {
		return nil, false, gameErr("could not trim the ForeverDB answer: " + err.Error())
	}
	return data, truncated, nil
}

// capList cuts the top-level array, or the array under "data" of the
// {"data":..., "meta":...} envelope, to maxListEntries. Everything else is
// passed through untouched.
func capList(body json.RawMessage) (json.RawMessage, bool, error) {
	var arr []json.RawMessage
	if json.Unmarshal(body, &arr) == nil {
		if len(arr) <= maxListEntries {
			return body, false, nil
		}
		cut, err := json.Marshal(arr[:maxListEntries])
		return cut, true, err
	}
	var env map[string]json.RawMessage
	if json.Unmarshal(body, &env) != nil {
		return body, false, nil
	}
	if json.Unmarshal(env["data"], &arr) != nil || len(arr) <= maxListEntries {
		return body, false, nil
	}
	cut, err := json.Marshal(arr[:maxListEntries])
	if err != nil {
		return nil, false, err
	}
	env["data"] = cut
	out, err := json.Marshal(env)
	return out, true, err
}

// gameError is the text the model sees for a failed ForeverDB call. Errors
// from the foreverdb package are already scrubbed of the key.
type gameErr string

func (e gameErr) Error() string { return string(e) }

func gameError(path string, err error) error {
	var rl *foreverdb.RateLimitError
	var he *foreverdb.HTTPError
	switch {
	case errors.Is(err, foreverdb.ErrQuota):
		return gameErr("ForeverDB quota used up for today")
	case errors.Is(err, keychain.ErrNotFound):
		return gameErr("No ForeverDB API key is stored; run `wow-mcp keys set foreverdb` in a terminal, then try again")
	case errors.As(err, &he) && (he.Status == http.StatusUnauthorized || he.Status == http.StatusForbidden):
		return gameErr("ForeverDB rejected the API key (HTTP " + strconv.Itoa(he.Status) + "); check it with `wow-mcp doctor` or store a new one with `wow-mcp keys set foreverdb`.")
	case errors.As(err, &he) && he.Status == http.StatusNotFound:
		return gameErr("ForeverDB has nothing at " + path)
	case errors.As(err, &rl):
		return gameErr(rl.Error())
	}
	return gameErr(err.Error())
}

// addGameTool registers a ForeverDB tool. plan turns the input into the
// lookup to run, or an error for bad input.
func addGameTool[In any](srv *mcp.Server, g GameSource, name, desc string, plan func(In) (gameFetch, error)) {
	mcp.AddTool(srv, &mcp.Tool{Name: name, Description: desc},
		func(ctx context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, any, error) {
			fetch, err := plan(in)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			data, truncated, err := fetch(ctx, g)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			out, err := json.Marshal(gameResult{Source: foreverDBSource, Data: data, Truncated: truncated})
			if err != nil {
				return errorResult("could not encode the ForeverDB answer: " + err.Error()), nil, nil
			}
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(out)}}}, nil, nil
		})
}

// single is a plan for one GET of path with query q.
func single(path string, q url.Values) (gameFetch, error) {
	return func(ctx context.Context, g GameSource) (any, bool, error) { return get(ctx, g, path, q) }, nil
}

func paged(q url.Values) url.Values {
	q.Set("limit", "25")
	return q
}

// pagedAt is paged with the optional offset into the full list.
func pagedAt(offset int) url.Values {
	q := paged(url.Values{})
	setPositive(q, "offset", offset)
	return q
}

// setPositive sets k when v is above zero (zero means "not given").
func setPositive(q url.Values, k string, v int) {
	if v > 0 {
		q.Set(k, strconv.Itoa(v))
	}
}

func setIf(q url.Values, k, v string) {
	if v = strings.TrimSpace(v); v != "" {
		q.Set(k, v)
	}
}

// checkSegment rejects a value that is empty or is a dot segment: url.PathEscape
// leaves "." and ".." as they are, so they would climb out of the endpoint.
func checkSegment(what, v string) error {
	switch v {
	case "":
		return errors.New(what + " is required")
	case ".", "..":
		return errors.New(what + " " + strconv.Quote(v) + " is not a valid identifier")
	}
	return nil
}

// byID plans GET <prefix>/<id> for a required identifier.
func byID(prefix, what, v string) (gameFetch, error) {
	v = strings.TrimSpace(v)
	if err := checkSegment(what, v); err != nil {
		return nil, err
	}
	return single(prefix+"/"+url.PathEscape(v), nil)
}

type (
	// pageIn is embedded in every tool that lists 25 entries at a time.
	pageIn struct {
		Offset int `json:"offset,omitempty" jsonschema:"entries to skip, for the next page: 25, then 50, and so on"`
	}
	searchItemsIn struct {
		pageIn
		Query    string `json:"query" jsonschema:"part of the item name"`
		Slot     string `json:"slot,omitempty" jsonschema:"inventory slot type ID (1 head, 5 chest, ...)"`
		Class    string `json:"class,omitempty" jsonschema:"item class ID (2 weapons, 4 armour, ...)"`
		Quality  *int   `json:"quality,omitempty" jsonschema:"0 Poor to 5 Legendary"`
		MinLevel int    `json:"minLevel,omitempty" jsonschema:"lowest required level"`
		MaxLevel int    `json:"maxLevel,omitempty" jsonschema:"highest required level"`
	}
	idIn struct {
		ID string `json:"id" jsonschema:"the numeric ID"`
	}
	listIn struct{ pageIn }
	bisIn  struct {
		Spec  string `json:"spec,omitempty" jsonschema:"spec ID such as hunter-beast-mastery; leave empty to list valid IDs"`
		Level int    `json:"level,omitempty" jsonschema:"character level 1 to 60; pass the character's level, otherwise the level cap is assumed"`
		Mode  string `json:"mode,omitempty" jsonschema:"pve (default) or pvp"`
		Side  string `json:"side,omitempty" jsonschema:"a for Alliance items only, h for Horde"`
		Easy  bool   `json:"easy,omitempty" jsonschema:"true hides hard to get picks: raids, high PvP ranks, rare drops"`
	}
	searchQuestsIn struct {
		pageIn
		Query    string `json:"query" jsonschema:"part of the quest name"`
		Zone     string `json:"zone,omitempty" jsonschema:"part of the zone name"`
		MinLevel int    `json:"minLevel,omitempty" jsonschema:"lowest quest level"`
		MaxLevel int    `json:"maxLevel,omitempty" jsonschema:"highest quest level"`
		Side     string `json:"side,omitempty" jsonschema:"alliance or horde"`
	}
	classIn struct {
		Name string `json:"name" jsonschema:"class name, for example druid"`
	}
	professionIn struct {
		pageIn
		Slug string `json:"slug" jsonschema:"profession slug, for example alchemy or first-aid"`
	}
	changesIn struct {
		pageIn
		Class string `json:"class,omitempty" jsonschema:"class name or ID, for example druid; narrows the buffs and nerfs"`
	}
	articlesIn struct {
		pageIn
		Query string `json:"query,omitempty" jsonschema:"text to look for"`
	}
	articleIn struct {
		Slug string `json:"slug" jsonschema:"the article slug from search_articles"`
	}
)

type noIn struct{}

const pageNote = " Lists come 25 entries at a time; the total is in meta, and offset 25, 50, ... fetches the next pages; a truncated flag means the answer was cut."

// registerGame adds the ForeverDB tools.
func registerGame(srv *mcp.Server, g GameSource) {
	addGameTool(srv, g, "search_items", "Search WoW Forever items by name, optionally by slot, item class, quality or required level (pass the character's level as maxLevel to find gear they can use)."+pageNote,
		func(in searchItemsIn) (gameFetch, error) {
			q := pagedAt(in.Offset)
			setPositive(q, "minLevel", in.MinLevel)
			setPositive(q, "maxLevel", in.MaxLevel)
			if in.Quality != nil {
				q.Set("quality", strconv.Itoa(*in.Quality))
			}
			setIf(q, "q", in.Query)
			setIf(q, "slot", in.Slot)
			setIf(q, "class", in.Class)
			return single("/items", q)
		})
	addGameTool(srv, g, "get_item", "Get one WoW Forever item by ID: stats, effects, binding, price and live-client changes.",
		func(in idIn) (gameFetch, error) { return byID("/items", "id", in.ID) })
	addGameTool(srv, g, "list_dungeons", "List WoW Forever dungeons and raids with level ranges, lowest level first."+pageNote,
		func(in listIn) (gameFetch, error) { return single("/dungeons", pagedAt(in.Offset)) })
	addGameTool(srv, g, "get_dungeon", "Get one dungeon by ID (from list_dungeons): bosses and notable loot.",
		func(in idIn) (gameFetch, error) { return byID("/dungeons", "id", in.ID) })
	addGameTool(srv, g, "get_bis", "Get best-in-slot gear and enchants for a class spec. Call it without a spec first to see the valid spec IDs, then again with one. Always pass the character's level; without it the level cap is assumed.",
		func(in bisIn) (gameFetch, error) {
			if strings.TrimSpace(in.Spec) == "" {
				return single("/bis", nil)
			}
			spec := strings.TrimSpace(in.Spec)
			if err := checkSegment("spec", spec); err != nil {
				return nil, err
			}
			q := url.Values{}
			setPositive(q, "level", in.Level)
			setIf(q, "mode", in.Mode)
			setIf(q, "side", in.Side)
			if in.Easy {
				q.Set("easy", "1")
			}
			return single("/bis/"+url.PathEscape(spec), q)
		})
	addGameTool(srv, g, "search_quests", "Search WoW Forever quests by name, optionally within a zone, a quest level range (pass the character's level) or a side."+pageNote,
		func(in searchQuestsIn) (gameFetch, error) {
			q := pagedAt(in.Offset)
			setPositive(q, "minLevel", in.MinLevel)
			setPositive(q, "maxLevel", in.MaxLevel)
			setIf(q, "side", in.Side)
			setIf(q, "q", in.Query)
			setIf(q, "zone", in.Zone)
			return single("/quests", q)
		})
	addGameTool(srv, g, "get_quest", "Get one quest by ID: guide, chain, who starts and ends it, and where.",
		func(in idIn) (gameFetch, error) { return byID("/quests", "id", in.ID) })
	addGameTool(srv, g, "list_zones", "List world map zones and continents with their map IDs."+pageNote,
		func(in listIn) (gameFetch, error) { return single("/zones", pagedAt(in.Offset)) })
	addGameTool(srv, g, "get_vendor", "Get one vendor by ID: where they stand and everything they sell, with prices.",
		func(in idIn) (gameFetch, error) { return byID("/vendors", "id", in.ID) })
	addGameTool(srv, g, "get_itemset", "Get one item set by ID: its pieces and set bonuses.",
		func(in idIn) (gameFetch, error) { return byID("/itemsets", "id", in.ID) })
	addGameTool(srv, g, "get_class", "Get one class's abilities and full talent trees by class name.",
		func(in classIn) (gameFetch, error) { return byID("/classes", "name", strings.ToLower(in.Name)) })
	addGameTool(srv, g, "get_profession", "Get one profession's ranks, trainers and recipes by slug."+pageNote,
		func(in professionIn) (gameFetch, error) {
			slug := strings.TrimSpace(in.Slug)
			if err := checkSegment("slug", slug); err != nil {
				return nil, err
			}
			return single("/professions/"+url.PathEscape(slug), pagedAt(in.Offset))
		})
	addGameTool(srv, g, "get_endgame", "Get how much of WoW Forever's level 40-60 game, raids included, players have confirmed.",
		func(noIn) (gameFetch, error) { return single("/endgame", nil) })
	addGameTool(srv, g, "get_recent_changes", "Get recent class buffs and nerfs per build together with what players saw differ from the game files. Pass class to narrow the buffs and nerfs to one class."+pageNote,
		func(in changesIn) (gameFetch, error) {
			return func(ctx context.Context, g GameSource) (any, bool, error) {
				bq := pagedAt(in.Offset)
				setIf(bq, "class", strings.ToLower(in.Class))
				bn, t1, err := get(ctx, g, "/buffs-nerfs", bq)
				if err != nil {
					return nil, false, err
				}
				cs, t2, err := get(ctx, g, "/changes-seen", pagedAt(in.Offset))
				if err != nil {
					return nil, false, err
				}
				return map[string]any{"buffs_nerfs": bn, "changes_seen": cs}, t1 || t2, nil
			}, nil
		})
	addGameTool(srv, g, "search_articles", "List ForeverDB news and articles, newest first, optionally filtered by text."+pageNote,
		func(in articlesIn) (gameFetch, error) {
			q := pagedAt(in.Offset)
			setIf(q, "q", in.Query)
			return single("/articles", q)
		})
	addGameTool(srv, g, "get_article", "Get one ForeverDB article by slug: its blocks and sources.",
		func(in articleIn) (gameFetch, error) { return byID("/articles", "slug", in.Slug) })
}
