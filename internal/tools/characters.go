package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"wowapp/internal/blizzard"
	"wowapp/internal/config"
)

const blizzardSource = "Blizzard WoW Forever profile API"

// charResult is what every character tool returns.
type charResult struct {
	Source    string `json:"source"`
	Character string `json:"character,omitempty"`
	Data      any    `json:"data"`
	Note      string `json:"note,omitempty"`
}

type charIn struct {
	Character string `json:"character,omitempty" jsonschema:"which character: a configured name (Leafy), or name and surname (Leafy Oakheart); leave empty for the main character"`
}

// resolveCharacter picks the character a tool call means. Empty is the main
// character; a bare name must be configured; a name and surname ("Leafy
// Oakheart" or "leafy-oakheart") is used as given, so an unconfigured
// character still works.
func resolveCharacter(configured []config.Character, s string) (config.Character, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return configured[0], nil
	}
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == ' ' || r == '-' })
	for _, c := range configured {
		if len(parts) == 1 && strings.EqualFold(parts[0], c.Name) {
			return c, nil
		}
		if len(parts) == 2 && strings.EqualFold(parts[0], c.Name) && strings.EqualFold(parts[1], c.Surname) {
			return c, nil
		}
	}
	if len(parts) == 2 {
		return config.Character{Name: parts[0], Surname: parts[1]}, nil
	}
	names := make([]string, len(configured))
	for i, c := range configured {
		names[i] = c.Name + " " + c.Surname
	}
	return config.Character{}, fmt.Errorf("no configured character %q; configured: %s. Give a name and surname for any other character", s, strings.Join(names, ", "))
}

// charError is the text the model sees for a failed Blizzard call.
func charError(err error) string {
	if errors.Is(err, blizzard.ErrLoginExpired) {
		return blizzard.ErrLoginExpired.Error()
	}
	return err.Error()
}

func textResult(v any) *mcp.CallToolResult {
	out, err := json.Marshal(v)
	if err != nil {
		return errorResult("could not encode the answer: " + err.Error())
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(out)}}}
}

func addCharTool(srv *mcp.Server, configured []config.Character, name, desc string, fetch func(context.Context, config.Character) (any, error)) {
	mcp.AddTool(srv, &mcp.Tool{Name: name, Description: desc},
		func(ctx context.Context, _ *mcp.CallToolRequest, in charIn) (*mcp.CallToolResult, any, error) {
			c, err := resolveCharacter(configured, in.Character)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			data, err := fetch(ctx, c)
			if err != nil {
				return errorResult(charError(err)), nil, nil
			}
			return textResult(charResult{Source: blizzardSource, Character: c.Name + " " + c.Surname, Data: data}), nil, nil
		})
}

type listCharactersIn struct{}

type listedCharacter struct {
	Name       string `json:"name"`
	Surname    string `json:"surname"`
	Main       bool   `json:"main,omitempty"`
	Configured bool   `json:"configured,omitempty"`
	Level      int    `json:"level,omitempty"`
	Class      string `json:"class,omitempty"`
	Race       string `json:"race,omitempty"`
	Faction    string `json:"faction,omitempty"`
}

func registerCharacters(srv *mcp.Server, chars CharacterSource, configured []config.Character) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_characters",
		Description: "List the person's WoW Forever characters: the configured ones (the main first) and, when logged in to Battle.net, every character on the account with level, class, race and faction.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ listCharactersIn) (*mcp.CallToolResult, any, error) {
		out := make([]listedCharacter, 0, len(configured))
		for i, c := range configured {
			out = append(out, listedCharacter{Name: c.Name, Surname: c.Surname, Main: i == 0, Configured: true})
		}
		res := charResult{Source: blizzardSource}
		account, err := chars.Account(ctx)
		switch {
		case errors.Is(err, blizzard.ErrLoginExpired):
			res.Note = "Only the configured characters are listed: " + blizzard.ErrLoginExpired.Error()
		case err != nil:
			res.Note = "Only the configured characters are listed; the account list failed: " + err.Error()
		}
		for _, a := range account {
			found := false
			for i := range out {
				if strings.EqualFold(out[i].Name, a.Name) && strings.EqualFold(out[i].Surname, a.Surname) {
					out[i].Level, out[i].Class, out[i].Race, out[i].Faction = a.Level, a.Class, a.Race, a.Faction
					found = true
				}
			}
			if !found {
				out = append(out, listedCharacter{Name: a.Name, Surname: a.Surname, Level: a.Level, Class: a.Class, Race: a.Race, Faction: a.Faction})
			}
		}
		res.Data = out
		return textResult(res), nil, nil
	})
	addCharTool(srv, configured, "get_character",
		"A character's overview: level, experience, class, race, faction, guild, average and equipped item level, last login.",
		chars.Summary)
	addCharTool(srv, configured, "get_equipment",
		"What a character is wearing, slot by slot: item name and ID, item level, quality, armor type, stats and enchants. Use with get_bis and get_item to suggest upgrades.",
		chars.Equipment)
	addCharTool(srv, configured, "get_talents",
		"A character's talents: points per tree (e.g. Balance, Feral Combat, Restoration) and each learned talent with its rank and effect.",
		chars.Talents)
	addCharTool(srv, configured, "get_stats",
		"A character's current stats: health, mana, primary stats, armor, attack and spell power, crit, hit, dodge, mana regen and resistances.",
		chars.Stats)
	addCharTool(srv, configured, "get_reputations",
		"A character's reputation with each faction: standing and progress within it.",
		chars.Reputations)
	addCharTool(srv, configured, "get_professions",
		"A character's professions (primary and secondary): skill, maximum skill and known recipes.",
		chars.Professions)
}
