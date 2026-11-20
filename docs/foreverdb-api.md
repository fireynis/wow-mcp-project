# ForeverDB API notes

Source: https://foreverdb.net/developers (fetched 2026-11-19; raw HTML read, text transcribed below).
Base URL: `https://foreverdb.net/api/v1`. Read-only. No API key was available, so nothing here was
verified against live responses. "Not documented" means the page does not say; confirm against the
live API before relying on it.

## General

- Auth: key in `X-API-Key` header (or `Authorization: Bearer <key>`). Example from the page:
  `curl -H "X-API-Key: fdb_…" https://foreverdb.net/api/v1/items/19019`
- Envelope (page text, verbatim): "Every answer is JSON with `data` and `meta` (the game build it comes
  from, and for lists the total and the next page)." So: `{"data": ..., "meta": {...}}`. Exact meta
  field names (build, total, next page / next offset) are NOT documented. Other meta fields mentioned
  in endpoint text: `meta.client` (developer ID, e.g. `dev-0012`), `meta.counts` (on buffs-nerfs, per class).
  **The page contains no example JSON responses**, so Task 5 fixtures cannot be copied from the docs;
  they must be hand-built from the field names below or recorded from the live API with a key.
- Paging on every list endpoint that has it: `limit` (1 to 100, default 25), `offset` (skip this many).
  Endpoints with no limit/offset listed: `/meta`, `/classes`, `/professions`, `/bis`, `/endgame`,
  `/battlegrounds`, `/poisons`, `/disenchant`, `/merchants-favor`.
- User-Agent: send one naming the app. Cloudflare returns 403 (error code 1010) for some default
  library User-Agents (e.g. Python urllib) before the request reaches ForeverDB.
- Errors / limits: past the daily free quota -> HTTP 402 with code `subscription_required`; past a
  per-minute or daily limit -> HTTP 429 with `Retry-After` header. The 402/429 body shape is not documented.
- Limits count per account across all keys: free 5,000/day and 30/minute; paid ($5.99/month) 10,000/day
  and 60/minute.
- Profession slugs: alchemy, blacksmithing, enchanting, engineering, leatherworking, tailoring,
  herbalism, mining, skinning, cooking, first-aid, fishing.

## Terms (verbatim from the page)

- "You must credit ForeverDB.net as the source, with a visible link to https://foreverdb.net, wherever
  you show the data." Suggested line: `<a href="https://foreverdb.net">Data from ForeverDB.net</a>`
- "On the free tier you must credit ForeverDB.net with a link wherever you show the data. On the paid
  plan the credit is optional."
- "Don't resell the data or present it as your own, and keep to the limits."
- "Never ship a key inside a plugin or app you give out: anyone can read it there. Fetch the data
  yourself and bundle it with your releases."
- "Each key says what it is for. A website key is for one website you run, verified with your developer
  ID, and is switched off at once on a second website. A plugin key (an addon, app or bot) and a personal
  key are switched off at once if used on any website." (A personal/plugin key is what wow-mcp uses; never
  call from a website.)
- "Free tier: 5,000 requests a day and 30 a minute for every account ... Past that the answer is 402
  with the code subscription_required."
- "Past a limit the answer is 429 with Retry-After."
- Up to 3 keys per account; deleting the account deletes its keys; suspended accounts' keys stop working.
- "Answers carry invisible marks that tell us which key the data was served to, so we can see where it is
  republished."
- Recorded per key per UTC day (kept 90 days, no IPs): endpoints and resource IDs called, requesting
  website (Origin/Referer host or "server"), client program family, country.
- Data from the WoW Forever client is Blizzard's; ForeverDB isn't affiliated with Blizzard. Classic data
  and what players saw in game are labelled as such.

## Endpoints used by Task 5

Documentation level per endpoint: "params" = query parameters documented; "shape" = prose description of
response fields only (no JSON example, no types).

### GET /meta  (partially documented)
"The client build the data comes from, the level cap and how many of each thing there are." No params.
Field names not documented.

### GET /items  (params documented; item list row fields not documented)
Items from the WoW Forever client, sorted by ID ascending. Rows/details also carry `betaEvent`
(historical beta send-off copies; "Event copies are not normal launch recommendations"), `speculation`,
`speculationReasons`.
Params: `q` (name contains), `quality` (0 Poor to 5 Legendary), `class` (item class ID: 2 weapons,
4 armour...), `slot` (inventory type ID: 1 head, 5 chest...), `minLevel`, `maxLevel` (required level),
`limit`, `offset`.

