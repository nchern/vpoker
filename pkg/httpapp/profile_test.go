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

	"github.com/nchern/vpoker/pkg/version"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() { version.SetTest() }

func TestProfileUpdateShould(t *testing.T) {
	const retpath = "/ret/path"
	const updatedUsername = "FooBar"
	var expectedProfilePage bytes.Buffer
	tpl, err := template.ParseFiles(filepath.Join("../../", "web/profile.html"))
	require.NoError(t, err)
	require.NoError(t, tpl.Execute(&expectedProfilePage, m{
		"Retpath":  "",
		"Version":  "test",
		"Username": updatedUsername,
	}))
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
			expectedProfilePage.String(),
			[]string{newLastName(time.Now(), updatedUsername).String()},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Now()
			endpoint := "/users/profile?ret_path=" + tt.givenRetpath
			newTestContext("POST", endpoint, strings.NewReader(tt.givenForm.Encode())).
				withUser(now).
				withSession(now).
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
					require.Len(t, rec.Result().Cookies(), len(tt.expectedCookies))
					for i, c := range rec.Result().Cookies() {
						assert.Equal(t, tt.expectedCookies[i], c.String())
					}
				})
		})
	}
}
