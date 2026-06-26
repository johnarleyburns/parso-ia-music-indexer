package listenability

import (
	"math"
	"strings"
)

const Version = "listenability-v2"

const (
	MinTrackSeconds       = 60
	TargetTrackSeconds    = 90
	PreferredMaxSeconds   = 1800
	LongformMaxSeconds    = 2700
	IncludeThreshold      = 0.50
	HardExcludeThreshold  = 0.25
)

const (
	weightDuration         = 0.30
	weightAlbumShape       = 0.25
	weightContentType      = 0.25
	weightTechnicalQuality = 0.15
	weightMetadataHygiene  = 0.05
)

type TrackEvidence struct {
	TrackID      int
	AlbumID      string
	Title        string
	Filename     string
	DurationSec  float64
	BitrateKbps  int
	QualityScore float64
	Tags         string
	Album        AlbumEvidence
	Prompt       PromptEvidence
}

type AlbumEvidence struct {
	AlbumID             string
	Title               string
	Creator             string
	Subjects            string
	Genres              string
	TrackCount          int
	PositiveDurationCnt int
	AvgDurationSec      float64
	MedianDurationSec   float64
	TotalDurationSec    float64
	Short30Ratio        float64
	Short60Ratio        float64
	Short90Ratio        float64
	HasChannelDump      bool
}

type PromptEvidence struct {
	MusicSimilarity       float64
	NegativeSimilarity    float64
	MusicMinusNegative    float64
	StrongestNegativeName string
}

type Result struct {
	Score      float64
	Tier       string
	Decision   string
	Stream     string
	Reasons    []string
	Components map[string]float64
	Version    string

	PositiveDurationCnt int
}

var PositivePrompts = []string{
	"a complete music track",
	"a full song with melody and rhythm",
	"an instrumental music performance",
	"a recorded musical performance",
}

var NegativePrompts = []string{
	"a short sound effect",
	"spoken word audiobook narration",
	"language lesson speech sound",
	"applause and crowd noise",
	"silence or test tone",
	"isolated drum hit sound",
	"multitrack channel stem",
}

func ScoreTrack(e TrackEvidence) Result {
	r := Result{
		Version:    Version,
		Components: make(map[string]float64),
	}

	durScore, _ := DurationScore(e.DurationSec)
	r.Components["duration"] = durScore

	albumShapeScore := AlbumShapeScore(e.Album)
	r.Components["album_shape"] = albumShapeScore

	contentTypeScore := ContentScore(e)
	r.Components["content_type"] = contentTypeScore

	technicalQualityScore := math.Min(1.0, e.QualityScore)
	r.Components["technical_quality"] = technicalQualityScore

	metadataScore := MetadataHygieneScore(e.Title, e.Filename, e.Tags, e.BitrateKbps)
	r.Components["metadata_hygiene"] = metadataScore

	r.Score = r.Components["duration"]*weightDuration +
		r.Components["album_shape"]*weightAlbumShape +
		r.Components["content_type"]*weightContentType +
		r.Components["technical_quality"]*weightTechnicalQuality +
		r.Components["metadata_hygiene"]*weightMetadataHygiene

	r.Score = math.Round(r.Score*10000) / 10000

	if e.DurationSec <= 0 {
		r.Score = 0.0
	}

	r.Reasons = collectTrackReasons(e, r)
	r.Tier = classifyTier(r.Score)
	r.Stream, r.Decision = classifyStreamDecision(e, r)

	return r
}

func ScoreAlbum(e AlbumEvidence) Result {
	r := Result{
		Version:    Version,
		Components: make(map[string]float64),
	}

	durComp := AlbumAvgDurationScore(e.AvgDurationSec)
	r.Components["avg_duration"] = durComp

	shortPenalty := e.Short60Ratio
	r.Components["short60_ratio"] = shortPenalty

	var shapeScore float64
	if e.PositiveDurationCnt > 0 {
		shapeScore = durComp * (1.0 - shortPenalty)
	}
	r.Components["album_shape"] = shapeScore

	r.Score = shapeScore

	isNonMusic := IsNonMusicMetadata(e.Title, e.Creator, e.Subjects, e.Genres)
	r.Components["non_music_meta"] = boolToFloat(isNonMusic)

	if e.TrackCount >= 5 && e.AvgDurationSec < 15 && e.Short60Ratio >= 0.80 {
		r.Reasons = append(r.Reasons, "album_avg_duration_below_15s")
		r.Reasons = append(r.Reasons, "album_short60_ratio_above_80pct")
		if e.HasChannelDump {
			r.Score = math.Max(r.Score, 0.25)
		} else {
			r.Score = 0.0
		}
	}
	if e.AvgDurationSec < 30 {
		r.Reasons = append(r.Reasons, "album_avg_duration_below_30s")
		if r.Score > 0.2 {
			r.Score = math.Min(r.Score, 0.2)
		}
	}
	if e.Short60Ratio >= 0.80 {
		r.Reasons = append(r.Reasons, "album_short60_ratio_above_80pct")
		if r.Score > 0.3 {
			r.Score = math.Min(r.Score, 0.3)
		}
	}

	r.Score = math.Round(r.Score*10000) / 10000
	r.Tier = classifyTier(r.Score)
	r.Stream, r.Decision = classifyAlbumStreamDecision(e, r)
	r.PositiveDurationCnt = e.PositiveDurationCnt

	return r
}

