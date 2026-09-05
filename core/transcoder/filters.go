package transcoder

import (
	"os/exec"
	"strings"
	"sync"

	"streamingestarr/utils"
)

var (
	filtersOnce sync.Once
	filtersList string
)

// HasFilter reports whether this ffmpeg build carries a filter — asked
// once, cached. A filter graph that names a missing filter kills the
// whole broadcast at spawn, so the graph is chosen by what exists.
func HasFilter(name string) bool {
	filtersOnce.Do(func() {
		out, err := exec.Command(utils.ValidatedFfmpegPath(""), "-hide_banner", "-filters").Output() // nolint: gosec
		if err == nil {
			filtersList = string(out)
		}
	})
	for _, line := range strings.Split(filtersList, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == name {
			return true
		}
	}
	return false
}
