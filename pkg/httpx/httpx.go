// Package httpx extends standard http package with convinience functions and shortcuts
package httpx

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base32"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nchern/vpoker/pkg/logger"
)

// ContextHeader represents a specific type for keys in Request.Context
type ContextHeader string

const (
	// RequestHeaderName is a name of the header that contains request id
	RequestHeaderName = "X-Request-Id"

	requestIDKey ContextHeader = "request_id"

	ClientIPKey ContextHeader = "client_ip"
)

// ErrFinished indicates that a request is already finished and no need to write the response
// Intended to use in case of web sockets when the response is handled by external libs
var ErrFinished = errors.New("request already finished")

// RequestID returns a request id associated with a given context
func RequestID(ctx context.Context) string {
	return fmt.Sprintf("%s", ctx.Value(requestIDKey))
}

// IsMobile detects whether request was made from a mobile device
func IsMobile(r *http.Request) bool {
	mobileKeywords := []string{
		"Mobile", "Android", "iPhone", "iPad", "iPod", "Windows Phone",
		"BlackBerry", "Opera Mini", "Opera Mobi",
	}
	for _, keyword := range mobileKeywords {
		if strings.Contains(r.UserAgent(), keyword) {
			return true
		}
	}
	return false
}

// Response represents http response
type Response struct {
	code int

	url string

	cookies []*http.Cookie

	contentType string

	body []byte
}

func (r *Response) writeCookies(w http.ResponseWriter) {
	for _, c := range r.cookies {
		http.SetCookie(w, c)
	}
}

// SetCookie sets a cookie on this response
func (r *Response) SetCookie(cookie *http.Cookie) *Response {
	r.cookies = append(r.cookies, cookie)
	return r
}

// Code returns HTTP code of this request
func (r *Response) Code() int { return r.code }

// String returns a simple plain text http response with a given code
func String(code int, lines ...string) *Response {
	msg := ""
	if len(lines) != 0 {
		msg = strings.Join(lines, "\n")
	} else {
		msg = http.StatusText(code)
	}
	return &Response{code: code, body: []byte(msg)}
}

// JSON returns a response with JSON-serialed object body for a given object
func JSON(code int, obj any) *Response {
	b, err := json.Marshal(obj)
	if err != nil {
		panic(err)
	}
	return &Response{code: code, body: b, contentType: "application/json"}
}

// Render returns a response with a rendered template
func Render(code int, t *template.Template, data any, cookies ...*http.Cookie) (*Response, error) {
	buf := &bytes.Buffer{}
	if err := t.Execute(buf, data); err != nil {
		return nil, err
	}
	resp := String(http.StatusOK, buf.String())
	for _, c := range cookies {
		resp.SetCookie(c)
	}
	return resp, nil
}

// RenderFile is similar to Render, except it reads a template from disk every call
func RenderFile(code int, tplFilename string, data any, cookies ...*http.Cookie) (*Response, error) {
	t, err := template.ParseFiles(tplFilename)
	if err != nil {
		return nil, err
	}
	return Render(code, t, data, cookies...)
}

// Redirect returns an HTTP redirect response
func Redirect(url string) *Response {
	return &Response{code: http.StatusFound, url: url}
}

// Error represents a http error that a handler can return to the client
type Error struct {
	Code    int
	Message string
}

// NewError creates an instance of Error struct
func NewError(code int, msg string) *Error {
	return &Error{
		Code:    code,
		Message: msg,
	}
}

// Error returns error message and makes Error comply go error interface
func (e *Error) Error() string {
	return e.Message
}

// RequestHandler makes writing http handlers more natural:
//
//	each handler would be terminated by returning a response object or an error
type RequestHandler func(r *http.Request) (*Response, error)

func mkRequestID() string {
	randomBytes := make([]byte, 10)
	_, err := rand.Read(randomBytes)
	if err != nil {
		id := uuid.New().String()
		logger.Error.Printf("request_id=%s %s", id, err)
		return id
	}
	return base32.StdEncoding.EncodeToString(randomBytes)
}

func writeResponse(
	r *http.Request,
	w http.ResponseWriter,
	code int,
	body []byte,
	requestID string,
	clientIP string,
	startedAt time.Time) {

	w.WriteHeader(code)
	if _, err := w.Write(body); err != nil {
		if err != http.ErrHijacked { // if this was called in the context of web sockets, write would not work
			// last resort, can't write error back
			logger.Error.Printf("%s %s request_id=%s code=%d response write failed: %s",
				r.Method, r.URL, requestID, code, err)
		}
	}
	logger.Info.Printf("%s %s code=%d request_id=%s client_ip=%s duration_ms=%d finish",
		r.Method, r.URL, code, requestID, clientIP, int(time.Since(startedAt)/time.Millisecond))
}

// H makes a http handler suitable for usage in go standard http lib out of httpx.RequestHandler
func H(fn RequestHandler) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		startedAt := time.Now()
		requestID := r.Header.Get(RequestHeaderName)
		if requestID == "" {
			requestID = mkRequestID()
		}
		clientIP := r.Header.Get("X-Real-IP")
		if clientIP == "" {
			clientIP = r.RemoteAddr
		}
		logger.Info.Printf("%s %s request_id=%s client_ip=%s begin", r.Method, r.URL, requestID, clientIP)
		logger.Info.Printf("request_id=%s client_ip=%s browser: %s", requestID, clientIP, r.UserAgent())
		if strings.Contains(r.UserAgent(), "Bot") {
			writeResponse(r,
				w,
				http.StatusForbidden,
				[]byte("bots are not allowed"), requestID, clientIP, startedAt)
			return
		}
		w.Header().Set(RequestHeaderName, requestID)
		r = r.WithContext(context.WithValue(r.Context(), requestIDKey, requestID))
		r = r.WithContext(context.WithValue(r.Context(), ClientIPKey, clientIP))
		res, err := fn(r)
		if err != nil {
			if err == ErrFinished {
				logger.Info.Printf("%s %s request_id=%s client_ip=%s finish",
					r.Method, r.URL, requestID, clientIP)
				return
			}
			msg := ""
			code := http.StatusInternalServerError
			switch e := err.(type) {
			case *Error:
				code = e.Code
				msg = e.Message
			default:
				msg = fmt.Sprintf("%s: %s\n", http.StatusText(code), err)
			}
			logger.Error.Printf("%s %s request_id=%s client_ip=%s %s",
				r.Method, r.URL, requestID, clientIP, err)
			writeResponse(r, w, code, []byte(msg), requestID, clientIP, startedAt)
			return
		}
		res.writeCookies(w)
		if res.code == http.StatusFound || res.code == http.StatusMovedPermanently {
			logger.Info.Printf("%s %s code=%d request_id=%s client_ip=%s redirect_to=%s",
				r.Method, r.URL, res.code, requestID, clientIP, res.url)
			http.Redirect(w, r, res.url, res.code)
			return
		}
		if res.contentType != "" {
			w.Header().Set("Content-Type", res.contentType)
		}
		writeResponse(r, w, res.code, res.body, requestID, clientIP, startedAt)
	}
}
