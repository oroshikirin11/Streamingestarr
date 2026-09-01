package transcoder

import (
	"bufio"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	log "github.com/sirupsen/logrus"
	"github.com/teris-io/shortid"

	"streamingestarr/config"
	"streamingestarr/logging"
	"streamingestarr/models"
	"streamingestarr/persistence/channelrepository"
	"streamingestarr/persistence/configrepository"
	"streamingestarr/utils"
)

var _commandExec *exec.Cmd

type execInfo struct {
	binPath string
	command []string
	environ []string
}

func (e *execInfo) String() string {
	return strings.Join(e.environ, " ") + " " + e.binPath + " " + strings.Join(e.command, " ")
}

// Transcoder is a single instance of a video transcoder.
type Transcoder struct {
	codec Codec

	// channelID scopes the per-broadcast config reads (sniffed inbound
	// codecs) to the room this transcoder serves.
	channelID string

	stdin *io.PipeReader

	TranscoderCompleted  func(error)
	playlistOutputPath   string
	segmentFormat        string
	ffmpegPath           string
	segmentIdentifier    string
	internalListenerPort string
	input                string
	segmentOutputPath    string
	variants             []HLSVariant

	currentStreamOutputSettings []models.StreamOutputVariant
	currentLatencyLevel         models.LatencyLevel
	appendToStream              bool
	isEvent                     bool
}

// HLSVariant is a combination of settings that results in a single HLS stream.
type HLSVariant struct {
	audioBitrate string // The audio bitrate

	videoSize VideoSize // Resizes the video via scaling
	index     int

	framerate    int // The output framerate
	videoBitrate int // The output bitrate

	cpuUsageLevel      int  // The amount of hardware to use for encoding a stream
	isVideoPassthrough bool // Override all settings and just copy the video stream

	isAudioPassthrough bool // Override all settings and just copy the audio stream
}

// VideoSize is the scaled size of the video output.
type VideoSize struct {
	Width  int
	Height int
}

// For limiting the output bitrate
// https://trac.ffmpeg.org/wiki/Limiting%20the%20output%20bitrate
// https://developer.apple.com/documentation/http_live_streaming/about_apple_s_http_live_streaming_tools
// Adjust the max & buffer size until the output bitrate doesn't exceed the ~+10% that Apple's media validator
// complains about.

// getAllocatedVideoBitrate returns the video bitrate we allocate after making some room for audio.
// 192 is pretty average.
func (v *HLSVariant) getAllocatedVideoBitrate() int {
	return int(float64(v.videoBitrate) - 192)
}

// getMaxVideoBitrate returns the maximum video bitrate we allow the encoder to support.
func (v *HLSVariant) getMaxVideoBitrate() int {
	return int(float64(v.getAllocatedVideoBitrate()) * 1.08)
}

// getBufferSize returns how often it checks the bitrate of encoded segments to see if it's too high/low.
func (v *HLSVariant) getBufferSize() int {
	return int(float64(v.getMaxVideoBitrate()))
}

// getString returns a WxH formatted getString for scaling video output.
func (v *VideoSize) getString() string {
	widthString := strconv.Itoa(v.Width)
	heightString := strconv.Itoa(v.Height)

	if widthString != "0" && heightString != "0" {
		return widthString + ":" + heightString
	} else if widthString != "0" {
		return widthString + ":-2"
	} else if heightString != "0" {
		return "-2:" + heightString
	}

	return ""
}

// Stop will stop the transcoder and kill all processing.
func (t *Transcoder) Stop() {
	log.Traceln("Transcoder STOP requested.")
	err := _commandExec.Process.Kill()
	if err != nil {
		log.Errorln(err)
	}
}

