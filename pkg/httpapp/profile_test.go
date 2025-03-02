package httpapp

import (
	"bytes"
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nchern/vpoker/pkg/poker"
	"github.com/nchern/vpoker/pkg/version"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() { version.SetTest() }

func profilePage(t *testing.T, userName string) string {
	var buf bytes.Buffer
	tpl, err := template.ParseFiles(filepath.Join("../../", "web/profile.html"))
	require.NoError(t, err)
	require.NoError(t, tpl.Execute(&buf, m{
		"Retpath":  "",
		"Version":  "test",
		"Username": userName,
	}))
	return buf.String()
}

func TestProfileUpdateShould(t *testing.T) {
	const retpath = "/ret/path"
	const updatedUsername = "FooBar"
	var tests = []struct {
		name string

		givenForm    url.Values
		givenRetpath string

		expectedUserName string

		expectedCode     int
		expectedResponse string
		expectedCookies  []string
	}{
		{"fail on empty user name",
			url.Values{"user_name": []string{"$foo"}},
			"",
			defaultTestUserName,
			http.StatusBadRequest,
			"invalid characters in user name",
			[]string{},
		},
		{"fail on bad characters in user name",
			url.Values{"user_name": []string{""}},
			"",
			defaultTestUserName,
			http.StatusBadRequest,
			"invalid characters in user name",
			[]string{},
		},
		{"update name and redirect if retpath provided",
			url.Values{"user_name": []string{updatedUsername}},
			retpath,
			updatedUsername,
			http.StatusFound,
			"",
			[]string{newLastName(time.Now(), updatedUsername).String()},
		},
		{"update name",
			url.Values{"user_name": []string{updatedUsername}},
			"",
			updatedUsername,
			http.StatusOK,
			profilePage(t, updatedUsername),
			[]string{newLastName(time.Now(), updatedUsername).String()},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			endpoint := "/users/profile?ret_path=" + tt.givenRetpath
			newTestContext("POST", endpoint, strings.NewReader(tt.givenForm.Encode())).
				withUser().
				withSession().
				withHeader("Content-Type", "application/x-www-form-urlencoded").
				test(func(tc *testContext, rec *httptest.ResponseRecorder) {
					assert.Equal(t, tt.expectedCode, rec.Result().StatusCode)
					assert.Equal(t, tt.expectedResponse, rec.Body.String())
					actualUser, _ := tc.server.users.Get(tc.userUnderTest.ID)
					assert.Equal(t, tt.expectedUserName, actualUser.Name)
					if tt.givenRetpath != "" {
						actualLocation, _ := rec.Result().Location()
						assert.Equal(t, tt.givenRetpath, actualLocation.Path)
					}
					if len(tt.expectedCookies) > 0 {
						tt.expectedCookies = append([]string{
							newSession(tc.now, tc.userUnderTest).toCookie().String(),
						}, tt.expectedCookies...)
					}
					require.Len(t, rec.Result().Cookies(), len(tt.expectedCookies))
					for i, c := range rec.Result().Cookies() {
						assert.Equal(t, tt.expectedCookies[i], c.String())
					}
				})
		})
	}
}

func TestProfileUpdateShouldReturnExistingUserCookieOnSameUserName(t *testing.T) {
	now := time.Now()
	const existingUsername = "existing"
	existingUser := poker.NewUser(uuid.New(), existingUsername, now)
	form := (url.Values{"user_name": []string{existingUsername}}).Encode()
	expectedCookies := []string{
		newSession(now, existingUser).toCookie().String(),
		newLastName(now, existingUsername).String(),
	}

	var tests = []struct {
		name         string
		given        string
		expectedCode int
		expectedBody string
	}{
		{"on profile page",
			"/users/profile",
			http.StatusOK,
			profilePage(t, existingUsername),
		},
		{"with ret_path",
			"/users/profile?ret_path=/",
			http.StatusFound,
			""},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			tc := newTestContext("POST", tt.given, strings.NewReader(form)).
				setNow(now).
				withUser().
				withSession().
				withHeader("Content-Type", "application/x-www-form-urlencoded")

			tc.server.users.Set(existingUser.ID, existingUser)

			tc.test(func(tc *testContext, rec *httptest.ResponseRecorder) {
				assert.Equal(t, tt.expectedCode, rec.Result().StatusCode)
				assert.Equal(t, tt.expectedBody, rec.Body.String())

				// initial user was not touched
				requestUser, _ := tc.server.users.Get(tc.userUnderTest.ID)
				assert.Equal(t, defaultTestUserName, requestUser.Name)

				require.Len(t, rec.Result().Cookies(), len(expectedCookies))
				for i, c := range rec.Result().Cookies() {
					assert.Equal(t, expectedCookies[i], c.String())
				}
				oldSession := tc.session.toCookie().String()
				assert.NotEqual(t, oldSession, rec.Result().Cookies()[0].String())
			})
		})
	}
}
