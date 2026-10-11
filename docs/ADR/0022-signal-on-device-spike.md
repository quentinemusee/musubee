# ADR 0022 — Signal on the device, spike: libsignal builds for the core's targets, and mautrix-signal can reach Signal's staging servers with a patch

- **Status**: accepted
- **Date**: 2026-10-11
- **Task**: T2.6 (spike part)
- **Deciders**: Claude Code (design assistant), under the maintainer's standing autonomy instruction; the decisions listed under "Left to the maintainer" are theirs

## Context

T2.6 asks for Signal on the device: mautrix-signal's bridgev2 connector inside the core, like Telegram's (ADR 0014), tested against Signal's staging servers with a test account registered by the maintainer (ADR 0005). Two questions had to be answered before writing any connector code:

1. **Can the native part be built for every target of the core?** mautrix-signal (`go.mau.fi/mautrix-signal`) is Go, but its cryptography is Signal's own library, libsignal, written in Rust, called through its C API (`libsignal-ffi`, a static library) by cgo (`pkg/libsignalgo`). The core runs on Windows, Linux, macOS (Electron, ADR 0013) and Android (four ABIs, ADR 0011); iOS comes later (ADR 0015).
2. **Can it be tested without paying for phone numbers?** The maintainer refused paid test numbers (2026-10-11). ADR 0005 found that Signal's staging environment exists and that signal-cli can target it, but left open whether mautrix-signal can.

Constraints in force during the spike: nothing was to be added to the core (no new Go dependency), and nothing was to be downloaded on the maintainer's machine except Rust targets and crates (authorised 2026-10-11); build tools (protoc, CMake, the NDK) were only installed on GitHub's runners, by a workflow that runs on spike branches only (`.github/workflows/libsignal-spike.yml`).

## Options considered

### For Signal on the device

- **A. mautrix-signal's connector in the core, with libsignal-ffi linked statically** (the spike's subject). The same model as Telegram: one process, real end-to-end encryption on the device, bridgev2 handles the Matrix side. Cost: cgo and a Rust build for every target.
- **B. signal-cli (Java) as a sidecar process**: a JVM on the desktop and no in-process path on Android; a second protocol stack beside bridgev2. Rejected: it does not fit the core's model (ADR 0009) and would weigh more than libsignal itself.
- **C. Signal only through a hosted bridge**: no build problem, but the bridge host sees the plaintext (`CLAUDE.md` §2). Kept as the fallback for iOS until E3.3, as for every network.

### For the test account

- **Production Signal with a dedicated number**: needs a number per test account, which the maintainer will not pay for.
- **Staging, with the maintainer's own number** (proposed): a number registered on staging is a separate account from the same number on production. The maintainer registers it once with signal-cli (`--service-environment staging`), as the primary device, entering the captcha and the SMS code themselves; the core links to it as a secondary device, as mautrix-signal always does. With one number, the journey can only use "Note to Self"; a second number lent for free would allow a real conversation.

## Decision

**Feasible with reservations (go)** for the connector, with these conditions:

