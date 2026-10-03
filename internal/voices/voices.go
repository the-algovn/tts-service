package voices

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// ErrNotFound reports a voice id with no metadata record.
var ErrNotFound = errors.New("voice not found")

// InvalidError reports caller input that can never succeed as sent.
type InvalidError struct{ Reason string }

func (e *InvalidError) Error() string { return "invalid voice: " + e.Reason }

// Voice is one registered voice. ID is the bare registry id ("v_<12hex>").
type Voice struct {
	ID          string    `json:"id"`
	Label       string    `json:"label"`
	Gender      string    `json:"gender"`
	RefText     string    `json:"ref_text"`
	Source      string    `json:"source"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// NewVoice is the caller-supplied part of a Voice.
type NewVoice struct {
	Label, Gender, RefText, Source, Description string
}

// ValidateNew checks the fields of a voice about to be created. It returns an
// *InvalidError naming the first bad field, or nil.
func ValidateNew(in NewVoice) error {
	switch {
	case strings.TrimSpace(in.Label) == "":
		return &InvalidError{"label is required"}
	case utf8.RuneCountInString(in.Label) > 80:
		return &InvalidError{"label exceeds 80 characters"}
	case in.Gender != "MALE" && in.Gender != "FEMALE" && in.Gender != "NEUTRAL":
		return &InvalidError{"gender must be MALE, FEMALE or NEUTRAL"}
	case strings.TrimSpace(in.RefText) == "":
		return &InvalidError{"ref_text is required"}
	case utf8.RuneCountInString(in.RefText) > 500:
		return &InvalidError{"ref_text exceeds 500 characters"}
	}
	return nil
}

// Registry stores voices as voices/<id>/ref.wav plus voices/<id>/voice.json.
// voice.json is written last and is what makes a voice exist, so a crash
// between the two writes leaves an orphan clip, never a half voice.
type Registry struct {
	s   Store
	now func() time.Time
}

// NewRegistry returns a Registry over s; now stamps CreatedAt.
func NewRegistry(s Store, now func() time.Time) *Registry {
	return &Registry{s: s, now: now}
}

func refKey(id string) string  { return "voices/" + id + "/ref.wav" }
func metaKey(id string) string { return "voices/" + id + "/voice.json" }

func newID() (string, error) {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "v_" + hex.EncodeToString(b), nil
}

// Create validates in, stores refWAV and the metadata, and returns the new
// voice. Errors: *InvalidError for bad input, otherwise a store error.
func (r *Registry) Create(ctx context.Context, in NewVoice, refWAV []byte) (Voice, error) {
	if err := ValidateNew(in); err != nil {
		return Voice{}, err
	}
	id, err := newID()
	if err != nil {
		return Voice{}, err
	}
	v := Voice{ID: id, Label: strings.TrimSpace(in.Label), Gender: in.Gender,
		RefText: strings.TrimSpace(in.RefText), Source: in.Source,
		Description: in.Description, CreatedAt: r.now().UTC()}
	meta, err := json.Marshal(v)
	if err != nil {
		return Voice{}, err
	}
	if err := r.s.Put(ctx, refKey(id), refWAV); err != nil {
		return Voice{}, fmt.Errorf("store ref: %w", err)
	}
	if err := r.s.Put(ctx, metaKey(id), meta); err != nil {
		return Voice{}, fmt.Errorf("store meta: %w", err)
	}
	return v, nil
}

// Get returns the voice and its reference WAV. ErrNotFound when the id has no
// metadata record.
func (r *Registry) Get(ctx context.Context, id string) (Voice, []byte, error) {
	v, err := r.meta(ctx, id)
	if err != nil {
		return Voice{}, nil, err
	}
	wav, ok, err := r.s.Get(ctx, refKey(id))
	if err != nil {
		return Voice{}, nil, err
	}
	if !ok {
		return Voice{}, nil, ErrNotFound
	}
	return v, wav, nil
}

func (r *Registry) meta(ctx context.Context, id string) (Voice, error) {
	raw, ok, err := r.s.Get(ctx, metaKey(id))
	if err != nil {
		return Voice{}, err
	}
	if !ok {
		return Voice{}, ErrNotFound
	}
	var v Voice
	if err := json.Unmarshal(raw, &v); err != nil {
		return Voice{}, fmt.Errorf("decode %s: %w", id, err)
	}
	return v, nil
}

// List returns every voice with a metadata record, oldest first.
func (r *Registry) List(ctx context.Context) ([]Voice, error) {
	keys, err := r.s.List(ctx, "voices/")
	if err != nil {
		return nil, err
	}
	var out []Voice
	for _, k := range keys {
		if !strings.HasSuffix(k, "/voice.json") {
			continue
		}
		id := strings.TrimSuffix(strings.TrimPrefix(k, "voices/"), "/voice.json")
		v, err := r.meta(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

// Delete removes the voice. Metadata goes first so the voice stops existing
// even if removing the clip fails. ErrNotFound when absent.
func (r *Registry) Delete(ctx context.Context, id string) error {
	if _, err := r.meta(ctx, id); err != nil {
		return err
	}
	if err := r.s.Delete(ctx, metaKey(id)); err != nil {
		return err
	}
	return r.s.Delete(ctx, refKey(id))
}
