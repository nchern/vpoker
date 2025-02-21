package httpapp

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TODO_TECHDEBT: test new user handler {"/users/new", "GET"}

func TestAuthProtectedHandlersShouldReturnUnauthorizedOnNoAuth(t *testing.T) {
	var tests = []struct {
		url    string
		method string
	}{
		{"/games/00000000-0000-0000-0000-000000000000/state", "GET"},
		{"/games/00000000-0000-0000-0000-000000000000/join", "GET"},
		{"/games/00000000-0000-0000-0000-000000000000/update", "POST"},
		{"/games/00000000-0000-0000-0000-000000000000/update_many", "POST"},
		{"/games/00000000-0000-0000-0000-000000000000/show_card", "POST"},
		{"/games/00000000-0000-0000-0000-000000000000/take_card", "POST"},
		{"/games/00000000-0000-0000-0000-000000000000/give_card", "POST"},
		{"/games/00000000-0000-0000-0000-000000000000/listen", "GET"},
		{"/games/00000000-0000-0000-0000-000000000000/shuffle", "GET"},
		{"/games/00000000-0000-0000-0000-000000000000/kick", "POST"},
		{"/users/profile", "POST"},
	}
	for _, tt := range tests {
		t.Run(tt.method+"_"+tt.url, func(t *testing.T) {
			underTest := New()
			router := BindRoutes(underTest)

			req := httptest.NewRequest(tt.method, tt.url, nil)
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			res := rec.Result()
			defer res.Body.Close()

			assert.Equal(t, http.StatusUnauthorized, res.StatusCode)
			b, err := io.ReadAll(res.Body)
			require.NoError(t, err)
			require.Equal(t, "Unauthorized", string(b))
		})
	}
}

func TestHandlersShouldRedirectOnNoAuth(t *testing.T) {
	var tests = []struct {
		url    string
		method string
	}{
		{"/users/profile", "GET"},
		{"/games/00000000-0000-0000-0000-000000000000", "GET"},
		{"/games/new", "GET"},
	}
	for _, tt := range tests {
		t.Run(tt.method+"_"+tt.url, func(t *testing.T) {
			underTest := New()
			router := BindRoutes(underTest)

			req := httptest.NewRequest(tt.method, tt.url, nil)
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			res := rec.Result()
			defer res.Body.Close()

			assert.Equal(t, http.StatusFound, res.StatusCode)
			loc, err := res.Location()
			require.NoError(t, err)
			assert.Equal(t, "/users/new", loc.Path)
		})
	}
}
