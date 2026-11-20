package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"wowapp/internal/foreverdb"
	"wowapp/internal/keychain"
)

// fakeGame serves files from testdata/foreverdb, keyed by request path, and
// records what was asked.
type fakeGame struct {
	mu    sync.Mutex
	files map[string]string // path -> fixture file name
	raw   map[string]string // path -> inline JSON
	err   error
	calls []call
}

type call struct {
	path  string
	query url.Values
}

func (f *fakeGame) Get(_ context.Context, path string, q url.Values) (json.RawMessage, error) {
	f.mu.Lock()
	f.calls = append(f.calls, call{path, q})
	f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	if s, ok := f.raw[path]; ok {
		return json.RawMessage(s), nil
	}
	if name, ok := f.files[path]; ok {
		b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "foreverdb", name))
		if err != nil {
			return nil, err
		}
		return b, nil
	}
	return json.RawMessage(`{"data":{},"meta":{}}`), nil
}

func callTool(t *testing.T, g GameSource, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	cs := connect(t, New(Deps{Game: g}))
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool %s: %v", name, err)
	}
	return res
}

func text(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if len(res.Content) != 1 {
		t.Fatalf("content = %+v", res.Content)
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("content = %T", res.Content[0])
	}
	return tc.Text
}

func decode(t *testing.T, res *mcp.CallToolResult) map[string]json.RawMessage {
	t.Helper()
	if res.IsError {
		t.Fatalf("error result: %s", text(t, res))
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text(t, res)), &m); err != nil {
		t.Fatalf("result is not JSON: %v\n%s", err, text(t, res))
	}
	return m
}

var gameToolNames = []string{
	"search_items", "get_item", "list_dungeons", "get_dungeon", "get_bis",
	"search_quests", "get_quest", "list_zones", "get_vendor", "get_itemset",
	"get_class", "get_profession", "get_endgame", "get_recent_changes",
	"search_articles", "get_article",
}

func TestGameToolsRegistered(t *testing.T) {
	cs := connect(t, New(Deps{Game: &fakeGame{}}))
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, tl := range res.Tools {
		if tl.Description == "" {
			t.Errorf("%s has no description", tl.Name)
		}
		got = append(got, tl.Name)
	}
	want := append([]string(nil), gameToolNames...)
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("tools = %v\nwant    %v", got, want)
	}
}

func TestGetBisPassesSpec(t *testing.T) {
	g := &fakeGame{files: map[string]string{"/bis/balance-druid": "bis-balance-druid.json"}}
	res := callTool(t, g, "get_bis", map[string]any{"spec": "balance-druid"})
	m := decode(t, res)
	if string(m["source"]) != `"https://foreverdb.net"` {
		t.Errorf("source = %s", m["source"])
	}
	if !strings.Contains(string(m["data"]), "Halo of Transcendence") {
		t.Errorf("data = %s", m["data"])
	}
	if len(g.calls) != 1 || g.calls[0].path != "/bis/balance-druid" {
		t.Errorf("calls = %+v", g.calls)
	}
}

func TestGetBisWithoutSpecCallsIndex(t *testing.T) {
	g := &fakeGame{files: map[string]string{"/bis": "bis-index.json"}}
	res := callTool(t, g, "get_bis", map[string]any{})
	decode(t, res)
	if len(g.calls) != 1 || g.calls[0].path != "/bis" {
		t.Errorf("calls = %+v", g.calls)
	}
}

func bigList(n int) string {
	var items []string
	for i := 0; i < n; i++ {
		items = append(items, fmt.Sprintf(`{"id":%d}`, i))
	}
	return strings.Join(items, ",")
}

