# Changelog

## v0.2.0 (2026-11-03)

- Inboxes move to Postgres (schema `x360_xmessaging` in database `x360`, one document per inbox,
  `X360_XMESSAGING_DATABASE_URL`, required: the service refuses to start without it). An existing `inbox.json`
  (`X360_XMESSAGING_STATE`) is imported once on first start and left in place.

## v0.1.0

- Live service 18: inbox list (0x513), fetch (0x514), details (0x503), flags (0x504), delete (0x505) and
  send (0x50A), with a per-XUID JSON inbox (125 messages, expiry, read flag).
- Internal `GET /internal/v1/unread` on :10118 for x360-xpnfront.
