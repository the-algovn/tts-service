# tts-service

Shared Vietnamese text-to-speech gRPC service (`algovn.tts.v1.TTSService`).
Internal gRPC only -- never registered at a gateway.

Config: `~/.algovn/tts.env` (`KEY=VALUE` lines, `#` comments), process env
always wins. Listens on `:9490` (`LISTEN_ADDR`).
