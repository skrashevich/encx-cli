# Engine tools for AI agents

The `agenttools` package turns the Encounter engine into a catalog of tools an LLM
agent can call. One catalog serves every agent surface in the project:

| Surface | Where it lives | How it reaches the catalog |
| --- | --- | --- |
| In-app assistant on iOS | `mobile/encxmobile` (`AgentSession`) | PicoClaw `tools.RunToolLoop` over a `tools.ToolRegistry` |
| `encli --llm` and `encli -web` | `cmd/encli` | PicoClaw `tools.RunToolLoop`; the extended CLI tool catalog is registered through a compatibility adapter |
| Standalone PicoClaw, Claude Code, any MCP client | `agentmcp` + `encli mcp` | MCP over stdio |

The CLI catalog predates `agenttools` and is larger: it includes game-editor
operations, local-file access and Wikipedia helpers. Those tool implementations
still print JSON to stdout and route failures through `fatal()`, so a compatibility
adapter captures their result, serializes execution, and exposes them as PicoClaw
tools. PicoClaw now owns provider calls, conversation/tool-call iteration and the
iteration limit for both CLI surfaces. `agenttools` remains the reusable,
CLI-independent engine catalog used by mobile and MCP.

## Catalog

Read tools — always available:

| Tool | Reads |
| --- | --- |
| `enc_profile` | login, name, rank, team, points |
| `enc_team` | team name, pending invitations, management actions |
| `enc_domain_games` | games advertised on the domain front page |
| `enc_game_list` | upcoming and active games with schedule and type |
| `enc_game_timeout` | seconds until a game starts |
| `enc_game_state` | game, level list and the active level in full |
| `enc_level` | one level: tasks, sectors, bonuses, hints, messages |
| `enc_action_log` | codes submitted on a level, with correctness and penalties |
| `enc_view_image` | fetches a picture from level content and returns it to the model |
| `enc_image_info` | pixel size and format of a picture, plus the parts it splits into |
| `enc_crop_image` | returns one rectangle of a picture at its own resolution |
| `enc_split_image` | cuts a collage apart and returns every part as its own image |
| `enc_game_statistics` | level breakdown, team rankings, per-level timings |

`enc_level` and `enc_game_state` list every image referenced by the level's HTML
(task, bonuses, hints, organizer messages) with a note about where each came
from. `enc_view_image` then downloads one **through the authenticated session** —
most Encounter attachments are not public — and attaches it to the tool result as
an inline `data:image/…` entry, which PicoClaw's providers turn into an
`image_url` part. Without it a model only ever sees a link it cannot open.

Any public host is allowed, because authors host task images wherever they like.
Fetches are bounded at 8 MB per file, and addresses on the local network
(loopback, RFC 1918, link-local) are refused: a URL taken from game content is
untrusted input and the device may sit inside a private network. Set
`ResourceOptions.RestrictToDomain` to limit fetches to the game's own domain.
Image results are never memoized — a picture is megabytes of base64.

## Cutting a task picture up

A task picture is regularly a collage: several unrelated pictures pasted into
one file, each one a separate clue, and one of them often a screenshot whose
text is the answer. Handing that file to a model whole loses the answer, because
a provider shrinks a picture to roughly 1568 pixels on its longer side before
the model sees it — a 1200×500 collage arrives with its screenshot text below
the resolution it can be read at.

```
enc_image_info  → 1200×500 jpeg, 3 parts detected along the horizontal axis
enc_split_image → part 1 (401×500), part 2 (436×500), part 3 (363×500),
                  each attached as its own image
enc_crop_image  → one rectangle, for a detail inside a part
```

The parts are found in the picture itself rather than guessed: a line of pixels
that holds one colour along its whole length is a separator, not content, and
the cut goes through the middle of it. Runs of plain background at the ends of
the picture are margins and are never cut on. The detector tolerates a spread of
16 per channel, because a collage is saved as JPEG and JPEG rings around the
hard edge between a panel and its separator.

`parts` is a request rather than a promise: when the picture's own separators
produce exactly that many panels they are used, and otherwise the side is
divided evenly and the result says `"method": "equal"` so the model knows a cut
may run through content. Without `parts`, a picture that shows no separators is
reported as such instead of being cut at invented positions.

