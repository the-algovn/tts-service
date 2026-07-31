// Package catalog knows which voices exist, which backend serves each, and how
// a caller's voice id maps onto them.
package catalog

import "strings"

// ProviderGoogle is the default for an unnamespaced id.
const ProviderGoogle = "google"

type Voice struct {
	ID            string
	Label         string
	Provider      string
	Tier          string
	Gender        string
	FreeTierChars int64
}

// Resolve splits "provider:name". An id with no provider prefix resolves to
// google, because ids persisted before this service existed are bare Google
// voice names.
func Resolve(voiceID string) (provider, name string) {
	p, n, ok := strings.Cut(voiceID, ":")
	if !ok {
		return ProviderGoogle, voiceID
	}
	return p, n
}

// VieNeuVoices lists the self-hosted voices served by the VieNeu-TTS-v3-Turbo
// pod. Read from the running container via Vieneu().list_preset_voices() --
// see .superpowers/sdd/2026-07-31-shared-tts-service/task-9-step5-report.md.
// These 14 names are bundled inside the pinned model revision; regenerate
// this list if that revision ever changes.
func VieNeuVoices() []Voice {
	return []Voice{
		vieNeuVoice("Minh Đức", "MALE", "nam, miền Bắc, tin tức"),
		vieNeuVoice("Phạm Tuyên", "MALE", "nam, miền Bắc, tự nhiên"),
		vieNeuVoice("Thái Sơn", "MALE", "nam, miền Nam, đọc truyện"),
		vieNeuVoice("Xuân Vĩnh", "MALE", "nam, miền Nam, tự nhiên"),
		vieNeuVoice("Thanh Bình", "MALE", "nam, miền Bắc, đọc truyện"),
		vieNeuVoice("Trúc Ly", "FEMALE", "nữ, miền Bắc, tự nhiên"),
		vieNeuVoice("Ngọc Linh", "FEMALE", "nữ, miền Bắc, đọc truyện"),
		vieNeuVoice("Đoan Trang", "FEMALE", "nữ, miền Bắc, tự nhiên"),
		vieNeuVoice("Mai Anh", "FEMALE", "nữ, miền Bắc, tin tức"),
		vieNeuVoice("Thục Đoan", "FEMALE", "nữ, miền Nam, đọc truyện"),
		vieNeuVoice("Minh Triết", "MALE", "nam, miền Nam, tin tức"),
		vieNeuVoice("Thùy Dung", "FEMALE", "nữ, miền Nam, tin tức"),
		vieNeuVoice("Quang Sơn", "MALE", "nam, miền Trung, tự nhiên"),
		vieNeuVoice("Ngọc Trân", "FEMALE", "nữ, miền Trung, tự nhiên"),
	}
}

func vieNeuVoice(name, gender, description string) Voice {
	return Voice{
		ID:            "vieneu:" + name,
		Label:         name + " — " + description,
		Provider:      "vieneu",
		Tier:          "self-hosted",
		Gender:        gender,
		FreeTierChars: 0,
	}
}
