#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Quentin Raimbaud
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Builds libsignal-ffi, the C library of libsignal that mautrix-signal's
# libsignalgo links, for the host: Linux, macOS, or Windows x86_64 with the
# GNU toolchain of MSYS2 UCRT64 (docs/ADR/0022, docs/ADR/0025).
#
#   scripts/build-libsignal.sh OUTPUT_DIR
#
# OUTPUT_DIR receives libsignal_ffi.a and cgo-ldflags, the extra linker flags
# of the host (empty on Linux and macOS). Build Go code that uses Signal with:
#
#   LIBRARY_PATH=OUTPUT_DIR CGO_LDFLAGS="-LOUTPUT_DIR $(cat OUTPUT_DIR/cgo-ldflags)" \
#     go test -tags=goolm,musubee_signal ...
#
# Needs git, a Rust toolchain manager (rustup), protoc, cmake and a C/C++
# compiler; on Windows, MSYS2 UCRT64 first in PATH with its gcc, cmake,
# ninja, nasm, clang, protobuf, dlfcn and zlib packages (CLAUDE.md section 8).
# LIBSIGNAL_SRC sets where the sources are fetched (default: build/ under the
# current directory, or RUNNER_TEMP on CI). CARGO_HOME is honoured: on
# Windows, the BoringSSL patch below edits a file of its registry.
set -euo pipefail

# The libsignal of mautrix-signal v0.2609.0 (core/replace/mautrix-signal):
# its pkg/libsignalgo/libsignal submodule, tag v0.102.2.
LIBSIGNAL_COMMIT=8fc2113bda042fc972a166a24b974b0d34155c6c

out=${1:?usage: build-libsignal.sh OUTPUT_DIR}
mkdir -p "$out"
out=$(cd "$out" && pwd)
src=${LIBSIGNAL_SRC:-${RUNNER_TEMP:-$PWD/build}/libsignal-$LIBSIGNAL_COMMIT}

if [ ! -d "$src/.git" ]; then
  git init -q "$src"
  git -C "$src" remote add origin https://github.com/signalapp/libsignal.git
  git -C "$src" fetch -q --depth 1 origin "$LIBSIGNAL_COMMIT"
  git -C "$src" checkout -q FETCH_HEAD
fi
test "$(git -C "$src" rev-parse HEAD)" = "$LIBSIGNAL_COMMIT"
cd "$src"

windows=false
case "$(uname -s)" in MINGW* | MSYS* | CYGWIN*) windows=true ;; esac

if $windows; then
  # libsignal must be a GNU-ABI static library for Go's MinGW link, so the
  # Rust host toolchain is the GNU one: with the MSVC host, boring-sys
  # treats the build as cross-compiling and CMake picks cl.
  RUSTUP_TOOLCHAIN="$(cat rust-toolchain)-x86_64-pc-windows-gnu"
  export CMAKE_GENERATOR=Ninja CC=gcc CXX=g++
  LIBCLANG_PATH=$(cygpath -w "$(dirname "$(command -v clang)")")
  export LIBCLANG_PATH
else
  RUSTUP_TOOLCHAIN="$(cat rust-toolchain)"
fi
export RUSTUP_TOOLCHAIN
# rustup looks for itself under CARGO_HOME: run it with the default one.
env -u CARGO_HOME rustup toolchain install "$RUSTUP_TOOLCHAIN" --profile minimal > /dev/null
rustc --version
cargo fetch > /dev/null

if $windows; then
  # The BoringSSL of signalapp/boring signal-v5.2.0 calls fiat_p256_adx_mul
  # and fiat_p256_adx_sqr whenever GCC targets x86_64, but their assembly is
  # only built for ELF and Apple targets: MinGW links fail on these two
  # symbols (run 38141910249). Upstream BoringSSL has since narrowed the
  # condition to ELF and Apple; apply the same condition to the fetched
  # sources. Three lines carry it; anything else means boring-sys changed
  # and this patch must be looked at again.
  cargo_home=${CARGO_HOME:-$HOME/.cargo}
  header=$(find "$cargo_home" -path '*boringssl/third_party/fiat/p256_64.h' | head -1)
  sed -i 's/^#if !defined(OPENSSL_NO_ASM) \&\& defined(__GNUC__) \&\& defined(__x86_64__)$/#if !defined(OPENSSL_NO_ASM) \&\& defined(__GNUC__) \&\& defined(__x86_64__) \&\& (defined(__ELF__) || defined(__APPLE__))/' "$header"
  test "$(grep -c '(defined(__ELF__) || defined(__APPLE__))' "$header")" = 3
fi

# mautrix-signal's build-rust.sh, plus the list of native libraries.
start=$(date +%s)
if ! RUSTFLAGS="-Ctarget-feature=-crt-static --print native-static-libs" RUSTC_WRAPPER="" \
  cargo build -p libsignal-ffi --profile=release > "$out/build.log" 2>&1; then
  tail -50 "$out/build.log"
  exit 1
fi
echo "Build time: $(( $(date +%s) - start )) s"
cp target/release/libsignal_ffi.a "$out/"

: > "$out/cgo-ldflags"
if $windows; then
  # Go's link step also needs the system libraries that rustc lists, and
  # libwindows.0.52.0.a, an import library that cargo passes to rustc with
  # -L: the one of the windows_x86_64_gnu crate, since cargo fetch downloads
  # the crates of every target and an aarch64 copy links with "machine type
  # arm64 conflicts with x64" (run 38143238631).
  cp "$(find "${CARGO_HOME:-$HOME/.cargo}/registry/src" -path '*/windows_x86_64_gnu-*/lib/libwindows.0.52.0.a' | head -1)" "$out/"
  grep -h "native-static-libs:" "$out/build.log" | head -1 | sed 's/.*native-static-libs: //' > "$out/cgo-ldflags"
fi
ls -l "$out"
