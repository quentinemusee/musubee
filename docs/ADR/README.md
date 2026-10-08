# Architecture Decision Records (ADR)

One ADR per structural decision. Every phase 1 spike ends with a go / no-go ADR. An accepted ADR is not rewritten: it is superseded by a new ADR that cites it.

New ADR: copy [`0000-template.md`](0000-template.md) to `NNNN-short-title.md` (next number, four digits) and add it to the index below.

Every ADR separates what is **verified**, **assumed** and **unknown**, and cites its sources with the date they were accessed.

## Index

| No. | Title | Status | Task |
|---|---|---|---|
| [0001](0001-stack.md) | Tech stack: Go core, React UI, Electron and Capacitor | accepted | T0.1 |
| [0002](0002-license-and-reuse.md) | AGPL-3.0-or-later license, DCO and REUSE check | accepted | T0.1 |
| [0003](0003-third-party-skills-guardrails.md) | Guardrails for third-party Claude Code skills | accepted | T0.2 |
| [0004](0004-integration-test-environment.md) | Integration test environment: Synapse and PostgreSQL with Docker Compose | accepted | T0.3 |
| [0005](0005-telegram-test-environment.md) | Telegram test environment for end-to-end bridge tests | superseded by 0006 (findings valid) | T0.4 |
| [0006](0006-telegram-e2e-with-test-bots.md) | Telegram end-to-end tests with dedicated test bots | accepted | T0.4 |
| [0007](0007-mobile-e2e-tool.md) | Mobile end-to-end test tool: Appium with WebdriverIO | accepted | T0.5 |
| [0008](0008-dependency-license-audit.md) | Automated license audit of Go and npm dependencies | accepted | T0.6 |
| [0009](0009-bridgev2-in-process.md) | Running bridgev2 connectors in-process, without a homeserver | accepted | T1.1 |
| [0010](0010-core-shared-library.md) | The core as a C shared library (Windows and Linux) | accepted | T1.2 |
