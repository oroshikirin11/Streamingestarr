package webhooks

import (
	"time"

	"github.com/teris-io/shortid"
	"streamingestarr/models"
	"streamingestarr/persistence/configrepository"
)

// SendStreamStatusEvent will send all webhook destinations the current stream status.
func SendStreamStatusEvent(eventType models.EventType) {
	sendStreamStatusEvent(eventType, shortid.MustGenerate(), time.Now())
}

func sendStreamStatusEvent(eventType models.EventType, id string, timestamp time.Time) {
	configRepository := configrepository.Get()

	SendEventToWebhooks(WebhookEvent{
		Type: eventType,
		EventData: map[string]interface{}{
			"id":          id,
			"name":        configRepository.GetServerName(),
			"summary":     configRepository.GetServerSummary(),
			"streamTitle": configRepository.GetStreamTitle(),
			"status":      getStatus(),
			"serverURL":   getServerURL(),
			"timestamp":   timestamp,
		},
	})
}