1. libsignal-ffi builds for Linux, macOS and the four Android ABIs, and libsignalgo's Go tests pass on Linux and macOS with mautrix-signal's own link flags; on Android the test binaries link for the four ABIs (not run: no device in the spike). On Windows too, after three fixes to the build environment and one to BoringSSL's sources (see "Windows" below): libsignalgo's Go tests pass, and mautrix-signal links with GNU ld and with LLVM's lld.
2. Signal makes the core a **cgo** program on every platform where it is built in. The desktop core is pure Go today (`CGO_ENABLED=0`, ADR 0014's measurements). The connector goes **behind a build tag** (`musubee_signal`), so that builds without it stay pure Go, and the desktop builds that include it use the C toolchains already required for `core/ffi` (MSYS2 UCRT64 on Windows, ADR 0010).
3. libsignal is built **once per libsignal commit and target** on CI and cached: 3½ to 12 minutes per target is too long to repeat on every pull request.
4. Staging needs a **patch of signalmeow** (mautrix-signal's Signal client), which hard-codes the production servers. The points to change and their staging values are known and checked (see "Staging"). The preferred form is an upstream pull request that makes the environment configurable; until it lands, a patched copy pinned by the core.

## Measurements

Runs of `.github/workflows/libsignal-spike.yml` on GitHub's hosted runners, 2026-10-11: mautrix-signal v0.2609.0, its pinned libsignal (submodule commit `8fc2113`, `libsignal-ffi` v0.102.2, `boring-sys` v5.2.0), Rust 1.98.1 (libsignal's `rust-toolchain`), Go 1.27.1, NDK 28.2.13676358, Android API 24. Runs 38138082247 (Linux, macOS), 38138873227 (Android, Windows), 38140618561, 38141910249, 38143238631 and 38144630702 (Windows link).

| Target | Build time | `libsignal_ffi.a` | Go tests of libsignalgo | mautrix-signal binary, stripped |
|---|---|---|---|---|
| Linux x86_64 (ubuntu-latest) | 229 s | 217.6 MB | **pass** (0.77 s) | 60.7 MB |
| macOS arm64 (macos-latest) | 442 s | 130.0 MB | **pass** (0.71 s) | 60.0 MB |
| Windows x86_64, `x86_64-pc-windows-gnu` | 370 to 717 s (3 runs) | 164.0 MB | **pass** | 59.7 MB |
| Android arm64-v8a | 238 s | 65.2 MB | linked (27.0 MB), not run | 40.4 MB |
| Android armeabi-v7a | 204 s | 50.5 MB | linked (24.0 MB), not run | 36.9 MB |
| Android x86_64 | 297 s | 64.7 MB | linked (27.9 MB), not run | 43.7 MB |
| Android x86 | 291 s | 47.4 MB | linked (27.2 MB), not run | 40.7 MB |

The archives are large because they carry every crate of libsignal's C API and their debug information; only what the Go code calls is linked. The binary sizes are mautrix-signal's whole bridge program (its own Matrix appservice code included), an upper bound for what the connector adds to the core: for comparison, mautrix-telegram's connector took the core from 18 to 54 MB on Windows and from 17 to 56 MB on Android (ADR 0014).

Native libraries that rustc asks to link (`--print native-static-libs`), which the core's cgo flags must carry:

| Target | Libraries |
|---|---|
| Linux | `-lstdc++ -lgcc_s -lutil -lrt -lpthread -lm -ldl -lc` |
| macOS | `-framework Security -framework CoreFoundation -lc++ -liconv -lSystem -lc -lm` |
| Windows (GNU) | `-lwindows.0.52.0` (found with `-L` in the `windows_x86_64_gnu` crate, see below) `-lstdc++ -ladvapi32 -lbcrypt -lkernel32 -lntdll -luserenv -lws2_32 -ldbghelp` |
| Android | `-lc++ -ldl -llog -lunwind -lm -lc` |

What the builds needed, beyond mautrix-signal's `build-rust.sh`:

- **Android**: the environment of libsignal's own Android build (`java/build_jni.sh`): the NDK's clang as C compiler and linker per target triple, the size-optimised release profile (`opt-level=s`, fat LTO, one codegen unit), `-DOPENSSL_SMALL`, BoringSSL's C++ runtime linked statically (`BORING_BSSL_RUST_CPPLIB=static:-bundle=c++`), and the `aes_armv8` and `curve25519_dalek_bits="64"` cfgs. mautrix-signal's cgo flags then link as they are.
- **Windows**: Rust's GNU toolchain must be the **host**, not only the target (`RUSTUP_TOOLCHAIN=1.98.1-x86_64-pc-windows-gnu`, `CC=gcc`, `CXX=g++`, CMake with Ninja, MSYS2 UCRT64's GCC, clang, NASM and protobuf). With the MSVC host, `boring-sys` sees a cross build (MSVC host, GNU target), turns off its assembly and makes CMake look for `cl` (run 38138082247).
- **macOS**: the linker warns that objects were built for macOS 26.5 and linked for 26.0; the tests pass, but the core's build should set `MACOSX_DEPLOYMENT_TARGET` for both Rust and Go.

### Windows

Linking libsignal into a Go program on Windows took four runs to get right. Each step below is needed, and the workflow, as merged, does them all (the four-way comparison that follows was a diagnostics step of the spike branch, run 38144630702):

1. **UCRT64 first in `PATH`.** In GitHub's bash steps, Git for Windows' `/mingw64/bin` comes before `/c/msys64/ucrt64/bin`. UCRT64's `ld` then loads Git's MinGW runtime DLLs and exits with status 5 or 1 **without a message**, which is the trap `CLAUDE.md` §8 describes for the maintainer's machine (runs 38138873227, 38140618561). The core's Windows builds already need UCRT64 first (ADR 0010); the Signal build must not lose that.
2. **`-L` to the `windows_x86_64_gnu` crate's `lib` directory.** rustc asks for `-lwindows.0.52.0`, an import library that the `windows-targets` crates ship and that cargo passes to rustc with `-L`; Go's link step does not know where it is ("unable to find library", run 38140618561). `cargo fetch` downloads the crates of every target, so the directory must be the x86-64 GNU one: the first match is the aarch64 library, and the link fails with "machine type arm64 conflicts with x64" (run 38143238631).
3. **BoringSSL's P-256 assembly guard.** The BoringSSL that `boring-sys` v5.2.0 builds (submodule commit `e2a57cfb`) enables its ADX assembly for P-256 in `third_party/fiat/p256_64.h` whenever `__GNUC__` and `__x86_64__` are defined, which MinGW's GCC does, but the assembly file is written for ELF and Mach-O only and is not built for Windows: the link fails on `undefined reference to fiat_p256_adx_mul` (run 38141910249). Current BoringSSL guards it with `defined(__ELF__) || defined(__APPLE__)`; the spike adds that condition with `sed` after `cargo fetch` (three lines; run 38143238631) and the link succeeds (run 38144630702). The fallback, `OPENSSL_NO_ASM`, would turn off all of BoringSSL's assembly and slow every cryptographic operation down. The patch disappears when signalapp/boring moves to a BoringSSL with the new guard.

