# SPDX-FileCopyrightText: 2026 Quentin Raimbaud
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# mautrix-telegram (AGPL-3.0-or-later, https://github.com/mautrix/telegram) at a
# pinned commit, with test-servers.patch applied so that the bridge can connect
# to Telegram's test environment. Used by infra/compose.telegram.yml for the
# end-to-end tests only; never published.
#
# Base images are pinned by digest (tag kept for readability).

FROM golang:1-alpine3.24@sha256:8a5910f31396cd4d89662f56c68b3ae31d374308270a1c3bd96672ee5ed43414 AS builder

# mautrix-telegram commit of 2026-10-06 (after release v0.2609.0).
ARG MAUTRIX_TELEGRAM_COMMIT=9b2a6e3ea348b37ff0076db5473e08acb71b0767

RUN apk add --no-cache git ca-certificates build-base olm-dev

WORKDIR /build
RUN git init -q . \
    && git remote add origin https://github.com/mautrix/telegram.git \
    && git fetch -q --depth 1 origin "${MAUTRIX_TELEGRAM_COMMIT}" \
    && git checkout -q FETCH_HEAD

COPY test-servers.patch /tmp/test-servers.patch
RUN git apply --whitespace=nowarn /tmp/test-servers.patch && ./build.sh

FROM alpine:3.24@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6

RUN apk add --no-cache ca-certificates olm

COPY --from=builder /build/mautrix-telegram /usr/bin/mautrix-telegram

# The configuration and registration are generated per run by infra/testenv
# and mounted at /data; the database is created there too.
WORKDIR /data
ENTRYPOINT ["/usr/bin/mautrix-telegram"]
CMD ["--config", "/data/config.yaml", "--no-update"]
