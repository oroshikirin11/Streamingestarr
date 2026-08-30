package models

// Channel is one theater: an ingest target with its own stream state, HLS
// output and (eventually) chat room. The service ships with exactly one,
// but everything downstream is keyed by channel so a second one is a data
// row, not a rework (docs/design.md §8).
type Channel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
