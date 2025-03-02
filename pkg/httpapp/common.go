package httpapp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/gorilla/schema"
	"github.com/gorilla/websocket"
	"github.com/nchern/vpoker/pkg/httpx"
	"github.com/nchern/vpoker/pkg/logger"
	"github.com/nchern/vpoker/pkg/poker"
)

type m map[string]any

func logError(err error, tag string) {
	if err != nil {
		logger.Error.Printf("%s: %s", tag, err)
	}
}

type session struct {
	UserID    uuid.UUID `json:"user_id"`
	CreatedAt time.Time `json:"created_at"`
	Name      string    `json:"name"`

	user *poker.User `json:"-"`
}

func newSession(now time.Time, u *poker.User) *session {
	return &session{
		CreatedAt: now,
		UserID:    u.ID,
		Name:      u.Name,
	}
}

func (s *session) toJSON() []byte {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return b
}

func (s *session) toCookie() *http.Cookie {
	val := base64.URLEncoding.EncodeToString(s.toJSON())
	return &http.Cookie{
		Path:    "/",
		Value:   val,
		Name:    "session",
		Expires: s.CreatedAt.Add(cookieExpiresAt),
	}
}

func (s *session) parseFromCookie(v string) error {
	if v == "" {
		return fmt.Errorf("empty cookie")
	}
	b, err := base64.URLEncoding.DecodeString(v)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, s); err != nil {
		return err
	}
	return nil
}

func randomString() string {
	number := rand.Intn(10000) + 1
	return strconv.Itoa(number)
}

func newEmptySession() *http.Cookie {
	return &http.Cookie{
		Path:   "/",
		MaxAge: 0, // Deletes the cookie immediately
		Name:   "session",
		Value:  "",
	}
}

func newLastName(now time.Time, v string) *http.Cookie {
	return &http.Cookie{
		Path:    "/",
		Value:   v,
		Name:    "last_name",
		Expires: now.Add(cookieExpiresAt),
	}
}

type Context struct {
	ctx   context.Context
	table *poker.Table
	user  *poker.User
}

func (c *Context) String() string {
	fields := []string{fmt.Sprintf("request_id=%s", httpx.RequestID(c.ctx))}
	fields = append(fields, fmt.Sprintf("client_ip=%s", c.ctx.Value(httpx.ClientIPKey)))
	if c.user != nil {
		fields = append(fields, "user_name="+c.user.Name)
	}
	if c.table != nil {
		fields = append(fields, "table_id="+c.table.ID.String())
	}
	return strings.Join(fields, " ")
}

type contextBuilder struct {
	err error

	ctx *Context
}

func newContextBuilder(ctx context.Context) *contextBuilder {
	return &contextBuilder{
		ctx: &Context{
			ctx: ctx,
		},
	}
}

func (b *contextBuilder) build() (*Context, error) {
	if b.err != nil {
		return nil, b.err
	}
	return b.ctx, nil
}

func (b *contextBuilder) withTable(s *Server, r *http.Request, idParam string) *contextBuilder {
	if b.err != nil {
		return b
	}
	id := mux.Vars(r)[idParam]
	tableID, err := uuid.Parse(id)
	if err != nil {
		logger.Error.Println("bad uuid=" + id)
		b.err = httpx.NewError(http.StatusBadRequest, "bad id: "+id)
		return b
	}
	table, found := s.tables.Get(tableID)
	if !found {
		b.err = httpx.NewError(http.StatusNotFound, "table not found")
		return b
	}
	b.ctx.table = table
	return b
}

func (b *contextBuilder) withUser(s *Server, r *http.Request) *contextBuilder {
	if b.err != nil {
		return b
	}
	sess, err := getUserFromSession(r, s.users)
	if err != nil {
		b.err = err
		return b
	}
	b.ctx.user = sess.user
	return b
}

const retPathKey = "ret_path"

func sanitizedRetpath(u *url.URL) string {
	s := u.Query().Get(retPathKey)
	if !strings.HasPrefix(s, "/") {
		logger.Info.Printf("bad ret_path: %s", s)
		return ""
	}
	if len(s) > 1024 {
		logger.Info.Printf("ret_path too long: %s", s)
		return ""
	}
	return s
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func parseForm(r *http.Request, frm any) error {
	if err := r.ParseForm(); err != nil {
		return err
	}
	return schema.NewDecoder().Decode(frm, r.Form)
}

func getUserFromSession(r *http.Request, users poker.UserMap) (*session, error) {
	sess := &session{}
	cookie, err := r.Cookie("session")
	if err != nil {
		return sess, nil // no cookie - return new empty session
	}
	if err := sess.parseFromCookie(cookie.Value); err != nil {
		logger.Info.Printf("bad cookie: %s", err)
		return nil, err
	}
	u, found := users.Get(sess.UserID)
	if found {
		sess.user = u
	}
	return sess, nil
}
