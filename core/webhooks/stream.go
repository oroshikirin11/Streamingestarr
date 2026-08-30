package webhooks

import (
	"time"

	"github.com/teris-io/shortid"
	"streamingestarr/models"
	"streamingestarr/persistence/configrepository"
)

// SendStreamStatusEvent will send all webhook destinations the current
// stream status. Payloads carry the channel the event belongs to.
func SendStreamStatusEvent(eventType models.EventType, channelID string) {
	sendStreamStatusEvent(eventType, channelID, shortid.MustGenerate(), time.Now())
}

func sendStreamStatusEvent(eventType models.EventType, channelID string, id string, timestamp time.Time) {
	configRepository := configrepository.Get()

	SendEventToWebhooks(WebhookEvent{
		Type: eventType,
		EventData: map[string]interface{}{
			"id":          id,
			"channel":     channelID,
			"name":        configRepository.GetServerName(),
			"summary":     configRepository.GetServerSummary(),
			"streamTitle": configRepository.GetStreamTitle(),
			"status":      getStatus(),
			"serverURL":   getServerURL(),
			"timestamp":   timestamp,
		},
	})
}
