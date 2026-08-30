package configrepository

import (
	"strings"

	log "github.com/sirupsen/logrus"
	"streamingestarr/core/data"
	"streamingestarr/webserver/handlers/generated"
)

const (
	datastoreValuesVersion   = 4
	datastoreValueVersionKey = "DATA_STORE_VERSION"
)

func migrateDatastoreValues(datastore *data.Datastore, configRepository ConfigRepository) {
	currentVersion, _ := datastore.GetNumber(datastoreValueVersionKey)
	if currentVersion == 0 {
		currentVersion = datastoreValuesVersion
	}

	for v := currentVersion; v < datastoreValuesVersion; v++ {
		log.Infof("Migration datastore values from %d to %d\n", int(v), int(v+1))
		switch v {
		case 0:
			migrateToDatastoreValues1(datastore)
		case 1:
			migrateToDatastoreValues2(datastore, configRepository)
		case 2:
			migrateToDatastoreValues3ServingEndpoint3(configRepository)
		case 3:
			migrateToDatastoreValues4(datastore, configRepository)
		default:
			log.Fatalln("missing datastore values migration step")
		}
	}
	if err := datastore.SetNumber(datastoreValueVersionKey, datastoreValuesVersion); err != nil {
		log.Errorln("error setting datastore value version:", err)
	}
}

func migrateToDatastoreValues1(datastore *data.Datastore) {
	// Migrate the forbidden usernames to be a slice instead of a string.
	forbiddenUsernamesString, _ := datastore.GetString(blockedUsernamesKey)
	if forbiddenUsernamesString != "" {
		forbiddenUsernamesSlice := strings.Split(forbiddenUsernamesString, ",")
		if err := datastore.SetStringSlice(blockedUsernamesKey, forbiddenUsernamesSlice); err != nil {
			log.Errorln("error migrating blocked username list:", err)
		}
	}

	// Migrate the suggested usernames to be a slice instead of a string.
	suggestedUsernamesString, _ := datastore.GetString(suggestedUsernamesKey)
	if suggestedUsernamesString != "" {
		suggestedUsernamesSlice := strings.Split(suggestedUsernamesString, ",")
		if err := datastore.SetStringSlice(suggestedUsernamesKey, suggestedUsernamesSlice); err != nil {
			log.Errorln("error migrating suggested username list:", err)
		}
	}
}

func migrateToDatastoreValues2(datastore *data.Datastore, configRepository ConfigRepository) {
	oldAdminPassword, _ := datastore.GetString("stream_key")
	// Avoids double hashing the password
	_ = datastore.SetString("admin_password_key", oldAdminPassword)
	comment := "Default stream key"
	_ = configRepository.SetStreamKeys([]generated.StreamKey{
		{Key: &oldAdminPassword, Comment: &comment},
	})
}

func migrateToDatastoreValues3ServingEndpoint3(configRepository ConfigRepository) {
	// S3 storage support was removed; the S3 serving-endpoint migration is no
	// longer applicable and is intentionally a no-op.
}

func migrateToDatastoreValues4(datastore *data.Datastore, configRepository ConfigRepository) {
	unhashed_pass, _ := datastore.GetString("admin_password_key")
	err := configRepository.SetAdminPassword(unhashed_pass)
	if err != nil {
		log.Fatalln("error migrating admin password:", err)
	}
}
