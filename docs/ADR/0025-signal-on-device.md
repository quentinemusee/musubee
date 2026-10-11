# ADR 0025 — Signal on the device: mautrix-signal in the core behind a build tag, from a patched copy that can reach Signal's staging service

- **Status**: accepted
- **Date**: 2026-10-11
- **Task**: T2.6 (connector part; the spike is ADR 0022)
- **Deciders**: Claude Code (design assistant), under the maintainer's standing autonomy instruction; the maintainer's answers to ADR 0022's open points (2026-10-11) are quoted under "Context"

## Context

ADR 0022 found Signal on the device feasible: libsignal's C library (`libsignal-ffi`, Rust) builds for every desktop and Android target, and mautrix-signal's Signal client (signalmeow) can reach Signal's staging service once patched. It left five points to the maintainer, who answered on 2026-10-11:

1. **Test environment**: staging, with the maintainer's own number. A number registered on staging is a separate account from the same number on the public service (ADR 0005), so the tests touch nobody's real Signal account; `CLAUDE.md` §6.3 forbids real user accounts in tests, and this is not one.
2. **Downloads** on the maintainer's machine: protoc, signal-cli, the mautrix-signal Go dependency: authorised.
3. **A second number**: maybe later; until then a single account, so the journey goes through "Note to Self".
4. **The staging patch's form**: a copy in our repository ("we will see about making it public if it can help the community"), rather than an upstream pull request for now.
5. **The sender certificate observation**: report it to mautrix-signal's maintainers if the analysis is sure and it was never raised.

## Decision

