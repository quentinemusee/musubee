# mautrix-signal, Musubee's copy

This directory is a copy of [mautrix-signal](https://github.com/mautrix/signal)
at tag `v0.2609.0` (commit `2541762364a9e524e4ce6fd23dc95f98ee49ae47`), licensed
AGPL-3.0-or-later; the files that come from Signal keep their own
`AGPL-3.0-only` tags. `core/go.mod` points the module `go.mau.fi/mautrix-signal`
here with a `replace` directive, so Musubee builds this copy instead of the
published module.

Why a copy rather than the module: the Signal servers, the contact discovery
enclave and the zkgroup parameters are constants upstream, so the bridge can
only talk to Signal's production service. Our tests use Signal's staging
service (ADR 0022), and the maintainer chose to keep a patched copy in our
repository (ADR 0025).

## What was copied

- `pkg/`, `go.mod`, `go.sum`, `LICENSE`, `LICENSE.exceptions`, `README.md`.
- Not copied: `cmd/` (the standalone bridge program), the build scripts, the
  Docker files, and the `pkg/libsignalgo/libsignal` submodule (libsignal's
  Rust sources; the libsignal-ffi library is built separately, see ADR 0022).

The first commit that adds this directory holds the upstream files unchanged
(apart from line endings, normalized to LF by the repository's
`.gitattributes`). Every Musubee change comes in later commits, so
`git log -p -- core/replace/mautrix-signal` shows them all.

## Musubee's changes

None yet. Each change will be listed here with its reason.

## Updating

Copy the new tag's files over this directory in a commit of their own, then
re-apply the changes listed above. Report the bugs we fix to upstream.
