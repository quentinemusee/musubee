#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Quentin Raimbaud
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Builds libsignal-ffi, the C library of libsignal that mautrix-signal's
# libsignalgo links: for the host (Linux, macOS, or Windows x86_64 with the
# GNU toolchain of MSYS2 UCRT64), or for one Android ABI with the NDK of the
# app (docs/ADR/0022, docs/ADR/0025, docs/ADR/0028).
#
#   scripts/build-libsignal.sh OUTPUT_DIR [ANDROID_ABI]
#
# OUTPUT_DIR receives libsignal_ffi.a and cgo-ldflags, the extra linker flags
# of the target (empty on Linux, macOS and Android), and for Android a
# libc++.a that links the C++ runtime statically. Build Go code that uses
# Signal with:
#
#   LIBRARY_PATH=OUTPUT_DIR CGO_LDFLAGS="-LOUTPUT_DIR $(cat OUTPUT_DIR/cgo-ldflags)" \
#     go test -tags=goolm,musubee_signal ...
#
# ANDROID_ABI is arm64-v8a, armeabi-v7a, x86_64 or x86. Android builds need
# ANDROID_NDK_HOME set to the NDK (28.2.13676358, the app's), and ANDROID_API
# (default 24, the app's minSdkVersion); Go then builds with GOOS=android,
# CGO_ENABLED=1 and CC set to the NDK's clang for that ABI and API, as the
# app's Gradle build does (apps/mobile/android). On a Windows host, give Go
# "clang.exe --target=x86_64-linux-android24" (for x86_64) rather than the
# NDK's .cmd wrapper: a link with libsignal exceeds cmd.exe's limit on the
# length of a command line.
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

out=${1:?usage: build-libsignal.sh OUTPUT_DIR [ANDROID_ABI]}
abi=${2:-}

# Android: the Rust target, and the NDK clang's target prefix (armv7a, not
# armv7, for 32-bit ARM), as in the app's Gradle build.
if [ -n "$abi" ]; then
  case "$abi" in
    arm64-v8a) target=aarch64-linux-android clang=aarch64-linux-android ;;
    armeabi-v7a) target=armv7-linux-androideabi clang=armv7a-linux-androideabi ;;
    x86_64) target=x86_64-linux-android clang=x86_64-linux-android ;;
    x86) target=i686-linux-android clang=i686-linux-android ;;
    *) echo "unknown Android ABI $abi: arm64-v8a, armeabi-v7a, x86_64 or x86" >&2; exit 2 ;;
  esac
  case "$(uname -s)" in
    Linux) ndk_host=linux-x86_64 ;;
    Darwin) ndk_host=darwin-x86_64 ;;
    MINGW* | MSYS* | CYGWIN*) ndk_host=windows-x86_64 ;;
    *) echo "no Android NDK for this host: $(uname -s)" >&2; exit 2 ;;
  esac
  ndk_bin=${ANDROID_NDK_HOME:?Android builds need ANDROID_NDK_HOME}/toolchains/llvm/prebuilt/$ndk_host/bin
  ndk_cc=$ndk_bin/$clang${ANDROID_API:-24}-clang
  ndk_ar=$ndk_bin/llvm-ar
  if [ "$ndk_host" = windows-x86_64 ]; then
    # Rust and CMake are Windows programs: Windows paths, and the NDK's
    # .cmd wrapper of clang for the target and API.
    ndk_cc=$(cygpath -m "$ndk_cc").cmd
    ndk_ar=$(cygpath -m "$ndk_ar").exe
  fi
  if [ ! -f "$ndk_cc" ]; then
    echo "no NDK clang at $ndk_cc" >&2
    exit 2
  fi
fi

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
if [ -n "$abi" ]; then
  env -u CARGO_HOME rustup target add --toolchain "$RUSTUP_TOOLCHAIN" "$target" > /dev/null
fi
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

if [ -n "$abi" ]; then
  # The environment of libsignal's own Android build (java/build_jni.sh),
  # verified for the four ABIs in ADR 0022: the NDK's clang as C compiler
  # and linker, the size-optimised release profile, BoringSSL's C++ runtime
  # linked statically.
  target_cc=$(echo "$target" | tr '-' '_')
  target_env=$(echo "$target" | tr '[:lower:]-' '[:upper:]_')
  export "CC_$target_cc=$ndk_cc" "CARGO_TARGET_${target_env}_LINKER=$ndk_cc"
  export TARGET_AR="$ndk_ar"
  export CARGO_PROFILE_RELEASE_OPT_LEVEL=s CARGO_PROFILE_RELEASE_LTO=fat CARGO_PROFILE_RELEASE_CODEGEN_UNITS=1
  export CFLAGS=-DOPENSSL_SMALL CXXFLAGS=-DOPENSSL_SMALL BORING_BSSL_RUST_CPPLIB=static:-bundle=c++
  rustflags='--cfg aes_armv8 --cfg curve25519_dalek_bits="64" --print native-static-libs'
  target_args=(--target "$target")
  lib=target/$target/release/libsignal_ffi.a
else
  # mautrix-signal's build-rust.sh, plus the list of native libraries.
  rustflags="-Ctarget-feature=-crt-static --print native-static-libs"
  target_args=()
  lib=target/release/libsignal_ffi.a
fi

start=$(date +%s)
if ! RUSTFLAGS="$rustflags" RUSTC_WRAPPER="" \
  cargo build -p libsignal-ffi --profile=release ${target_args[@]+"${target_args[@]}"} > "$out/build.log" 2>&1; then
  tail -50 "$out/build.log"
  exit 1
fi
echo "Build time: $(( $(date +%s) - start )) s"
cp "$lib" "$out/"

: > "$out/cgo-ldflags"
if [ -n "$abi" ]; then
  # The C++ runtime that BoringSSL needs must be linked statically: a device
  # has no libc++_shared.so unless an app ships it. libsignalgo links with
  # -lstdc++, which the NDK's clang turns into -lc++, which the NDK resolves
  # to libc++_shared.so; -static-libstdc++ does not change that in a C link
  # (run 38168466838). This libc++.a, a linker script like the NDK's own
  # static one, comes first through -L OUTPUT_DIR.
  echo "INPUT(-lc++_static -lc++abi)" > "$out/libc++.a"
fi
if $windows && [ -z "$abi" ]; then
  # Go's link step also needs the system libraries that rustc lists, and
  # libwindows.0.52.0.a, an import library that cargo passes to rustc with
  # -L: the one of the windows_x86_64_gnu crate, since cargo fetch downloads
  # the crates of every target and an aarch64 copy links with "machine type
  # arm64 conflicts with x64" (run 38143238631).
  cp "$(find "${CARGO_HOME:-$HOME/.cargo}/registry/src" -path '*/windows_x86_64_gnu-*/lib/libwindows.0.52.0.a' | head -1)" "$out/"
  grep -h "native-static-libs:" "$out/build.log" | head -1 | sed 's/.*native-static-libs: //' > "$out/cgo-ldflags"
fi
ls -l "$out"
