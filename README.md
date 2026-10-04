# tts-service

Shared Vietnamese text-to-speech gRPC service (`algovn.tts.v1.TTSService`).
Internal gRPC only -- never registered at a gateway.

Config: `~/.algovn/tts.env` (`KEY=VALUE` lines, `#` comments), process env
always wins. Listens on `:9490` (`LISTEN_ADDR`).

## Engine

One self-hosted engine: VoxCPM2, served by the pod in `deploy/voxcpm/`.
Set `VOXCPM_URL` (plus `MINIO_ENDPOINT` for the voice registry and cache) to
enable it. Voices live in the registry (`CreateVoice`, `DesignVoice`,
`DeleteVoice`, `ListVoices`) and are addressed as `voxcpm:<id>`. A voice id
without a `provider:` prefix is rejected with `InvalidArgument`.

Synthesis is free: `cost_usd` and `free_tier_chars_per_month` are always 0.
A `fake` backend (silence) is always registered for dev and tests; it is never
listed by `ListVoices`.
