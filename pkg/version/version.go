package version

import (
	"fmt"
	"time"
)

const (
	development = "dev"
	test        = "test"
)

var value = development

func SetTest() {
	value = test
}

// Version returns current version of the app
func Version() string {
	return value
}

// JSVersion returns a version suitable to be used for JS sources versioning
// in browser for cache busting
func JSVersion() string {
	if value == development {
		return fmt.Sprintf("%d", time.Now().UnixMilli())
	}
	if value == test {
		return value
	}
	return value
}