// Start will execute the transcoding process with the settings previously set.
func (t *Transcoder) Start(shouldLog bool) {
	_lastTranscoderLogMessage = ""

	flags := t.getFlags()
	if shouldLog {
		log.Infof("Processing video using codec %s with %d output qualities configured.", t.codec.DisplayName(), len(t.variants))
	}
	createVariantDirectories(t.segmentOutputPath)
	command := flags.String()

	if config.EnableDebugFeatures {
		log.Println(command)
	}

	_commandExec = exec.Command(flags.binPath, flags.command...) // nolint: gosec
	_commandExec.Env = flags.environ

	if t.stdin != nil {
		_commandExec.Stdin = t.stdin
	}

	stdout, err := _commandExec.StderrPipe()
	if err != nil {
		log.Fatalln(err)
	}

	if err := _commandExec.Start(); err != nil {
		log.Errorln("Transcoder error. See", logging.GetTranscoderLogFilePath(), "for full output to debug.")
		log.Panicln(err, command)
	}

	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			handleTranscoderMessage(line)
		}
	}()

	err = _commandExec.Wait()
	if t.TranscoderCompleted != nil {
		t.TranscoderCompleted(err)
	}

	if err != nil {
		log.Errorln("transcoding error. look at", logging.GetTranscoderLogFilePath(), "to help debug. your copy of ffmpeg may not support your selected codec of", t.codec.Name())
	}
}

// SetLatencyLevel will set the latency level for the instance of the transcoder.
func (t *Transcoder) SetLatencyLevel(level models.LatencyLevel) {
	t.currentLatencyLevel = level
}

// SetIsEvent will allow you to set a stream as an "event".
func (t *Transcoder) SetIsEvent(isEvent bool) {
	t.isEvent = isEvent
}

func (t *Transcoder) GetString() string {
	ffmpegFlags := t.getFlags()
	return ffmpegFlags.String()
}

func (t *Transcoder) getFlags() *execInfo {
	port := t.internalListenerPort
	localListenerAddress := "http://127.0.0.1:" + port

	hlsOptionFlags := []string{
		"program_date_time",
		"independent_segments",
	}

	if t.appendToStream {
		hlsOptionFlags = append(hlsOptionFlags, "append_list")
	}

	if t.segmentIdentifier == "" {
		t.segmentIdentifier = shortid.MustGenerate()
	}

	hlsEventString := make([]string, 0, 2)
	if t.isEvent {
		hlsEventString = append(hlsEventString, "-hls_playlist_type", "event")
	} else {
		// Don't let the transcoder close the playlist. We do it manually.
		hlsOptionFlags = append(hlsOptionFlags, "omit_endlist")
	}

	hlsOptionsString := make([]string, 0, len(hlsOptionFlags))
	if len(hlsOptionFlags) > 0 {
		hlsOptionsString = append(hlsOptionsString, "-hls_flags", strings.Join(hlsOptionFlags, "+"))
	}

	logPath := logging.GetTranscoderLogFilePath()
	reportEnv := fmt.Sprintf(`FFREPORT=file=%s:level=32`, logPath)
	if runtime.GOOS == "windows" {
		logPath = strings.ReplaceAll(logPath, "\\", "/")
		reportEnv = fmt.Sprintf(`FFREPORT=file=%s:level=32`, logPath)
	}
	environ := []string{
		reportEnv,
	}
	ffmpegFlags := make([]string, 0, 64)
	ffmpegFlags = append(ffmpegFlags, "-hide_banner", "-loglevel", "warning")
	ffmpegFlags = append(ffmpegFlags, t.codec.GlobalFlags()...)
	ffmpegFlags = append(ffmpegFlags, []string{
		"-fflags", "+genpts", // Generate presentation time stamp if missing
		"-flags", "+cgop", // Force closed GOPs
		"-i", t.input,
	}...)

	ffmpegFlags = append(ffmpegFlags, t.getVariantsString()...)
	ffmpegFlags = append(ffmpegFlags, []string{
		// HLS Output
		"-f", "hls",

		"-hls_time", strconv.Itoa(t.currentLatencyLevel.SecondsPerSegment), // Length of each segment
		"-hls_list_size", strconv.Itoa(t.currentLatencyLevel.SegmentCount), // Max # in variant playlist
	}...)
	ffmpegFlags = append(ffmpegFlags, hlsOptionsString...)
	ffmpegFlags = append(ffmpegFlags, hlsEventString...)
	segmentExtension := ".ts"
	if t.segmentFormat == "fmp4" {
		// fMP4/CMAF segments — the container that carries AV1 and HEVC
		// in HLS, which mpegts segments cannot.
		segmentExtension = ".m4s"
		ffmpegFlags = append(ffmpegFlags, []string{
			"-hls_segment_type", "fmp4",
			"-hls_fmp4_init_filename", "init-" + t.segmentIdentifier + ".mp4",
		}...)
		// mpegts sources (SRT ingest, the offline clip) carry AAC as ADTS,
		// which cannot be copied into fMP4 without this filter. Verified
		// harmless for ASC input (RTMP/FLV) and re-encodes — but it refuses
		// to INITIALIZE on any other audio codec and kills the session, so
		// it only rides when the audio is AAC. The SRT ingest sniffs the
		// codec from the stream head before this spawn; unknown ("") means
		// RTMP or a failed sniff, both of which are AAC territory.
		if audio := config.GetInboundAudioCodec(t.channelID); audio == "" || audio == "aac" {
			ffmpegFlags = append(ffmpegFlags, "-bsf:a", "aac_adtstoasc")
		}
	} else {
		ffmpegFlags = append(ffmpegFlags, []string{
			"-segment_format_options", "mpegts_flags=mpegts_copyts=1",
		}...)
	}
	ffmpegFlags = append(ffmpegFlags, t.codec.ExtraArguments()...)

	ffmpegFlags = append(ffmpegFlags, []string{
		// Video settings
		"-pix_fmt", t.codec.PixelFormat(),
		"-sc_threshold", "0", // Disable scene change detection for creating segments

		// Filenames
		"-master_pl_name", "stream.m3u8",

		"-hls_segment_filename", localListenerAddress + "/%v/stream-" + t.segmentIdentifier + "-%d" + segmentExtension, // Send HLS segments back to us over HTTP
		"-max_muxing_queue_size", "400", // Workaround for Too many packets error: https://trac.ffmpeg.org/ticket/6375?cversion=0

		"-method", "PUT", // HLS results sent back to us will be over PUTs

		localListenerAddress + "/%v/stream.m3u8", // Send HLS playlists back to us over HTTP
	}...)

	return &execInfo{
		binPath: t.ffmpegPath,
		command: ffmpegFlags,
		environ: environ,
	}
}

