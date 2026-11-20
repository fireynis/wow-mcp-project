package tools

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"wowapp/internal/forevercompanion"
)

type fakeCompanion struct {
	path  string
	query url.Values
	data  string
	err   error
}

func (f *fakeCompanion) Get(_ context.Context, path string, q url.Values) (forevercompanion.Answer, error) {
	f.path, f.query = path, q
	if f.err != nil {
		return forevercompanion.Answer{}, f.err
	}
	data := f.data
	if data == "" {
		data = `{"ok":true}`
	}
	return forevercompanion.Answer{Data: json.RawMessage(data), Attribution: "Data from Forever Companion (https://forevercompanion.io). Please link back."}, nil
}

func callCompanion(t *testing.T, f *fakeCompanion, name string, args map[string]any) (*mcp.CallToolResult, string) {
	t.Helper()
	cs := connect(t, New(Deps{Companion: f}))
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	return res, res.Content[0].(*mcp.TextContent).Text
}

func TestCompanionToolsRegistered(t *testing.T) {
	cs := connect(t, New(Deps{Companion: &fakeCompanion{}}))
	list, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range list.Tools {
		names = append(names, tool.Name)
		if !strings.Contains(tool.Description, "Forever Companion") {
			t.Errorf("%s description does not name the source", tool.Name)
		}
	}
	want := []string{"companion_class_quests", "companion_get_dungeon", "companion_get_item", "companion_get_quest", "companion_list_dungeons", "companion_list_rares", "companion_list_reputations", "companion_list_zones", "companion_search_items", "companion_search_quests"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Errorf("tools = %v\nwant    %v", names, want)
	}
}

func TestCompanionResultCarriesAttribution(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "forevercompanion", "dungeon.json"))
	if err != nil {
		t.Fatal(err)
	}
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	json.Unmarshal(b, &env)
	f := &fakeCompanion{data: string(env.Data)}
	res, text := callCompanion(t, f, "companion_get_dungeon", map[string]any{"id": "The-Deadmines"})
	if res.IsError || f.path != "/dungeons/the-deadmines" {
		t.Fatalf("path %q, result %.200s", f.path, text)
	}
	var out companionResult
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatal(err)
	}
	if out.Source != "https://forevercompanion.io" || !strings.Contains(out.Attribution, "link back") || !strings.Contains(string(out.Data), "Edwin VanCleef") {
		t.Errorf("result = %.300s", text)
	}
}

func TestCompanionQueries(t *testing.T) {
	for _, tc := range []struct {
		tool string
		args map[string]any
		path string
		q    string
	}{
		{"companion_list_dungeons", map[string]any{"faction": "Alliance", "minLevel": 15, "maxLevel": 20}, "/dungeons", "faction=alliance&maxLevel=20&minLevel=15"},
		{"companion_search_quests", map[string]any{"type": "dungeon", "dungeon": "The-Deadmines", "class": "Druid", "offset": 25}, "/quests", "class=druid&dungeon=the-deadmines&limit=25&offset=25&type=dungeon"},
		{"companion_get_quest", map[string]any{"id": 214}, "/quests/214", ""},
		{"companion_search_items", map[string]any{"q": "robe", "quality": "Rare", "ids": "6465, 2041"}, "/items", "ids=6465%2C2041&limit=25&q=robe&quality=rare"},
		{"companion_get_item", map[string]any{"id": 6465}, "/items/6465", ""},
		{"companion_list_rares", map[string]any{"zone": "Westfall"}, "/rares", "zone=Westfall"},
		{"companion_list_zones", map[string]any{"minLevel": 15, "maxLevel": 25}, "/zones", "maxLevel=25&minLevel=15"},
		{"companion_list_reputations", nil, "/reputations", ""},
		{"companion_class_quests", map[string]any{"class": "Druid", "faction": "alliance"}, "/class-quests", "class=druid&faction=alliance"},
	} {
		f := &fakeCompanion{}
		res, text := callCompanion(t, f, tc.tool, tc.args)
		if res.IsError || f.path != tc.path || f.query.Encode() != tc.q {
			t.Errorf("%s: %s?%s, want %s?%s (%s)", tc.tool, f.path, f.query.Encode(), tc.path, tc.q, text)
		}
	}
}

func TestCompanionRejectsBadInput(t *testing.T) {
	for _, tc := range []struct {
		tool string
		args map[string]any
	}{
		{"companion_list_dungeons", map[string]any{"faction": "neutral"}},
		{"companion_get_dungeon", map[string]any{"id": ".."}},
		{"companion_get_quest", map[string]any{"id": 0}},
		{"companion_search_quests", map[string]any{"type": "raid"}},
		{"companion_search_items", map[string]any{"quality": "shiny"}},
	} {
		f := &fakeCompanion{}
		if res, text := callCompanion(t, f, tc.tool, tc.args); !res.IsError || f.path != "" {
			t.Errorf("%s %v: want an error without a request, got %s", tc.tool, tc.args, text)
		}
	}
}

func TestCompanionTruncatesLongLists(t *testing.T) {
	items := make([]string, 40)
	for i := range items {
		items[i] = `{"id":` + string(rune('0'+i%10)) + `}`
	}
	f := &fakeCompanion{data: "[" + strings.Join(items, ",") + "]"}
	_, text := callCompanion(t, f, "companion_list_rares", nil)
	var out struct {
		Data      []json.RawMessage `json:"data"`
		Truncated bool              `json:"truncated"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Data) != 25 || !out.Truncated {
		t.Errorf("%d entries, truncated %v; want 25 and true", len(out.Data), out.Truncated)
	}
}

func TestCompanionErrors(t *testing.T) {
	f := &fakeCompanion{err: forevercompanion.ErrNotFound}
	if res, text := callCompanion(t, f, "companion_get_dungeon", map[string]any{"id": "nope"}); !res.IsError || text != "Forever Companion has nothing at /dungeons/nope" {
		t.Errorf("result %q", text)
	}
	f = &fakeCompanion{err: &forevercompanion.RateLimitError{}}
	if res, text := callCompanion(t, f, "companion_list_zones", nil); !res.IsError || !strings.Contains(text, "rate limit") {
		t.Errorf("result %q", text)
	}
}
