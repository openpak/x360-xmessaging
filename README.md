# Xbox 360 Message Inbox

OpenPak's Xbox 360 message inbox (Live service 18, `xmessaging`).

| | |
| --- | --- |
| Network | Xbox 360 |
| Role | Per-player inbox and message send |
| Depends on | [`x360-common`](https://github.com/openpak/x360-common) (vendored), [`x360-xuacs`](https://github.com/openpak/x360-xuacs) internal API |
| Image | `ghcr.io/openpak/x360-xmessaging` |
| Used by | XAM through the security gateway; `x360-xpnfront` reads the unread count |
| Licence | AGPL-3.0-only |

## What it serves

| Path | XRL | What |
| --- | --- | --- |
| `POST /xpnfront/xmessaging.srf` | 0x513, 0x514, 0x503, 0x504, 0x505, 0x50A | Inbox list, fetch, details, flags, delete, send |
| `GET /internal/v1/unread?xuid=` (:10118) | | Unread count for presence |
| `GET /healthz` | | Health probe |

Identity headers are trusted only with `X360_GATEWAY_KEY`. State is in Postgres (`X360_XMESSAGING_DATABASE_URL`, schema `x360_xmessaging`; `X360_XMESSAGING_STATE` is only the old inbox.json, imported once).

## Run

```sh
GOWORK=off go run -mod=vendor ./cmd/x360-xmessaging
```

## Docs

- [Protocol](docs/protocol.md)
- [Evidence](docs/evidence.md)
- [Deploy](docs/deploy.md)

## Licence

AGPL-3.0-only. See [LICENSE](LICENSE).
