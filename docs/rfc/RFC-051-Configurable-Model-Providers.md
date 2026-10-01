---
status: accepted
owner: Reticle Project
updated: 2026-10-01
---

# RFC-051: Configurable Model Providers

## 1. Purpose

Let an operator connect an OpenAI-compatible LLM service without adding a
provider-specific branch to Reticle. A provider profile binds an endpoint and
model catalog to the name of an environment variable; the secret value remains
outside saved Reticle configuration.

## 2. Motivation

The original router knew a fixed list of hosted services and credential slots.
That made each new compatible service a source-code and release concern even
when its request protocol was already supported. Reticle's orchestration goal
requires model choice to remain replaceable: workers should receive a routed
model contract, not provider-specific credentials or SDK logic.

This RFC does not claim that one API key describes an integration by itself.
An operator must also supply a protocol, base URL and, when discovery is not
available, model IDs. V1 supports the common OpenAI-compatible protocol;
providers with materially different authentication or request semantics need a
reviewed adapter rather than arbitrary headers or executable configuration.

## 3. Configuration and secret boundary

Profiles are stored in the workspace-local `.reticle/providers.json` snapshot.
Each profile contains:

- a stable lowercase provider ID and optional display name;
- `protocol: openai-compatible`;
- an HTTPS base URL (plain HTTP is allowed only on loopback);
- an optional API-key environment-variable name;
- a model-discovery path, defaulting to `/models`;
- a model source: merged catalog discovery (default) or static-only;
- optional static model IDs and metadata;
- enabled state and declared tool-support confidence.

The file never contains the credential value. Values remain in `.env` or the
launch environment and are resolved only by Forge. Studio edits profiles and
secret values on separate settings surfaces. `.reticle/providers.json` is local
runtime state and remains ignored by Git.

## 4. Discovery and routing

Forge loads built-in providers, local runtimes and configured profiles into one
catalog. A discovered upstream model `vendor/model` in profile `example` has the
routing identity `example/vendor/model`, while its request identity remains
`vendor/model`. Static and discovered entries are deduplicated.

Discovery is a bounded authenticated `GET` to the configured model path. It
does not follow redirects, does not log secrets, accepts at most a bounded JSON
body and model count, and reports `ready`, `degraded`, `missing_credential`,
`discovery_unreachable`, `provider_error` or `disabled`. A static catalog may
remain usable in degraded discovery state. Authentication and quota failures
can make a credential unavailable; network discovery failures do not masquerade
as rate limits.

When catalog entries advertise compatible endpoints, Forge admits only entries
that include the chat endpoint and reports how many non-chat models it excluded.
Entries without endpoint metadata remain eligible because the OpenAI-compatible
catalog schema does not require such metadata; a provider-specific runtime
rejection quarantines only that model. Static-only mode is the deterministic
operator override for mixed catalogs that omit endpoint metadata. It requires
at least one static model and skips the catalog request entirely.

Catalog refresh preserves enable/disable choices for model identities that
still exist. Routing, capacity limits, cooldowns, Bayesian outcomes and retry
safety apply to configured models through the same machinery as built-ins.

## 5. Worker contract

The dispatcher passes a selected configured model's request model, API base and
credential-variable name in trusted worker parameters. The worker invokes the
existing OpenAI-compatible LiteLLM path. It receives only credentials associated
with models admitted by the router; a task cannot name an arbitrary environment
variable to obtain an unrelated secret.

This is an additive extension to Worker Protocol v1. Model-visible prompts and
tool results never contain the API key.

## 6. Studio and control API

The authenticated loopback control service exposes provider list, create/update,
delete and refresh operations. Studio's Models section can manage profiles,
show discovery state and list their models in the ordinary catalog. The API Keys
section recognizes environment variables referenced by profiles.

Profile writes use validation and atomic replacement. Invalid URLs, identifiers,
environment-variable names, model metadata and unsupported protocols are
rejected before persistence.

## 7. Acceptance criteria

1. An arbitrary environment-variable name can be bound to a compatible endpoint
   without source changes.
2. Dynamic `/models` discovery and static fallback catalogs both route requests
   with the upstream model ID and configured base URL.
3. Secrets are absent from provider snapshots, telemetry and model context.
4. Redirects cannot forward bearer credentials, remote plaintext HTTP is
   rejected, and discovery inputs and outputs are bounded.
5. Provider CRUD and refresh work through Studio and authenticated control APIs.
6. Existing built-in providers and local runtimes remain compatible.
7. Mixed-purpose catalogs cannot route entries that explicitly advertise only
   embedding, reranking or other non-chat endpoints, and static-only profiles
   can bypass ambiguous discovery.

## 8. Implementation

Accepted and implemented on 30 September 2026 in `runtime/routing`, the
dispatcher/worker bridge, Forge's control API and Studio settings. NVIDIA NIM
and LLM7 are initial local profile examples; neither is a provider-specific
runtime branch.