With those, run 38144630702 linked and ran libsignalgo's tests four ways: GNU ld (13 s to link) and LLVM's lld (1 s), each with the archive as built and with its debug sections stripped (`strip --strip-debug`: 164.0 to 97.2 MB). All four test binaries pass. mautrix-signal itself, stripped (`-s -w`), is 59.7 MB with either linker. lld links ten times faster; GNU ld is what the core uses today, and what the merged workflow uses. The Rust build took 717 s, 639 s and 370 s in three runs: GitHub's Windows runners vary, which is one more reason for the cache (decision 3).

## Staging

signalmeow has no setting for the server environment. The points to change, read in mautrix-signal v0.2609.0:

| Where | Production value | Staging value | Source of the staging value |
|---|---|---|---|
| `pkg/signalmeow/web/web.go`: chat, storage and CDN hosts | `chat.signal.org`, `storage.signal.org`, `cdn.signal.org`, `cdn2.signal.org`, `cdn3.signal.org` | `chat.staging.signal.org`, `storage-staging.signal.org`, `cdn-staging.signal.org`, `cdn2-staging.signal.org`, `cdn3-staging.signal.org` | Signal-Android `app/build.gradle.kts`, staging build type |
| `pkg/signalmeow/web/grpc.go`: `GRPCTarget` | `dns://grpc.chat.signal.org:443` | `grpc.chat.staging.signal.org:443` | same |
| `contactdiscovery.go`: CDSI host and enclave measurement | `cdsi.signal.org`, `15637fa1…` | `cdsi.staging.signal.org`, `6d9b9649fa3a337754a98059c66d48ac77aaca5299d3b27d6ed1e646c7c81c0a` | libsignal `rust/attest/src/constants.rs` (commit `4beb029`); the production value there equals signalmeow's |
| `misc.go`: zkgroup server public parameters (`prod-server-public-params.dat`) | the embedded file | the staging parameters | Signal-Android `app/build.gradle.kts` (line 496 for staging; the production value at line 290 equals signalmeow's file, base64-encoded) |

TLS: every staging host presents a chain that ends at one of the root certificates signalmeow already embeds (checked with `openssl s_client` against each host, 2026-10-11); the gRPC hosts chain to the ed25519 root. No certificate change is needed.

Sealed sender: signalmeow decrypts sealed-sender messages with `SealedSenderDecryptToUSMC` and then reads the sender from the sender certificate **without calling `SenderCertificate.Validate`** against Signal's trust roots (`pkg/signalmeow/receiving_decrypt.go`). This is why staging needs no trust-root change. It is also a **security observation**: the server's signature on the certificate, which binds a sender's account to their identity key, is not checked. Whether this allows a sender to claim another account depends on how signalmeow's identity store treats a new identity key for a known account; not analysed in the spike (see "State of knowledge"). It must be settled before Signal is offered to users, and reported to mautrix-signal's maintainers privately if it is exploitable.

## Licenses

- libsignal is **AGPL-3.0-only**; mautrix-signal is AGPL-3.0 (its `LICENSE`), with `LICENSE.exceptions` granting exceptions to Beeper and Element only, which we do not need. Our code stays AGPL-3.0-or-later; a core built with Signal is a combined work that can only be distributed under AGPL-3.0 (version 3), which is compatible with our license. `NOTICE` and the dependency audit (ADR 0008) must say so when the connector lands.
- New Go dependencies that mautrix-signal brings (among them `github.com/coder/websocket`, `github.com/emersion/go-vcard`, `github.com/mattn/go-pointer`, `github.com/tidwall/gjson`, `google.golang.org/grpc`, `google.golang.org/genproto`) and libsignal's Rust crates must pass the license audit before they enter the core. mautrix-signal uses the same mautrix-go (v0.31.0) and `go.mau.fi/util` (v0.10.1) as the core.
- Not decided: the AGPL and Apple's App Store question (`CLAUDE.md` §3) is unchanged by libsignal, which is also AGPL; Signal's own apps are GPL/AGPL and distributed there.

## State of knowledge

| Claim | Status | Source |
|---|---|---|
| libsignal-ffi builds for Linux x86_64, macOS arm64 and the four Android ABIs with the settings above | **verified** (run) | runs 38138082247, 38138873227 |
| libsignalgo's Go tests pass on Linux and macOS with mautrix-signal's link flags | **verified** (run) | run 38138082247 |
| libsignalgo's test binary links for the four Android ABIs | **verified** (run); the tests were not run on a device or emulator | run 38138873227 |
| Windows: libsignal-ffi builds with Rust's GNU toolchain and MSYS2 UCRT64 | **verified** (run) | run 38138873227 |
| Windows: with UCRT64 first in `PATH`, `-L` to the x86-64 `libwindows`, and BoringSSL's P-256 guard patched, libsignalgo's tests pass and mautrix-signal links with GNU ld and lld | **verified** (run) | run 38144630702 |
| Windows: without the guard patch, the link fails on `fiat_p256_adx_mul` | **verified** (run) | run 38141910249 |
| The staging hosts, CDSI measurement and zkgroup parameters above are Signal's | **verified** (read in Signal-Android and libsignal; TLS checked) | see Sources |
| A mautrix-signal patched with them can link a device to a staging account and exchange messages | **assumed**: needs the staging account (maintainer) | the connector task |
| signalmeow does not validate sender certificates | **verified** (read in the code) | `pkg/signalmeow/receiving_decrypt.go`, v0.2609.0 |
| Whether that allows impersonation | **unknown**: to analyse before users get Signal | the connector task |
| Memory and battery cost of Signal in the core on Android | **unknown**: measured with the connector, as for Telegram (ADR 0014) | the connector task |
| iOS: libsignal for `aarch64-apple-ios` in the XCFramework, and its weight in the NSE | **unknown**: libsignal ships iOS builds itself; deferred with the Apple work (E3.3) | — |

## Left to the maintainer

1. **The Signal test environment**: staging with the maintainer's own number (proposed above), or no automated Signal journey for now. The maintainer said "I decide later" (2026-10-11). Registering needs a captcha and an SMS code, which only the maintainer may enter; a script will prepare everything else.
2. **Downloads on the maintainer's machine** for the connector work: protoc, signal-cli (staging registration), and the mautrix-signal Go dependency in the core. Not authorised yet; CI runners can do the builds meanwhile.
3. **A second phone number lent for free**, if one is available, for a two-person journey instead of "Note to Self".
4. **The staging patch's form**: an upstream pull request to mautrix-signal (outward-facing: the maintainer's call), and meanwhile a fork or a patched copy.
5. **The sender-certificate observation**: whether and how to report it to mautrix-signal's maintainers once analysed.

