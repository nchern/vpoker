package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"text/template"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/gorilla/schema"
	"github.com/gorilla/websocket"
	"github.com/nchern/vpoker/pkg/httpx"
	"github.com/nchern/vpoker/pkg/logger"
	"github.com/nchern/vpoker/pkg/poker"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

const (
	maxPlayers      = 3
	cookieExpiresAt = 30 * 24 * time.Hour

	statePath = "/tmp/vpoker.json"
)

var (
	index      = template.Must(template.ParseFiles("web/index.html"))
	pokerTable = template.Must(template.ParseFiles("web/poker.html"))
	profile    = template.Must(template.ParseFiles("web/profile.html"))

	errChanClosed = errors.New("channel closed")

	usernameValidator = regexp.MustCompile("(?i)^[a-z0-9_-]+?$")
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

func (s *session) toJSON() []byte {
	b, err := json.Marshal(s)
	dieIf(err)
	return b
}

func (s *session) toCookie() string {
	return base64.URLEncoding.EncodeToString(s.toJSON())
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

func newSessionCookie(now time.Time, v string) *http.Cookie {
	return &http.Cookie{
		Path:    "/",
		Value:   v,
		Name:    "session",
		Expires: now.Add(cookieExpiresAt),
	}
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

func (b *contextBuilder) withTable(s *server, r *http.Request, idParam string) *contextBuilder {
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

func (b *contextBuilder) withUser(s *server, r *http.Request) *contextBuilder {
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

type ItemUpdatedResponse struct {
	Updated *poker.TableItem `json:"updated"`
}

type stateFile struct {
	path string
	lock sync.RWMutex
}

func NewStateFile(path string) *stateFile {
	return &stateFile{path: path}
}

func (s *stateFile) save(marshalers ...json.Marshaler) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	f, err := os.Create(statePath)
	defer func() { logError(f.Close(), "stateFile.save os.Create") }()
	if err != nil {
		return err
	}
	for _, v := range marshalers {
		b, err := v.MarshalJSON()
		if err != nil {
			return err
		}
		if _, err := f.Write(b); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(f); err != nil {
			return err
		}
	}
	return err
}

func (s *stateFile) load(unmarshalers ...json.Unmarshaler) error {
	s.lock.RLock()
	defer s.lock.RUnlock()
	if err := os.MkdirAll(path.Dir(statePath), 0700); err != nil {
		return err
	}
	f, err := os.Open(statePath)
	defer func() { logError(f.Close(), "stateFile.load os.Open") }()
	if err != nil {
		return err
	}
	r := bufio.NewReader(f)
	for _, v := range unmarshalers {
		l, err := r.ReadString('\n')
		if err != nil {
			return err
		}
		if err := v.UnmarshalJSON([]byte(l)); err != nil {
			return err
		}
	}
	return nil
}

type server struct {
	endpoint string

	tables poker.TableMap
	users  poker.UserMap

	state *stateFile
}

func handlePush(ctx *Context, conn *websocket.Conn, update *poker.Push) error {
	if update == nil {
		// channel closed, teminating this update loop
		msg := "terminated by another connection"
		logger.Info.Printf("ws %s web socket connection %s", ctx, msg)
		if err := conn.WriteJSON(poker.NewPushDisconnected()); err != nil {
			logger.Error.Printf("%s conn.WriteMessage %s", ctx, err)
		}
		return errChanClosed
	}
	logger.Debug.Printf("ws %s push_begin: %s", ctx, update.Type)
	resp, err := update.DeepCopy()
	if err != nil {
		return err
	}
	for _, it := range resp.Items {
		it.ApplyVisibilityRules(ctx.user)
	}
	if err := conn.WriteJSON(resp); err != nil {
		return fmt.Errorf("conn.WriteJSON: %w", err)
	}
	logger.Debug.Printf("%s push_finished: %s", ctx, update.Type)
	return nil
}

func (s *server) pushTableUpdates(w http.ResponseWriter, r *http.Request) {
	// Pushes loop gets terminated in the following cases:
	// - disconnections from the client
	// - channel externally closed - a new web socket connection by the same player
	httpx.H(authenticated(s.users, func(r *http.Request) (*httpx.Response, error) {
		ctx, err := newContextBuilder(r.Context()).withUser(s, r).withTable(s, r, "id").build()
		if err != nil {
			return nil, err
		}
		var p *poker.Player
		updates := make(chan *poker.Push)
		if err := ctx.table.Update(func(t *poker.Table) error {
			p = t.Players[ctx.user.ID]
			if p == nil {
				return httpx.NewError(http.StatusForbidden, "you are not at the table")
			}
			p.Subscribe(updates)
			return nil
		}); err != nil {
			return nil, err
		}
		hdrs := http.Header{}
		hdrs.Set(httpx.RequestHeaderName, httpx.RequestID(ctx.ctx))
		conn, err := upgrader.Upgrade(w, r, hdrs) // after .Upgrade normal http responses are not posible
		if err != nil {
			return nil, fmt.Errorf("upgrader.Upgrade: %w", err)
		}
		defer conn.Close()
		logger.Debug.Printf("ws %s pushes_start", ctx)
		for {
			var err error
			select {
			case update := <-updates:
				if err = handlePush(ctx, conn, update); err != nil {
					if errors.Is(err, errChanClosed) {
						return nil, httpx.ErrFinished // terminate the loop only if channel got closed
					}
					logger.Error.Printf("ws %s %s", ctx, err)
				}
			case <-time.After(15 * time.Second): // check state periodically
				if err = conn.WriteMessage(websocket.PingMessage, []byte("ping")); err != nil {
					logger.Error.Printf("ws %s %s", ctx, err)
					// decide how to unsubscribe - race conditions
					// unable to write - close this connection
					// return nil, fmt.Errorf("websocket_ping write error: %w", err)
				}
				// no need to read - browser does not automatically send a response
			}
			if errors.Is(err, syscall.EPIPE) || errors.Is(err, syscall.ECONNRESET) {
				close(updates)
				logger.Info.Printf("ws %s pushes_finish", ctx)
				return nil, httpx.ErrFinished // terminate the loop
			}
		}
	}))(w, r)
}

func (s *server) shuffle(ctx *Context, r *http.Request) (*httpx.Response, error) {
	if err := ctx.table.Update(func(t *poker.Table) error {
		if t.Players[ctx.user.ID] == nil {
			return httpx.NewError(http.StatusForbidden, "you are not at the table")
		}
		ctx.table.Shuffle()
		return nil
	}); err != nil {
		return nil, err
	}
	// notify others
	// push updates: potentially long operation - check
	ctx.table.NotifyOthers(ctx.user, poker.NewPushRefresh())
	return httpx.Redirect(fmt.Sprintf("/games/%s", ctx.table.ID)), nil
}

func (s *server) showCard(ctx *Context, r *http.Request) (*httpx.Response, error) {
	req := map[string]int{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, err
	}
	id, found := req["id"]
	if !found {
		return nil, httpx.NewError(http.StatusBadRequest, "id field is missing")
	}
	var updated poker.TableItem
	if err := ctx.table.Update(func(t *poker.Table) error {
		if t.Players[ctx.user.ID] == nil {
			return httpx.NewError(http.StatusForbidden, "you are not at the table")
		}
		item := t.Items.Get(id)
		if item == nil {
			return httpx.NewError(http.StatusNotFound, "item not found")
		}
		if err := item.Show(ctx.user); err != nil {
			return err
		}
		updated = *item
		return nil
	}); err != nil {
		return nil, err
	}
	// push updates: potentially long operation - check
	ctx.table.NotifyOthers(ctx.user, poker.NewPushItems(&updated))
	return httpx.JSON(http.StatusOK, &ItemUpdatedResponse{Updated: &updated}), nil
}

func (s *server) giveCard(ctx *Context, r *http.Request) (*httpx.Response, error) {
	var frm struct {
		ID     int       `schema:"id,reqiured"`
		UserID uuid.UUID `schema:"user_id,reqiured"`
	}
	if err := parseForm(r, &frm); err != nil {
		return nil, httpx.NewError(http.StatusBadRequest, "bad params: "+err.Error())
	}

	var updated poker.TableItem
	if err := ctx.table.Update(func(t *poker.Table) error {
		if t.Players[ctx.user.ID] == nil {
			return httpx.NewError(http.StatusForbidden, "you are not at the table")
		}
		recepient := t.Players[frm.UserID]
		if recepient == nil {
			return httpx.NewError(http.StatusForbidden, "recepient is not at the table")
		}
		item := t.Items.Get(int(frm.ID))
		if item == nil {
			return httpx.NewError(http.StatusNotFound, "item not found")
		}
		updated = *item.Take(recepient.User)
		return nil
	}); err != nil {
		return nil, err
	}
	updated.Side = poker.Cover
	ctx.table.NotifyOthers(ctx.user, poker.NewPushItems(&updated))
	return httpx.JSON(http.StatusOK, ItemUpdatedResponse{Updated: &updated}), nil
}

func (s *server) takeCard(ctx *Context, r *http.Request) (*httpx.Response, error) {
	req := map[string]int{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, err
	}
	id, found := req["id"]
	if !found {
		return nil, httpx.NewError(http.StatusBadRequest, "id field is missing")
	}
	var updated poker.TableItem
	if err := ctx.table.Update(func(t *poker.Table) error {
		if t.Players[ctx.user.ID] == nil {
			return httpx.NewError(http.StatusForbidden, "you are not at the table")
		}
		item := t.Items.Get(id)
		if item == nil {
			return httpx.NewError(http.StatusNotFound, "item not found")
		}
		updated = *item.Take(ctx.user)
		return nil
	}); err != nil {
		return nil, err
	}
	updated.Side = poker.Face
	// push updates: potentially long operation - check
	ctx.table.NotifyOthers(ctx.user, poker.NewPushItems(&updated))
	return httpx.JSON(http.StatusOK, ItemUpdatedResponse{Updated: &updated}), nil
}

func (s *server) profile(r *http.Request) (*httpx.Response, error) {
	sess, err := getUserFromSession(r, s.users)
	if err != nil {
		return nil, err
	}
	return httpx.RenderFile(http.StatusOK, "web/profile.html", m{
		"Retpath":  sanitizedRetpath(r.URL),
		"Username": sess.user.Name,
	})
}

func (s *server) updateProfile(r *http.Request) (*httpx.Response, error) {
	if err := r.ParseForm(); err != nil {
		return nil, err
	}
	sess, err := getUserFromSession(r, s.users)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(r.FormValue("user_name"))
	if !usernameValidator.MatchString(name) {
		return httpx.String(http.StatusBadRequest, "invalid characters in user name"), nil
	}
	if err := s.users.Update(sess.user.ID, func(u *poker.User) error {
		u.Name = name
		sess.user = u
		return nil
	}); err != nil {
		return nil, err
	}
	lastNameCookie := newLastName(time.Now(), name)
	if retPath := sanitizedRetpath(r.URL); retPath != "" {
		return httpx.Redirect(retPath).SetCookie(lastNameCookie), nil
	}
	return httpx.Render(
		http.StatusOK,
		profile,
		m{"Username": sess.user.Name},
		lastNameCookie)
}

func updateItem(ctx *Context, r *http.Request) (*poker.TableItem, error) {
	curUser, table := ctx.user, ctx.table
	if table.Players[curUser.ID] == nil {
		return nil, httpx.NewError(http.StatusForbidden, "you are not at the table")
	}
	var src poker.TableItem
	if err := json.NewDecoder(r.Body).Decode(&src); err != nil {
		return nil, err
	}
	logger.Debug.Printf("%s update: %+v", ctx, src)
	dest := table.Items.Get(src.ID)
	if dest == nil {
		return nil, httpx.NewError(http.StatusNotFound, "item not found")
	}
	if err := dest.UpdateFrom(curUser, &src); err != nil {
		return nil, httpx.NewError(http.StatusBadRequest, err.Error())
	}
	return dest, nil
}

func (s *server) updateTable(ctx *Context, r *http.Request) (*httpx.Response, error) {
	curUser, table := ctx.user, ctx.table
	var updated poker.TableItem
	if err := table.Update(func(t *poker.Table) error {
		up, err := updateItem(ctx, r)
		if err != nil {
			return err
		}
		updated = *up
		return nil
	}); err != nil {
		return nil, err
	}
	logger.Debug.Printf("%s update dest=%+v", ctx, updated)
	// push updates: potentially long operation - check
	ctx.table.NotifyOthers(curUser, poker.NewPushItems(&updated))
	return httpx.JSON(http.StatusOK, ItemUpdatedResponse{Updated: &updated}), nil
}

func (s *server) kickPlayer(ctx *Context, r *http.Request) (*httpx.Response, error) {
	var frm struct {
		Username string `schema:"name,reqiured"`
	}
	if err := parseForm(r, &frm); err != nil {
		return nil, err
	}
	if err := ctx.table.Update(func(t *poker.Table) error {
		return t.KickPlayer(frm.Username)
	}); err != nil {
		return nil, err
	}
	return httpx.String(http.StatusOK, "successfully kicked"), nil
}

func (s *server) joinTable(ctx *Context, r *http.Request) (*httpx.Response, error) {
	var players map[uuid.UUID]*poker.Player
	var updated []*poker.TableItem
	if err := ctx.table.Update(func(t *poker.Table) error {
		hasJoined := t.Players[ctx.user.ID] != nil
		if hasJoined {
			return nil
		}
		logger.Debug.Printf("players_joind=%d", len(t.Players))
		if len(t.Players) >= maxPlayers {
			return httpx.NewError(http.StatusForbidden, "this table is full")
		}
		updated = t.Join(ctx.user)
		players = t.Players
		return nil
	}); err != nil {
		return nil, err
	}
	// push updates: potentially long operation - check
	ctx.table.NotifyOthers(ctx.user, poker.NewPushPlayerJoined(players, updated...))
	return httpx.Redirect(fmt.Sprintf("/games/%s", ctx.table.ID)), nil
}

func (s *server) renderTable(ctx *Context, r *http.Request) (*httpx.Response, error) {
	curUser, table := ctx.user, ctx.table
	players := []*poker.Player{}
	errRedirect := errors.New("redirect")
	if err := table.ReadLock(func(t *poker.Table) error {
		if t.Players[curUser.ID] == nil {
			return errRedirect
		}
		for _, v := range t.Players {
			players = append(players, v)
		}
		return nil
	}); err != nil {
		if err == errRedirect {
			return httpx.Redirect(fmt.Sprintf("/games/%s/join", table.ID)), nil
		}
		return nil, err
	}
	return httpx.RenderFile(http.StatusOK, "web/poker.html", m{
		"Players":  players,
		"TableID":  table.ID,
		"Username": curUser.Name,
	})
}

func getTableState(curUser *poker.User, table *poker.Table) (*poker.Table, error) {
	var tableCopy *poker.Table
	if err := table.ReadLock(func(t *poker.Table) error {
		if t.Players[curUser.ID] == nil {
			return httpx.NewError(http.StatusForbidden, "you are not at the table")
		}
		var err error
		// deep copy the current table - items must be modified
		// as their content differes for different users due to ownership
		tableCopy, err = t.DeepCopy()
		return err
	}); err != nil {
		return nil, err
	}
	for _, it := range tableCopy.Items {
		it.ApplyVisibilityRules(curUser)
	}
	return tableCopy, nil
}

func (s *server) tableState(ctx *Context, r *http.Request) (*httpx.Response, error) {
	tableCopy, err := getTableState(ctx.user, ctx.table)
	if err != nil {
		return nil, err
	}
	return httpx.JSON(http.StatusOK, tableCopy), nil
}

func (s *server) newTable(r *http.Request) (*httpx.Response, error) {
	sess, err := getUserFromSession(r, s.users)
	if err != nil {
		return nil, err
	}
	curUser := sess.user
	logger.Info.Printf("user_id=%s action=table_created", curUser.ID)

	table := poker.NewTable(uuid.New(), 50).StartGame()
	s.tables.Set(table.ID, table)
	table.Join(curUser)

	return httpx.Redirect(fmt.Sprintf("/games/%s", table.ID)), nil
}

func (s *server) newUser(r *http.Request) (*httpx.Response, error) {
	redirectTo := sanitizedRetpath(r.URL)
	if redirectTo == "" {
		redirectTo = "/"
	}
	old, err := getUserFromSession(r, s.users)
	if err != nil || old.user == nil {
		// Cookie not found or empty: create and set a new one
		name := "Anon" + randomString()
		if ln, err := r.Cookie("last_name"); err == nil {
			if ln.Value != "" {
				name = ln.Value
			}
		}
		shouldChangeName := strings.HasPrefix(strings.ToLower(name), "anon")
		now := time.Now()
		u := poker.NewUser(uuid.New(), name, now)
		s.users.Set(u.ID, u)
		sess := &session{UserID: u.ID, CreatedAt: now, Name: u.Name}
		cookie := newSessionCookie(now, sess.toCookie())
		ctx, err := newContextBuilder(r.Context()).build()
		if err != nil {
			return nil, err
		}
		logger.Info.Printf("%s user_name=%s user_registered %s", ctx, name, r.UserAgent())
		if shouldChangeName {
			redirectTo = fmt.Sprintf("/users/profile?%s=%s", retPathKey, redirectTo)
		}
		return httpx.Redirect(redirectTo).
			SetCookie(cookie).
			SetCookie(newLastName(now, name)), nil
	}
	return httpx.Redirect(redirectTo), nil
}

func (s *server) index(r *http.Request) (*httpx.Response, error) {
	username := "anonymous"
	var emptySess *http.Cookie
	sess, err := getUserFromSession(r, s.users)
	if err != nil {
		emptySess = newEmptySession()
	}
	if sess != nil && sess.user != nil {
		username = sess.user.Name
	} else {
		emptySess = newEmptySession()
	}

	return httpx.RenderFile(http.StatusOK, "web/index.html", m{
		"Username": username,
	}, emptySess)
}

func (s *server) loadState() error { return s.state.load(s.users, s.tables) }

func (s *server) saveState() error { return s.state.save(s.users, s.tables) }

func saveStateLoop(s *server) {
	const saveStateEvery = 10 * time.Second
	for range time.Tick(saveStateEvery) {
		if err := s.saveState(); err != nil {
			logger.Error.Printf("saveStateLoop: %s", err)
		}
	}
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

func authenticated(users poker.UserMap, f httpx.RequestHandler) httpx.RequestHandler {
	return func(r *http.Request) (*httpx.Response, error) {
		sess, err := getUserFromSession(r, users)
		if err != nil {
			return httpx.String(http.StatusUnauthorized), nil
		}
		if sess.user == nil {
			return httpx.String(http.StatusUnauthorized), nil
		}
		return f(r)
	}
}

func handleSignalsLoop(srv *server) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	for s := range signals {
		logger.Info.Printf("received: %s; saving state...", s)
		if err := srv.saveState(); err != nil {
			logger.Error.Printf("server.saveState %s", err)
		}
		if s == syscall.SIGTERM || s == os.Interrupt {
			logger.Info.Printf("graceful shutdown: %s", s)
			break
		}
		os.Exit(1)
	}
	os.Exit(0)
}

// TODO: add metrics
// TODO: connect metrics to Graphana
// TODO: decide what to do with abandoned tables. Now they not only stay in memory but also
// keep websocket groutines/channels forever
func main() {
	s := &server{
		endpoint: ":8080",
		state:    NewStateFile(statePath),

		tables: poker.NewTableMapSyncronized(),
		users:  poker.NewUserMapSyncronized(),
	}
	if err := s.loadState(); err != nil {
		logger.Error.Printf("server.loadState %s", err)
	}
	auth := func(f httpx.RequestHandler) httpx.RequestHandler {
		return authenticated(s.users, f)
	}
	tableHandler := func(fn func(*Context, *http.Request) (*httpx.Response, error)) httpx.RequestHandler {
		return func(r *http.Request) (*httpx.Response, error) {
			ctx, err := newContextBuilder(r.Context()).withUser(s, r).withTable(s, r, "id").build()
			if err != nil {
				return nil, err
			}
			return fn(ctx, r)
		}
	}
	redirectIfNoAuth := func(url string, f httpx.RequestHandler) httpx.RequestHandler {
		return func(r *http.Request) (*httpx.Response, error) {
			resp, err := auth(f)(r)
			if err != nil {
				return nil, err
			}
			if resp.Code() == http.StatusUnauthorized {
				return httpx.Redirect(fmt.Sprintf("%s?ret_path=%s", url, r.URL.Path)), nil
			}
			return resp, nil
		}
	}

	r := mux.NewRouter()

	r.HandleFunc("/", httpx.H(s.index)).Methods("GET")
	r.HandleFunc("/log", httpx.H(func(r *http.Request) (*httpx.Response, error) {
		return httpx.JSON(http.StatusOK, m{}), nil
	})).Methods("GET")

	r.HandleFunc("/games/new", httpx.H(redirectIfNoAuth("/users/new", s.newTable)))

	r.HandleFunc("/games/{id:[a-z0-9-]+}",
		httpx.H(redirectIfNoAuth("/users/new", tableHandler(s.renderTable)))).Methods("GET")
	r.HandleFunc("/games/{id:[a-z0-9-]+}/state",
		httpx.H(auth(tableHandler(s.tableState)))).Methods("GET")
	r.HandleFunc("/games/{id:[a-z0-9-]+}/join",
		httpx.H(auth(tableHandler(s.joinTable)))).Methods("GET")
	r.HandleFunc("/games/{id:[a-z0-9-]+}/update",
		httpx.H(auth(tableHandler(s.updateTable)))).Methods("POST")
	r.HandleFunc("/games/{id:[a-z0-9-]+}/show_card",
		httpx.H(auth(tableHandler(s.showCard)))).Methods("POST")
	r.HandleFunc("/games/{id:[a-z0-9-]+}/take_card",
		httpx.H(auth(tableHandler(s.takeCard)))).Methods("POST")
	r.HandleFunc("/games/{id:[a-z0-9-]+}/give_card",
		httpx.H(auth(tableHandler(s.giveCard)))).Methods("POST")
	r.HandleFunc("/games/{id:[a-z0-9-]+}/listen",
		s.pushTableUpdates).Methods("GET")
	r.HandleFunc("/games/{id:[a-z0-9-]+}/shuffle",
		httpx.H(auth(tableHandler(s.shuffle)))).Methods("GET")
	r.HandleFunc("/games/{id:[a-z0-9-]+}/kick",
		httpx.H(auth(tableHandler(s.kickPlayer)))).Methods("POST")

	r.HandleFunc("/users/new", httpx.H(s.newUser))
	r.HandleFunc("/users/profile",
		httpx.H(redirectIfNoAuth("/users/new", s.profile))).
		Methods("GET")
	r.HandleFunc("/users/profile",
		httpx.H(auth(s.updateProfile))).
		Methods("POST")

	http.Handle("/", r)

	http.Handle("/robots.txt",
		http.StripPrefix("/", http.FileServer(http.Dir("./web/"))))

	http.Handle("/static/",
		http.StripPrefix("/static/", http.FileServer(http.Dir("./web/static"))))

	go handleSignalsLoop(s)
	go saveStateLoop(s)

	logger.Info.Printf("Start listening on %s", s.endpoint)
	must(http.ListenAndServe(s.endpoint, nil))
}

func must(err error) {
	dieIf(err)
}

func dieIf(err error) {
	if err != nil {
		logger.Error.Println(err)
		os.Exit(1)
	}
}
