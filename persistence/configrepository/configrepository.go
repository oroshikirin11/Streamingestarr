package configrepository

import (
	"time"

	"streamingestarr/models"
	"streamingestarr/utils"
	"streamingestarr/webserver/handlers/generated"
)

type ConfigRepository interface {
	GetExtraPageBodyContent() string
	SetExtraPageBodyContent(content string) error
	GetStreamTitle() string
	SetStreamTitle(title string) error
	GetAdminPassword() string
	GetViewerUsername() string
	GetChatNameReservationDays() int
	SetChatNameReservationDays(days int) error
	SetViewerUsername(username string) error
	GetViewerPasswordHash() string
	SetViewerPasswordHash(hash string) error
	SetAdminPassword(key string) error
	GetLogoPath() string
	SetLogoPath(logo string) error
	SetLogoUniquenessString(uniqueness string) error
	GetLogoUniquenessString() string
	GetServerSummary() string
	SetServerSummary(summary string) error
	GetServerWelcomeMessage() string
	SetServerWelcomeMessage(welcomeMessage string) error
	GetServerName() string
	SetServerName(name string) error
	GetServerURL() string
	SetServerURL(url string) error
	GetHTTPPortNumber() int
	SetWebsocketOverrideHost(host string) error
	GetWebsocketOverrideHost() string
	SetHTTPPortNumber(port float64) error
	GetHTTPListenAddress() string
	SetHTTPListenAddress(address string) error
	GetRTMPPortNumber() int
	SetRTMPPortNumber(port float64) error
	GetServerMetadataTags() []string
	SetServerMetadataTags(tags []string) error
	GetSocialHandles() []models.SocialHandle
	SetSocialHandles(socialHandles []models.SocialHandle) error
	GetPeakSessionViewerCount() int
	SetPeakSessionViewerCount(count int) error
	GetPeakOverallViewerCount() int
	SetPeakOverallViewerCount(count int) error
	GetLastDisconnectTime() (*utils.NullTime, error)
	SetLastDisconnectTime(disconnectTime time.Time) error
	SetNSFW(isNSFW bool) error
	GetNSFW() bool
	SetFfmpegPath(path string) error
	GetFfMpegPath() string
	GetStreamLatencyLevel() models.LatencyLevel
	SetStreamLatencyLevel(level float64) error
	GetStreamOutputVariants() []models.StreamOutputVariant
	SetStreamOutputVariants(variants []models.StreamOutputVariant) error
	SetChatDisabled(disabled bool) error
	GetChatDisabled() bool
	SetChatEstablishedUsersOnlyMode(enabled bool) error
	GetChatEstbalishedUsersOnlyMode() bool
	SetChatSpamProtectionEnabled(enabled bool) error
	GetChatSpamProtectionEnabled() bool
	SetChatSlurFilterEnabled(enabled bool) error
	GetChatSlurFilterEnabled() bool
	SetChatRequireAuthentication(enabled bool) error
	GetChatRequireAuthentication() bool
	GetExternalActions() []models.ExternalAction
	SetExternalActions(actions []models.ExternalAction) error
	SetCustomStyles(styles string) error
	GetCustomStyles() string
	SetCustomJavascript(styles string) error
	GetCustomJavascript() string
	SetVideoCodec(codec string) error
	GetVideoCodec() string
	VerifySettings() error
	FindHighestVideoQualityIndex(qualities []models.StreamOutputVariant) (int, bool)
	GetForbiddenUsernameList() []string
	SetForbiddenUsernameList(usernames []string) error
	GetSuggestedUsernamesList() []string
	SetSuggestedUsernamesList(usernames []string) error
	GetServerInitTime() (*utils.NullTime, error)
	SetServerInitTime(t time.Time) error
	SetChatJoinMessagesEnabled(enabled bool) error
	GetChatJoinPartMessagesEnabled() bool
	SetNotificationsEnabled(enabled bool) error
	GetNotificationsEnabled() bool
	GetHideViewerCount() bool
	SetHideViewerCount(hide bool) error
	GetCustomOfflineMessage() string
	SetCustomOfflineMessage(message string) error
	SetCustomColorVariableValues(variables map[string]string) error
	GetCustomColorVariableValues() map[string]string
	GetStreamKeys() []generated.StreamKey
	SetStreamKeys(actions []generated.StreamKey) error
	SetDisableSearchIndexing(disableSearchIndexing bool) error
	GetDisableSearchIndexing() bool
	GetVideoServingEndpoint() string
	SetVideoServingEndpoint(message string) error
	GetPublicKey() string
	GetPrivateKey() string
	SetPublicKey(key string) error
	SetPrivateKey(key string) error
	GetFaviconPath() string
	SetFaviconPath(favicon string) error
}
