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

	"wowapp/internal/forevercompanion"
)

// CompanionSource answers read-only Forever Companion queries.
type CompanionSource interface {
	Get(ctx context.Context, path string, q url.Values) (forevercompanion.Answer, error)
}

// companionResult is what every Forever Companion tool returns. Attribution
// carries the credit the API asks for, with its link.
type companionResult struct {
	Source      string          `json:"source"`
	Attribution string          `json:"attribution"`
	Data        json.RawMessage `json:"data"`
	Page        json.RawMessage `json:"page,omitempty"`
	Truncated   bool            `json:"truncated,omitempty"`
}

func companionError(path string, err error) string {
	var rl *forevercompanion.RateLimitError
	var he *forevercompanion.HTTPError
	switch {
	case errors.Is(err, forevercompanion.ErrNotFound):
		return "Forever Companion has nothing at " + path
	case errors.As(err, &rl):
		return rl.Error()
	case errors.As(err, &he) && he.Status >= http.StatusInternalServerError:
		return "Forever Companion is unavailable right now (HTTP " + strconv.Itoa(he.Status) + ")"
	}
	return err.Error()
}

func addCompanionTool[In any](srv *mcp.Server, src CompanionSource, name, desc string, plan func(In) (string, url.Values, error)) {
	mcp.AddTool(srv, &mcp.Tool{Name: name, Description: desc + " Source: Forever Companion (forevercompanion.io), a fan site; no key needed."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, any, error) {
			path, q, err := plan(in)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			a, err := src.Get(ctx, path, q)
			if err != nil {
				return errorResult(companionError(path, err)), nil, nil
			}
			data, truncated, err := capList(a.Data)
			if err != nil {
				return errorResult("could not trim the Forever Companion answer: " + err.Error()), nil, nil
			}
			return textResult(companionResult{
				Source: forevercompanion.Site, Attribution: a.Attribution,
				Data: data, Page: a.Page, Truncated: truncated,
			}), nil, nil
		})
}

// faction checks an optional faction filter.
func faction(q url.Values, v string) error {
	switch v = strings.ToLower(strings.TrimSpace(v)); v {
	case "":
		return nil
	case "alliance", "horde":
		q.Set("faction", v)
		return nil
	}
	return errors.New(`faction must be "alliance" or "horde"`)
}

func levels(q url.Values, min, max int) {
	setPositive(q, "minLevel", min)
	setPositive(q, "maxLevel", max)
}

type (
	levelRangeIn struct {
		Faction  string `json:"faction,omitempty" jsonschema:"alliance or horde; leave empty for both"`
		MinLevel int    `json:"minLevel,omitempty" jsonschema:"lowest level of the range"`
		MaxLevel int    `json:"maxLevel,omitempty" jsonschema:"highest level of the range"`
	}
	companionDungeonsIn struct {
		levelRangeIn
		Query string `json:"q,omitempty" jsonschema:"part of the dungeon name"`
	}
	companionSlugIn struct {
		ID string `json:"id" jsonschema:"the dungeon ID from companion_list_dungeons, such as the-deadmines"`
	}
	companionQuestsIn struct {
		levelRangeIn
		pageIn
		Type    string `json:"type,omitempty" jsonschema:"dungeon or elite; leave empty for both"`
		Dungeon string `json:"dungeon,omitempty" jsonschema:"dungeon ID such as the-deadmines"`
		Class   string `json:"class,omitempty" jsonschema:"class name such as druid"`
		Query   string `json:"q,omitempty" jsonschema:"part of the quest name"`
	}
	companionNumIn struct {
		ID int `json:"id" jsonschema:"the numeric game ID"`
	}
	companionItemsIn struct {
		pageIn
		Query    string `json:"q,omitempty" jsonschema:"part of the item name"`
		Quality  string `json:"quality,omitempty" jsonschema:"poor, common, uncommon, rare, epic or legendary"`
		Slot     string `json:"slot,omitempty" jsonschema:"slot name such as Chest or Finger"`
		IDs      string `json:"ids,omitempty" jsonschema:"comma-separated item IDs, to look up several at once"`
		MinLevel int    `json:"minLevel,omitempty" jsonschema:"lowest required level"`
		MaxLevel int    `json:"maxLevel,omitempty" jsonschema:"highest required level"`
	}
	companionRaresIn struct {
		Zone    string `json:"zone,omitempty" jsonschema:"zone name such as Westfall"`
		Faction string `json:"faction,omitempty" jsonschema:"alliance or horde"`
		Query   string `json:"q,omitempty" jsonschema:"part of the rare's name"`
	}
	companionClassQuestsIn struct {
		Class   string `json:"class,omitempty" jsonschema:"class name such as druid"`
		Faction string `json:"faction,omitempty" jsonschema:"alliance or horde"`
	}
)

func companionPaged(q url.Values, offset int) {
	q.Set("limit", strconv.Itoa(maxListEntries))
	setPositive(q, "offset", offset)
}

