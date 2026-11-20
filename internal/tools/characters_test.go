package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"wowapp/internal/blizzard"
	"wowapp/internal/config"
)

var (
	mainChar = config.Character{Name: "Leafy", Surname: "Oakheart"}
	altChar  = config.Character{Name: "Twink", Surname: "Oakheart"}
)

// fakeChars records which character each call was for.
type fakeChars struct {
	asked      []config.Character
	err        error
	account    []blizzard.AccountCharacter
	accountErr error
}

func (f *fakeChars) Account(context.Context) ([]blizzard.AccountCharacter, error) {
	return f.account, f.accountErr
}

func (f *fakeChars) one(c config.Character, what string) (any, error) {
	f.asked = append(f.asked, c)
	if f.err != nil {
		return nil, f.err
	}
	return map[string]string{"what": what, "who": c.Name}, nil
}

func (f *fakeChars) Summary(_ context.Context, c config.Character) (any, error) {
	return f.one(c, "summary")
}
func (f *fakeChars) Equipment(_ context.Context, c config.Character) (any, error) {
	return f.one(c, "equipment")
}
func (f *fakeChars) Talents(_ context.Context, c config.Character) (any, error) {
	return f.one(c, "talents")
}
func (f *fakeChars) Stats(_ context.Context, c config.Character) (any, error) {
	return f.one(c, "stats")
}
func (f *fakeChars) Reputations(_ context.Context, c config.Character) (any, error) {
	return f.one(c, "reputations")
}
func (f *fakeChars) Professions(_ context.Context, c config.Character) (any, error) {
	return f.one(c, "professions")
}

func callChar(t *testing.T, f *fakeChars, name string, args map[string]any) (*mcp.CallToolResult, string) {
	t.Helper()
	cs := connect(t, New(Deps{Chars: f, Characters: []config.Character{mainChar, altChar}}))
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	return res, res.Content[0].(*mcp.TextContent).Text
}

func TestCharacterToolsRegistered(t *testing.T) {
	cs := connect(t, New(Deps{Chars: &fakeChars{}, Characters: []config.Character{mainChar}}))
	list, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, tool := range list.Tools {
		got[tool.Name] = true
	}
	for _, name := range []string{"list_characters", "get_character", "get_equipment", "get_talents", "get_stats", "get_reputations", "get_professions"} {
		if !got[name] {
			t.Errorf("tool %s not registered", name)
		}
	}
	if len(got) != 7 {
		t.Errorf("%d tools, want only the 7 character tools without a game source", len(got))
	}
}

func TestDefaultsToMain(t *testing.T) {
	f := &fakeChars{}
	res, text := callChar(t, f, "get_equipment", nil)
	if res.IsError || len(f.asked) != 1 || f.asked[0] != mainChar {
		t.Fatalf("asked %v, result %s", f.asked, text)
	}
	var out charResult
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatal(err)
	}
	if out.Source != blizzardSource || out.Character != "Leafy Oakheart" {
		t.Errorf("result = %+v", out)
	}
}

func TestResolvesConfiguredAndFullNames(t *testing.T) {
	for in, want := range map[string]config.Character{
		"twink":          altChar,
		"Twink Oakheart": altChar,
		"leafy-oakheart": mainChar,
		"Other Person":   {Name: "Other", Surname: "Person"},
	} {
		f := &fakeChars{}
		if res, text := callChar(t, f, "get_stats", map[string]any{"character": in}); res.IsError || f.asked[0] != want {
			t.Errorf("%q: asked %v, want %v (%s)", in, f.asked, want, text)
		}
	}
}

func TestUnknownCharacterNamesConfigured(t *testing.T) {
	f := &fakeChars{}
	res, text := callChar(t, f, "get_talents", map[string]any{"character": "Nobody"})
	if !res.IsError || len(f.asked) != 0 {
		t.Fatalf("want an error without a call, got %s", text)
	}
	if !strings.Contains(text, "Leafy Oakheart") || !strings.Contains(text, "Twink Oakheart") {
		t.Errorf("error %q does not list the configured characters", text)
	}
}

func TestLoginExpiredSurfacesExactCopy(t *testing.T) {
	f := &fakeChars{err: blizzard.ErrLoginExpired}
	res, text := callChar(t, f, "get_character", nil)
	if !res.IsError || text != "Your Battle.net login has expired; run wow-mcp login." {
		t.Errorf("result %q, want the exact login copy", text)
	}
}

func TestListCharactersMergesAccount(t *testing.T) {
	f := &fakeChars{account: []blizzard.AccountCharacter{
		{Name: "Leafy", Surname: "Oakheart", Level: 17, Class: "Druid", Race: "Night Elf", Faction: "Alliance"},
		{Name: "Bank", Surname: "Alt", Level: 1, Class: "Mage", Race: "Human", Faction: "Alliance"},
	}}
	res, text := callChar(t, f, "list_characters", nil)
	if res.IsError {
		t.Fatal(text)
	}
	var out struct {
		Data []listedCharacter `json:"data"`
		Note string            `json:"note"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Data) != 3 || !out.Data[0].Main || out.Data[0].Level != 17 || out.Data[1].Name != "Twink" || out.Data[2].Name != "Bank" || out.Data[2].Configured {
		t.Errorf("characters = %+v", out.Data)
	}
	if out.Note != "" {
		t.Errorf("note = %q, want none", out.Note)
	}
}

func TestListCharactersWithoutLogin(t *testing.T) {
	f := &fakeChars{accountErr: blizzard.ErrLoginExpired}
	res, text := callChar(t, f, "list_characters", nil)
	if res.IsError || !strings.Contains(text, "Twink") || !strings.Contains(text, "run wow-mcp login") {
		t.Errorf("result %s, want the configured characters and the login hint", text)
	}
}
