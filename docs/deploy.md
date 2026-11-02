# Deploy

Primary host, quadlet `deploy/openpak-x360-xmessaging.container` (IP 10.89.2.209 on openpak.network). It needs
`~/openpak/deploy/x360-xmessaging/internal.env` (`X360_XMESSAGING_INTERNAL_KEY`, mode 0600), a writable
`state/` directory, and the shared gateway-key and `x360-xuacs-internal.env` files. Merge `deploy/traefik.yml`
into the shared dynamic file (back it up first). Service 18 must be in the KDC's D7 list; the gateway routes it
to Traefik by default.
