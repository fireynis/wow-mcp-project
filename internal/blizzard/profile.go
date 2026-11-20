package blizzard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"wowapp/internal/config"
)

// Profile reads WoW Forever characters from the profile API and trims each
// answer to names and values: no hrefs, media links or colours. Character
// pages answer with the application token; only the account's character list
// needs the Battle.net login (docs/blizzard-forever-api.md).
type Profile struct {
	Client *Client
	Region string
}

func (p *Profile) namespace() string { return "profile-forever-" + p.Region }

// get fetches path and decodes it into v. A 404 becomes an error naming the
// character so the model can tell the person what to fix.
func (p *Profile) get(ctx context.Context, c *config.Character, path string, user bool, v any) error {
	body, err := p.Client.Get(ctx, path, p.namespace(), user)
	if errors.Is(err, ErrNotFound) && c != nil {
		return fmt.Errorf("Blizzard has no WoW Forever character %s %s (%s); check the name and surname in ~/.config/wow-app/config.toml", c.Name, c.Surname, Slug(*c))
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("Blizzard: could not read the answer for %s: %w", path, err)
	}
	return nil
}

type named struct {
	Name string `json:"name"`
}

// AccountCharacter is one character on the logged-in Battle.net account.
type AccountCharacter struct {
	Name    string `json:"name"`
	Surname string `json:"surname"`
	Level   int    `json:"level"`
	Class   string `json:"class"`
	Race    string `json:"race"`
	Faction string `json:"faction"`
}

// Account lists the characters on the logged-in Battle.net account. It needs
// the user token: ErrLoginExpired without a valid login.
func (p *Profile) Account(ctx context.Context) ([]AccountCharacter, error) {
	var raw struct {
		Accounts []struct {
			Characters []struct {
				Name    string `json:"name"`
				Surname string `json:"surname"`
				Level   int    `json:"level"`
				Class   named  `json:"playable_class"`
				Race    named  `json:"playable_race"`
				Faction named  `json:"faction"`
			} `json:"characters"`
		} `json:"wow_accounts"`
	}
	if err := p.get(ctx, nil, "/profile/user/wow", true, &raw); err != nil {
		return nil, err
	}
	out := []AccountCharacter{}
	for _, a := range raw.Accounts {
		for _, c := range a.Characters {
			out = append(out, AccountCharacter{c.Name, c.Surname, c.Level, c.Class.Name, c.Race.Name, c.Faction.Name})
		}
	}
	return out, nil
}

// Summary is a character's overview.
type Summary struct {
	Name              string `json:"name"`
	Surname           string `json:"surname"`
	Level             int    `json:"level"`
	Experience        int    `json:"experience"`
	Class             string `json:"class"`
	Race              string `json:"race"`
	Gender            string `json:"gender"`
	Faction           string `json:"faction"`
	Guild             string `json:"guild,omitempty"`
	AverageItemLevel  int    `json:"average_item_level"`
	EquippedItemLevel int    `json:"equipped_item_level"`
	Ruleset           string `json:"ruleset,omitempty"`
	LastLogin         string `json:"last_login,omitempty"`
}

func (p *Profile) Summary(ctx context.Context, c config.Character) (any, error) {
	var raw struct {
		Name              string `json:"name"`
		Surname           string `json:"surname"`
		Level             int    `json:"level"`
		Experience        int    `json:"experience"`
		Class             named  `json:"character_class"`
		Race              named  `json:"race"`
		Gender            named  `json:"gender"`
		Faction           named  `json:"faction"`
		Guild             *named `json:"guild"`
		AverageItemLevel  int    `json:"average_item_level"`
		EquippedItemLevel int    `json:"equipped_item_level"`
		Ruleset           *named `json:"ruleset"`
		LastLogin         int64  `json:"last_login_timestamp"`
	}
	if err := p.get(ctx, &c, CharPath(c), false, &raw); err != nil {
		return nil, err
	}
	s := Summary{
		Name: raw.Name, Surname: raw.Surname, Level: raw.Level, Experience: raw.Experience,
		Class: raw.Class.Name, Race: raw.Race.Name, Gender: raw.Gender.Name, Faction: raw.Faction.Name,
		AverageItemLevel: raw.AverageItemLevel, EquippedItemLevel: raw.EquippedItemLevel,
	}
	if raw.Guild != nil {
		s.Guild = raw.Guild.Name
	}
	if raw.Ruleset != nil {
		s.Ruleset = raw.Ruleset.Name
	}
	if raw.LastLogin > 0 {
		s.LastLogin = time.UnixMilli(raw.LastLogin).UTC().Format(time.RFC3339)
	}
	return s, nil
}