Limits, all of them there to keep one tool call affordable:

| Limit | Value | Why |
| --- | --- | --- |
| `max_dimension` | 1568 by default, 4096 at most | the resolution a model reads at; larger fragments are shrunk by block averaging, not by dropping pixels |
| `parts` | 2 to 8 | more images than a model can hold in one turn, and a file that looks like it has more parts is textured rather than assembled |
| minimum part | 2% of the side, at least 8 px | detection noise should not become a "part"; a sliver is folded into its neighbour so the parts still cover the picture |
| caching | `enc_image_info` is memoized, the two picture tools are not | the measurement is a few numbers, a fragment is base64 in the megabytes |
| decoded size | 25 megapixels | the 8 MB fetch cap bounds bytes, not pixels: a few kilobytes of PNG header can declare a picture that would need gigabytes to decode, and a URL from game content is untrusted input. The size is read from the header and refused before anything is allocated |

Decoding accepts JPEG, PNG, GIF, WebP, BMP and TIFF, and the decoder rather than
the server's `Content-Type` has the last word: a JPEG served as
`application/octet-stream` is common enough that trusting the header would
refuse pictures that decode perfectly well. A fragment of a JPEG is re-encoded
as JPEG; line art and screenshots stay PNG unless the result is heavy enough to
be a photograph.

## Request pacing

`agenttools.NewPaced(engine, interval)` wraps an `Engine` so concurrent tools
cannot burst requests at Encounter. An LLM runtime executes the tool calls of one
turn in parallel, and Encounter answers a burst with its anti-spam page instead
of JSON — which reads to a player as a broken session on an account that is
perfectly fine. The default floor is 350 ms, matching what the iOS app already
applies to its own traffic. The embedded agent always goes through it.

Relatedly, `encx` no longer reports every HTML reply as an expired session: the
anti-spam page is raised as an anti-spam error, an actual login page as an
expired session, and anything else as a neutral "engine returned HTML, retry
shortly".

Mutating tools — gated by the access policy:

| Tool | Changes |
| --- | --- |
| `enc_send_code` | submits a level or sector answer |
| `enc_send_bonus_code` | submits a bonus answer |
| `enc_take_penalty_hint` | takes a penalty hint (adds penalty time, irreversible) |
| `enc_enter_game` | submits an application to join a game |

Tool results are compact JSON projections rather than raw engine payloads: a
`GameModel` carries HTML task text and dozens of presentation flags that would
otherwise consume the model's context window on every call.

## Access policies

| Policy | Behaviour |
| --- | --- |
| `readonly` | mutating tools are not published at all, and a direct call is refused |
| `approve` | mutating tools are published, but each call must be confirmed before it reaches the engine |
| `full` | mutating calls run immediately |

`approve` is the library default (`agenttools.DefaultPolicy`) and needs a
`Confirmer`. The iOS app implements it as a confirmation sheet; the MCP server
implements it with MCP elicitation.

## Read caching

`Options.ReadCacheTTL` memoizes read-tool results. Within a single answer a model
commonly re-reads the same facts several times, and each read is a paced round
trip to a slow engine.

Caching is off by default, and it is deliberately conservative when on:

- only read tools are cached, keyed by tool name **and arguments**;
- any mutating call clears the whole cache, because submitting a code can change
  the level, the sectors and the action log at once;
- `Catalog.InvalidateCache()` lets a caller drop everything between requests.

The embedded agent uses a 2-minute TTL and invalidates at the start of every
turn, so a cached level can never answer a new question — the game moves while
the player reads.

The `encli mcp` subcommand deliberately defaults to `readonly` instead: an MCP
server is usually started unattended, and an unattended process should not be
able to burn penalty time.

## MCP server

```sh
encli mcp -domain tech.en.cx                      # read-only (default)
encli mcp -domain tech.en.cx -security approve    # each mutation confirmed by the client
encli mcp -domain tech.en.cx -security full       # unattended mutations
```

