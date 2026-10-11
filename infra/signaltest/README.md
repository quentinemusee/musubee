# Signal end-to-end test (staging)

The test in this directory runs the core with Signal inside (build tag `musubee_signal`, ADR 0025) against **Signal's staging service**, never against Signal's public service. A number registered on staging is a separate account from the same number on the public service: the test account does not touch anyone's real Signal account.

signal-cli plays the user's phone. It is the primary device of the test account, and it links the core as a secondary device with the URI of the core's QR code, as the Signal app does when the user scans it. With a single test account, messages go through "Note to Self".

## One-time setup (the maintainer)

1. Install Java 25 or later and signal-cli 0.14.9 or later (on the maintainer's machine: `D:\SDK\jdk-25.0.4.1+1-jre` and `D:\SDK\signal-cli-0.14.9`).
2. Register the number on staging. The script opens the captcha page and asks for the captcha link, then for the code that Signal sends by SMS (`-Voice` for a call). Only the maintainer enters them:

   ```powershell
   $env:JAVA_HOME = "D:\SDK\jdk-25.0.4.1+1-jre"
   $env:MUSUBEE_SIGNAL_CLI = "D:\SDK\signal-cli-0.14.9\bin\signal-cli.bat"
   powershell -ExecutionPolicy Bypass -File infra/signaltest/register-staging.ps1 -Number +33612345678 -DataDir D:\SDK\signal-staging-account
   ```

   `-DataDir` then holds the test account's private keys. It stays outside the repository; never commit it, paste it in a chat or print it in a log.

## Running the test

Build libsignal-ffi once (`scripts/build-libsignal.sh`, see its header), then:

```bash
export JAVA_HOME=/d/SDK/jdk-25.0.4.1+1-jre
export MUSUBEE_SIGNAL_CLI=D:/SDK/signal-cli-0.14.9          # the installation directory
export MUSUBEE_SIGNAL_CLI_DATA=D:/SDK/signal-staging-account
export MUSUBEE_SIGNAL_NUMBER=+33612345678
out=D:/SDK/build/libsignal-out
PATH="/d/SDK/msys64/ucrt64/bin:$PATH" CGO_ENABLED=1 LIBRARY_PATH=$out CGO_LDFLAGS="-L$out $(cat $out/cgo-ldflags)" \
  go test -tags=goolm,signal,musubee_signal -count=1 -v ./infra/signaltest/
```

The test:

1. unlinks the devices that an earlier run left linked, and drains the messages waiting for the primary device;
2. starts the core's QR code login and links the core with `signal-cli addDevice`;
3. sends a Note to Self from signal-cli and waits for it in the core;
4. answers from the core in the same conversation and waits for it in signal-cli (a sync message);
5. logs the core out, unlinks what is left, and checks that the core's log holds neither message.

Without the variables, the test is skipped; with `MUSUBEE_REQUIRE_SIGNAL_E2E=1` it fails instead. The account has one primary device: never run two of these tests at once.

## On CI

Not yet: the test needs signal-cli's state of the test account as an encrypted GitHub secret, which the maintainer sets (`CLAUDE.md` §6.3). Until then it runs on the maintainer's machine.
