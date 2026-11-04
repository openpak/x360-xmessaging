# Deploy

Primary host, quadlet `deploy/openpak-x360-xmessaging.container` (IP 10.89.2.209 on openpak.network). It needs
`~/openpak/deploy/x360-xmessaging/internal.env` (`X360_XMESSAGING_INTERNAL_KEY`, mode 0600), a writable
`state/` directory, and the shared gateway-key and `x360-xuacs-internal.env` files. Merge `deploy/traefik.yml`
into the shared dynamic file (back it up first). Service 18 must be in the KDC's D7 list; the gateway routes it
to Traefik by default.

## Account erasure

`DELETE /internal/accounts/{account_id}/data` with `Authorization: Bearer $ACCOUNT_ERASE_KEY` on a separate listener `ERASE_ADDR` (default `:9099`, container network only, never published; not served while the key is unset). Reply `{"service":"x360-xmessaging","rows":N}`; one `account erased` log line per erasure.

Website step name: `x360-xmessaging`.

Deletes the account's inbox documents (`box/<xuid>`). Anonymised, not deleted: messages the account sent that sit in other inboxes (sender XUID and tag blanked; the recipient owns that mail). Needs the existing xuacs internal settings. Run before x360-xuacs.
