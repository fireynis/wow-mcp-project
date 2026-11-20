# wow-mcp

A read-only [MCP](https://modelcontextprotocol.io) server (stdio) that gives Claude WoW Forever game
data from [ForeverDB](https://foreverdb.net) and [Forever Companion](https://forevercompanion.io),
and your characters from Blizzard's API. It never writes to Blizzard, either data source or the game.

## Status

The 16 ForeverDB tools, the 10 Forever Companion tools and the 7 character tools (Blizzard's WoW Forever profile API) work.
Character pages need only the Blizzard client; the Battle.net login (`wow-mcp login`, valid about a
day) is needed just for `list_characters` to show every character on the account. See
`docs/blizzard-forever-api.md` for what the profile API returns.

## Data sources and terms

Game data comes from [ForeverDB](https://foreverdb.net) (https://foreverdb.net); every game tool
result carries `"source": "https://foreverdb.net"`. Under ForeverDB's terms you must credit it with a
visible link wherever you show the data, you must not resell the data or present it as your own, and
a key must never be shipped inside something you give out. This repo contains no key; yours stays in
your own login Keychain.

Never commit a key here: this repo is public. `.githooks/pre-commit` refuses staged changes that
look like a key, secret or token; enable it once per clone with `git config core.hooksPath .githooks`.

Game data also comes from [Forever Companion](https://forevercompanion.io) (https://forevercompanion.io),
a free fan API with no key (120 requests a minute): dungeons with bosses, quests and loot, dungeon
and elite quests, items with where they drop, rares, zones, reputations and class quests. Its data is
gathered from ForeverChanges, Wowhead and Zockify and checked in game by players. Every
`companion_*` tool result carries `"source": "https://forevercompanion.io"` and Forever Companion's
own attribution text; it asks for a link back wherever its data is shown. Forever Companion is
fan-made and not affiliated with Blizzard Entertainment.

## Build

```sh
go build -o build/wow-mcp ./cmd/wow-mcp
```

## Set up

1. **Register a Blizzard client** at https://develop.battle.net/access/clients. Set the redirect URI
   to exactly `http://localhost:47830/callback`. Note the client id and secret.
2. **Request a ForeverDB API key** at https://foreverdb.net/developers (a personal or plugin key,
   not a website key).
3. **Store the secrets** in the macOS login Keychain (service `wow-app`). Each is read from standard
   input, so it never appears in argv or shell history if you type it at the prompt:

   ```sh
   build/wow-mcp keys set blizzard-client-id
   build/wow-mcp keys set blizzard-client-secret
   build/wow-mcp keys set foreverdb
   build/wow-mcp keys list        # which are stored, never their values
   ```

   The accounts are `blizzard-client-id`, `blizzard-client-secret`, `foreverdb`,
   `blizzard-user-token` and `blizzard-user-token-expiry`. The last two are written by `login`;
   `blizzard-user-token-expiry` is stored as Unix seconds.
4. **Write the config** (below).
5. **Log in to Battle.net**: `build/wow-mcp login` opens your browser and stores the user token. The
   login expires; when it does, character requests answer
   `Your Battle.net login has expired; run wow-mcp login.` and you run it again.
6. **Check everything**: `build/wow-mcp doctor` prints one line per check (`ok`, `warn`, `skip` or
   `FAIL` with the fix) and exits 1 only if a check FAILs. It checks the config, each Keychain
   account, the Blizzard app token, ForeverDB `/meta` with your key, and that your main character
   can be found. The Battle.net login (missing, expired, or expiring within 2 hours) is only a
   `warn`, reported once, because just the character tools need it and they are not built yet. It
   ends with which tool groups `serve` registers today (game tools: registered; character tools:
   not built yet).
7. **Add it to Claude Code**:

   ```sh
   claude mcp add wow -- /absolute/path/to/wow-app/build/wow-mcp serve
   ```

`wow-mcp probe [-out dir]` asks Blizzard what the Forever profile API returns for your main
character and writes every answer, tokens scrubbed, to a directory.

## Config

`~/.config/wow-app/config.toml` holds no secrets. A missing required value or an unknown key is an
error naming it.

```toml
region = "us"                      # us, eu or tw (required)
cache_dir = "/Users/you/Library/Caches/wow-app"   # optional; absolute or ~/..., this is the default
static_namespace = "static-forever-us"            # optional

[main]                             # required
name = "Leafy"
surname = "Oakheart"

[[characters]]                     # optional, repeatable
name = "Twink"
surname = "Oakheart"
```

WoW Forever has no realms: the Blizzard API addresses a character as `name-surname`
(`/profile/wow/character/leafy-oakheart`). `wow-mcp probe` lists every character on your
Battle.net account with its surname.

## Tools

`search_items`, `get_item`, `list_dungeons`, `get_dungeon`, `get_bis`, `search_quests`, `get_quest`,
`list_zones`, `get_vendor`, `get_itemset`, `get_class`, `get_profession`, `get_endgame`,
`get_recent_changes`, `search_articles`, `get_article`.

Forever Companion tools (no key needed): `companion_list_dungeons`, `companion_get_dungeon`,
`companion_search_quests`, `companion_get_quest`, `companion_search_items`, `companion_get_item`,
`companion_list_rares`, `companion_list_zones`, `companion_list_reputations`,
`companion_class_quests`.

Character tools: `list_characters`, `get_character`, `get_equipment`, `get_talents`, `get_stats`,
`get_reputations`, `get_professions`. Each takes an optional `character`: a configured name
(`Leafy`) or a name and surname (`Leafy Oakheart`); without it, the `[main]` character. Answers are
trimmed to names and values (no links or media).

Lists come 25 entries at a time with a `truncated` flag when an answer was cut. If the ForeverDB key
is missing the tools say to run `wow-mcp keys set foreverdb`.
