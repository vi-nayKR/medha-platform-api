package service

import "os"

// Passwordless developer access is opt-in and limited to explicitly configured
// fixture identities. This guard also applies to direct service callers.
func developmentLoginEnabled() bool {
	env := os.Getenv("ENVIRONMENT")
	return (env == "development" || env == "dev") && os.Getenv("ENABLE_DEV_LOGIN") == "true"
}

func developmentPhoneAllowed(phone string) bool {
	return developmentLoginEnabled() && phone != "" &&
		(phone == os.Getenv("DEV_PANDIT_PHONE") || phone == os.Getenv("DEV_YAJMAN_PHONE"))
}
