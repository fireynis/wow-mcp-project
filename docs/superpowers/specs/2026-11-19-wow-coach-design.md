# WoW coach: ask WoW Forever questions about your own character, out loud

Status: approved by Jeremy, 2026-11-19.

Build order (Claude's recommendation, approved with the doc): two tracks.
The ForeverDB tools, wow-mcp's skeleton and the WoW key do not wait for step 0
or the Blizzard client; only the character tools are gated on step 0. If step
0 shows Blizzard can't return gear and talents, the rest still ships, the
character tools return a clear "can't read your character yet" error that the
key speaks, and the browser fallback is designed with Jeremy afterwards.

Two pieces of work in two repos:

- **wow-mcp** (new, `~/projects/wow-app`): an MCP server that gives Claude
  read-only tools for Jeremy's WoW Forever characters (Blizzard's API) and for
  WoW Forever game data (ForeverDB's API).
- **The WoW key** (new, `~/projects/sessiondeck`): a second ask-style Stream
  Deck key. Press, speak, press; Claude, with wow-mcp and web search, answers
  in a few spoken sentences.

## Intent

Jeremy plays **WoW Forever** (the Classic-Era-based version, level cap 60,
launched November 2026) on **GeForce NOW**, as a druid. Between pulls, after a
dungeon, or while questing he wants to ask things like:

- "Where can I get better gear for my druid, and which dungeons should I run?"
- "What should I upgrade next?"
- "How should I be weighting my stats?"
- "Where should I go next?"

and hear an answer built from **his actual character** (gear, talents, level,
reputations, professions) plus **accurate WoW Forever game knowledge** (loot
tables, drop chances, BiS lists, quests, zones), without leaving the game. The
Ask key in sessiondeck already does press-speak-press with a spoken answer
from a Perplexity web search; it knows nothing about his character or
Forever's data.

Jeremy wants the game knowledge and character access to live in an **MCP
server Claude uses**, not baked into a prompt, so the same tools also work in
Claude Code at the desk.

## Goals

- A **WoW** Stream Deck key with the Ask key's flow and looks: press, speak,
  press, hear one to a few short sentences. The existing Ask key (Perplexity)
  stays as it is, for fast general questions.
- Answers use Jeremy's live character data and ForeverDB data, through tools
  Claude chooses to call, plus web search for guides and anything the two APIs
  don't cover.
- wow-mcp is usable on its own from Claude Code (`claude mcp add`).
- Failures are audible (as the Ask key), and a failure caused by an expired
  Battle.net login is *spoken* ("Your Battle.net login has expired; run
  wow-mcp login"), not just the error sound, so Jeremy knows what to fix.

## Non-goals

- Real-time or in-combat coaching. Nothing reads the running game: on GeForce
  NOW the client runs on NVIDIA's machine, so addons, SavedVariables and the
  combat log are out of reach.
- Fight analysis ("how did that last fight go"). Needs Warcraft Logs and a
  logger in the group; dropped for now by Jeremy.
- Retail WoW, other Classic flavours, Mythic+, Raider.IO.
- claude.ai / remote MCP access. Local stdio only.
- Follow-up questions or memory between asks. Each ask stands alone, as today.
- Wowhead. No official API, and Jeremy named ForeverDB as the database.

## Anti-goals

- **Faked or guessed character data.** If neither Blizzard route returns
  Jeremy's gear, the builder stops and brings it back to Jeremy (see
  Character data, step 0). No placeholder data, no "for now" stubs.
- Any API key, client secret or user token in a config file, argv, a log line
  or the answers file. Keychain only.
- Anything that writes to Blizzard, ForeverDB or the game, or types into a
  tmux pane or Claude Code session.
- The background `claude -p` showing up on the deck as a session, firing
  Jeremy's hooks, or loading his CLAUDE.md, memory, skills or plugins.

## Approaches

**How Claude is reached** (chosen by Jeremy, 2026-11-19):

1. **`claude -p` from sessiondeck's daemon, with wow-mcp attached (chosen).**
   Uses Jeremy's Claude subscription, gets web search and the tool loop from
   Claude Code for free. Slower than Perplexity (the 2026-11-17 Ask-key
   measurement was ~9 s for a CLI round trip with one tool; this will be
   longer with several tool calls). Jeremy: "I'm not worried about Claude
   being slow." The hooks/session risk is handled the way murmur's
   `claude_cli` cleanup provider already does it
   (`~/projects/murmur/internal/cleanup/claudecli.go`).
2. Claude's API directly from the daemon, the daemon acting as MCP client.
   Faster and more controllable, but needs a separately billed API key and an
   MCP client and tool loop in Go. Jeremy first agreed to this, then switched
   to 1.
3. Keep Perplexity and paste character data into the question. No MCP, so it
   misses what Jeremy asked for. Rejected.

**Where the game knowledge comes from:**

- **ForeverDB's API (chosen by Jeremy).** Documented, JSON, read-only, keyed
  (`foreverdb.net/developers`): items, spells, quests, vendors, dungeons with
  bosses and drops, zones, classes, item sets, professions, BiS by spec,
  endgame, builds, buffs/nerfs, articles. Free tier: 5,000 requests/day,
  30/minute. Plus Claude Code's WebSearch/WebFetch for guides.
- Wowhead: no official API, discourages scraping, and retail-focused.
  Rejected.

**Where character data comes from** (chosen by Jeremy): Blizzard's Profile
API for Forever, which is **undocumented**. ForeverDB's Armory is fed by its
in-game addon (not usable on GeForce NOW) and has no API. If Blizzard's API
can't return the data, Jeremy accepts a "browser-y, scrape-y" route for his
own character as the fallback (see step 0).

**Which key** (my recommendation, accepted by Jeremy): a separate WoW key,
not replacing Perplexity on the Ask key. Keeps the ~2 s general path and keeps
the two features independent when one breaks.

## Design

### Character data (wow-mcp → Blizzard)

What is known, all from community probing on the Blizzard API forums, nothing
confirmed by Blizzard (thread "World of Warcraft: Forever API Namespace?",
us.forums.blizzard.com/…/59774, post of 2026-11-02):

- Namespaces `profile-forever-{region}`, `dynamic-forever-{region}`, and a
  *versioned* static one, e.g. `static-1.60.1_70399-forever-{region}` (plain
  `static-forever-*` returns 403). US, EU, TW work; KR 403.
- `/profile/user/wow?namespace=profile-forever-{region}` lists the account's
  characters **with a user token** (OAuth authorization-code flow).
- `/profile/wow/character/{realmSlug}/{name}` exists in `profile-forever`;
  name lookups returned empty 404s as of early November.

**Step 0, before anything else is built: probe.** With Jeremy's Blizzard
developer client (he registers it at develop.battle.net) and his login, find
out what actually comes back for his druid: the account list, the character
summary, and the equipment, talents/specializations, statistics, reputations
and professions sub-resources, under both a client-credentials token and a
user token. Record the findings (endpoints, namespaces, sample shapes with
nothing secret) in `docs/blizzard-forever-api.md`.

- If gear and talents come back: build on that, using the least-privileged
  token that works.
- If they don't: **stop and bring it to Jeremy** with what was found. The
  fallback he has allowed is reading his own character from a page he can log
  in to in a browser; which page, and how, is decided with him then. Not
  designed here.

**Tokens.** Client-credentials tokens are fetched and refreshed by wow-mcp
on its own. If a user token is needed, `wow-mcp login` runs the
authorization-code flow: opens the browser, listens on a fixed
`127.0.0.1` port for the redirect (that URI is registered on the Blizzard
client), stores the token and its expiry in the Keychain. Blizzard user tokens
are believed to last about 24 hours with no refresh token, so re-login may be
daily; the builder confirms this in step 0. An expired or missing user token
makes the character tools return an error whose text says to run
`wow-mcp login`; the WoW key speaks that text.

### Game data (wow-mcp → ForeverDB)

Base `https://foreverdb.net/api/v1`, key in the `X-API-Key` header. Jeremy
requests the key (Battle.net login on foreverdb.net, approved by hand).

- Respect the limits: on 429, honour `Retry-After` once within the call's
  budget, then fail; 402 (`subscription_required`, over quota) fails with a
  clear message.
- Cache responses on disk (game data changes with builds, not by the minute).
  Builder picks TTLs; `GET /meta` (build date, hash) can invalidate the cache.
- ForeverDB's terms require crediting foreverdb.net with a visible link
  wherever the data is shown. A spoken answer has nowhere to show it; every
  tool result carries `"source": "https://foreverdb.net"`, and the WoW key's
  answers-file lines carry it too. No reselling, key never shipped.

### wow-mcp tools

All read-only. Results are trimmed JSON: what Claude needs to reason, not the
raw API payload (keep token use and latency down). Exact names, parameters and
trimming are the builder's, after seeing real responses. The set to cover:

- **Characters (Blizzard):** list my characters; a character's summary (level,
  class, race, spec/talent points, guild, realm); equipped gear per slot (item
  id, name, item level, stats, enchants); talents; stats; reputations;
  professions. Defaults to the configured main character when no name is
  given.