func DurationScore(seconds float64) (score float64, confidence float64) {
	if seconds <= 0 {
		return 0.0, 0.0
	}
	if seconds < MinTrackSeconds {
		return 0.0, 0.0
	}
	if seconds < TargetTrackSeconds {
		t := (seconds - MinTrackSeconds) / (TargetTrackSeconds - MinTrackSeconds)
		return 0.65 + t*0.25, 0.7
	}
	if seconds <= PreferredMaxSeconds {
		return 1.0, 1.0
	}
	if seconds <= LongformMaxSeconds {
		t := (seconds - PreferredMaxSeconds) / (LongformMaxSeconds - PreferredMaxSeconds)
		return 0.75 - t*0.75, 0.5
	}
	return 0.0, 0.0
}

func AlbumShapeScore(e AlbumEvidence) float64 {
	if e.PositiveDurationCnt == 0 {
		return 0.0
	}
	durComp := AlbumAvgDurationScore(e.AvgDurationSec)
	shortPenalty := e.Short60Ratio
	return math.Max(0.0, durComp*(1.0-shortPenalty))
}

func AlbumAvgDurationScore(avgSeconds float64) float64 {
	if avgSeconds <= 0 {
		return 0.0
	}
	if avgSeconds < 30 {
		return 0.1
	}
	if avgSeconds < 60 {
		t := (avgSeconds - 30) / 30
		return 0.1 + t*0.2
	}
	if avgSeconds <= TargetTrackSeconds {
		t := (avgSeconds - 60) / (TargetTrackSeconds - 60)
		return 0.3 + t*0.4
	}
	if avgSeconds <= PreferredMaxSeconds {
		return 0.7 + (avgSeconds-TargetTrackSeconds)/(PreferredMaxSeconds-TargetTrackSeconds)*0.3
	}
	return 1.0
}

func ContentScore(e TrackEvidence) float64 {
	if e.Prompt.MusicSimilarity == 0 && e.Prompt.NegativeSimilarity == 0 {
		return metaContentScore(e.Album, e.Title, e.Filename)
	}
	margin := e.Prompt.MusicMinusNegative
	if margin > 0 {
		return 0.5 + math.Min(0.5, margin*1.5)
	}
	return 0.5 - math.Min(0.5, -margin*1.5)
}

func metaContentScore(album AlbumEvidence, title, filename string) float64 {
	score := 0.5
	if IsNonMusicMetadata(album.Title, album.Creator, album.Subjects, album.Genres) {
		score -= 0.3
	}
	if IsSemiNonMusicMetadata(album.Title, album.Creator, album.Subjects, album.Genres) {
		score -= 0.15
	}
	patterns := titlePatternReasons(title, filename)
	if len(patterns) > 0 {
		score -= 0.2 * float64(len(patterns))
	}
	return math.Max(0.0, score)
}

func MetadataHygieneScore(title, filename, tags string, bitrate int) float64 {
	score := 1.0
	if title == "" {
		score -= 0.15
	}
	if filename == "" {
		score -= 0.1
	}
	if tags == "" {
		score -= 0.05
	}
	if bitrate > 0 && bitrate < 64 {
		score -= 0.1
	}
	if bitrate > 0 && bitrate < 32 {
		score -= 0.15
	}
	if strings.Contains(strings.ToLower(title), "untitled") {
		score -= 0.1
	}
	return math.Max(0.0, score)
}

func collectTrackReasons(e TrackEvidence, r Result) []string {
	var reasons []string
	dur := e.DurationSec

	if dur <= 0 {
		reasons = append(reasons, "duration_zero_or_missing")
	} else if dur < MinTrackSeconds {
		reasons = append(reasons, "duration_below_60s")
	} else if dur < TargetTrackSeconds {
		reasons = append(reasons, "duration_below_90s")
	} else if dur > LongformMaxSeconds {
		reasons = append(reasons, "duration_above_45m")
	} else if dur > PreferredMaxSeconds {
		reasons = append(reasons, "longform_candidate")
	}

	if dur <= 0 {
		reasons = append(reasons, "no_audio_url")
	} else {
		reasons = append(reasons, "has_audio_url")
	}

	titlePatterns := titlePatternReasons(e.Title, e.Filename)
	reasons = append(reasons, titlePatterns...)

	if e.Album.Short60Ratio >= 0.80 {
		reasons = append(reasons, "album_short60_ratio_above_80pct")
	}
	if e.Album.PositiveDurationCnt > 0 && e.Album.AvgDurationSec < 30 {
		reasons = append(reasons, "album_avg_duration_below_30s")
	}

	if e.Prompt.MusicMinusNegative < 0 {
		reasons = append(reasons, "negative_prompt_dominant")
		if e.Prompt.StrongestNegativeName != "" {
			reasons = append(reasons, "strongest_negative_"+e.Prompt.StrongestNegativeName)
		}
	}

	if IsNonMusicMetadata(e.Album.Title, e.Album.Creator, e.Album.Subjects, e.Album.Genres) {
		reasons = append(reasons, "non_music_metadata")
	}

	if IsSemiNonMusicMetadata(e.Album.Title, e.Album.Creator, e.Album.Subjects, e.Album.Genres) {
		reasons = append(reasons, "semi_non_music_metadata")
	}

	return reasons
}

