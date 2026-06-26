package listenability

import (
	"testing"
)

func TestDurationScore(t *testing.T) {
	tests := []struct {
		name     string
		seconds  float64
		minScore float64
		maxScore float64
	}{
		{"zero", 0, 0.0, 0.0},
		{"negative", -5, 0.0, 0.0},
		{"5s", 5, 0.0, 0.0},
		{"45s", 45, 0.0, 0.0},
		{"59s", 59, 0.0, 0.0},
		{"60s", 60, 0.64, 0.66},
		{"75s", 75, 0.74, 0.80},
		{"90s", 90, 0.99, 1.01},
		{"180s", 180, 0.99, 1.01},
		{"900s", 900, 0.99, 1.01},
		{"1000s", 1000, 0.69, 0.76},
		{"1200s", 1200, 0.59, 0.66},
		{"1500s", 1500, 0.44, 0.46},
		{"1600s", 1600, -0.01, 0.01},
		{"2000s", 2000, -0.01, 0.01},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score, _ := DurationScore(tt.seconds)
			if score < tt.minScore || score > tt.maxScore {
				t.Errorf("DurationScore(%.0f) = %.4f, want [%.4f, %.4f]", tt.seconds, score, tt.minScore, tt.maxScore)
			}
		})
	}
}

func TestDurationScoreBelow60(t *testing.T) {
	for _, sec := range []float64{0, 1, 10, 30, 45, 59.9} {
		score, _ := DurationScore(sec)
		if score != 0.0 {
			t.Errorf("DurationScore(%.1f) = %.4f, want 0.0 (hard exclude under 60s)", sec, score)
		}
	}
}

func TestAlbumShapeScore(t *testing.T) {
	tests := []struct {
		name    string
		ev      AlbumEvidence
		maxVal  float64
	}{
		{
			name: "empty_album",
			ev:   AlbumEvidence{PositiveDurationCnt: 0},
			maxVal: 0.0,
		},
		{
			name: "good_album",
			ev: AlbumEvidence{
				PositiveDurationCnt: 10,
				AvgDurationSec:      180,
				Short60Ratio:        0.1,
			},
			maxVal: 1.0,
		},
		{
			name: "short_album",
			ev: AlbumEvidence{
				PositiveDurationCnt: 20,
				AvgDurationSec:      3,
				Short60Ratio:        0.95,
			},
			maxVal: 0.1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := AlbumShapeScore(tt.ev)
			if score > tt.maxVal {
				t.Errorf("AlbumShapeScore() = %.4f, want <= %.4f", score, tt.maxVal)
			}
		})
	}
}

func TestScoreAlbumHardReject(t *testing.T) {
	ev := AlbumEvidence{
		TrackCount:          10,
		PositiveDurationCnt: 10,
		AvgDurationSec:      3,
		Short60Ratio:        0.95,
	}
	r := ScoreAlbum(ev)
	if r.Score > 0.01 {
		t.Errorf("ScoreAlbum for avg=3s short60=0.95 = %.4f, want near 0", r.Score)
	}
	foundShort60 := false
	foundAvgDur := false
	for _, reason := range r.Reasons {
		if reason == "album_short60_ratio_above_80pct" {
			foundShort60 = true
		}
		if reason == "album_avg_duration_below_15s" {
			foundAvgDur = true
		}
	}
	if !foundShort60 {
		t.Error("expected reason 'album_short60_ratio_above_80pct'")
	}
	if !foundAvgDur {
		t.Error("expected reason 'album_avg_duration_below_15s'")
	}
}

