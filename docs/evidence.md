# Evidence

Wire layouts, limits and the minimal replies come from static RE of XAM 17559 (decompile FUN_81a4ecf0,
FUN_81a4f2d0, FUN_81a4ef10, FUN_81a4f980, FUN_81a4e988, FUN_81a5f4a0; record parsing FUN_818e20d8 and
FUN_818e5718; inbox cap 125 in FUN_818e5718/818e5970), with the replies checked by the XRL response decoder
(`xrlcodec.py decresp`). Nothing here has been seen from a console yet.

Inferred, not verified: the read flag bit, the HRESULTs for a missing message, the title id of server-sent
messages (0xFFFE07D1), the meaning of 0x50B and of the byte order of numeric property values. The stock dash
cannot list or compose messages (Guide.Friends.xex is absent); only the toast and inbox sync run natively.