func getVariantFromConfigQuality(quality models.StreamOutputVariant, index int, channelID string) HLSVariant {
	variant := HLSVariant{}
	variant.index = index
	variant.isAudioPassthrough = quality.IsAudioPassthrough
	variant.isVideoPassthrough = quality.IsVideoPassthrough

	// Never transcode HDR. Every encoder here targets 8-bit yuv420p, which
	// would clip PQ/HLG to washed-out SDR with no tone-mapping. Force
	// passthrough so the HDR bitstream is copied through intact.
	if !variant.isVideoPassthrough && config.GetInboundVideoRange(channelID) != config.VideoRangeSDR {
		log.Warnf("inbound stream is HDR (%s); forcing video passthrough for variant %d to preserve it", config.GetInboundVideoRange(channelID), index)
		variant.isVideoPassthrough = true
	}

	// If no audio bitrate is specified then we pass through original audio
	if quality.AudioBitrate == 0 {
		variant.isAudioPassthrough = true
	}

	if quality.VideoBitrate == 0 {
		quality.VideoBitrate = 1200
	}

	// If the video is being passed through then
	// don't continue to set options on the variant.
	if variant.isVideoPassthrough {
		return variant
	}

	// Set a default, reasonable preset if one is not provided.
	// "superfast" and "ultrafast" are generally not recommended since they look bad.
	// https://trac.ffmpeg.org/wiki/Encode/H.264
	variant.cpuUsageLevel = quality.CPUUsageLevel

	variant.SetVideoBitrate(quality.VideoBitrate)
	variant.SetAudioBitrate(strconv.Itoa(quality.AudioBitrate) + "k")
	variant.SetVideoScalingWidth(quality.ScaledWidth)
	variant.SetVideoScalingHeight(quality.ScaledHeight)
	variant.SetVideoFramerate(quality.GetFramerate())

	return variant
}