// Equipment is what a character is wearing.
type Equipment struct {
	Slots []Slot `json:"slots"`
}

// Slot is one equipped item.
type Slot struct {
	Slot      string   `json:"slot"`
	Name      string   `json:"name"`
	ItemID    int      `json:"item_id"`
	ItemLevel int      `json:"item_level"`
	Quality   string   `json:"quality"`
	Type      string   `json:"type,omitempty"` // item class and subclass, e.g. "Armor, Leather"
	Armor     int      `json:"armor,omitempty"`
	Stats     []string `json:"stats,omitempty"`
	Enchants  []string `json:"enchants,omitempty"`
}

func (p *Profile) Equipment(ctx context.Context, c config.Character) (any, error) {
	var raw struct {
		Items []struct {
			Item struct {
				ID int `json:"id"`
			} `json:"item"`
			Slot     named  `json:"slot"`
			Name     string `json:"name"`
			Quality  named  `json:"quality"`
			Class    named  `json:"item_class"`
			Subclass named  `json:"item_subclass"`
			Level    struct {
				Value int `json:"value"`
			} `json:"level"`
			Armor struct {
				Value int `json:"value"`
			} `json:"armor"`
			Stats []struct {
				Display struct {
					Text string `json:"display_string"`
				} `json:"display"`
			} `json:"stats"`
			Enchantments []struct {
				Text string `json:"display_string"`
			} `json:"enchantments"`
		} `json:"equipped_items"`
	}
	if err := p.get(ctx, &c, CharPath(c)+"/equipment", false, &raw); err != nil {
		return nil, err
	}
	eq := Equipment{Slots: []Slot{}}
	for _, it := range raw.Items {
		s := Slot{
			Slot: it.Slot.Name, Name: it.Name, ItemID: it.Item.ID, ItemLevel: it.Level.Value,
			Quality: it.Quality.Name, Armor: it.Armor.Value,
			Type: strings.Trim(it.Class.Name+", "+it.Subclass.Name, ", "),
		}
		for _, st := range it.Stats {
			if st.Display.Text != "" {
				s.Stats = append(s.Stats, st.Display.Text)
			}
		}
		for _, en := range it.Enchantments {
			if en.Text != "" {
				s.Enchants = append(s.Enchants, en.Text)
			}
		}
		eq.Slots = append(eq.Slots, s)
	}
	return eq, nil
}

// Talents are a character's talent trees with the points spent in each.
type Talents struct {
	Groups []TalentGroup `json:"groups"`
}

// TalentGroup is one talent loadout; Active marks the one in use.
type TalentGroup struct {
	Active bool         `json:"active"`
	Trees  []TalentTree `json:"trees"`
}

// TalentTree is one tree (e.g. Balance) and its learned talents.
type TalentTree struct {
	Name    string   `json:"name"`
	Points  int      `json:"points"`
	Talents []Talent `json:"talents"`
}

// Talent is one learned talent.
type Talent struct {
	Name        string `json:"name"`
	Rank        int    `json:"rank"`
	Description string `json:"description,omitempty"`
}

