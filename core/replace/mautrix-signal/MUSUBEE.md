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

Every changed line carries a comment that starts with `Musubee:`, so
`grep -rn "Musubee:" .` finds them; new files carry Musubee's copyright.

1. **A choice of Signal service** (production or staging), for our tests
   (ADR 0022, ADR 0025).
   - `pkg/signalmeow/web/web.go`, `pkg/signalmeow/web/grpc.go`: the host names
     and the gRPC target are variables instead of constants.
   - `pkg/signalmeow/contactdiscovery.go`: the contact discovery server and
     enclave in use are variables.
   - `pkg/signalmeow/misc.go`: a comment only; the zkgroup parameters were
     already a variable.
   - `pkg/signalmeow/environment.go` (new): `SetEnvironment`, which points all
     of the above at production or staging, with the values of Signal-Android
     and libsignal v0.102.2 (sources in ADR 0025).
   - `pkg/signalmeow/staging-server-public-params.dat` (new): the staging
     zkgroup parameters, decoded from Signal-Android's build file.
   - `pkg/signalmeow/environment_musubee_test.go` (new): `SetEnvironment`.

## Updating

Copy the new tag's files over this directory in a commit of their own, then
re-apply the changes listed above. Report the bugs we fix to upstream.