// NewTranscoder will return a new Transcoder, populated by the config.
// NewTranscoder builds a transcoder for one channel's broadcast; channelID
// scopes the sniffed-codec lookups that pick the segment container and
// audio bitstream filter.
func NewTranscoder(channelID string) *Transcoder {
	configRepository := configrepository.Get()
	ffmpegPath := utils.ValidatedFfmpegPath(configRepository.GetFfMpegPath())

	transcoder := new(Transcoder)
	transcoder.channelID = channelID
	transcoder.ffmpegPath = ffmpegPath
	transcoder.internalListenerPort = config.InternalHLSListenerPort

	transcoder.currentStreamOutputSettings = channelrepository.GetEffectiveOutputVariants(channelID)
	transcoder.currentLatencyLevel = channelrepository.GetEffectiveLatencyLevel(channelID)
	transcoder.codec = getCodec(configRepository.GetVideoCodec())
	transcoder.segmentOutputPath = config.HLSStoragePath
	transcoder.playlistOutputPath = config.HLSStoragePath
	transcoder.segmentFormat = resolveSegmentFormat(channelrepository.GetEffectiveSegmentFormat(channelID), channelID)

	transcoder.input = "pipe:0" // stdin

	for index, quality := range transcoder.currentStreamOutputSettings {
		variant := getVariantFromConfigQuality(quality, index, channelID)
		transcoder.AddVariant(variant)
	}

	return transcoder
}

// Uses `map` https://www.ffmpeg.org/ffmpeg-all.html#Stream-specifiers-1 https://www.ffmpeg.org/ffmpeg-all.html#Advanced-options
func (v *HLSVariant) getVariantString(t *Transcoder) []string {
	variantEncoderCommands := v.getVideoQualityString(t)
	variantEncoderCommands = append(variantEncoderCommands, v.getAudioQualityString()...)

	if (v.videoSize.Width != 0 || v.videoSize.Height != 0) && !v.isVideoPassthrough {
		// Order here matters, you must scale before changing hardware formats
		filters := []string{
			v.getScalingString(t.codec.Scaler()),
		}
		if t.codec.ExtraFilters() != "" {
			filters = append(filters, t.codec.ExtraFilters())
		}
		scalingAlgorithm := "bilinear"
		variantEncoderCommands = append(variantEncoderCommands, []string{
			"-sws_flags", scalingAlgorithm,
			"-filter:v:" + strconv.Itoa(v.index), strings.Join(filters, ","),
		}...)
	} else if t.codec.ExtraFilters() != "" && !v.isVideoPassthrough {
		variantEncoderCommands = append(variantEncoderCommands, []string{
			"-filter:v:" + strconv.Itoa(v.index), t.codec.ExtraFilters(),
		}...)
	}

	preset := t.codec.GetPresetForLevel(v.cpuUsageLevel)
	if preset != "" {
		variantEncoderCommands = append(variantEncoderCommands, []string{
			"-preset", preset,
		}...)
	}

	return variantEncoderCommands
}

// Get the command flags for the variants.
func (t *Transcoder) getVariantsString() []string {
	variantsCommandFlags := make([]string, 0, len(t.variants)*20)
	streamMap := make([]string, 0, len(t.variants))

	for _, variant := range t.variants {
		variantsCommandFlags = append(variantsCommandFlags, variant.getVariantString(t)...)
		streamMap = append(streamMap, fmt.Sprintf("v:%d,a:%d", variant.index, variant.index))
	}
	variantsCommandFlags = append(variantsCommandFlags, "-var_stream_map", strings.Join(streamMap, " "))
	return variantsCommandFlags
}

// Video Scaling
// https://trac.ffmpeg.org/wiki/Scaling
// If we'd like to keep the aspect ratio, we need to specify only one component, either width or height.
// Some codecs require the size of width and height to be a multiple of n. You can achieve this by setting the width or height to -n.

// SetVideoScalingWidth will set the scaled video width of this variant.
func (v *HLSVariant) SetVideoScalingWidth(width int) {
	v.videoSize.Width = width
}

// SetVideoScalingHeight will set the scaled video height of this variant.
func (v *HLSVariant) SetVideoScalingHeight(height int) {
	v.videoSize.Height = height
}

func (v *HLSVariant) getScalingString(scaler string) string {
	if scaler == "" {
		scaler = "scale"
	}
	return fmt.Sprintf("%s=%s", scaler, v.videoSize.getString())
}

// Video Quality

// SetVideoBitrate will set the output bitrate of this variant's video.
func (v *HLSVariant) SetVideoBitrate(bitrate int) {
	v.videoBitrate = bitrate
}

