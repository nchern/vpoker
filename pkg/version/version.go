package version

import (
	"fmt"
	"time"
)

const DEVELOPMENT = "dev"

var value = DEVELOPMENT

// Version returns current version of the app
func Version() string {
	return value
}

// JSVersion returns a version suitable to be used for JS sources versioning
// in browser for cache busting
func JSVersion() string {
	if value == DEVELOPMENT {
		return fmt.Sprintf("%d", time.Now().UnixMilli())
	}
	return value
}
