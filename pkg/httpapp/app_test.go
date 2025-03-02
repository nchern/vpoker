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

const defaultTestUserName = "tester"

type testContext struct {
	server  *Server
	request *http.Request

	tableUnderTest *poker.Table
	userUnderTest  *poker.User
	session        *session

	now time.Time
}

func newTestContext(method string, url string, body io.Reader) *testContext {
	server := New()
	server.templatesPath = "../../"
	res := &testContext{
		server:  server,
		request: httptest.NewRequest(method, url, body),
	}
	return res.setNow(time.Now())
}

func (tc *testContext) setNow(now time.Time) *testContext {
	tc.now = now
	tc.server.now = func() time.Time { return tc.now }
	return tc

}
func (tc *testContext) withHeader(name string, val string) *testContext {
	tc.request.Header.Set(name, val)
	return tc
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
	tc.userUnderTest = poker.NewUser(uuid.New(), defaultTestUserName, now)
	tc.server.users.Set(tc.userUnderTest.ID, tc.userUnderTest)
	return tc
}

func (tc *testContext) useUser(user *poker.User) *testContext {
	tc.userUnderTest = user
	tc.server.users.Set(tc.userUnderTest.ID, tc.userUnderTest)
	return tc
}

func (tc *testContext) withSession(now time.Time) *testContext {
	tc.session = newSession(now, tc.userUnderTest)
	tc.request.AddCookie(tc.session.toCookie())
	return tc
}

func (tc *testContext) test(fn func(tc *testContext, resp *httptest.ResponseRecorder)) {
	rec := httptest.NewRecorder()
	router := BindRoutes(tc.server)
	router.ServeHTTP(rec, tc.request)

	fn(tc, rec)
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
				test(func(tc *testContext, rec *httptest.ResponseRecorder) {
					assert.Equal(t, http.StatusUnauthorized, rec.Result().StatusCode)
					assert.Equal(t, "Unauthorized", rec.Body.String())
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
				test(func(tc *testContext, rec *httptest.ResponseRecorder) {

					assert.Equal(t, http.StatusFound, rec.Result().StatusCode)
					loc, err := rec.Result().Location()
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
				test(func(tc *testContext, rec *httptest.ResponseRecorder) {

					assert.Equal(t, http.StatusForbidden, rec.Result().StatusCode)
					assert.Equal(t, "you are not at the table", rec.Body.String())
				})
		})
	}
}
