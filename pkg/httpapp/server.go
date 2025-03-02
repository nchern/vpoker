package httpapp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/nchern/vpoker/pkg/httpx"
	"github.com/nchern/vpoker/pkg/logger"
	"github.com/nchern/vpoker/pkg/poker"
	"github.com/nchern/vpoker/pkg/version"
)

const (
	cookieExpiresAt = 30 * 24 * time.Hour
)

var (
	errChanClosed = errors.New("channel closed")

	usernameValidator = regexp.MustCompile("(?i)^[a-zа-яЁё0-9_-]+?$")
)

type ItemUpdatedResponse struct {
	Updated *poker.TableItem `json:"updated"`
}

// Server is a game http app server
type Server struct {
	tables poker.TableMap
	users  poker.UserMap

	state *stateFile

	templatesPath string

	now func() time.Time
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

func (s *Server) pushTableUpdates(w http.ResponseWriter, r *http.Request) {
	// Pushes loop gets terminated in the following cases:
	// - disconnections from the client
	// - channel externally closed - a new web socket connection by the same player
	httpx.H(authenticated(s.users, func(r *http.Request) (*httpx.Response, error) {
		ctx, err := newContextBuilder(r.Context()).withUser(s, r).withTable(s, r, "id").build()
		if err != nil {
			return nil, err
		}
		updates := make(chan *poker.Push)
		if err := ctx.table.UpdateBy(ctx.user.ID, func(t *poker.Table, p *poker.Player) error {
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

func (s *Server) shuffle(ctx *Context, r *http.Request) (*httpx.Response, error) {
	if err := ctx.table.UpdateBy(ctx.user.ID, func(t *poker.Table, p *poker.Player) error {
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

func (s *Server) showCard(ctx *Context, r *http.Request) (*httpx.Response, error) {
	req := map[string]int{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, err
	}
	id, found := req["id"]
	if !found {
		return nil, httpx.NewError(http.StatusBadRequest, "id field is missing")
	}
	var updated poker.TableItem
	if err := ctx.table.UpdateBy(ctx.user.ID, func(t *poker.Table, p *poker.Player) error {
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

func (s *Server) giveCard(ctx *Context, r *http.Request) (*httpx.Response, error) {
	var frm struct {
		ID     int       `schema:"id,reqiured"`
		UserID uuid.UUID `schema:"user_id,reqiured"`
	}
	if err := parseForm(r, &frm); err != nil {
		return nil, httpx.NewError(http.StatusBadRequest, "bad params: "+err.Error())
	}

	var updated poker.TableItem
	if err := ctx.table.UpdateBy(ctx.user.ID, func(t *poker.Table, recepient *poker.Player) error {
		item := t.Items.Get(frm.ID)
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

func (s *Server) takeCard(ctx *Context, r *http.Request) (*httpx.Response, error) {
	req := map[string]int{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, err
	}
	id, found := req["id"]
	if !found {
		return nil, httpx.NewError(http.StatusBadRequest, "id field is missing")
	}
	var updated poker.TableItem
	if err := ctx.table.UpdateBy(ctx.user.ID, func(t *poker.Table, p *poker.Player) error {
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

func (s *Server) profile(r *http.Request) (*httpx.Response, error) {
	sess, err := getUserFromSession(r, s.users)
	if err != nil {
		return nil, err
	}
	return httpx.RenderFile(http.StatusOK, filepath.Join(s.templatesPath, "web/profile.html"), m{
		"Retpath":  sanitizedRetpath(r.URL),
		"Username": sess.user.Name,
		"Version":  version.JSVersion(),
	})
}

func (s *Server) updateProfile(r *http.Request) (*httpx.Response, error) {
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
	var user *poker.User
	// XXX: O(n)
	s.users.Each(func(uid uuid.UUID, u *poker.User) bool {
		if strings.EqualFold(u.Name, name) {
			user = u
			return false
		}
		return true
	})
	if user == nil {
		if err := s.users.Update(sess.user.ID, func(u *poker.User) error {
			u.Name = name
			user = u
			return nil
		}); err != nil {
			return nil, err
		}
	}
	cookies := []*http.Cookie{
		newSession(s.now(), user).toCookie(),
		newLastName(s.now(), name),
	}
	if retPath := sanitizedRetpath(r.URL); retPath != "" {
		return httpx.Redirect(retPath).SetCookie(cookies...), nil
	}
	return httpx.RenderFile(
		http.StatusOK,
		filepath.Join(s.templatesPath, "web/profile.html"),
		m{
			"Username": user.Name,
			"Version":  version.JSVersion(),
		},
		cookies...)
}

func updateItem(ctx *Context, r *http.Request) (*poker.TableItem, error) {
	curUser, table := ctx.user, ctx.table
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

func (s *Server) updateMany(ctx *Context, r *http.Request) (*httpx.Response, error) {
	curUser, table := ctx.user, ctx.table
	var updated []*poker.TableItem
	var frm struct {
		Items []*poker.TableItem `json:"items"`
	}
	if err := table.UpdateBy(curUser.ID, func(t *poker.Table, p *poker.Player) error {
		var buf bytes.Buffer
		rdr := io.TeeReader(r.Body, &buf)
		if err := json.NewDecoder(rdr).Decode(&frm); err != nil {
			return err
		}
		logger.Debug.Printf("UpdateMany: len=%d %s", len(frm.Items), &buf)
		for _, v := range frm.Items {
			dest := table.Items.Get(v.ID)
			if dest == nil {
				logger.Error.Printf("item_id=%d not found", v.ID)
				continue
			}
			if err := dest.UpdateFrom(curUser, v); err != nil {
				logger.Error.Printf("update: item_id=%d %+v %s", dest.ID, v, err)
				continue
			}
			updated = append(updated, v)
		}
		frm.Items = updated
		return nil
	}); err != nil {
		return nil, err
	}
	for _, v := range updated {
		logger.Debug.Printf("%s updated %+v", ctx, v)
	}
	// push updates: potentially long operation - check
	ctx.table.NotifyOthers(curUser, poker.NewPushItems(updated...))
	return httpx.JSON(http.StatusOK, frm), nil
}

func (s *Server) updateTable(ctx *Context, r *http.Request) (*httpx.Response, error) {
	curUser, table := ctx.user, ctx.table
	var updated poker.TableItem
	if err := table.UpdateBy(curUser.ID, func(t *poker.Table, p *poker.Player) error {
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

func (s *Server) kickPlayer(ctx *Context, r *http.Request) (*httpx.Response, error) {
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

func (s *Server) joinTable(ctx *Context, r *http.Request) (*httpx.Response, error) {
	var players map[uuid.UUID]*poker.Player
	var updated []*poker.TableItem
	if err := ctx.table.Update(func(t *poker.Table) error {
		hasJoined := t.Players[ctx.user.ID] != nil
		if hasJoined {
			return nil
		}
		var err error
		if updated, err = t.Join(ctx.user); err != nil {
			return err
		}
		logger.Debug.Printf("joinTable.joined %s players=%d", ctx, len(t.Players))
		players = t.Players
		return nil
	}); err != nil {
		return nil, err
	}
	// push updates: potentially long operation - check
	ctx.table.NotifyOthers(ctx.user, poker.NewPushPlayerJoined(players, updated...))
	return httpx.Redirect(fmt.Sprintf("/games/%s", ctx.table.ID)), nil
}

func (s *Server) renderTable(ctx *Context, r *http.Request) (*httpx.Response, error) {
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
	return httpx.RenderFile(http.StatusOK, filepath.Join(s.templatesPath, "web/poker.html"), m{
		"Players":  players,
		"TableID":  table.ID,
		"Username": curUser.Name,
		"Version":  version.JSVersion(),
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

func (s *Server) tableState(ctx *Context, r *http.Request) (*httpx.Response, error) {
	tableCopy, err := getTableState(ctx.user, ctx.table)
	if err != nil {
		return nil, err
	}
	return httpx.JSON(http.StatusOK, tableCopy), nil
}

func (s *Server) newTable(r *http.Request) (*httpx.Response, error) {
	sess, err := getUserFromSession(r, s.users)
	if err != nil {
		return nil, err
	}
	curUser := sess.user
	logger.Info.Printf("user_id=%s action=table_created", curUser.ID)

	table := poker.NewTable(uuid.New(), 50).StartGame()
	if _, err := table.Join(curUser); err != nil {
		return nil, err
	}
	s.tables.Set(table.ID, table)

	return httpx.Redirect(fmt.Sprintf("/games/%s", table.ID)), nil
}

func (s *Server) newUser(r *http.Request) (*httpx.Response, error) {
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
		now := s.now()
		u := poker.NewUser(uuid.New(), name, now)
		s.users.Set(u.ID, u)
		sess := &session{UserID: u.ID, CreatedAt: now, Name: u.Name}
		cookie := sess.toCookie()
		ctx, err := newContextBuilder(r.Context()).build()
		if err != nil {
			return nil, err
		}
		logger.Info.Printf("%s user_id=%s user_name=%s user_registered %s", ctx, u.ID, name, r.UserAgent())
		if shouldChangeName {
			redirectTo = fmt.Sprintf("/users/profile?%s=%s", retPathKey, redirectTo)
		}
		return httpx.Redirect(redirectTo).
			SetCookie(cookie).
			SetCookie(newLastName(now, name)), nil
	}
	return httpx.Redirect(redirectTo), nil
}

func (s *Server) index(r *http.Request) (*httpx.Response, error) {
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

	return httpx.RenderFile(http.StatusOK, filepath.Join(s.templatesPath, "web/index.html"), m{
		"Username": username,
		"Version":  version.JSVersion(),
	}, emptySess)
}

// LoadState loads this server state
func (s *Server) LoadState() error { return s.state.load(s.users, s.tables) }

// SaveState saves this server state
func (s *Server) SaveState() error { return s.state.save(s.users, s.tables) }

// New returns new instance of this app http server
func New() *Server {
	return &Server{
		templatesPath: ".",

		state: NewStateFile(statePath),

		tables: poker.NewTableMapSyncronized(),
		users:  poker.NewUserMapSyncronized(),

		now: func() time.Time { return time.Now() },
	}
}