func (p *Profile) Talents(ctx context.Context, c config.Character) (any, error) {
	var raw struct {
		Groups []struct {
			Active bool `json:"is_active"`
			Specs  []struct {
				Name    string `json:"specialization_name"`
				Points  int    `json:"spent_points"`
				Talents []struct {
					Talent  named `json:"talent"`
					Rank    int   `json:"talent_rank"`
					Tooltip struct {
						Description string `json:"description"`
					} `json:"spell_tooltip"`
				} `json:"talents"`
			} `json:"specializations"`
		} `json:"specialization_groups"`
	}
	if err := p.get(ctx, &c, CharPath(c)+"/specializations", false, &raw); err != nil {
		return nil, err
	}
	out := Talents{Groups: []TalentGroup{}}
	for _, g := range raw.Groups {
		tg := TalentGroup{Active: g.Active, Trees: []TalentTree{}}
		for _, sp := range g.Specs {
			tree := TalentTree{Name: sp.Name, Points: sp.Points, Talents: []Talent{}}
			for _, t := range sp.Talents {
				tree.Talents = append(tree.Talents, Talent{t.Talent.Name, t.Rank, t.Tooltip.Description})
			}
			tg.Trees = append(tg.Trees, tree)
		}
		out.Groups = append(out.Groups, tg)
	}
	return out, nil
}

// Stats are a character's effective stats. Percentages are as Blizzard
// reports them (4.43 means 4.43%).
type Stats struct {
	Health          int            `json:"health"`
	Power           int            `json:"power"`
	PowerType       string         `json:"power_type"`
	Strength        int            `json:"strength"`
	Agility         int            `json:"agility"`
	Stamina         int            `json:"stamina"`
	Intellect       int            `json:"intellect"`
	Spirit          int            `json:"spirit"`
	Armor           int            `json:"armor"`
	Defense         int            `json:"defense"`
	AttackPower     int            `json:"attack_power"`
	SpellPower      int            `json:"spell_power"`
	SpellHealing    int            `json:"spell_healing"`
	MeleeCrit       float64        `json:"melee_crit_pct"`
	RangedCrit      float64        `json:"ranged_crit_pct"`
	SpellCrit       float64        `json:"spell_crit_pct"`
	MeleeHit        float64        `json:"melee_hit_pct"`
	SpellHit        float64        `json:"spell_hit_pct"`
	Dodge           float64        `json:"dodge_pct"`
	Parry           float64        `json:"parry_pct"`
	Block           float64        `json:"block_pct"`
	ManaRegen       float64        `json:"mana_regen"`
	ManaRegenCombat float64        `json:"mana_regen_combat"`
	MainHandDPS     float64        `json:"main_hand_dps"`
	Resistances     map[string]int `json:"resistances"`
}

func (p *Profile) Stats(ctx context.Context, c config.Character) (any, error) {
	type eff struct {
		Effective int `json:"effective"`
	}
	type pct struct {
		Value float64 `json:"value"`
	}
	var raw struct {
		Health          int     `json:"health"`
		Power           int     `json:"power"`
		PowerType       named   `json:"power_type"`
		Strength        eff     `json:"strength"`
		Agility         eff     `json:"agility"`
		Stamina         eff     `json:"stamina"`
		Intellect       eff     `json:"intellect"`
		Spirit          eff     `json:"spirit"`
		Armor           eff     `json:"armor"`
		Defense         eff     `json:"defense"`
		AttackPower     int     `json:"attack_power"`
		SpellPower      int     `json:"spell_power"`
		SpellHealing    int     `json:"spell_healing"`
		MeleeCrit       pct     `json:"melee_crit"`
		RangedCrit      pct     `json:"ranged_crit"`
		SpellCrit       pct     `json:"spell_crit"`
		MeleeHit        pct     `json:"melee_hit"`
		SpellHit        pct     `json:"spell_hit"`
		Dodge           pct     `json:"dodge"`
		Parry           pct     `json:"parry"`
		Block           pct     `json:"block"`
		ManaRegen       float64 `json:"mana_regen"`
		ManaRegenCombat float64 `json:"mana_regen_combat"`
		MainHandDPS     float64 `json:"main_hand_dps"`
		Fire            eff     `json:"fire_resistance"`
		Frost           eff     `json:"frost_resistance"`
		Nature          eff     `json:"nature_resistance"`
		Shadow          eff     `json:"shadow_resistance"`
		Arcane          eff     `json:"arcane_resistance"`
	}
	if err := p.get(ctx, &c, CharPath(c)+"/statistics", false, &raw); err != nil {
		return nil, err
	}
	return Stats{
		Health: raw.Health, Power: raw.Power, PowerType: raw.PowerType.Name,
		Strength: raw.Strength.Effective, Agility: raw.Agility.Effective, Stamina: raw.Stamina.Effective,
		Intellect: raw.Intellect.Effective, Spirit: raw.Spirit.Effective,
		Armor: raw.Armor.Effective, Defense: raw.Defense.Effective,
		AttackPower: raw.AttackPower, SpellPower: raw.SpellPower, SpellHealing: raw.SpellHealing,
		MeleeCrit: round2(raw.MeleeCrit.Value), RangedCrit: round2(raw.RangedCrit.Value), SpellCrit: round2(raw.SpellCrit.Value),
		MeleeHit: round2(raw.MeleeHit.Value), SpellHit: round2(raw.SpellHit.Value),
		Dodge: round2(raw.Dodge.Value), Parry: round2(raw.Parry.Value), Block: round2(raw.Block.Value),
		ManaRegen: round2(raw.ManaRegen), ManaRegenCombat: round2(raw.ManaRegenCombat), MainHandDPS: round2(raw.MainHandDPS),
		Resistances: map[string]int{
			"fire": raw.Fire.Effective, "frost": raw.Frost.Effective, "nature": raw.Nature.Effective,
			"shadow": raw.Shadow.Effective, "arcane": raw.Arcane.Effective,
		},
	}, nil
}

