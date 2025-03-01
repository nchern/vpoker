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
	"github.com/nchern/vpoker/pkg/testx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testContext struct {
	server  *Server
	request *http.Request

	tableUnderTest *poker.Table
	userUnderTest  *poker.User
	session        *session
}

func newTestContext(method string, url string, body io.Reader) *testContext {
	return &testContext{
		server:  New(),
		request: httptest.NewRequest(method, url, body),
	}
}

func (tc *testContext) withTable(ids ...uuid.UUID) *testContext {
	id := uuid.New()
	if len(ids) > 0 {
		id = ids[0]
	}
	tc.tableUnderTest = poker.NewTable(id, 1)
	tc.server.tables.Set(tc.tableUnderTest.ID, tc.tableUnderTest)
	return tc
}

func (tc *testContext) withUser(now time.Time) *testContext {
	tc.userUnderTest = poker.NewUser(uuid.New(), "tester", now)
	tc.server.users.Set(tc.userUnderTest.ID, tc.userUnderTest)
	return tc
}

func (tc *testContext) withSession(now time.Time) *testContext {
	tc.session = newSession(now, tc.userUnderTest)
	tc.request.AddCookie(tc.session.toCookie(now))
	return tc
}

func (tc *testContext) test(fn func(resp *http.Response)) {
	rec := httptest.NewRecorder()
	router := BindRoutes(tc.server)
	router.ServeHTTP(rec, tc.request)

	res := rec.Result()
	defer res.Body.Close()
	fn(res)
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

			newTestContext(tt.method, tt.url, nil).
				test(func(res *http.Response) {
					assert.Equal(t, http.StatusUnauthorized, res.StatusCode)
					testx.AssertReader(t, "Unauthorized", res.Body)
				})
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

			newTestContext(tt.method, tt.url, nil).
				test(func(res *http.Response) {

					assert.Equal(t, http.StatusFound, res.StatusCode)
					loc, err := res.Location()
					require.NoError(t, err)
					assert.Equal(t, "/users/new", loc.Path)
				})
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
			now := time.Now()
			tableID := uuid.New()
			path := fmt.Sprintf(tt.url, tableID)
			var reqBody io.Reader = nil
			if tt.givenBody != "" {
				reqBody = bytes.NewBuffer([]byte(tt.givenBody))
			}
			newTestContext(tt.method, path, reqBody).
				withTable(tableID).
				withUser(now).
				withSession(now).
				test(func(res *http.Response) {

					assert.Equal(t, http.StatusForbidden, res.StatusCode)
					testx.AssertReader(t, "you are not at the table", res.Body)
				})
		})
	}
}