The server speaks MCP on stdin/stdout, so **stdout carries the protocol** — all
diagnostics go to stderr. Authentication uses the same saved session as every
other `encli` command: run `encli login` first, or pass `-login`/`-password`
(or `ENCX_LOGIN` / `ENCX_PASSWORD`).

Under `-security approve` the server asks the client to confirm each mutating
call through MCP elicitation. Clients that do not implement elicitation get a
refusal, never a silent mutation.

Pictures reach an MCP client as image content blocks next to the tool's JSON:
the inline `data:image/…` entries a picture tool produces are decoded into
`ImageContent`, because a client that only received the JSON would be told about
a picture it was never handed. Media that is not a decodable inline image is
skipped rather than passed on as a block the client cannot render.

### Pointing PicoClaw at it

```sh
picoclaw mcp add encx-engine -- encli mcp -domain tech.en.cx -security approve
```

or, in PicoClaw's config file:

```json
{
  "mcp": {
    "servers": {
      "encx-engine": {
        "enabled": true,
        "type": "stdio",
        "command": "encli",
        "args": ["mcp", "-domain", "tech.en.cx", "-security", "approve"],
        "env": {
          "ENCX_LOGIN": "player",
          "ENCX_PASSWORD": "…"
        }
      }
    }
  }
}
```

## Embedding the catalog in Go

```go
catalog, err := agenttools.NewCatalog(client, agenttools.Options{
    Policy:    agenttools.PolicyApprove,
    Confirmer: myConfirmer,
})
if err != nil {
    return err
}

registry := tools.NewToolRegistry()
catalog.Register(registry)          // registry is PicoClaw's *tools.ToolRegistry
```

`catalog.SystemPromptAddendum()` returns a short description of the engine and
the active policy, meant to be appended to the agent's system prompt.

## In-app agent (iOS)

`mobile/encxmobile` exposes the whole thing to Swift through gomobile:

```go
session, err := client.NewAgentSession(`{
  "provider": "openai",
  "model": "gpt-5.4",
  "api_key": "…",
  "policy": "approve"
}`)
```

`provider` selects one of PicoClaw's OpenAI-compatible protocols, and `api_base`
overrides the endpoint. That endpoint has to serve **`/chat/completions`**:
PicoClaw's HTTP provider builds the URL as `api_base + "/chat/completions"`
(`pkg/providers/openai_compat/provider.go`), so a gateway that only implements
the Responses API answers 404. A failed turn names the endpoint and the model so
this is diagnosable from the app.

### ChatGPT subscription instead of an API key

Set `auth_method` to `codex` and pass a credential obtained from the device
login:

```go
login, err := encxmobile.StartCodexDeviceLogin()
// show login.UserCode(), send the user to login.VerifyURL()
credentialJSON, err := login.Wait(300)

session, err := client.NewAgentSession(`{
  "model": "gpt-5.4-codex",
  "auth_method": "codex",
  "codex_credential": ` + strconv.Quote(credentialJSON) + `,
  "policy": "approve"
}`)
```

The device flow avoids a redirect URI and a local callback listener, which a
phone cannot provide. Tokens are refreshed inside the session; the host reads
`CodexCredentialJSON()` after a turn to persist a refreshed token. PicoClaw's own
credential store is not used — it reads and writes `~/.picoclaw`, which does not
exist on iOS.

This path authenticates against OpenAI with the Codex CLI's public client ID,
which is what PicoClaw itself does. Whether a ChatGPT subscription may be used
this way is between the account owner and OpenAI's terms.

`AgentSession` reports progress as JSON events (`turn_started`, `tool_started`,
`tool_finished`, `turn_finished`, `turn_failed`) through `AgentDelegate.OnEvent`,
and asks for authorization through `AgentDelegate.OnConfirmationRequest`, which
the host answers with `ResolveConfirmation(callID, approved)`.

Only the visible transcript survives between turns. Tool output is deliberately
not replayed: game state changes while the player reads, so a cached level would
be worse than a fresh read.

## Third-party notice

The agent runtime is [PicoClaw](https://github.com/sipeed/picoclaw) (MIT),
vendored as a Go module dependency. Its MIT notice applies to the PicoClaw code
this project links against.