func round2(f float64) float64 {
	if f < 0 {
		return -round2(-f)
	}
	return float64(int64(f*100+0.5)) / 100
}

// Reputation is one faction standing; Value of Max is progress within the
// standing.
type Reputation struct {
	Faction  string `json:"faction"`
	Standing string `json:"standing"`
	Value    int    `json:"value"`
	Max      int    `json:"max"`
}

func (p *Profile) Reputations(ctx context.Context, c config.Character) (any, error) {
	var raw struct {
		Reps []struct {
			Faction  named `json:"faction"`
			Standing struct {
				Name  string `json:"name"`
				Value int    `json:"value"`
				Max   int    `json:"max"`
			} `json:"standing"`
		} `json:"reputations"`
	}
	if err := p.get(ctx, &c, CharPath(c)+"/reputations", false, &raw); err != nil {
		return nil, err
	}
	out := []Reputation{}
	for _, r := range raw.Reps {
		out = append(out, Reputation{r.Faction.Name, r.Standing.Name, r.Standing.Value, r.Standing.Max})
	}
	return out, nil
}

// Profession is one profession with its skill and known recipes.
type Profession struct {
	Name    string   `json:"name"`
	Kind    string   `json:"kind"` // primary or secondary
	Skill   int      `json:"skill"`
	Max     int      `json:"max_skill"`
	Recipes []string `json:"known_recipes,omitempty"`
}

func (p *Profile) Professions(ctx context.Context, c config.Character) (any, error) {
	type prof struct {
		Profession named `json:"profession"`
		Tiers      []struct {
			Skill   int     `json:"skill_points"`
			Max     int     `json:"max_skill_points"`
			Recipes []named `json:"known_recipes"`
		} `json:"tiers"`
	}
	var raw struct {
		Primaries   []prof `json:"primaries"`
		Secondaries []prof `json:"secondaries"`
	}
	if err := p.get(ctx, &c, CharPath(c)+"/professions", false, &raw); err != nil {
		return nil, err
	}
	out := []Profession{}
	add := func(kind string, ps []prof) {
		for _, pr := range ps {
			o := Profession{Name: pr.Profession.Name, Kind: kind}
			for _, t := range pr.Tiers {
				o.Skill += t.Skill
				o.Max += t.Max
				for _, r := range t.Recipes {
					o.Recipes = append(o.Recipes, r.Name)
				}
			}
			out = append(out, o)
		}
	}
	add("primary", raw.Primaries)
	add("secondary", raw.Secondaries)
	return out, nil
}
