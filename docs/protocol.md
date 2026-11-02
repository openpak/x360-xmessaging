# Protocol: x360-xmessaging

Live service 18, `POST /xpnfront/xmessaging.srf`, XAM 17559. Request body: one element, a 32-byte header
(`u32 msg_type, u32 msg_len, u32 seq, sgaddr[20]`) then `msg_len` bytes, all little endian. Every reply is a
zeroed 32-byte header (the console discards it), `u32 HRESULT`, then the payload. The first `u64` of every
request is an XUID that must be on the tunnel's ticket (`X-X360-Users`), else 403.

| id | msg_type | request after the header | reply after the HRESULT |
|---|---|---|---|
| 0x513 list | 0x1B | u64 xuid | u16 n, n x 92-byte record (newest first) |
| 0x514 fetch | 0x1C | u64 xuid, u32 id | 92-byte record |
| 0x503 details | 0xF | u64 xuid, u32 id, u32 set, u32 clear | first 52 bytes of the record, then the stored properties |
| 0x504 flags | 0x15 | same | none |
| 0x505 delete | 0x10 | u64 xuid, u32 id, u32 block_sender | none |
| 0x50A send | 0x11 | sender u64, context u64, flags u32, logon u32, expire u16 (max 0xA8C0), props size u16, n u16 (1..100), type u8, gamertag[15], n x u64 recipient, props | u16 n, n x {u64 xuid, u32 hr} |

Flags: `flags = (flags | set) & ~clear`; `FlagRead` is 0x01000000 (inferred). Record: sender xuid, context, sent
(seconds since 2003-01-01 UTC), id (per inbox, increasing), flags, title id, expire minutes, details size, type,
gamertag, 19 UTF-16 characters of the text property 0x06C4. Properties are stored as received. Limits: 125
messages per inbox (oldest dropped), expired messages (sent + expire minutes) vanish, expire 0 is not stored,
properties at most 4092 bytes. Unknown message: HRESULT 0x80155A04 (inferred); 0x503 without properties:
0x80155A05. A recipient with no account gets 0x80004005 in the send reply; the others still receive it. The
stored gamertag is the sender's, from x360-xuacs, not the one in the request. 0x50B is not implemented (400).
Blocking the sender on delete is only logged: the account core has no write API for blocks.

## Internal API (port 10118)

`GET /internal/v1/unread?xuid=<16 hex>` with header `X-Internal-Key: $X360_XMESSAGING_INTERNAL_KEY` returns
`{"unread":N}` (live messages without `FlagRead`). 403 for a wrong key, 400 for a malformed XUID. The listener
only starts when the key is set. x360-xpnfront should answer the presence-alive reply with HRESULT 0x001512F0
for a user whose count rose.
