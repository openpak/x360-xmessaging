# Changelog

## v0.3.0 (2026-11-04)

- Account erasure (card #120). `DELETE /internal/accounts/{account_id}/data` with `Authorization: Bearer $ACCOUNT_ERASE_KEY` on a separate listener `ERASE_ADDR` (default `:9099`, container network only, never published; not served while the key is unset). Reply `{"service":"x360-xmessaging","rows":N}`; one `account erased` log line per erasure. The account's XUIDs come from x360-xuacs (`xuacsapi.AccountXUIDs`, existing `X360_XUACS_INTERNAL_URL`/`_KEY`). Deletes the account's inboxes; messages it sent that sit in other users' inboxes stay for the recipient with the sender XUID and gamertag blanked.

## v0.2.1 (2026-11-03)

- Every request logs one stderr line, `http <method> <path> -> <status> <duration>ms` (x360-common v0.5.1 `httplog`): no query, headers, cookies, tokens or bodies; `/healthz` skipped.

## v0.2.0 (2026-11-03)

- Inboxes move to Postgres (schema `x360_xmessaging` in database `x360`, one document per inbox,
  `X360_XMESSAGING_DATABASE_URL`, required: the service refuses to start without it). An existing `inbox.json`
  (`X360_XMESSAGING_STATE`) is imported once on first start and left in place.

## v0.1.0

- Live service 18: inbox list (0x513), fetch (0x514), details (0x503), flags (0x504), delete (0x505) and
  send (0x50A), with a per-XUID JSON inbox (125 messages, expiry, read flag).
- Internal `GET /internal/v1/unread` on :10118 for x360-xpnfront.
