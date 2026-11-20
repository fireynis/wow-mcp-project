# Blizzard WoW Forever profile API: probe findings (2026-11-20)

Found with `wow-mcp probe` and a few direct calls, region `us`. Undocumented; may change. Names
below are placeholders.

## Addressing

- WoW Forever has **no realms**. A character is `/profile/wow/character/{name}-{surname}`, both
  lowercased (`leafy-oakheart`). `/data/wow/realm/index` in `dynamic-forever-us` returns 404.
- The account list `/profile/user/wow` (namespace `profile-forever-us`) gives each character's
  `name`, `surname`, `level`, `playable_class`, `playable_race`, `faction` and a `character.href`
  carrying the slug. Its entries have no `realm` field.
- Static references point at a versioned namespace, e.g. `static-1.60.1_70399-forever-us`;
  `static-forever-us` also answers (`/data/wow/item/19019` → 200).

## Tokens

| Path | app token (client credentials) | user token (`wow.profile`) |
|---|---|---|
| `/profile/user/wow` | 403 | 200 |
| `/profile/wow/character/{slug}` and every sub-page below | 200 | 200 |

So character pages need only the app token; the Battle.net login is needed just to list the
account's characters.

## Character pages (`?namespace=profile-forever-us&locale=en_US`)

| Sub-path | Status | Holds |
|---|---|---|
| (summary) | 200 | name, surname, level, experience, race, character_class, faction, guild, average/equipped item level, last_login_timestamp, `ruleset`, `is_remix`, links to the pages below |
| `/equipment` | 200 | `equipped_items[]`: slot, item id, name, quality, item level (`level.value`), item_class/subclass, inventory_type, `stats[].display.display_string`, sell_price |
| `/specializations` | 200 | `specialization_groups[]` (`is_active`) → `specializations[]` per tree (e.g. Balance) with `spent_points` and `talents[]` (talent name, rank, spell_tooltip description) |
| `/statistics` | 200 | health, power, primary stats, spirit, crit/hit/haste, spell power, armor, defense, dodge/parry/block, resistances, weapon damage |
| `/reputations` | 200 | `reputations[]`: faction name, standing name |
| `/professions` | 200 | `primaries[]`/`secondaries[]`: profession, `tiers[]` skill_points / max_skill_points, known_recipes |
| `/character-media` | 200 | render image URLs |
| `/appearance` | 200 | appearance |
| `/achievements` | 200 | achievements |
| `/pvp-summary` | 200 | pvp summary |
| `/quests/completed` | 200 | completed quests |
| `/talents` | 404 | use `/specializations` |
| `/hunter-pets` | 404 | |

Item ids include custom Forever items (six digits, e.g. 2xxxxx) next to classic ids.