func dataLen(t *testing.T, m map[string]json.RawMessage) int {
	t.Helper()
	var d []json.RawMessage
	if err := json.Unmarshal(m["data"], &d); err != nil {
		// envelope: {"data":[...],"meta":...}
		var env struct {
			Data []json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(m["data"], &env); err != nil {
			t.Fatalf("data = %s", m["data"])
		}
		return len(env.Data)
	}
	return len(d)
}

func TestListTruncatesAt25(t *testing.T) {
	for name, body := range map[string]string{
		"envelope": `{"data":[` + bigList(60) + `],"meta":{"total":60}}`,
		"bare":     `[` + bigList(60) + `]`,
	} {
		t.Run(name, func(t *testing.T) {
			g := &fakeGame{raw: map[string]string{"/items": body}}
			m := decode(t, callTool(t, g, "search_items", map[string]any{"query": "sword"}))
			if n := dataLen(t, m); n != 25 {
				t.Errorf("entries = %d, want 25", n)
			}
			if string(m["truncated"]) != "true" {
				t.Errorf("truncated = %s", m["truncated"])
			}
		})
	}
}

func TestShortListNotTruncated(t *testing.T) {
	g := &fakeGame{files: map[string]string{"/items": "items.json"}}
	m := decode(t, callTool(t, g, "search_items", map[string]any{"query": "x"}))
	if _, ok := m["truncated"]; ok {
		t.Errorf("unexpected truncated flag: %s", m["truncated"])
	}
	if n := dataLen(t, m); n != 2 {
		t.Errorf("entries = %d", n)
	}
}

func TestQuotaErrorIsReadable(t *testing.T) {
	res := callTool(t, &fakeGame{err: foreverdb.ErrQuota}, "get_item", map[string]any{"id": "19019"})
	if !res.IsError || text(t, res) != "ForeverDB quota used up for today" {
		t.Errorf("result = %v %q", res.IsError, text(t, res))
	}
}

func TestMissingKeyNamesCommand(t *testing.T) {
	err := fmt.Errorf("ForeverDB key: %w", keychain.ErrNotFound)
	res := callTool(t, &fakeGame{err: err}, "get_item", map[string]any{"id": "1"})
	if !res.IsError || !strings.Contains(text(t, res), "wow-mcp keys set foreverdb") {
		t.Errorf("result = %v %q", res.IsError, text(t, res))
	}
}

func TestNotFoundAndOtherErrorsAreReadable(t *testing.T) {
	res := callTool(t, &fakeGame{err: &foreverdb.HTTPError{Status: 404, Body: "nope"}}, "get_bis", map[string]any{"spec": "druid-nope"})
	if !res.IsError || text(t, res) != "ForeverDB has nothing at /bis/druid-nope" {
		t.Errorf("404 = %v %q", res.IsError, text(t, res))
	}
	res = callTool(t, &fakeGame{err: &foreverdb.RateLimitError{Retried: true}}, "get_item", map[string]any{"id": "1"})
	if !res.IsError || !strings.Contains(text(t, res), "rate limit") {
		t.Errorf("429 = %v %q", res.IsError, text(t, res))
	}
	res = callTool(t, &fakeGame{err: &foreverdb.HTTPError{Status: 500, Body: "boom"}}, "get_item", map[string]any{"id": "1"})
	if !res.IsError || !strings.Contains(text(t, res), "HTTP 500") {
		t.Errorf("500 = %v %q", res.IsError, text(t, res))
	}
}

func TestRecentChangesCombinesBoth(t *testing.T) {
	g := &fakeGame{files: map[string]string{"/buffs-nerfs": "buffs-nerfs.json", "/changes-seen": "changes-seen.json"}}
	m := decode(t, callTool(t, g, "get_recent_changes", nil))
	var d map[string]json.RawMessage
	if err := json.Unmarshal(m["data"], &d); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(d["buffs_nerfs"]), "warrior") || !strings.Contains(string(d["changes_seen"]), "vendor") {
		t.Errorf("data = %s", m["data"])
	}
	if len(g.calls) != 2 {
		t.Errorf("calls = %+v", g.calls)
	}
}

func TestRecentChangesError(t *testing.T) {
	res := callTool(t, &fakeGame{err: foreverdb.ErrQuota}, "get_recent_changes", nil)
	if !res.IsError || text(t, res) != "ForeverDB quota used up for today" {
		t.Errorf("result = %q", text(t, res))
	}
}

func TestToolRequests(t *testing.T) {
	tests := []struct {
		tool  string
		args  map[string]any
		path  string
		query string
	}{
		{"search_items", map[string]any{"query": "thunder", "slot": "13", "class": "2"}, "/items", "class=2&limit=25&q=thunder&slot=13"},
		{"get_item", map[string]any{"id": "19019"}, "/items/19019", ""},
		{"list_dungeons", nil, "/dungeons", "limit=25"},
		{"get_dungeon", map[string]any{"id": "5"}, "/dungeons/5", ""},
		{"search_quests", map[string]any{"query": "wolf", "zone": "Elwynn"}, "/quests", "limit=25&q=wolf&zone=Elwynn"},
		{"get_quest", map[string]any{"id": "33"}, "/quests/33", ""},
		{"list_zones", nil, "/zones", "limit=25"},
		{"get_vendor", map[string]any{"id": "1"}, "/vendors/1", ""},
		{"get_itemset", map[string]any{"id": "2"}, "/itemsets/2", ""},
		{"get_class", map[string]any{"name": "Druid"}, "/classes/druid", ""},
		{"get_profession", map[string]any{"slug": "first-aid"}, "/professions/first-aid", "limit=25"},
		{"get_endgame", nil, "/endgame", ""},
		{"search_articles", map[string]any{"query": "raid"}, "/articles", "limit=25&q=raid"},
		{"search_articles", nil, "/articles", "limit=25"},
		{"get_article", map[string]any{"slug": "hello world"}, "/articles/hello%20world", ""},
	}
	for _, tt := range tests {
		t.Run(tt.tool+tt.query, func(t *testing.T) {
			g := &fakeGame{}
			decode(t, callTool(t, g, tt.tool, tt.args))
			if len(g.calls) != 1 {
				t.Fatalf("calls = %+v", g.calls)
			}
			if g.calls[0].path != tt.path {
				t.Errorf("path = %q, want %q", g.calls[0].path, tt.path)
			}
			if got := g.calls[0].query.Encode(); got != tt.query {
				t.Errorf("query = %q, want %q", got, tt.query)
			}
		})
	}
}

func TestEmptyIDIsAnError(t *testing.T) {
	g := &fakeGame{}
	res := callTool(t, g, "get_item", map[string]any{"id": ""})
	if !res.IsError || len(g.calls) != 0 {
		t.Errorf("result = %v calls = %v", res.IsError, g.calls)
	}
}