func (v *HLSVariant) getVideoQualityString(t *Transcoder) []string {
	if v.isVideoPassthrough {
		return []string{
			"-map", "v:0", fmt.Sprintf("-c:v:%d", v.index), "copy",
		}
	}

	gop := v.framerate * t.currentLatencyLevel.SecondsPerSegment // force an i-frame every segment
	cmd := make([]string, 0, 20)
	cmd = append(cmd,
		"-map", "v:0",
		fmt.Sprintf("-c:v:%d", v.index), t.codec.Name(), // Video codec used for this variant
		fmt.Sprintf("-b:v:%d", v.index), fmt.Sprintf("%dk", v.getAllocatedVideoBitrate()), // The average bitrate for this variant allowing space for audio
		fmt.Sprintf("-maxrate:v:%d", v.index), fmt.Sprintf("%dk", v.getMaxVideoBitrate()), // The max bitrate allowed for this variant
		fmt.Sprintf("-g:v:%d", v.index), fmt.Sprintf("%d", gop), // Suggested interval where i-frames are encoded into the segments
		fmt.Sprintf("-keyint_min:v:%d", v.index), fmt.Sprintf("%d", gop), // minimum i-keyframe interval
		fmt.Sprintf("-r:v:%d", v.index), fmt.Sprintf("%d", v.framerate),
	)
	cmd = append(cmd, t.codec.VariantFlags(v)...)

	return cmd
}

// SetVideoFramerate will set the output framerate of this variant's video.
func (v *HLSVariant) SetVideoFramerate(framerate int) {
	v.framerate = framerate
}

// SetCPUUsageLevel will set the hardware usage of this variant.
func (v *HLSVariant) SetCPUUsageLevel(level int) {
	v.cpuUsageLevel = level
}

// Audio Quality

// SetAudioBitrate will set the output framerate of this variant's audio.
func (v *HLSVariant) SetAudioBitrate(bitrate string) {
	v.audioBitrate = bitrate
}

func (v *HLSVariant) getAudioQualityString() []string {
	if v.isAudioPassthrough {
		return []string{
			"-map", "a:0?", fmt.Sprintf("-c:a:%d", v.index), "copy",
		}
	}

	// libfdk_aac is not a part of every ffmpeg install, so use "aac" instead
	encoderCodec := "aac"
	return []string{
		"-map", "a:0?",
		fmt.Sprintf("-c:a:%d", v.index), encoderCodec,
		fmt.Sprintf("-b:a:%d", v.index), v.audioBitrate,
	}
}

// AddVariant adds a new HLS variant to include in the output.
func (t *Transcoder) AddVariant(variant HLSVariant) {
	variant.index = len(t.variants)
	t.variants = append(t.variants, variant)
}

// SetInput sets the input stream on the filesystem.
func (t *Transcoder) SetInput(input string) {
	t.input = input
}

// SetStdin sets the Stdin of the ffmpeg command.
func (t *Transcoder) SetStdin(pipe *io.PipeReader) {
	t.stdin = pipe
}

// SetOutputPath sets the root directory that should include playlists and video segments.
func (t *Transcoder) SetOutputPath(output string) {
	t.segmentOutputPath = output
	t.playlistOutputPath = output
}

// SetIdentifier enables appending a unique identifier to segment file name.
func (t *Transcoder) SetIdentifier(output string) {
	t.segmentIdentifier = output
}

// SetInternalHTTPPort will set the port to be used for internal communication.
func (t *Transcoder) SetInternalHTTPPort(port string) {
	t.internalListenerPort = port
}

// resolveSegmentFormat turns the stored choice into a concrete container.
// The explicit values win; "auto" decides per broadcast from the codec the
// SRT ingest sniffed before this spawn. Only AV1 forces fMP4 — mpegts
// simply cannot carry it, and segmenting it into ts silently destroys the
// video track while audio plays on. HEVC and H.264 keep the proven ts
// path, and unknown (RTMP, or a failed sniff) is H.264 territory.
func resolveSegmentFormat(stored, channelID string) string {
	if stored == "ts" || stored == "fmp4" {
		return stored
	}
	if config.GetInboundVideoCodec(channelID) == "av1" {
		log.Infoln("segment container: auto chose fMP4 — the inbound stream is AV1, which mpegts cannot carry")
		return "fmp4"
	}
	return "ts"
}

// SetSegmentFormat overrides the HLS segment container ("ts" or "fmp4").
func (t *Transcoder) SetSegmentFormat(format string) {
	t.segmentFormat = format
}

// SetCodec will set the codec to be used for the transocder.
func (t *Transcoder) SetCodec(codecName string) {
	t.codec = getCodec(codecName)
}