- **Game (ForeverDB):** search and get items (by name, slot, class/spec
  usability where the API allows); dungeons and raids with bosses and drops;
  BiS for a spec; quests; zones; vendors; item sets; classes; professions;
  endgame; buffs/nerfs and recent changes; articles.

Typical question path: "what should I upgrade next" → my gear → BiS for my
spec → which dungeon or quest drops the nearest upgrade, with drop chance.

### wow-mcp itself

- Go, in `~/projects/wow-app` (Jeremy's daemons are Go), stdio MCP server
  using the official Go MCP SDK. Binary `wow-mcp`, with subcommands `serve`
  (default, what MCP clients run), `login`, `keys set <name>` (reads the value
  from stdin, like `sessiondeck keys set`), and `doctor` (config present, keys
  present, Blizzard token works, ForeverDB key works, main character found).
- Config `~/.config/wow-app/config.toml`: region, main character
  (realm + name), optional other characters, cache directory, and the static
  namespace version if it must be pinned. A missing required value is an
  error naming it.
- Secrets in the login Keychain, service `wow-app`: Blizzard client id and
  secret, ForeverDB key, Blizzard user token.
- Tests: unit tests against `httptest` servers with recorded (scrubbed)
  responses; tests that hit the real APIs skip under `testing.Short()`.
- At the desk: `claude mcp add wow -- <path>/wow-mcp serve`.

### The WoW key (sessiondeck)

Reuses the Ask key's machinery (`internal/ask`, `internal/daemon/ask.go`):
the murmur recording, stages, the press-to-stop behaviour, ElevenLabs voice,
`afplay`, the error sound, and `asks.jsonl`. Only the answering step changes:
instead of Perplexity, a **Claude CLI answerer** (a second implementation of
`ask.Searcher`).

- **One ask at a time across both keys.** A press on one while the other is
  busy is refused (alerts), as a second press is refused today.
- **The `claude -p` process** copies murmur's isolation: fresh empty temp
  working dir, environment built from scratch, `--setting-sources ""`,
  `disableAllHooks`, `crossSessionInbound: refuse`,
  `--disable-slash-commands`, `--no-session-persistence`, `--no-chrome`, the
  `CLAUDE_CODE_DISABLE_*` variables, `--system-prompt-file`, question on
  stdin, `--output-format json`. Differences from murmur: an `--mcp-config`
  file naming only wow-mcp with `--strict-mcp-config`; tools limited to
  wow-mcp's tools plus `WebSearch` and `WebFetch` (nothing that edits files or
  runs commands); `--max-turns` high enough for several tool calls
  (builder's call, ~10).
- **`--safe-mode` is left out (Claude's call).** The CLI describes it as
  disabling MCP servers, which would switch off wow-mcp. The builder first
  checks with a test whether it also drops servers given by `--mcp-config`;
  if it doesn't, keep it. Without it, isolation rests on
  `--setting-sources ""` (no user settings, so no enabled plugins), the hook
  and cross-session settings, `--strict-mcp-config`, the explicit tool
  allowlist, the `CLAUDE_CODE_DISABLE_*` variables, the empty working dir and
  the scratch environment. The builder verifies, with a test run, that no
  hook fires, no session appears on the deck and no plugin or skill loads.
- **`ask.Searcher` may change shape (Claude's call).** It returns only a
  string today; the WoW answerer also needs to report tools called and the
  ForeverDB credit for the log. The builder may widen the interface.
- **System prompt:** answering a WoW Forever player who is playing and will
  hear the answer read aloud; use the tools for anything about their
  character or Forever's game data rather than memory; prefer ForeverDB over
  web results for Forever facts (Classic-era web guides may be wrong for
  Forever); one to four short spoken sentences, no markdown, lists, links or
  citations; lead with the answer; if a tool says the Battle.net login has
  expired, say exactly that.
- **Timeout:** the WoW key's answer budget is separate from the Ask key's and
  longer (default 90 s, configurable).
- **Config:** a new `[wow]` section: `enabled`, `claude_path` (default
  `~/.local/bin/claude`), `model` (default `sonnet`), `mcp_path` (the wow-mcp
  binary), `timeout`. Voice settings are shared with `[ask]`. `enabled` needs
  the murmur backend, as `[ask]` does. `sessiondeck doctor` checks the claude
  and wow-mcp binaries exist.
- **Log:** WoW asks go into `asks.jsonl` with a `key: "wow"` field, the tools
  called (names only), and the ForeverDB credit when it was used. As today,
  the daemon log never holds the question or the answer.
- **CLI:** `sessiondeck wow`, one press, like `sessiondeck ask`.
- **Plugin:** a new WoW action with the Ask key's states and looks, its own
  glyph and label.

## Constraints

- macOS, Apple Silicon; Go for wow-mcp and the daemon; the Stream Deck plugin
  is TypeScript (`@elgato/streamdeck`), as in sessiondeck.
- Blizzard's Forever API is undocumented and may change or close without
  notice; the builder isolates it behind one package so a change is a local
  fix.
- ForeverDB rate limits (30/min) bound how many tool calls one answer can
  make; the cache matters.
- Claude subscription usage, not API billing.

## Left to the builder

- Exact tool names, parameters and result trimming.
- Cache TTLs and layout.
- `--max-turns`, the model's effort setting.
- How the Claude CLI answerer is shared or kept apart from murmur's code
  (copying the flag set into sessiondeck is fine; it's a separate repo).
- The WoW key's glyph.
- Order of work, after step 0 comes first.

## What Jeremy needs to do

- Register a Blizzard developer client (develop.battle.net), with the
  redirect URI wow-mcp's `login` uses.
- Request a ForeverDB API key (foreverdb.net/developers).
- Store both with `wow-mcp keys set`, plus the ElevenLabs key sessiondeck
  already has.