## Consequences

- The connector task can start once the decisions above are taken; nothing in the core changed in the spike.
- CI grows a libsignal build job with a cache per target and libsignal commit; Windows and Android builds of the core with Signal need it. On Windows the job patches BoringSSL's P-256 guard after `cargo fetch`, and checks that it patched three lines, so that a new `boring-sys` that no longer needs it, or moves the file, is noticed.
- The core's builds split in two: pure Go without Signal (fast, no toolchain), cgo with Signal. Electron and Android releases will ship the cgo build.
- ADR 0005's open point ("Whether mautrix-signal can target staging is unknown") is answered: yes, with the patch above.
- Revisit if libsignal stops building for a target, if its size on Android becomes a problem, or if mautrix-signal makes the environment configurable upstream (then no patch).

## Sources

- mautrix-signal v0.2609.0 (`github.com/mautrix/signal`): `build-rust.sh`, `pkg/libsignalgo` (cflags, `sealedsender.go`, `sendercertificate.go`), `pkg/signalmeow` (`web/web.go`, `web/grpc.go`, `contactdiscovery.go`, `misc.go`, `receiving_decrypt.go`, `sending.go`), `LICENSE`, `LICENSE.exceptions`; read 2026-10-11.
- libsignal (`github.com/signalapp/libsignal`), commit `8fc2113` (mautrix-signal's submodule): `rust/bridge/ffi/Cargo.toml`, `java/build_jni.sh`, `bin/build_helpers.sh`, `rust-toolchain`; commit `4beb029`: `rust/attest/src/constants.rs`; read 2026-10-11.
- signalapp/boring, tag `signal-v5.2.0`: `boring-sys/main.rs` (cross-compilation detection); read 2026-10-11.
- Signal-Android, `app/build.gradle.kts` (production and staging build configuration); read 2026-10-11.
- signal-cli README and manual (`--service-environment staging`), via ADR 0005.
- GitHub Actions runs 38138082247, 38138873227, 38140618561, 38141910249, 38143238631 and 38144630702 of `libsignal-spike.yml` on `spike/t2.6-libsignal`.