func TestTitlePatternReasons(t *testing.T) {
	tests := []struct {
		title    string
		filename string
		want     string
	}{
		{"Ch 1", "ch1.mp3", "channel_dump_title_pattern"},
		{"Channel 2", "channel2.mp3", "channel_dump_title_pattern"},
		{"test tone 440hz", "tone.mp3", "test_tone_pattern"},
		{"silence between tracks", "silence.mp3", "silence_pattern"},
		{"Normal Song", "song.mp3", ""},
	}

	for _, tt := range tests {
		t.Run(tt.title, func(t *testing.T) {
			reasons := titlePatternReasons(tt.title, tt.filename)
			if tt.want == "" {
				if len(reasons) > 0 {
					t.Errorf("titlePatternReasons(%q, %q) = %v, want empty", tt.title, tt.filename, reasons)
				}
				return
			}
			found := false
			for _, r := range reasons {
				if r == tt.want {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("titlePatternReasons(%q, %q) = %v, want to contain %q", tt.title, tt.filename, reasons, tt.want)
			}
		})
	}
}

func TestIsNonMusicMetadata(t *testing.T) {
	tests := []struct {
		name     string
		title    string
		creator  string
		subjects string
		genres   string
		nonMusic bool
	}{
		{"music_album", "Greatest Hits", "Some Band", "Rock; Music", "Rock", false},
		{"audiobook", "The Great Novel", "Author", "Audiobook; Fiction", "Audiobooks", true},
		{"spoken_word", "Lecture Series", "Professor", "Spoken Word", "Speech", true},
		{"sound_effects", "SFX Pack", "Studio", "Sound Effects", "SFX", true},
		{"test_tones", "Test Tones 20-20kHz", "Lab", "Test Tones; Audio", "Test", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsNonMusicMetadata(tt.title, tt.creator, tt.subjects, tt.genres)
			if got != tt.nonMusic {
				t.Errorf("IsNonMusicMetadata(%q, %q, %q, %q) = %v, want %v",
					tt.title, tt.creator, tt.subjects, tt.genres, got, tt.nonMusic)
			}
		})
	}
}

func TestClassifyTier(t *testing.T) {
	tests := []struct {
		score float64
		tier  string
	}{
		{0.90, "excellent"},
		{0.85, "excellent"},
		{0.75, "good"},
		{0.70, "good"},
		{0.55, "borderline"},
		{0.50, "borderline"},
		{0.30, "poor"},
		{0.25, "poor"},
		{0.10, "unusable"},
		{0.00, "unusable"},
	}
	for _, tt := range tests {
		t.Run(tt.tier, func(t *testing.T) {
			got := classifyTier(tt.score)
			if got != tt.tier {
				t.Errorf("classifyTier(%.2f) = %q, want %q", tt.score, got, tt.tier)
			}
		})
	}
}

func TestScoreTrackHardExclude60s(t *testing.T) {
	ev := TrackEvidence{
		DurationSec: 30,
		Album: AlbumEvidence{
			PositiveDurationCnt: 10,
			AvgDurationSec:      200,
			Short60Ratio:        0.1,
		},
		QualityScore: 0.8,
	}
	r := ScoreTrack(ev)
	if r.Stream != "excluded" {
		t.Errorf("30s track stream = %q, want 'excluded'", r.Stream)
	}
	if r.Decision != "exclude" {
		t.Errorf("30s track decision = %q, want 'exclude'", r.Decision)
	}
}

func TestScoreTrackLongform(t *testing.T) {
	ev := TrackEvidence{
		DurationSec: 1000,
		Album: AlbumEvidence{
			PositiveDurationCnt: 10,
			AvgDurationSec:      200,
			Short60Ratio:        0.1,
		},
		QualityScore: 0.8,
	}
	r := ScoreTrack(ev)
	if r.Stream != "longform_candidate" {
		t.Errorf("1000s track stream = %q, want 'longform_candidate'", r.Stream)
	}
}

func TestScoreTrackGood(t *testing.T) {
	ev := TrackEvidence{
		DurationSec: 200,
		Album: AlbumEvidence{
			PositiveDurationCnt: 10,
			AvgDurationSec:      200,
			Short60Ratio:        0.1,
			Title:               "Greatest Hits",
			Creator:             "Artist",
		},
		QualityScore: 0.8,
		Title:        "Hit Song",
		Filename:     "hit_song.mp3",
		Tags:         "rock, pop, 80s",
		BitrateKbps:  192,
	}
	r := ScoreTrack(ev)
	if r.Score < 0.7 {
		t.Errorf("good track score = %.4f, want >= 0.7", r.Score)
	}
	if r.Decision != "include" {
		t.Errorf("good track decision = %q, want 'include'", r.Decision)
	}
	if r.Stream != "default" {
		t.Errorf("good track stream = %q, want 'default'", r.Stream)
	}
}

