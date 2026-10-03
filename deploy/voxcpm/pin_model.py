"""Pin + enforce the HF revision this image bakes in (licence-reviewed:
openbmb/VoxCPM2, Apache-2.0). Fails the build if `main` has drifted, then
downloads exactly that revision so the offline runtime resolves to it."""
from huggingface_hub import HfApi, snapshot_download

REPO = "openbmb/VoxCPM2"
PINNED = "32279effe8c19989596f05d353d1447f51d9e915"

actual = HfApi().model_info(REPO).sha
if actual != PINNED:
    raise SystemExit(f"REVISION DRIFT: {REPO} main is {actual!r}, pinned {PINNED!r}. "
                     "Re-review the licence before updating this pin.")
snapshot_download(REPO, revision=PINNED)

from voxcpm import VoxCPM  # noqa: E402

VoxCPM.from_pretrained(REPO, load_denoiser=False, optimize=False, device="cpu")
print("VoxCPM2 loaded OK from the pinned cache.")
