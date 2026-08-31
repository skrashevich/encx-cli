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