func titlePatternReasons(title, filename string) []string {
	lower := strings.ToLower(title + " " + filename)
	var reasons []string

	channelPatterns := []string{"ch 1", "ch 2", "ch 3", "ch 4", "ch 5",
		"channel 1", "channel 2", "track 1", "track 2"}
	for _, p := range channelPatterns {
		if strings.Contains(lower, p) {
			reasons = append(reasons, "channel_dump_title_pattern")
			break
		}
	}

	if strings.Contains(lower, "test tone") || strings.Contains(lower, "testtone") {
		reasons = append(reasons, "test_tone_pattern")
	}
	if strings.Contains(lower, "sine") && strings.Contains(lower, "hz") {
		reasons = append(reasons, "sine_wave_pattern")
	}
	if strings.Contains(lower, "silence") {
		reasons = append(reasons, "silence_pattern")
	}
	if strings.Contains(lower, "noise") && !strings.Contains(lower, "noisy") {
		reasons = append(reasons, "noise_pattern")
	}

	return reasons
}

func IsNonMusicMetadata(title, creator, subjects, genres string) bool {
	combined := strings.ToLower(title + " " + creator + " " + subjects + " " + genres)
	nonMusicTerms := []string{
		"audiobook", "spoken word", "language instruction",
		"sound effects", "sound effect", "sfx",
		"test tones", "test tone", "silence", "noise",
	}
	for _, term := range nonMusicTerms {
		if strings.Contains(combined, term) {
			return true
		}
	}
	return false
}

func IsSemiNonMusicMetadata(title, creator, subjects, genres string) bool {
	combined := strings.ToLower(title + " " + creator + " " + subjects + " " + genres)
	semiNonMusicTerms := []string{
		"field recording", "environmental sound",
		"applause", "crowd noise",
	}
	for _, term := range semiNonMusicTerms {
		if strings.Contains(combined, term) {
			return true
		}
	}
	return false
}

func IsStoryMetadata(title, creator, subjects, genres string) bool {
	combined := strings.ToLower(title + " " + creator + " " + subjects + " " + genres)
	storyTerms := []string{"story", "stories", "tale", "tales", "fable", "fables"}
	count := 0
	for _, term := range storyTerms {
		if strings.Contains(combined, term) {
			count++
		}
	}
	return count >= 2
}

func classifyTier(score float64) string {
	switch {
	case score >= 0.85:
		return "excellent"
	case score >= 0.70:
		return "good"
	case score >= 0.50:
		return "borderline"
	case score >= 0.25:
		return "poor"
	default:
		return "unusable"
	}
}

func classifyStreamDecision(e TrackEvidence, r Result) (stream, decision string) {
	if e.DurationSec <= 0 {
		return "excluded", "exclude"
	}
	if e.DurationSec < MinTrackSeconds {
		return "excluded", "exclude"
	}
	if e.DurationSec > LongformMaxSeconds {
		return "excluded", "exclude"
	}
	if e.DurationSec > PreferredMaxSeconds {
		if r.Score >= 0.25 {
			return "longform_candidate", "demote"
		}
		return "excluded", "exclude"
	}

	nonMusic := IsNonMusicMetadata(e.Album.Title, e.Album.Creator, e.Album.Subjects, e.Album.Genres)
	story := IsStoryMetadata(e.Album.Title, e.Album.Creator, e.Album.Subjects, e.Album.Genres)
	if nonMusic {
		return "excluded", "exclude"
	}
	if story {
		return "excluded", "exclude"
	}

	if r.Score >= IncludeThreshold {
		return "default", "include"
	}
	if r.Score >= HardExcludeThreshold {
		return "default", "demote"
	}
	return "excluded", "exclude"
}

func classifyAlbumStreamDecision(e AlbumEvidence, r Result) (stream, decision string) {
	nonMusic := IsNonMusicMetadata(e.Title, e.Creator, e.Subjects, e.Genres)
	if nonMusic {
		return "excluded", "exclude"
	}
	if e.TrackCount >= 5 && e.AvgDurationSec < 15 && e.Short60Ratio >= 0.80 {
		if e.HasChannelDump {
			return "longform_candidate", "demote"
		}
		return "excluded", "exclude"
	}
	if r.Score >= IncludeThreshold {
		return "default", "include"
	}
	if r.Score >= HardExcludeThreshold {
		return "default", "demote"
	}
	return "excluded", "exclude"
}

func boolToFloat(b bool) float64 {
	if b {
		return 1.0
	}
	return 0.0
}
