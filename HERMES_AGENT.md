# Hermes Agent native harness

grok2api can be used as the inference backend for Hermes Agent without replacing the Hermes agent loop.

The recommended path is:

```text
Hermes Agent
  -> xAI provider identity
  -> codex_responses transport
  -> grok2api /v1/responses
  -> Grok Build account pool
```

Hermes remains responsible for its native tools, skills, memory, MCP integrations, approvals, delegation, terminal/filesystem access, and agent loop. grok2api only supplies the model inference transport and multi-account Grok Build routing.

## Why the xAI provider identity matters

Do not put 9Router or a Chat Completions translation layer between Hermes and grok2api when native Hermes behavior is the goal.

Hermes uses the provider identity to enable xAI-specific Responses behavior, including:

- Responses-native function tools and stable `call_id` replay
- xAI tool-schema compatibility
- Grok reasoning-effort handling
- body-level `prompt_cache_key`
- stable `x-grok-conv-id` session affinity
- Responses streaming rather than Chat Completions tool-call reconstruction

`x-grok-conv-id` carries stable Hermes session affinity into Grok Build. `Hermes-Agent/*` explicitly identifies the Hermes harness for narrowly scoped compatibility handling. Neither signal grants tools or broadens the client's declared capability surface.

## Hermes configuration

Point the built-in Hermes `xai` provider at grok2api instead of defining grok2api as a generic Chat Completions endpoint.

```yaml
# ~/.hermes/config.yaml
model:
  provider: xai
  default: grok-4.5
  base_url: http://127.0.0.1:8000/v1

  # Recommended when the xAI hostname is replaced by a local gateway.
  # The normal xAI hostname-based Hermes User-Agent is otherwise not guaranteed
  # to be installed on every request path.
  default_headers:
    User-Agent: Hermes-Agent/grok2api
```

Use a grok2api client key as the xAI credential:

```bash
# ~/.hermes/.env
XAI_API_KEY=g2a_xxx_xxx
```

The built-in `xai` provider already selects Hermes' `codex_responses` transport. Do not change it to `chat_completions`.

For a remote grok2api deployment, replace the loopback URL with the HTTPS gateway URL.

## Protocol behavior

On the native path, grok2api keeps the parts of the Responses protocol that Hermes needs for agent execution:

| Hermes behavior | grok2api path |
| --- | --- |
| Function tools with JSON Schema | Preserved on `/v1/responses` |
| Multi-parameter tools | Preserved as structured function schemas |
| Tool call identity | Stable `call_id` is preserved |
| Tool results | `function_call_output` is preserved; Hermes `terminal` JSON envelopes are rendered upstream in Grok Build's native model-facing `exit: N\nstdout` form |
| Parallel tool calls | Preserved unless a specific upstream compatibility rule requires serialization |
| Reasoning | Responses reasoning fields and encrypted reasoning replay are supported |
| Streaming | Responses SSE is forwarded with compatibility normalization |
| Session affinity | `prompt_cache_key` and `x-grok-conv-id` are mapped to the Build cache/session route |
| Multi-turn | Responses history and `previous_response_id` are supported |
| Compaction state | grok2api's Responses compaction/replay support remains available |

Prompt-cache routing never invents hosted tools or broadens `tool_choice`; the client owns the capability surface. When a client explicitly declares an upstream-executed `x_search`, grok2api still filters the completed internal search subcalls from the downstream stream so Hermes does not execute them again as local tools.

## Verification

First verify the grok2api client key and model route:

```bash
curl -sS http://127.0.0.1:8000/v1/models \
  -H "Authorization: Bearer g2a_xxx_xxx"
```

Then run Hermes with the configured model and use a task that requires more than one tool call, for example reading a file, writing a change, and running a command. The request should go to `POST /v1/responses`, not `/v1/chat/completions`.

For troubleshooting, confirm these properties on an audited Hermes request:

- `prompt_cache_key` is present in the Responses body for a stable session.
- `x-grok-conv-id` is present on normal xAI/Hermes session turns.
- client functions remain `type: "function"`.
- follow-up tool results contain the same `call_id`.
- the downstream stream contains Responses events such as `response.output_item.*` and `response.function_call_arguments.*`.

If Hermes is configured as a generic custom provider, it can still call a Responses-compatible endpoint when explicitly configured for `codex_responses`, but it will not receive all xAI-specific Hermes request shaping. For maximum Grok/Hermes fidelity, keep `provider: xai`.
