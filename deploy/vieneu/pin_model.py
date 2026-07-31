"""Pin + enforce the HF repos this image depends on.

`vieneu`'s own fetch calls never pass a `revision=` -- they always resolve
whatever `main` currently points to. That is a licence risk here (the
sibling repo `pnnbao-ump/VieNeu-TTS-v3-Turbo` ships several licences under
one org; the assets we use are Apache-2.0, verified by hand -- see
README.md). So before anything else touches the network in this build:

  1. Ask the Hub API what `main` resolves to for both repos right now.
  2. Assert that matches the pinned, licence-reviewed commit below. A
     mismatch fails the build loudly instead of silently baking in
     different (and un-reviewed) bytes.
  3. Fetch exactly the files `vieneu` would fetch on its own (same
     `allow_patterns` subset, so the image doesn't grow), at the pinned
     revision explicitly.

`Vieneu()` is then constructed once more (unpinned, as the SDK always
does), which reuses this same content-addressed cache instead of
downloading again, and populates `refs/main` for the offline runtime
lookup (HF_HUB_OFFLINE=1 is set after this script runs).
"""
from huggingface_hub import HfApi, snapshot_download

# repo_id -> (pinned commit, licence, allow_patterns matching what the
# `vieneu` SDK's own onnx_runtime_lite.OnnxV3LiteEngine._fetch() calls pull)
PINNED = {
    "pnnbao-ump/VieNeu-TTS-v3-Turbo": (
        "75ff82a72f54d55ed389e1eeb12041d3c4bac7d4",
        "Apache-2.0",
        ["config.json", "onnx_int8/*"],
    ),
    "OpenMOSS-Team/MOSS-Audio-Tokenizer-Nano-ONNX": (
        "ceff0d0749bfb3fa2d61149794ec6feef0d1e1ae",
        "Apache-2.0",
        [
            "moss_audio_tokenizer_decode_full.onnx",
            "moss_audio_tokenizer_decode_shared.data",
            "moss_audio_tokenizer_decode_step.onnx",
            "codec_browser_onnx_meta.json",
            "moss_audio_tokenizer_encode.onnx",
            "moss_audio_tokenizer_encode.data",
        ],
    ),
}

api = HfApi()

for repo_id, (expected_sha, licence, patterns) in PINNED.items():
    actual_sha = api.model_info(repo_id).sha
    if actual_sha != expected_sha:
        raise SystemExit(
            f"REVISION DRIFT: {repo_id} main is now {actual_sha!r}, "
            f"expected pinned {expected_sha!r} ({licence}). "
            "Re-review the new commit's licence before updating this pin."
        )
    print(f"OK: {repo_id} main == {expected_sha} ({licence})")
    snapshot_download(repo_id, revision=expected_sha, allow_patterns=patterns)

# Reuses the cache populated above (same commit == same content-addressed
# blobs) and writes refs/main so the runtime, unpinned call inside
# `vieneu` resolves correctly while HF_HUB_OFFLINE=1.
from vieneu import Vieneu  # noqa: E402

Vieneu()
print("Vieneu() loaded OK from the pinned cache.")
