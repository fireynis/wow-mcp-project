package blizzard

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"wowapp/internal/config"
	"wowapp/internal/keychain"
)

var leafy = config.Character{Name: "Leafy", Surname: "Oakheart"}

// fixtureAPI serves testdata/blizzard by path: the scrubbed probe answers.
func fixtureAPI(t *testing.T) (*Profile, *[]string) {
	t.Helper()
	files := map[string]string{
		"/profile/user/wow":                                     "account.json",
		"/profile/wow/character/leafy-oakheart":                 "summary.json",
		"/profile/wow/character/leafy-oakheart/equipment":       "equipment.json",
		"/profile/wow/character/leafy-oakheart/specializations": "specializations.json",
		"/profile/wow/character/leafy-oakheart/statistics":      "statistics.json",
		"/profile/wow/character/leafy-oakheart/reputations":     "reputations.json",
		"/profile/wow/character/leafy-oakheart/professions":     "professions.json",
	}
	var seen []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Path+" "+r.URL.Query().Get("namespace")+" "+r.Header.Get("Authorization"))
		name, ok := files[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"code":404}`))
			return
		}
		b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "blizzard", name))
		if err != nil {
			t.Error(err)
		}
		w.Write(b)
	}))
	t.Cleanup(api.Close)
	return &Profile{Client: clientFor(t, api, credKeys()), Region: "us"}, &seen
}

// noHrefs fails if the encoded value carries links or media.
func noHrefs(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"href", "_links", "api.blizzard.com", "render.worldofwarcraft"} {
		if strings.Contains(string(b), bad) {
			t.Errorf("output contains %q: %s", bad, b)
		}
	}
	return string(b)
}

func TestSummaryTrimmed(t *testing.T) {
	p, seen := fixtureAPI(t)
	v, err := p.Summary(context.Background(), leafy)
	if err != nil {
		t.Fatal(err)
	}
	noHrefs(t, v)
	s := v.(Summary)
	if s.Name != "Leafy" || s.Surname != "Oakheart" || s.Level != 17 || s.Class != "Druid" || s.Faction != "Alliance" || s.Guild != "Test Guild" || s.EquippedItemLevel != 9 {
		t.Errorf("summary = %+v", s)
	}
	if !strings.HasPrefix(s.LastLogin, "2026-") {
		t.Errorf("LastLogin = %q, want an RFC 3339 time", s.LastLogin)
	}
	if got := (*seen)[0]; got != "/profile/wow/character/leafy-oakheart profile-forever-us Bearer app-tok-9" {
		t.Errorf("request = %q, want the app token in profile-forever-us", got)
	}
}

func TestEquipmentTrimmed(t *testing.T) {
	p, _ := fixtureAPI(t)
	v, err := p.Equipment(context.Background(), leafy)
	if err != nil {
		t.Fatal(err)
	}
	noHrefs(t, v)
	eq := v.(Equipment)
	if len(eq.Slots) != 10 {
		t.Fatalf("%d slots, want 10", len(eq.Slots))
	}
	var chest *Slot
	for i := range eq.Slots {
		if eq.Slots[i].Slot == "Chest" {
			chest = &eq.Slots[i]
		}
	}
	if chest == nil {
		t.Fatal("no Chest slot")
	}
	want := Slot{
		Slot: "Chest", Name: "Skywatcher's Vestments", ItemID: 257344, ItemLevel: 13, Quality: "Uncommon",
		Type: "Armor, Leather", Armor: 65, Stats: []string{"+1 Agility", "+2 Stamina"},
		Enchants: []string{"Enchanted: Stamina +1 and Armor +8"},
	}
	if b1, b2 := noHrefs(t, *chest), noHrefs(t, want); b1 != b2 {
		t.Errorf("chest = %s\nwant    %s", b1, b2)
	}
}

func TestTalentsTrimmed(t *testing.T) {
	p, _ := fixtureAPI(t)
	v, err := p.Talents(context.Background(), leafy)
	if err != nil {
		t.Fatal(err)
	}
	noHrefs(t, v)
	tal := v.(Talents)
	if len(tal.Groups) == 0 || !tal.Groups[0].Active {
		t.Fatalf("groups = %+v", tal.Groups)
	}
	balance := tal.Groups[0].Trees[0]
	if balance.Name != "Balance" || balance.Points != 2 || balance.Talents[0].Name != "Improved Wrath" || balance.Talents[0].Rank != 2 || !strings.Contains(balance.Talents[0].Description, "Wrath") {
		t.Errorf("balance = %+v", balance)
	}
}

func TestStatsTrimmed(t *testing.T) {
	p, _ := fixtureAPI(t)
	v, err := p.Stats(context.Background(), leafy)
	if err != nil {
		t.Fatal(err)
	}
	noHrefs(t, v)
	s := v.(Stats)
	if s.Health != 506 || s.PowerType != "Mana" || s.Strength != 29 || s.Spirit != 40 || s.Armor != 426 || s.SpellCrit != 4.43 || s.Resistances["fire"] != 0 {
		t.Errorf("stats = %+v", s)
	}
}

func TestReputationsTrimmed(t *testing.T) {
	p, _ := fixtureAPI(t)
	v, err := p.Reputations(context.Background(), leafy)
	if err != nil {
		t.Fatal(err)
	}
	noHrefs(t, v)
	reps := v.([]Reputation)
	if len(reps) != 8 || reps[0] != (Reputation{"Ironforge", "Neutral", 2412, 3000}) {
		t.Errorf("reputations = %+v", reps)
	}
}

func TestProfessionsTrimmed(t *testing.T) {
	p, _ := fixtureAPI(t)
	v, err := p.Professions(context.Background(), leafy)
	if err != nil {
		t.Fatal(err)
	}
	noHrefs(t, v)
	profs := v.([]Profession)
	lw := profs[0]
	if lw.Name != "Leatherworking" || lw.Kind != "primary" || lw.Skill != 63 || lw.Max != 150 || len(lw.Recipes) == 0 || lw.Recipes[0] != "Handstitched Leather Boots" {
		t.Errorf("leatherworking = %+v", lw)
	}
	last := profs[len(profs)-1]
	if last.Kind != "secondary" {
		t.Errorf("last profession = %+v, want a secondary", last)
	}
}

func TestAccountUsesUserToken(t *testing.T) {
	p, seen := fixtureAPI(t)
	p.Client.Auth.Keys = newKeys(
		keychain.BlizzardClientID, testID, keychain.BlizzardClientSecret, testSecret,
		keychain.BlizzardUserToken, "user-tok-7",
		keychain.BlizzardUserExpiry, strconv.FormatInt(p.Client.Auth.Now().Add(time.Hour).Unix(), 10))
	chars, err := p.Account(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(chars) != 1 || chars[0] != (AccountCharacter{"Leafy", "Oakheart", 17, "Druid", "High Order Skyborne", "Alliance"}) {
		t.Errorf("account = %+v", chars)
	}
	if got := (*seen)[0]; got != "/profile/user/wow profile-forever-us Bearer user-tok-7" {
		t.Errorf("request = %q, want the user token", got)
	}
}

func TestAccountWithoutLoginIsLoginExpired(t *testing.T) {
	p, _ := fixtureAPI(t)
	if _, err := p.Account(context.Background()); !errors.Is(err, ErrLoginExpired) {
		t.Errorf("err = %v, want ErrLoginExpired", err)
	}
}

func TestUnknownCharacterNamesIt(t *testing.T) {
	p, _ := fixtureAPI(t)
	_, err := p.Equipment(context.Background(), config.Character{Name: "Nobody", Surname: "Here"})
	if err == nil || !strings.Contains(err.Error(), "Nobody Here") || !strings.Contains(err.Error(), "config.toml") {
		t.Errorf("err = %v, want it to name the character and the config", err)
	}
}

// TestLiveEquipment reads the configured main character from the real API.
func TestLiveEquipment(t *testing.T) {
	if testing.Short() {
		t.Skip("needs the Blizzard API, the Keychain and ~/.config/wow-app/config.toml")
	}
	if os.Getenv("WOWMCP_LIVE") == "" {
		t.Skip("set WOWMCP_LIVE=1 to call the real Blizzard API")
	}
	cfg, err := config.Load(config.DefaultPath())
	if err != nil {
		t.Skip(err)
	}
	p := &Profile{Client: &Client{BaseURL: APIBase(cfg.Region), Auth: &Auth{Keys: keychain.Keychain{Path: keychain.SecurityPath, Runner: keychain.ExecRunner{}}}}, Region: cfg.Region}
	v, err := p.Equipment(context.Background(), cfg.Main)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.(Equipment).Slots) == 0 {
		t.Error("no equipped items")
	}
}
