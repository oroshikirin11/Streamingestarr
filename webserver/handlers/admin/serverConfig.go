package admin

import (
	"encoding/json"
	"net/http"

	log "github.com/sirupsen/logrus"
	"streamingestarr/config"
	"streamingestarr/core/transcoder"
	"streamingestarr/models"
	"streamingestarr/persistence/configrepository"
	"streamingestarr/utils"
	"streamingestarr/webserver/handlers/generated"
	"streamingestarr/webserver/router/middleware"
)

// GetServerConfig gets the config details of the server.
func GetServerConfig(w http.ResponseWriter, r *http.Request) {
	configRepository := configrepository.Get()
	ffmpeg := utils.ValidatedFfmpegPath(configRepository.GetFfMpegPath())
	usernameBlocklist := configRepository.GetForbiddenUsernameList()
	usernameSuggestions := configRepository.GetSuggestedUsernamesList()

	streamOutputVariants := configRepository.GetStreamOutputVariants()
	videoQualityVariants := make([]models.StreamOutputVariant, 0, len(streamOutputVariants))
	for _, variant := range streamOutputVariants {
		videoQualityVariants = append(videoQualityVariants, models.StreamOutputVariant{
			Name:               variant.GetName(),
			IsAudioPassthrough: variant.GetIsAudioPassthrough(),
			IsVideoPassthrough: variant.IsVideoPassthrough,
			Framerate:          variant.GetFramerate(),
			VideoBitrate:       variant.VideoBitrate,
			AudioBitrate:       variant.AudioBitrate,
			CPUUsageLevel:      variant.CPUUsageLevel,
			ScaledWidth:        variant.ScaledWidth,
			ScaledHeight:       variant.ScaledHeight,
		})
	}
	response := serverConfigAdminResponse{
		InstanceDetails: webConfigResponse{
			Name:                configRepository.GetServerName(),
			Summary:             configRepository.GetServerSummary(),
			Tags:                configRepository.GetServerMetadataTags(),
			ExtraPageContent:    configRepository.GetExtraPageBodyContent(),
			StreamTitle:         configRepository.GetStreamTitle(),
			WelcomeMessage:      configRepository.GetServerWelcomeMessage(),
			OfflineMessage:      configRepository.GetCustomOfflineMessage(),
			Logo:                configRepository.GetLogoPath(),
			SocialHandles:       configRepository.GetSocialHandles(),
			NSFW:                configRepository.GetNSFW(),
			CustomStyles:        configRepository.GetCustomStyles(),
			CustomJavascript:    configRepository.GetCustomJavascript(),
			AppearanceVariables: configRepository.GetCustomColorVariableValues(),
		},
		FFmpegPath:                ffmpeg,
		AdminPassword:             configRepository.GetAdminPassword(),
		StreamKeys:                configRepository.GetStreamKeys(),
		StreamKeyOverridden:       config.TemporaryStreamKey != "",
		WebServerPort:             config.WebServerPort,
		WebServerIP:               config.WebServerIP,
		RTMPServerPort:            configRepository.GetRTMPPortNumber(),
		ChatDisabled:              configRepository.GetChatDisabled(),
		ChatJoinMessagesEnabled:   configRepository.GetChatJoinPartMessagesEnabled(),
		SocketHostOverride:        configRepository.GetWebsocketOverrideHost(),
		VideoServingEndpoint:      configRepository.GetVideoServingEndpoint(),
		ChatEstablishedUserMode:   configRepository.GetChatEstbalishedUsersOnlyMode(),
		ChatSpamProtectionEnabled: configRepository.GetChatSpamProtectionEnabled(),
		ChatSlurFilterEnabled:     configRepository.GetChatSlurFilterEnabled(),
		ChatRequireAuthentication: configRepository.GetChatRequireAuthentication(),
		HideViewerCount:           configRepository.GetHideViewerCount(),
		ChatNameReservationDays:   configRepository.GetChatNameReservationDays(),
		DisableSearchIndexing:     configRepository.GetDisableSearchIndexing(),
		VideoSettings: videoSettings{
			VideoQualityVariants: videoQualityVariants,
			LatencyLevel:         configRepository.GetStreamLatencyLevel().Level,
		},
		ExternalActions:    configRepository.GetExternalActions(),
		SupportedCodecs:    transcoder.GetCodecs(ffmpeg),
		VideoCodec:         configRepository.GetVideoCodec(),
		VideoSegmentFormat: configRepository.GetVideoSegmentFormat(),
		ForbiddenUsernames: usernameBlocklist,
		SuggestedUsernames: usernameSuggestions,
	}

	w.Header().Set("Content-Type", "application/json")
	middleware.DisableCache(w)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Errorln(err)
	}
}

type serverConfigAdminResponse struct {
	InstanceDetails           webConfigResponse       `json:"instanceDetails"`
	FFmpegPath                string                  `json:"ffmpegPath"`
	AdminPassword             string                  `json:"adminPassword"`
	SocketHostOverride        string                  `json:"socketHostOverride,omitempty"`
	WebServerIP               string                  `json:"webServerIP"`
	VideoCodec                string                  `json:"videoCodec"`
	VideoSegmentFormat        string                  `json:"videoSegmentFormat"`
	VideoServingEndpoint      string                  `json:"videoServingEndpoint"`
	SupportedCodecs           []string                `json:"supportedCodecs"`
	ExternalActions           []models.ExternalAction `json:"externalActions"`
	ForbiddenUsernames        []string                `json:"forbiddenUsernames"`
	SuggestedUsernames        []string                `json:"suggestedUsernames"`
	StreamKeys                []generated.StreamKey   `json:"streamKeys"`
	VideoSettings             videoSettings           `json:"videoSettings"`
	RTMPServerPort            int                     `json:"rtmpServerPort"`
	WebServerPort             int                     `json:"webServerPort"`
	ChatNameReservationDays   int                     `json:"chatNameReservationDays"`
	ChatDisabled              bool                    `json:"chatDisabled"`
	ChatJoinMessagesEnabled   bool                    `json:"chatJoinMessagesEnabled"`
	ChatEstablishedUserMode   bool                    `json:"chatEstablishedUserMode"`
	ChatSpamProtectionEnabled bool                    `json:"chatSpamProtectionEnabled"`
	ChatSlurFilterEnabled     bool                    `json:"chatSlurFilterEnabled"`
	ChatRequireAuthentication bool                    `json:"chatRequireAuthentication"`
	DisableSearchIndexing     bool                    `json:"disableSearchIndexing"`
	StreamKeyOverridden       bool                    `json:"streamKeyOverridden"`
	HideViewerCount           bool                    `json:"hideViewerCount"`
}

type videoSettings struct {
	VideoQualityVariants []models.StreamOutputVariant `json:"videoQualityVariants"`
	LatencyLevel         int                          `json:"latencyLevel"`
}

type webConfigResponse struct {
	AppearanceVariables map[string]string     `json:"appearanceVariables"`
	Version             string                `json:"version"`
	WelcomeMessage      string                `json:"welcomeMessage"`
	OfflineMessage      string                `json:"offlineMessage"`
	Logo                string                `json:"logo"`
	Name                string                `json:"name"`
	ExtraPageContent    string                `json:"extraPageContent"`
	StreamTitle         string                `json:"streamTitle"` // What's going on with the current stream
	CustomStyles        string                `json:"customStyles"`
	CustomJavascript    string                `json:"customJavascript"`
	Summary             string                `json:"summary"`
	Tags                []string              `json:"tags"`
	SocialHandles       []models.SocialHandle `json:"socialHandles"`
	NSFW                bool                  `json:"nsfw"`
}
