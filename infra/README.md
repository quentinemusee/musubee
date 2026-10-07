# infra — realistic test environment

Services started with Docker for integration and end-to-end tests. Principle: **test against the real thing**, never against mocks (see [`docs/TESTING.md`](../docs/TESTING.md)).

**Status: empty.** Work planned in T0.3 (Synapse, PostgreSQL, dummybridge) and T0.4 (Telegram bridge on the official test servers).

## Planned contents

- `compose.test.yml`: Synapse (reference homeserver) + PostgreSQL + dummybridge.
- Start and stop scripts, service health checks.
- A fault-injection proxy (for example Toxiproxy) to simulate outages and latency.

## Rules

- **No secrets and no real accounts in this directory.** Local data (`infra/data/`) is ignored by Git.
- Docker images pinned by version (ideally by digest).

Target command (to be confirmed in T0.3):

```
docker compose -f infra/compose.test.yml up -d
```
