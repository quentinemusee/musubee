# Musubee

A free and open-source universal messenger: one place for your Telegram, Signal, WhatsApp and Matrix conversations, end-to-end encrypted, with the option to merge the threads of the same person. Musubee is built on [Matrix](https://matrix.org) and the [mautrix](https://github.com/mautrix) bridges, but hides Matrix entirely from the user.

> **Status: phase 0 (foundations).** There is no usable application yet. See [`docs/TASKS.md`](docs/TASKS.md).

## Target platforms

Android, iOS, Windows, macOS and Linux, as native applications (Electron on desktop, Capacitor on mobile). No web app for now.

## Repository layout

| Directory | Contents |
|---|---|
| [`core/`](core/) | Go core: domain, connectors (`bridgev2`), storage, local API |
| [`ui/`](ui/) | React + TypeScript + Vite interface, shared by every platform |
| [`apps/desktop/`](apps/desktop/) | Electron shell (Windows, macOS, Linux) |
| [`apps/mobile/`](apps/mobile/) | Capacitor shell and native Android / iOS projects (including the notification extension) |
| [`infra/`](infra/) | Realistic test environment (Synapse, PostgreSQL, bridges) with Docker Compose |
| [`docs/`](docs/) | Tasks, testing strategy, skills, Architecture Decision Records (ADR) |
| [`scripts/`](scripts/) | Repository tooling (license and language checks, skill installation) |

## What Musubee will not do

- **No audio/video calls across networks**: Musubee shows a call notification and opens the official app.
- **"Hosted bridge" mode**: when the bridge cannot run on the device, the bridge host sees messages in plaintext. The app always says so.

Details: [`CLAUDE.md`](CLAUDE.md), §3.

## Contributing

Read [`CONTRIBUTING.md`](CONTRIBUTING.md). Every commit must be signed off under the *Developer Certificate of Origin* (`git commit -s`). Every file must carry an SPDX header, and everything in the repository is written in English. Run the checks with:

```
python -m pip install -r scripts/requirements-dev.txt
python scripts/license_check.py
python scripts/language_check.py
```

## License

Copyright © 2026 Quentin Raimbaud.

Musubee is free software distributed under the **GNU Affero General Public License, version 3 or later** (`AGPL-3.0-or-later`). See [`LICENSE`](LICENSE) and [`NOTICE`](NOTICE). The repository follows the [REUSE](https://reuse.software) specification: the license of each file is given by its SPDX header or by [`REUSE.toml`](REUSE.toml).
