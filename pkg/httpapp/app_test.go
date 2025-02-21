package httpapp

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nchern/vpoker/pkg/poker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func assertReader(t *testing.T, expected string, r io.Reader) {
	b, err := io.ReadAll(r)
	require.NoError(t, err)
	assert.Equal(t, expected, string(b))
}

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

			req := httptest.NewRequest(tt.method, tt.url, nil)
			rec := httptest.NewRecorder()

			router := BindRoutes(underTest)
			router.ServeHTTP(rec, req)

			res := rec.Result()
			defer res.Body.Close()

			assert.Equal(t, http.StatusUnauthorized, res.StatusCode)
			assertReader(t, "Unauthorized", res.Body)
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

			req := httptest.NewRequest(tt.method, tt.url, nil)
			rec := httptest.NewRecorder()

			router := BindRoutes(underTest)
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

func TestTableHandlersShouldReturnErrorIfPlayerIsNotAtTheTable(t *testing.T) {
	var tests = []struct {
		url    string
		method string

		givenBody string
	}{
		{"/games/%s/state", "GET", ""},
		{"/games/%s/update", "POST", "{}"},
		{"/games/%s/update_many", "POST", "{}"},
		{"/games/%s/show_card", "POST", "{\"id\": 123}"},
		{"/games/%s/take_card", "POST", "{\"id\": 123}"},
		{"/games/%s/give_card", "POST", "{\"id\": 123}"},
		{"/games/%s/listen", "GET", ""},
		{"/games/%s/shuffle", "GET", ""},
	}
	for _, tt := range tests {
		t.Run(tt.method+"_"+tt.url, func(t *testing.T) {
			underTest := New()

			now := time.Now()
			u := poker.NewUser(uuid.New(), "tester", now)
			sess := newSession(now, u)
			underTest.users.Set(u.ID, u)

			tbl := poker.NewTable(uuid.New(), 1)
			underTest.tables.Set(tbl.ID, tbl)

			path := fmt.Sprintf(tt.url, tbl.ID)
			var reqBody io.Reader = nil
			if tt.givenBody != "" {
				reqBody = bytes.NewBuffer([]byte(tt.givenBody))
			}
			req := httptest.NewRequest(tt.method, path, reqBody)
			req.AddCookie(sess.toCookie(now))

			rec := httptest.NewRecorder()
			router := BindRoutes(underTest)
			router.ServeHTTP(rec, req)

			res := rec.Result()
			defer res.Body.Close()

			assert.Equal(t, http.StatusForbidden, res.StatusCode)
			assertReader(t, "you are not at the table", res.Body)
		})
	}
}