func TestScoreTrackNonMusic(t *testing.T) {
	ev := TrackEvidence{
		DurationSec: 200,
		Album: AlbumEvidence{
			PositiveDurationCnt: 10,
			AvgDurationSec:      200,
			Short60Ratio:        0.1,
			Title:               "Audiobook Collection",
			Creator:             "Narrator",
			Subjects:            "Audiobook; Fiction",
		},
		QualityScore: 0.9,
	}
	r := ScoreTrack(ev)
	if r.Stream != "excluded" {
		t.Errorf("non-music track stream = %q, want 'excluded'", r.Stream)
	}
}

func TestScoreTrackZeroDuration(t *testing.T) {
	ev := TrackEvidence{
		DurationSec: 0,
		Album: AlbumEvidence{
			PositiveDurationCnt: 10,
			AvgDurationSec:      200,
			Short60Ratio:        0.1,
		},
	}
	r := ScoreTrack(ev)
	if r.Score != 0.0 {
		t.Errorf("zero duration score = %.4f, want 0.0", r.Score)
	}
	if r.Stream != "excluded" {
		t.Errorf("zero duration stream = %q, want 'excluded'", r.Stream)
	}
}

func TestContentScoreWithPrompt(t *testing.T) {
	ev := TrackEvidence{
		Prompt: PromptEvidence{
			MusicSimilarity:    0.8,
			NegativeSimilarity: 0.2,
			MusicMinusNegative: 0.6,
		},
	}
	score := ContentScore(ev)
	if score < 0.7 {
		t.Errorf("ContentScore with strong music prompt = %.4f, want >= 0.7", score)
	}
}

func TestContentScoreNegativePrompt(t *testing.T) {
	ev := TrackEvidence{
		Prompt: PromptEvidence{
			MusicSimilarity:    0.2,
			NegativeSimilarity: 0.8,
			MusicMinusNegative: -0.6,
		},
	}
	score := ContentScore(ev)
	if score > 0.3 {
		t.Errorf("ContentScore with strong negative prompt = %.4f, want <= 0.3", score)
	}
}

func TestMetadataHygiene(t *testing.T) {
	tests := []struct {
		name     string
		title    string
		filename string
		tags     string
		bitrate  int
		maxScore float64
	}{
		{"perfect", "Song", "song.mp3", "rock, pop", 192, 1.0},
		{"no_title", "", "song.mp3", "rock", 192, 0.9},
		{"no_tags", "Song", "song.mp3", "", 192, 0.99},
		{"low_bitrate", "Song", "song.mp3", "rock", 48, 0.9},
		{"very_low_bitrate", "Song", "song.mp3", "", 16, 0.8},
		{"untitled", "Untitled", "untitled.mp3", "", 128, 0.85},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := MetadataHygieneScore(tt.title, tt.filename, tt.tags, tt.bitrate)
			if score > tt.maxScore {
				t.Errorf("MetadataHygieneScore() = %.4f, want <= %.4f", score, tt.maxScore)
			}
		})
	}
}

func TestIsStoryMetadata(t *testing.T) {
	tests := []struct {
		name    string
		title   string
		isStory bool
	}{
		{"music_album", "Greatest Hits of 1980s", false},
		{"double_story", "Stories and Tales from Around the World", true},
		{"music", "Greatest Hits", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsStoryMetadata(tt.title, "", "", "")
			if got != tt.isStory {
				t.Errorf("IsStoryMetadata(%q) = %v, want %v", tt.title, got, tt.isStory)
			}
		})
	}
}
