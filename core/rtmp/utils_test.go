package rtmp

import "testing"

// The contract: only the FINAL path segment is the key; the application
// path before it is whatever the encoder chose ("/live", something else,
// or nothing). Keys containing slashes are therefore not supported.
func Test_secretMatch(t *testing.T) {
	tests := []struct {
		name      string
		streamKey string
		path      string
		want      bool
	}{
		{"positive", "abc", "/live/abc", true},
		{"negative", "abc", "/live/def", false},
		{"positive with numbers", "abc123", "/live/abc123", true},
		{"negative with numbers", "abc123", "/live/def456", false},
		{"any app path", "abc", "/whatever/abc", true},
		{"no app path", "abc", "/abc", true},
		{"deep app path", "three", "/live/one/two/three", true},
		{"key with slashes is unsupported", "one/two/three", "/live/one/two/three", false},
		{"bad path", "anything", "nonsense", false},
		{"missing secret", "abc", "/live/", false},
		{"app path alone is not a key match", "abc", "/live", false},
		{"streamkey before app path", "streamkey", "/streamkey/live", false},
		{"trailing slash ignored", "abc", "/live/abc/", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := secretMatch(tt.streamKey, tt.path); got != tt.want {
				t.Errorf("secretMatch() = %v, want %v", got, tt.want)
			}
		})
	}
}
