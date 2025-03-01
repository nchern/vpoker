package httpx

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nchern/vpoker/pkg/testx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testHandler(
	req *http.Request,
	underTest func(w http.ResponseWriter, r *http.Request),
	testFn func(actual *http.Response)) {

	rec := httptest.NewRecorder()
	underTest(rec, req)

	res := rec.Result()
	defer res.Body.Close()
	testFn(res)
}

func TestHandlerShouldBlockBots(t *testing.T) {
	underTest := H(func(r *http.Request) (*Response, error) {
		panic("must never be called")
	})

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("User-Agent", "TestBot /1.0")

	testHandler(req, underTest, func(actual *http.Response) {
		assert.Equal(t, http.StatusForbidden, actual.StatusCode)
		testx.AssertReader(t, "bots are not allowed", actual.Body)
	})
}

func TestHandlerShouldHandleRedirect(t *testing.T) {
	underTest := H(func(r *http.Request) (*Response, error) {
		return Redirect("http://foo.bar"), nil
	})
	req := httptest.NewRequest("GET", "/", nil)

	testHandler(req, underTest, func(actual *http.Response) {
		assert.Equal(t, http.StatusFound, actual.StatusCode)
		testx.AssertReader(t, "<a href=\"http://foo.bar\">Found</a>.\n\n", actual.Body)
		u, err := actual.Location()
		require.NoError(t, err)
		assert.Equal(t, "http://foo.bar", u.String())
	})
}

func TestHandlerShould(t *testing.T) {
	testCookie := &http.Cookie{
		Path:    "/",
		Value:   "bar",
		Name:    "foo",
		Expires: time.Now(),
	}
	var tests = []struct {
		name                string
		expectedCode        int
		expectedBody        string
		expectedContentType []string
		expectedCookies     []string
		given               func(*http.Request) (*Response, error)
	}{
		{"respond with text",
			http.StatusOK,
			"test",
			nil,
			[]string{},
			func(r *http.Request) (*Response, error) {
				return String(http.StatusOK, "test"), nil
			},
		},
		{"respond json",
			http.StatusOK,
			"{\"foo\":123}",
			[]string{"application/json"},
			[]string{},
			func(r *http.Request) (*Response, error) {
				resp := map[string]int{"foo": 123}
				return JSON(http.StatusOK, resp), nil
			},
		},
		{"respond 500 if error is returned",
			http.StatusInternalServerError,
			"Internal Server Error: boom\n",
			nil,
			[]string{},
			func(r *http.Request) (*Response, error) {
				return nil, errors.New("boom")
			},
		},
		{"respond custom httpx error",
			http.StatusTooManyRequests,
			"limit error",
			nil,
			[]string{},
			func(r *http.Request) (*Response, error) {
				return nil, NewError(http.StatusTooManyRequests, "limit error")
			},
		},
		{"respond custom httpx error",
			http.StatusOK,
			"test",
			nil,
			[]string{testCookie.String()},
			func(r *http.Request) (*Response, error) {
				return String(200, "test").SetCookie(testCookie), nil
			},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			underTest := H(tt.given)

			testHandler(req, underTest, func(actual *http.Response) {
				assert.Equal(t, tt.expectedCode, actual.StatusCode)
				testx.AssertReader(t, tt.expectedBody, actual.Body)
				assert.NotEmpty(t, actual.Header[RequestHeaderName])
				assert.Equal(t, tt.expectedContentType, actual.Header["Content-Type"])
				for i, c := range actual.Cookies() {
					assert.Equal(t, tt.expectedCookies[i], c.String())
				}
			})
		})
	}
}