func registerCompanion(srv *mcp.Server, src CompanionSource) {
	addCompanionTool(srv, src, "companion_list_dungeons",
		"List WoW Forever dungeons with level range, zone, entrance, faction access, bosses and whether the dungeon is new in Forever. Filter by level range to find dungeons for a character's level.",
		func(in companionDungeonsIn) (string, url.Values, error) {
			q := url.Values{}
			if err := faction(q, in.Faction); err != nil {
				return "", nil, err
			}
			levels(q, in.MinLevel, in.MaxLevel)
			setIf(q, "q", in.Query)
			return "/dungeons", q, nil
		})
	addCompanionTool(srv, src, "companion_get_dungeon",
		"One WoW Forever dungeon in full: bosses, its quests with Forever XP and rewards, and its notable loot.",
		func(in companionSlugIn) (string, url.Values, error) {
			id := strings.ToLower(strings.TrimSpace(in.ID))
			if err := checkSegment("id", id); err != nil {
				return "", nil, err
			}
			return "/dungeons/" + url.PathEscape(id), nil, nil
		})
	addCompanionTool(srv, src, "companion_search_quests",
		"Search WoW Forever dungeon and elite quests: level, Forever XP, giver, objective, rewards and prerequisites. 25 at a time.",
		func(in companionQuestsIn) (string, url.Values, error) {
			q := url.Values{}
			if err := faction(q, in.Faction); err != nil {
				return "", nil, err
			}
			switch t := strings.ToLower(strings.TrimSpace(in.Type)); t {
			case "", "dungeon", "elite":
				setIf(q, "type", t)
			default:
				return "", nil, errors.New(`type must be "dungeon" or "elite"`)
			}
			levels(q, in.MinLevel, in.MaxLevel)
			setIf(q, "dungeon", strings.ToLower(in.Dungeon))
			setIf(q, "class", strings.ToLower(in.Class))
			setIf(q, "q", in.Query)
			companionPaged(q, in.Offset)
			return "/quests", q, nil
		})
	addCompanionTool(srv, src, "companion_get_quest",
		"One WoW Forever quest by its game quest ID: XP, giver, objective, rewards and prerequisites.",
		func(in companionNumIn) (string, url.Values, error) {
			if in.ID <= 0 {
				return "", nil, errors.New("id must be a positive quest ID")
			}
			return "/quests/" + strconv.Itoa(in.ID), nil, nil
		})
	addCompanionTool(srv, src, "companion_search_items",
		"Search WoW Forever items: quality, item level, required level, slot, armor type, stats and where each drops or which quest rewards it. 25 at a time.",
		func(in companionItemsIn) (string, url.Values, error) {
			q := url.Values{}
			switch v := strings.ToLower(strings.TrimSpace(in.Quality)); v {
			case "", "poor", "common", "uncommon", "rare", "epic", "legendary":
				setIf(q, "quality", v)
			default:
				return "", nil, errors.New("quality must be poor, common, uncommon, rare, epic or legendary")
			}
			setIf(q, "q", in.Query)
			setIf(q, "slot", in.Slot)
			setIf(q, "ids", strings.ReplaceAll(in.IDs, " ", ""))
			levels(q, in.MinLevel, in.MaxLevel)
			companionPaged(q, in.Offset)
			return "/items", q, nil
		})
	addCompanionTool(srv, src, "companion_get_item",
		"One WoW Forever item by ID: full tooltip (marking what Forever changed), stats, and the bosses that drop it or quests that reward it.",
		func(in companionNumIn) (string, url.Values, error) {
			if in.ID <= 0 {
				return "", nil, errors.New("id must be a positive item ID")
			}
			return "/items/" + strconv.Itoa(in.ID), nil, nil
		})
	addCompanionTool(srv, src, "companion_list_rares",
		"WoW Forever rare mobs: zone, level, map coordinates and loot.",
		func(in companionRaresIn) (string, url.Values, error) {
			q := url.Values{}
			if err := faction(q, in.Faction); err != nil {
				return "", nil, err
			}
			setIf(q, "zone", in.Zone)
			setIf(q, "q", in.Query)
			return "/rares", q, nil
		})
	addCompanionTool(srv, src, "companion_list_zones",
		"WoW Forever questing zones with their level ranges, including the new Forever zones. Filter by level range to pick where to quest next.",
		func(in levelRangeIn) (string, url.Values, error) {
			q := url.Values{}
			if err := faction(q, in.Faction); err != nil {
				return "", nil, err
			}
			levels(q, in.MinLevel, in.MaxLevel)
			return "/zones", q, nil
		})
	addCompanionTool(srv, src, "companion_list_reputations",
		"WoW Forever reputation factions and how to gain standing with them.",
		func(noIn) (string, url.Values, error) { return "/reputations", nil, nil })
	addCompanionTool(srv, src, "companion_class_quests",
		"WoW Forever class quests: the quests that unlock forms, pets, stances, totems and other class abilities.",
		func(in companionClassQuestsIn) (string, url.Values, error) {
			q := url.Values{}
			if err := faction(q, in.Faction); err != nil {
				return "", nil, err
			}
			setIf(q, "class", strings.ToLower(in.Class))
			return "/class-quests", q, nil
		})
}
