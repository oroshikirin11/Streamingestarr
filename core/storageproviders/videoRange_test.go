package storageproviders

import (
	"strings"
	"testing"

	"streamingestarr/config"
)

func TestInjectVideoRange(t *testing.T) {
	in := "#EXTM3U\n" +
		"#EXT-X-STREAM-INF:BANDWIDTH=6000000\n0/stream.m3u8\n" +
		"#EXT-X-STREAM-INF:BANDWIDTH=3000000\n1/stream.m3u8\n"
	out := injectVideoRange(in, "PQ")

	if strings.Count(out, "VIDEO-RANGE=PQ") != 2 {
		t.Fatalf("expected VIDEO-RANGE=PQ on both variants, got:\n%s", out)
	}
	// Idempotent: a second pass must not double-append.
	if again := injectVideoRange(out, "PQ"); again != out {
		t.Fatalf("injectVideoRange not idempotent:\n%s", again)
	}
	// Media lines and the header are untouched.
	if !strings.Contains(out, "\n0/stream.m3u8\n") || strings.Contains(out, "stream.m3u8,VIDEO-RANGE") {
		t.Fatalf("media/URI lines were altered:\n%s", out)
	}
}

func TestNormalizeVideoRange(t *testing.T) {
	cases := map[string]string{
		"pq":           config.VideoRangePQ,
		"HDR10":        config.VideoRangePQ,
		"smpte2084":    config.VideoRangePQ,
		"hlg":          config.VideoRangeHLG,
		"arib-std-b67": config.VideoRangeHLG,
		"":             config.VideoRangeSDR,
		"sdr":          config.VideoRangeSDR,
		"nonsense":     config.VideoRangeSDR,
	}
	for in, want := range cases {
		if got := config.NormalizeVideoRange(in); got != want {
			t.Errorf("NormalizeVideoRange(%q) = %q, want %q", in, got, want)
		}
	}
}