1. **mautrix-signal's connector runs in the core**, as mautrix-telegram's does (ADR 0014): `core/connector/signal` returns `connector.SignalConnector` with its default configuration, the device name "Musubee" (what the phone's list of linked devices shows) and no avatar for "Note to Self" (upstream's is an image on maunium.net's Matrix server, which the core does not reach). The core offers Signal when its configuration has a `signal` object (`{"environment": "staging"}` in the tests; production by default). mautrix-signal has one login flow, the QR code: the core is a secondary device of the user's phone, as with Signal Desktop.
2. **Behind the build tag `musubee_signal`**, as ADR 0022 required: the connector links libsignal-ffi through cgo, and builds without the tag stay pure Go. Without the tag, `core/connector/signal` is a stub, and a core configured for Signal refuses to open (`ErrNotBuilt`) instead of silently not offering it.
3. **mautrix-signal is copied into `core/replace/mautrix-signal`** at tag v0.2609.0, unchanged in a first commit, with Musubee's changes in later commits listed in its `MUSUBEE.md`; `core/go.mod` and `infra/go.mod` point the module there with `replace`. `REUSE.toml` keeps upstream's copyright and license for the copy.
4. **The copy can talk to staging**: `signalmeow.SetEnvironment` (new file `environment.go`) points the host names, the gRPC target, the contact discovery server and enclave, and the zkgroup parameters at production or staging; they were constants upstream. The choice applies to the whole process (package variables): a second connector asking for another service gets `ErrEnvironmentChanged`.
5. **No application ships Signal yet**, for two reasons: the secrets at rest (below), and the outcome of the sender certificate analysis (below). The desktop and Android builds keep building without the tag.
6. **CI builds libsignal-ffi and caches it** (`scripts/build-libsignal.sh`, the `signal` job of `checks.yml`), on Linux, Windows and macOS, then runs the copy's tests and the core's Signal tests with the tag.
7. **The journey test** (`infra/signaltest`) runs against staging with signal-cli as the user's phone. The maintainer registered their number on staging once (`infra/signaltest/register-staging.ps1`); the test passed on the first run (see Measurements). It runs on the maintainer's machine, not on CI yet.

### Staging values

ADR 0022 listed the hosts, the contact discovery enclave and the zkgroup parameters. Writing `SetEnvironment` checked them again and settled two details:

| Value | Production | Staging | Source |
|---|---|---|---|
| Chat, storage, CDN hosts | `chat.signal.org`, `storage.signal.org`, `cdn.signal.org`, `cdn2.signal.org`, `cdn3.signal.org` | `chat.staging.signal.org`, `storage-staging.signal.org`, `cdn-staging.signal.org`, `cdn2-staging.signal.org`, `cdn3-staging.signal.org` | Signal-Android `app/build.gradle.kts`; the same in signal-cli 0.14.9 `LiveConfig.java` / `StagingConfig.java` |
| gRPC target | `dns://grpc.chat.signal.org:443` | `dns://grpc.chat.staging.signal.org:443` (ADR 0022 wrote it without the `dns://` scheme that grpc-go's resolver needs) | libsignal `rust/net/src/env.rs`, `DOMAIN_CONFIG_CHAT_STAGING` |
| Contact discovery | `cdsi.signal.org`, enclave `15637fa1…` | `cdsi.staging.signal.org`, enclave `6d9b9649fa3a337754a98059c66d48ac77aaca5299d3b27d6ed1e646c7c81c0a` | libsignal v0.102.2 `rust/attest/src/constants.rs`: the enclave must be one that the linked libsignal's attestation accepts. signal-cli 0.14.9 still uses an older staging enclave (`0f6fd79c…`), which libsignal v0.102.2 no longer accepts |
| zkgroup parameters | upstream's `prod-server-public-params.dat` | `staging-server-public-params.dat` (new) | Signal-Android `app/build.gradle.kts`, staging flavour, base64-decoded; the same value as signal-cli's `StagingConfig.java`. The production value there equals upstream's file (checked) |
| CDN numbering | CDN 0 and 1 on the first host, 2 and 3 on the others | the same layout | upstream `web.CDNHosts` |

The staging hosts present certificates that chain to the roots signalmeow already embeds (ADR 0022).

### Sender certificate analysis (ADR 0022's open point)

ADR 0022 recorded that signalmeow decrypts sealed sender messages without validating the sender certificate, and left open whether this allows a sender to claim another account. **The analysis is done; the maintainer reports it privately to mautrix-signal's maintainers** (decision 5), through GitHub's private vulnerability reporting, never in a public issue. Its result, and Musubee's change to the copy if one is needed, stay out of this public repository until upstream has published a fix, or 90 days have passed since the report, whichever comes first; this ADR is then updated. Until then no Musubee build ships Signal, so no Musubee user depends on the outcome.

### Secrets at rest: why Signal waits

ADR 0019 requires that no session key be readable in the database file. mautrix-signal does not keep its secrets in bridgev2's login metadata, which the core seals, but in its own tables of the same database (`pkg/signalmeow/store/upgrades/00-latest.sql`), in plaintext:

| Table | Secrets |
|---|---|
| `signalmeow_device` | the account and phone-number identity key pairs, the account password, the master key, the account entropy pool, the backup keys |
| `signalmeow_sessions`, `signalmeow_pre_keys`, `signalmeow_kyber_pre_keys`, `signalmeow_sender_keys` | the Signal protocol sessions and their key material |
| `signalmeow_profile_keys`, `signalmeow_groups` | contacts' profile keys, groups' master keys |
| `signalmeow_event_buffer` | the **plaintext** of messages decrypted but not yet handled |

ADR 0019's driver seals one column of bridgev2's own table. Sealing signalmeow's is a follow-up task, before any build ships Signal: either the same driver extended to these columns (each is read and written by a handful of queries in `pkg/signalmeow/store`), or encrypting the whole database, the option that ADR 0019 deferred. The event buffer holds message content, which `CLAUDE.md` §6.8 also covers.

### CI and build times

The `signal` job keys its cache on `scripts/build-libsignal.sh`, which pins the libsignal commit, so the library is rebuilt only when the script changes. Windows needs MSYS2 UCRT64's GCC on every run (cgo links with it, ADR 0010), the other build tools only on a cache miss. On the maintainer's machine (Windows 11, MSYS2 UCRT64, Rust 1.98.1 GNU host) the script built libsignal-ffi in 774 s; ADR 0022 measured 229 to 717 s on GitHub's runners.

The script carries ADR 0022's Windows lessons: the GNU Rust host toolchain, UCRT64 first in `PATH`, the x86-64 `libwindows.0.52.0.a` copied next to the library, the system libraries that rustc lists written to `cgo-ldflags`, and the three-line patch of BoringSSL's P-256 assembly guard, which fails the build if it no longer matches three lines.

## Measurements

Windows 11, Go 1.27.1, MSYS2 UCRT64 GCC 16.2.0, 2026-10-11; `musubee-core.exe` built with `-trimpath -ldflags="-s -w"`:

| Build | Size |
|---|---|
| `-tags=goolm` (pure Go, what the desktop app ships today) | 56.3 MB |
| `-tags=goolm,musubee_signal` (cgo, libsignal-ffi linked statically) | 96.3 MB |

Signal adds 40 MB to the Windows core, on top of the 36 MB that Telegram added (ADR 0014). Most of it is libsignal: ADR 0022 measured mautrix-signal's whole bridge at 60 MB. Shrinking it (lld, stripping the archive's debug sections, which ADR 0022 found link the same) is left for when the desktop app ships Signal. Not measured yet: the Android size.

The staging journey (`infra/signaltest`, Windows, signal-cli 0.14.9 on Java 25, 2026-10-11), first run:

| Step | Time |
|---|---|
| QR code shown to the core linked (signal-cli `addDevice`) | 9.6 s |
| Note to Self from the phone to the core | 4.9 s |
| Answer from the core seen by the phone | 18.6 s (an upper bound: signal-cli receives in polls of up to 10 s) |
| Whole test, logout and checks included | 54.8 s |

The phone's device list then read `["" "Musubee"]` (the primary device has no name). The core, linked and connected, used 4.6 MiB of heap, 13.2 MiB from the system, and 31 goroutines: one measurement, on the desktop.

## Licenses

mautrix-signal is AGPL-3.0-or-later, with files from Signal under AGPL-3.0-only; libsignal is AGPL-3.0-only. A core built with Signal is therefore distributed under AGPL version 3 only, which our AGPL-3.0-or-later code allows. `go run ./scripts/licenseaudit` now audits the `musubee_signal` and `signal` tags too: the new modules (go-vcard and go-pointer, MIT; grpc and genproto, Apache-2.0; protobuf, BSD-3-Clause) are compatible. The audit does not see libsignal's Rust crates; libsignal's own policy (`acknowledgments/about.toml`) accepts only CC0-1.0, MIT, Apache-2.0, BSD-2-Clause, BSD-3-Clause, ISC, MPL-2.0, Zlib, AGPL-3.0-only and the Unicode licenses, all compatible, and a build that ships Signal must carry libsignal's acknowledgments. `NOTICE` lists all of them.

## State of knowledge

| Claim | Status | Source |
|---|---|---|
| The core with `musubee_signal` builds, offers Signal with the QR flow, and refuses a second environment in one process | **verified** (Windows, Go tests) | `core/connector/signal`, `core/embedded/signal_test.go` |
| Without the tag, a core configured for Signal does not open | **verified** (Go test) | `core/embedded/signal_stub_test.go` |
| mautrix-signal's own tests (libsignalgo, signalmeow) pass on our copy | **verified** (Windows); Linux and macOS on CI | the `signal` job |
| `SetEnvironment` switches every value above and back | **verified** (Go test) | `environment_musubee_test.go` |
| The staging values are Signal's | **verified** (read in Signal-Android, libsignal v0.102.2 and signal-cli 0.14.9) | see Sources |
| The core links to a staging account and exchanges Note to Self messages both ways | **verified** (Windows, staging, 2026-10-11); not on CI yet | `infra/signaltest` |
| The core's log holds no message text during the journey | **verified** (same run) | same |
| libsignal-ffi builds and the tests pass on CI's three systems with the cache | **verified once the pull request's CI is green** | the `signal` job |
| Memory and battery cost of Signal in the core on Android | **unknown**: measured when the connector goes into the Android build | — |
| signalmeow's secrets are in plaintext in the database | **verified** (read in the schema) | `pkg/signalmeow/store/upgrades/00-latest.sql` |

## Consequences

- Follow-up tasks: seal signalmeow's tables (before any build ships Signal); build libsignal for Android's four ABIs on CI and put the connector in the Android app; the same for the desktop app; report the sender certificate analysis privately to upstream (the maintainer), then publish it when its embargo ends; run the journey test on CI once the maintainer stores signal-cli's state as a secret.
- Updating mautrix-signal means copying the new tag over the copy in a commit of its own and re-applying `MUSUBEE.md`'s list; an upstream pull request making the environment configurable would remove most of it.
- Revisit if upstream makes the environment configurable, if signalmeow moves its secrets into bridgev2's login metadata, or if staging stops accepting registrations.

## Sources

- mautrix-signal v0.2609.0 (commit `2541762364a9e524e4ce6fd23dc95f98ee49ae47`): `pkg/connector` (`config.go`, `example-config.yaml`, `login.go`), `pkg/signalmeow` (`web/web.go`, `web/grpc.go`, `contactdiscovery.go`, `misc.go`, `receiving_decrypt.go`, `store/upgrades/00-latest.sql`); read 2026-10-11.
- libsignal v0.102.2 (commit `8fc2113bda042fc972a166a24b974b0d34155c6c`): `rust/net/src/env.rs`, `rust/attest/src/constants.rs`, `acknowledgments/about.toml`, `README.md`; read 2026-10-11.
- Signal-Android `app/build.gradle.kts` (production and staging configuration); read 2026-10-11.
- signal-cli 0.14.9: `LiveConfig.java`, `StagingConfig.java`, and the program itself (`--service-environment staging`); read and run 2026-10-11.
- ADR 0005, 0014, 0019, 0022.
