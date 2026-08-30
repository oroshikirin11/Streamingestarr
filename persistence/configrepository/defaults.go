package configrepository

import (
	"streamingestarr/config"
	"streamingestarr/models"
)

// PopulateDefaults will set default values in the database.
func (r *SqlConfigRepository) PopulateDefaults() {
	key := "HAS_POPULATED_DEFAULTS"

	r.datastore.WarmCache()

	defaults := config.GetDefaults()

	_ = r.SetAdminPassword(defaults.AdminPassword)
	_ = r.SetStreamKeys(defaults.StreamKeys)
	_ = r.SetHTTPPortNumber(float64(defaults.WebServerPort))
	_ = r.SetRTMPPortNumber(float64(defaults.RTMPServerPort))
	_ = r.SetLogoPath(defaults.Logo)
	_ = r.SetServerMetadataTags([]string{"streamingestarr", "streaming"})
	_ = r.SetServerSummary(defaults.Summary)
	_ = r.SetServerWelcomeMessage("")
	_ = r.SetServerName(defaults.Name)
	_ = r.SetExtraPageBodyContent(defaults.PageBodyContent)
	_ = r.SetSocialHandles([]models.SocialHandle{
		{
			Platform: "github",
			URL:      "https://streamingestarr",
		},
	})

	_ = r.datastore.SetBool(key, true)
}

// HasPopulatedDefaults will determine if the defaults have been inserted into the database.
func (r *SqlConfigRepository) HasPopulatedDefaults() bool {
	hasPopulated, err := r.datastore.GetBool("HAS_POPULATED_DEFAULTS")
	if err != nil {
		return false
	}
	return hasPopulated
}
