// package testx contains convenient shortcuts for testing
package testx

import (
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// AssertReader asserts that a given reader contains expected string
func AssertReader(t *testing.T, expected string, r io.Reader) {
	b, err := io.ReadAll(r)
	require.NoError(t, err)
	assert.Equal(t, expected, string(b))
}
