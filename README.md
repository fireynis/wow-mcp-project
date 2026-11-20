# wow-mcp

A read-only [MCP](https://modelcontextprotocol.io) server (stdio) that gives Claude WoW Forever game
data from [ForeverDB](https://foreverdb.net). It never writes to Blizzard, ForeverDB or the game.

## Status

The 16 game tools below work. The character tools (your gear, talents and so on from the Blizzard
API) are not built yet: they wait on the findings from `wow-mcp probe`, which shows what Blizzard's
Forever profile API actually returns. Until then `wow-mcp doctor` reports whether your main
character can be looked up.

## Data source and terms

Game data comes from [ForeverDB](https://foreverdb.net) (https://foreverdb.net); every game tool
result carries `"source": "https://foreverdb.net"`. Under ForeverDB's terms you must credit it with a
visible link wherever you show the data, you must not resell the data or present it as your own, and
a key must never be shipped inside something you give out. This repo contains no key; yours stays in
your own login Keychain.

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
   `FAIL` with the fix) and exits 1 if anything failed. It checks the config, each Keychain account,
   the Blizzard app token, the login and its expiry (warning under 2 hours), ForeverDB `/meta` with
   your key, and that your main character can be found.
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
cache_dir = "/Users/you/Library/Caches/wow-app"   # optional; this is the default
static_namespace = "static-forever-us"            # optional

[main]                             # required
realm = "stormrage-forever"
name = "Leafy"

[[characters]]                     # optional, repeatable
realm = "stormrage-forever"
name = "Twink"
```

## Tools

`search_items`, `get_item`, `list_dungeons`, `get_dungeon`, `get_bis`, `search_quests`, `get_quest`,
`list_zones`, `get_vendor`, `get_itemset`, `get_class`, `get_profession`, `get_endgame`,
`get_recent_changes`, `search_articles`, `get_article`.

Lists come 25 entries at a time with a `truncated` flag when an answer was cut. If the ForeverDB key
is missing the tools say to run `wow-mcp keys set foreverdb`.
