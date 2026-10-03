package chunk_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
	"golang.org/x/text/unicode/norm"

	"github.com/the-algovn/tts-service/internal/chunk"
)

func words(s string) []string { return strings.Fields(s) }

func TestShortTextIsOneChunk(t *testing.T) {
	require.Equal(t, []string{"Xin chao cac ban."}, chunk.Split("Xin chao cac ban.", 250, 400))
}

func TestMergesShortSentencesUpToTarget(t *testing.T) {
	in := "Chao buoi toi. Day la AlgoVN Radio! Ban co khoe khong? Minh la Duong Duong."
	got := chunk.Split(in, 40, 400)
	for _, c := range got {
		require.LessOrEqual(t, utf8.RuneCountInString(c), 60)
	}
	require.Equal(t, words(in), words(strings.Join(got, " ")))
	require.Greater(t, len(got), 1)
}

func TestSplitsAtNewlinesAndEllipsis(t *testing.T) {
	in := "Mot bai hat cu...\nMot dem mua."
	got := chunk.Split(in, 10, 400)
	require.Equal(t, []string{"Mot bai hat cu...", "Mot dem mua."}, got)
}

func TestRunOnSentenceIsCappedAtCommaOrSpace(t *testing.T) {
	in := strings.Repeat("loi bai hat nay that dep, ", 40)
	got := chunk.Split(in, 250, 400)
	for _, c := range got {
		require.LessOrEqual(t, utf8.RuneCountInString(c), 400)
		require.NotEqual(t, ' ', rune(c[len(c)-1]))
	}
	require.Equal(t, words(in), words(strings.Join(got, " ")))
}

func TestSingleHugeWordIsHardCut(t *testing.T) {
	in := strings.Repeat("a", 900)
	got := chunk.Split(in, 250, 400)
	require.Equal(t, in, strings.Join(got, ""))
	for _, c := range got {
		require.LessOrEqual(t, utf8.RuneCountInString(c), 400)
	}
}

func TestCombiningDiacriticsCountAsRunesAndSurvive(t *testing.T) {
	in := norm.NFD.String(strings.Repeat("Ti\u1ebfng Vi\u1ec7t th\u1eadt \u0111\u1eb9p, r\u1ea5t hay. ", 20))
	got := chunk.Split(in, 250, 400)
	require.Equal(t, words(in), words(strings.Join(got, " ")))
	for _, c := range got {
		require.LessOrEqual(t, utf8.RuneCountInString(c), 400)
	}
}

func TestEmptyAndWhitespace(t *testing.T) {
	require.Empty(t, chunk.Split("   \n ", 250, 400))
}
