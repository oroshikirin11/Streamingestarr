package config

import (
	"time"

	"streamingestarr/models"
	"streamingestarr/webserver/handlers/generated"
)

// Defaults will hold default configuration values.
type Defaults struct {
	PageBodyContent string

	Summary              string
	ServerWelcomeMessage string
	Logo                 string
	YPServer             string

	Title string

	DatabaseFilePath string

	WebServerIP   string
	Name          string
	AdminPassword string
	StreamKeys    []generated.StreamKey

	StreamVariants []models.StreamOutputVariant

	Tags               []string
	RTMPServerPort     int
	SegmentsInPlaylist int

	SegmentLengthSeconds int
	WebServerPort        int

	ChatEstablishedUserModeTimeDuration time.Duration

	YPEnabled bool
}

// GetDefaults will return default configuration values.
func GetDefaults() Defaults {
	defaultStreamKey := "abc123"
	defaultStreamKeyComment := "Default stream key"
	return Defaults{
		Name:                 "New Streamingestarr Theater",
		Summary:              "A private cinema powered by Streamingestarr.",
		ServerWelcomeMessage: "",
		Logo:                 "logo.svg",
		AdminPassword:        "abc123",
		StreamKeys: []generated.StreamKey{
			{Key: &defaultStreamKey, Comment: &defaultStreamKeyComment},
		},
		Tags: []string{
			"streamingestarr",
			"streaming",
		},

		PageBodyContent: `
# Welcome to Streamingestarr!

If you're the owner of this server, visit the admin panel to customize this page.
	`,

		DatabaseFilePath: "data/streamingestarr.db",

		YPEnabled: false,
		YPServer:  "https://owncast.directory",

		WebServerPort:  8080,
		WebServerIP:    "0.0.0.0",
		RTMPServerPort: 1935,

		ChatEstablishedUserModeTimeDuration: time.Minute * 15,

		StreamVariants: []models.StreamOutputVariant{
			{
				// Passthrough by default (design.md §2): our sender controls
				// its own encode, and re-encoding is what the inherited
				// 1.2 Mbps default did to an 11.7 Mbps stream — grain.
				Name:               "passthrough",
				IsVideoPassthrough: true,
				IsAudioPassthrough: true,
				CPUUsageLevel:      2,
			},
		},
	}
}
