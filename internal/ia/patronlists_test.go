package ia

import (
	"testing"
)

func TestListAPIURL(t *testing.T) {
	tests := []struct {
		name    string
		rawURL  string
		want    string
		wantErr bool
	}{
		{
			name:   "full details URL with slug",
			rawURL: "https://archive.org/details/@johnarleyburns/lists/3/tribal-music",
			want:   "https://archive.org/services/users/@johnarleyburns/lists/3",
		},
		{
			name:   "without slug",
			rawURL: "https://archive.org/details/@johnarleyburns/lists/3",
			want:   "https://archive.org/services/users/@johnarleyburns/lists/3",
		},
		{
			name:   "trailing slash",
			rawURL: "https://archive.org/details/@johnarleyburns/lists/3/tribal-music/",
			want:   "https://archive.org/services/users/@johnarleyburns/lists/3",
		},
		{
			name:   "different list id",
			rawURL: "https://archive.org/details/@otheruser/lists/42/some-playlist",
			want:   "https://archive.org/services/users/@otheruser/lists/42",
		},
		{
			name:    "missing @screenname",
			rawURL:  "https://archive.org/details/johnarleyburns/lists/3/tribal-music",
			wantErr: true,
		},
		{
			name:    "missing lists segment",
			rawURL:  "https://archive.org/details/@johnarleyburns/favorites",
			wantErr: true,
		},
		{
			name:    "non-numeric list id",
			rawURL:  "https://archive.org/details/@johnarleyburns/lists/abc/tribal-music",
			wantErr: true,
		},
		{
			name:    "empty url",
			rawURL:  "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ListAPIURL(tt.rawURL)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got url=%q", got)
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
