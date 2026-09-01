package transcoder

import (
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	log "github.com/sirupsen/logrus"
	"streamingestarr/persistence/channelrepository"
	"streamingestarr/utils"
)

var (
	_lastTranscoderLogMessage = ""
	l                         = &sync.RWMutex{}
)

var errorMap = map[string]string{
	"Unrecognized option 'vaapi_device'":        "you are likely trying to utilize a vaapi codec, but your version of ffmpeg or your hardware doesn't support it. change your codec to libx264 and restart your stream",
	"unable to open display":                    "your copy of ffmpeg is likely installed via snap packages. please uninstall and re-install via a non-snap method.",
	"Failed to open file 'http://127.0.0.1":     "error transcoding. make sure your version of ffmpeg is compatible with your selected codec or is recent enough",
	"can't configure encoder":                   "error with codec. if your copy of ffmpeg or your hardware does not support your selected codec you may need to select another",
	"Unable to parse option value":              "you are likely trying to utilize a specific codec, but your version of ffmpeg or your hardware doesn't support it. either fix your ffmpeg install or try changing your codec to libx264 and restart your stream",
	"OpenEncodeSessionEx failed: out of memory": "your NVIDIA gpu is limiting the number of concurrent stream qualities you can support. remove a stream output variant and try again.",
	"Cannot use rename on non file protocol, this may lead to races and temporary partial files": "",
	"No VA display found for device": "vaapi not enabled. either your copy of ffmpeg does not support it, your hardware does not support it, or you need to install additional drivers for your hardware.",
	"Could not find a valid device":  "your codec is either not supported or not configured properly",
	"H.264 bitstream error":          "transcoding content error playback issues may arise. you may want to use the default codec if you are not already.",
	"intel_enc_hw_context_init: Assertion 'encoder_context->mfc_context' failed": "if you are using Intel graphics you may be missing the i965-va-driver-shader drivers",
	`Unknown encoder 'h264_qsv'`:       "your copy of ffmpeg does not have support for Intel QuickSync encoding (h264_qsv). change the selected codec in your video settings",
	`Unknown encoder 'h264_vaapi'`:     "your copy of ffmpeg does not have support for VA-API encoding (h264_vaapi). change the selected codec in your video settings",
	`Unknown encoder 'h264_nvenc'`:     "your copy of ffmpeg does not have support for NVIDIA hardware encoding (h264_nvenc). change the selected codec in your video settings",
	`Unknown encoder 'h264_x264'`:      "your copy of ffmpeg does not have support for the default x264 codec (h264_x264). download a version of ffmpeg that supports this.",
	`Unrecognized option 'x264-params`: "your copy of ffmpeg does not have support for the default libx264 codec (h264_x264). download a version of ffmpeg that supports this.",
	`Failed to set value '/dev/dri/renderD128' for option 'vaapi_device': Invalid argument`: "failed to set va-api device to /dev/dri/renderD128. your system is likely not properly configured for va-api",
	`Stream map 'v:0' matches no streams`:                                                   "the stream provided looks to have no video included, it may be audio-only. streamingestarr requires a video stream.",

	// Generic error for a codec
	"Unrecognized option": "error with codec. if your copy of ffmpeg or your hardware does not support your selected codec you may need to select another",
}

var ignoredErrors = []string{
	"Duplicated segment filename detected",
	"Error while opening encoder for output stream",
	"Unable to parse option value",
	"Last message repeated",
	"Option not found",
	"use of closed network connection",
	"URL read error: End of file",
	"upload playlist failed, will retry with a new http session",
	"VBV underflow",
	"Cannot use rename on non file protocol",
	"Device creation failed",
	"Error parsing global options",
	"maybe the hls segment duration will not precise",
	"Non-monotonous DTS in output",
	// mp4 muxer advisory on passthrough of quirky input timing (the
	// sender's ~1fps still sections stamp nominal-fps durations); the
	// muxer clamps and moves on, but at one line per second per room it
	// drowned the warnings view.
	"Packet duration: ",
	// Input timestamp trouble floods one line PER PACKET during a bad
	// sender splice (seen: a pause/resume that re-based the timeline by
	// hours — thousands of lines in seconds). The segment ledger's seam
	// detection reports the same fact once, with a verdict.
	"timestamp discontinuity",
	"out of order",
	"frames duplicated",
	"To ignore this",
	"Driver does not support some wanted packed headers (wanted 0xd, found 0x1)",
	"Failed to allocate a vaapi/nv12 frame from a fixed pool of hardware frames.",
}

func handleTranscoderMessage(message string) {
	log.Debugln(message)

	l.Lock()
	defer l.Unlock()

	// Ignore certain messages that we don't care about.
	for _, error := range ignoredErrors {
		if strings.Contains(message, error) {
			return
		}
	}

	// Convert specific transcoding messages to human-readable messages.
	for error, displayMessage := range errorMap {
		if strings.Contains(message, error) {
			message = displayMessage
			break
		}
	}

	if message == "" {
		return
	}

	// No good comes from a flood of repeated messages.
	if message == _lastTranscoderLogMessage {
		return
	}

	log.Error(message)

	_lastTranscoderLogMessage = message
}

func createVariantDirectories(baseDir string) {
	// Create private hls data dirs. The final path element of baseDir IS
	// the channel ID (data/hls/<channel>), so the room's own ladder decides
	// how many variant dirs exist.
	utils.CleanupDirectory(baseDir)
	variants := channelrepository.GetEffectiveOutputVariants(filepath.Base(baseDir))
	if len(variants) != 0 {
		for index := range variants {
			if err := os.MkdirAll(path.Join(baseDir, strconv.Itoa(index)), 0o750); err != nil {
				log.Fatalln(err)
			}
		}
	} else {
		dir := path.Join(baseDir, strconv.Itoa(0))
		log.Traceln("Creating", dir)
		if err := os.MkdirAll(dir, 0o750); err != nil {
			log.Fatalln(err)
		}
	}
}