### GET /items/{id}  (shape in prose)
Stats, damage, speed, binding, sell price, effects; for disenchantable gear `disenchant`;
`liveClient` (what the live client changed since last beta build: each field's beta and live value);
`dataFromBuild` (details kept from an earlier build for an item the client still hides); also
`betaEvent`, `speculation`, `speculationReasons`. Exact key names for stats/damage/etc. not documented.

### GET /dungeons  (partially documented)
Every instance with level range, lowest level first. Params: `limit`, `offset`. Row field names not documented.

### GET /dungeons/{id}  (partially documented)
"Bosses and notable loot." Field names not documented. ID source: presumably the `/dungeons` list (not stated).

### GET /bis  (partially documented)
"Every class and spec the best in slot pages cover, with the ID to ask for." No params. The spec IDs are
NOT listed on the page and the index's JSON shape is not documented. Only example spec ID given:
`hunter-beast-mastery` (format `<class>-<spec-name>`, lowercase, hyphenated).

### GET /bis/{spec}  (params documented; response in prose)
`{spec}` is the spec ID from `/api/v1/bis`.
Params: `mode` (`pve` default or `pvp`), `level` (1 to 60, default level cap), `easy` (`1`: hide hard to
get: raids, high PvP ranks, rare drops; hard enchants go last), `side` (`a` or `h`: one faction's items).
Response (prose): for every slot the pick plus the next two with how far behind they are, how easy each
is to get and whether players saw it in Forever, and the best enchants/armor kits/arcanums for the pick
ranked 1 to 5 with materials and why one is hard to get. Key names not documented.

**Druid spec slugs: not documented, confirm against live /bis.** By analogy to the one example, likely
`druid-balance`, `druid-feral`, `druid-restoration`; this is a guess and must not be hard-coded without
checking the live `/bis` index. The tool should resolve the spec by looking it up in `/bis` (or accept
the ID verbatim and surface the API's 404).

### GET /quests  (params documented)
Quests with ForeverDB's guide, whether known in Forever, and who starts/ends each (`starts`, `ends`).
Sorted by ID. Params: `q` (name contains), `zone` (zone name contains), `minLevel`, `maxLevel`
(quest level), `side` (`alliance` or `horde`), `limit`, `offset`.

### GET /quests/{id}  (shape in prose; most detailed)
Fields named: quest level, side, zone, data source, guide link;
`chain`: previous/next quests, each with `questId`, `name`, `relation` (one of: Complete first,
Chain link, Must be active, Next quest, Quest line, Offered after), `source` (a list can hold
alternatives; "Offered after" is the order players saw the game offer it, not a confirmed requirement);
`questLine` (`name`, `part`, `of`) when the client has one;
`starts` and `ends`: giver/hand-in with `type` (npc, object or item), `id`, `name`, `location`
(`uiMapId`, `zone`, `x`, `y` as percent of zone map, or null; `instance` when inside a dungeon) and
source (seen in WoW Forever via the addon with a player count, or Classic data (CMaNGOS) not confirmed
in Forever). If no NPC is known: the client's quest map marker (type, id and name null) or, for
hand-in, where players' game map showed it. `others`: more NPCs that give/take the same quest.
Exact top-level key names for level/side/zone/guide not documented.

### GET /zones  (partially documented)
World map zones and continents. "The id is the map ID (UiMap) the other answers use." Params: `limit`,
`offset`. Other row fields not documented.

### GET /vendors/{id}  (shape in prose)
Where the vendor stands and everything they sell, prices in copper, and costs besides gold where
players' addons saw them (`costs`: Merchant's Favor and other currencies, or items).
Related list `GET /vendors` (not in Task 5 list): `q` (name or title contains), `zone`, `seen`
(`true`: only seen in Forever), `limit`, `offset`; rows include location and `reputationFaction`
(reputation discount 5% Friendly to 20% Exalted).

### GET /itemsets/{id}  (partially documented)
"Pieces and set bonuses." Field names not documented. List `GET /itemsets`: `q`, `limit`, `offset`;
rows carry pieces, `betaEvent`, `speculation`, `speculationReasons`.

### GET /classes/{name}  (shape in prose)
`{name}` is a class name (nine classes; list `GET /classes` takes no params). Every ability with ranks
and learned levels, plus all talent trees with tier, column, required level, rank spell IDs and
descriptions, and prerequisites. "Tiers and columns are 1-based." Name casing/format (e.g. `druid`) is not
stated; buffs-nerfs/builds examples use lowercase `warrior`.

### GET /professions/{slug}  (params documented; shape in prose)
Slugs listed above. Params: `q` (recipe name contains), `new` (`true`: only recipes new in Forever),
`changed` (`true`: only recipes changed from Classic), `limit`, `offset` (recipes are paged, "a page at a
time, by skill").
Response (prose): ranks with skill caps, trainers, recipes. Each recipe: skill colours (orange, yellow,
green, grey; `skill.orangeSource` is "classic" when orange comes from Classic trainer data; Classic's
own in `orangeClassic` when higher), reagents, what it makes (`count`; `min`/`max` for random count),
how you learn it; `learnedFrom.sources` (trainers and vendors per faction with location; vendor price
in copper for one purchase, stack, `pricePerItem`, costs besides gold, limited stock,
`reputationFaction`; quests and drops that give the recipe; whether seen in Forever);
`category` (trade-skill category with its parent); `trainerCostSource`; `recipeItemsHidden`
(`recipeItemsNotInClient` is the old name, kept). Related `GET /professions` (no params): all twelve
professions with recipe count, new/changed counts, trainer count.
Related `GET /gathering/{slug}` (herbalism, mining, skinning, fishing): param `zone` (map ID or part of
name; for skinning also lists beasts).

### GET /endgame  (partially documented)
No params. "How much of WoW Forever's level 40-60 game players have confirmed in Forever": quests by
level band (seen vs total), every dungeon from level 45 and every raid with drops seen in Forever and
Classic-only, logged boss fights, each raid's status (open, announced with date, or unknown, with
Blizzard's source), and raids Blizzard announced that the game files don't have yet. Key names not documented.

### GET /buffs-nerfs  (params documented)  -- PATH VERIFIED
The API path is **`/api/v1/buffs-nerfs`**. The page text says "As on `/buffs-and-nerfs`" - that is the
website page URL (the site's nav item "Buffs and nerfs"), not an API path. Use `/buffs-nerfs`.
Params: `class` (class ID or name, e.g. warrior), `build` (one build, e.g. `1.60.1.70790`),
`direction` (buff, nerf, changed, new or removed), `limit`, `offset`.
Response (prose): every class change per build, newest first, with `direction` (buff or nerf where the
numbers say; `changed` where they don't; `new`; `removed`), `dataOnly` (changed only in spell data),
`changes` (changed fields a player reads, a few at most), `moreChanges` (count of more in the build's
changelog); `meta.counts` sums per class (dataOnly ones as quiet).

### GET /changes-seen  (params documented)
What players' addons saw that differs from game files (prices, quest rewards and XP, item stats,
creature levels). Each is Verified (2+ players or checked against client) or Not confirmed. Newest first.
Counts only, never who saw it. Params: `kind` (item, npc, vendor, trainer or quest), `verified`
(`1` only Verified, `0` only Not confirmed), `limit`, `offset`. Field names not documented.

### GET /articles  (params documented)
ForeverDB's news and articles, newest first. Params: `type` ("news, guide..." - full value list not
documented), `limit`, `offset`. Row fields not documented (slug is the key for the detail call).

### GET /articles/{slug}  (partially documented)
"The article's blocks and sources." Block structure not documented.

## Other endpoints on the page (not used by Task 5, for reference)
`/spells` (`q`, limit, offset), `/spells/{id}`, `/vendors`, `/poisons`, `/disenchant`, `/classes`,
`/itemsets`, `/professions`, `/gathering/{slug}`, `/battlegrounds`, `/builds` (limit, offset),
`/builds/{build}` (`type`: ability|talent|spell|item|set; `class`; `change`: added|removed|changed; `q`;
limit; offset; `latest` accepted for newest build), `/merchants-favor`, `/posts` (limit, offset).

## Gaps to resolve before/while implementing (need a key)
1. No example responses anywhere; Task 5 fixtures must be recorded from the live API or hand-built.
2. `meta` key names (total, next page/offset, build) undocumented.
3. `/bis` index shape and the druid spec IDs undocumented.
4. 402/429 error body shape undocumented (status codes and `Retry-After` header are documented).
5. Detail response key names for dungeons, itemsets, endgame, articles, zones, meta undocumented.
6. `/classes/{name}` accepted name format undocumented.
