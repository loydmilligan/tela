#!/usr/bin/env python3
"""Render the LiteLLM proxy config from env at container start.

tela points TELA_LLM_URL / TELA_RAG_EMBED_URL at this proxy and calls the virtual
models "tela-chat" / "tela-embed". This script builds LiteLLM's model_list +
router_settings from env so the failover behaviour is fully configurable without
editing YAML — the whole point of "any routing option":

  TELA_AI_ROUTING (default "single"):
    single      — primary only, no relief, no retries. A plain pass-through.
    failover    — only-DOWN fallback: the primary is always used; the relief is
                  hit ONLY when the primary errors/times-out (a separate model
                  group reached via `fallbacks`, so normal traffic never spreads
                  to it). This is "keep using primary, fall back when down".
    loadbalance — throughput relief: primary + relief share one model group and
                  the router spreads load across both (strategy below), failing
                  over on error too. This is "use both to relieve load".

  TELA_AI_LB_STRATEGY (loadbalance only, default "latency-based-routing"):
    one of simple-shuffle | least-busy | usage-based-routing | latency-based-routing.
  TELA_AI_NUM_RETRIES   (default 0 for single, else 2)
  TELA_AI_ALLOWED_FAILS (default 2)   — failures before a deployment is cooled down
  TELA_AI_COOLDOWN      (default 30)  — seconds a failed deployment sits out
  TELA_AI_TIMEOUT       (default 120) — per-request timeout (seconds)

Per service (chat, embed) the upstreams come from:
  TELA_LLM_PRIMARY_{URL,MODEL,KEY,PROVIDER}   TELA_LLM_RELIEF_{URL,MODEL,KEY,PROVIDER}
  TELA_EMBED_PRIMARY_{URL,MODEL,KEY,PROVIDER}  TELA_EMBED_RELIEF_{URL,MODEL,KEY,PROVIDER}
PROVIDER defaults to "openai" (the OpenAI-compatible shape an Ollama /v1, mlx, or
most hosted providers speak); set it per-deployment for a native provider (e.g.
"anthropic"). A blank *_URL means that deployment is absent — so a blank relief
just means primary-only, whatever the routing mode.

The virtual model the proxy exposes is named after the PRIMARY model (not a fixed
alias), so tela keeps sending the SAME model string it always has — critical for
embeddings, whose chunk hash folds in the model name (a renamed model would mark
the whole corpus stale and re-embed it). So set TELA_LLM_MODEL / TELA_RAG_EMBED_MODEL
to the same value as the matching *_PRIMARY_MODEL.

Output is JSON (valid YAML) so there's no yaml dependency or quoting to get wrong.
"""
import json
import os

ROUTING = (os.getenv("TELA_AI_ROUTING") or "single").strip().lower()
LB_STRATEGY = (os.getenv("TELA_AI_LB_STRATEGY") or "latency-based-routing").strip()
ALLOWED_FAILS = int(os.getenv("TELA_AI_ALLOWED_FAILS") or 2)
COOLDOWN = int(os.getenv("TELA_AI_COOLDOWN") or 30)
TIMEOUT = int(os.getenv("TELA_AI_TIMEOUT") or 120)
NUM_RETRIES = int(os.getenv("TELA_AI_NUM_RETRIES") or (0 if ROUTING == "single" else 2))
MASTER_KEY = os.getenv("LITELLM_MASTER_KEY") or ""

if ROUTING not in ("single", "failover", "loadbalance"):
    raise SystemExit(f"TELA_AI_ROUTING must be single|failover|loadbalance, got {ROUTING!r}")
if not MASTER_KEY:
    raise SystemExit("LITELLM_MASTER_KEY is required")

# env-prefix per service; the model_name (client-facing alias) is the primary
# model's own name — see the module docstring on why it's not a fixed alias.
SERVICES = ["TELA_LLM", "TELA_EMBED"]


def env(prefix, role, field, default=""):
    return (os.getenv(f"{prefix}_{role}_{field}") or default).strip()


def deployment(model_name, prefix, role):
    url = env(prefix, role, "URL")
    if not url:
        return None
    model = env(prefix, role, "MODEL")
    provider = env(prefix, role, "PROVIDER", "openai")
    key = env(prefix, role, "KEY", "none") or "none"
    return {
        "model_name": model_name,
        "litellm_params": {
            # provider is the part before the first "/", so a model name that
            # itself contains slashes (e.g. mlx-community/Qwen3-…) still works.
            "model": f"{provider}/{model}",
            "api_base": url,
            "api_key": key,
        },
    }


model_list = []
fallbacks = []

for prefix in SERVICES:
    if not env(prefix, "PRIMARY", "URL"):
        continue  # service not configured
    # Client-facing model name = the primary model's own name (preserves tela's
    # embed chunk-hash). Falls back to a stable alias if MODEL is somehow blank.
    model_name = env(prefix, "PRIMARY", "MODEL") or (prefix.lower() + "-model")
    model_list.append(deployment(model_name, prefix, "PRIMARY"))

    if ROUTING == "single":
        continue
    relief = deployment(model_name, prefix, "RELIEF")
    if not relief:
        continue
    if ROUTING == "loadbalance":
        # Same model group → the router spreads load across both.
        model_list.append(relief)
    else:  # failover: relief is its own group, reached only via fallback-on-error
        relief["model_name"] = model_name + "-relief"
        model_list.append(relief)
        fallbacks.append({model_name: [model_name + "-relief"]})

if not model_list:
    raise SystemExit("no upstreams configured — set TELA_LLM_PRIMARY_URL / TELA_EMBED_PRIMARY_URL")

router_settings = {"num_retries": NUM_RETRIES, "timeout": TIMEOUT}
if ROUTING == "loadbalance":
    router_settings["routing_strategy"] = LB_STRATEGY
    router_settings["allowed_fails"] = ALLOWED_FAILS
    router_settings["cooldown_time"] = COOLDOWN
elif ROUTING == "failover":
    router_settings["fallbacks"] = fallbacks
    router_settings["allowed_fails"] = ALLOWED_FAILS
    router_settings["cooldown_time"] = COOLDOWN

config = {
    "model_list": model_list,
    "router_settings": router_settings,
    "litellm_settings": {
        "callbacks": ["prometheus"],
        "drop_params": True,
        "request_timeout": TIMEOUT,
    },
    "general_settings": {"master_key": MASTER_KEY},
}

print(json.dumps(config, indent=2))
