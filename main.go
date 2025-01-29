package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/nchern/vpoker/pkg/httpx"
	"github.com/nchern/vpoker/pkg/logger"
	"github.com/nchern/vpoker/pkg/models"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

const (
	maxPlayers      = 3
	cookieExpiresAt = 5 * 24 * time.Hour
)

var (
	index      = template.Must(template.ParseFiles("web/index.html"))
	pokerTable = template.Must(template.ParseFiles("web/poker.html"))
	profile    = template.Must(template.ParseFiles("web/profile.html"))
)

type m map[string]any

type session struct {
	UserID    uuid.UUID `json:"user_id"`
	CreatedAt time.Time `json:"created_at"`
	Name      string    `json:"name"`

	user *models.User `json:"-"`
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
	ctx  context.Context
	room *models.Room
	user *models.User
}

func (c *Context) String() string {
	fields := []string{fmt.Sprintf("request_id=%s", httpx.RequestID(c.ctx))}
	if c.user != nil {
		fields = append(fields, "user_name="+c.user.Name)
	}
	if c.room != nil {
		fields = append(fields, "room_id="+c.room.ID.String())
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

func (b *contextBuilder) withRoom(s *server, r *http.Request, idParam string) *contextBuilder {
	if b.err != nil {
		return b
	}
	id := mux.Vars(r)[idParam]
	roomID, err := uuid.Parse(id)
	if err != nil {
		logger.Error.Println("bad uuid=" + id)
		b.err = httpx.NewError(http.StatusBadRequest, "bad id: "+id)
		return b
	}
	room, found := s.rooms.Get(roomID)
	if !found {
		b.err = httpx.NewError(http.StatusBadRequest, "room not found")
		return b
	}
	b.ctx.room = room
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

func sanitizeRetpath(s string) string {
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

type server struct {
	endpoint string

	rooms models.RoomMap
	users models.UserMap
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

var errChanClosed = errors.New("channel closed")

func handlePush(ctx *Context, conn *websocket.Conn, update models.Event) error {
	logger.Debug.Printf("ws %s push_begin: %s", ctx, update)
	switch update {
	case "":
		// channel closed, teminating this update loop
		logger.Info.Printf("ws %s web socket connection terminated", ctx)
		if err := conn.WriteMessage(
			websocket.TextMessage, []byte("terminated by another connection")); err != nil {
			logger.Error.Printf("%s conn.WriteMessage %s", ctx, err)
		}
		return errChanClosed
	case models.UpdateAll:
		// logger.Debug.Printf("ws %s getRoomState.begin: %s", ctx, update)
		state, err := getRoomState(ctx.user, ctx.room)
		if err != nil {
			return fmt.Errorf("getRoomState: %w", err)
		}
		// logger.Debug.Printf("ws %s getRoomState.finish: %s", ctx, update)
		if err := conn.WriteJSON(state); err != nil {
			return fmt.Errorf("conn.WriteJSON: %w", err)
		}
	case models.Refresh:
		if err := conn.WriteMessage(websocket.TextMessage, []byte(update)); err != nil {
			return fmt.Errorf("conn.WriteMessage(Refresh): %w", err)
		}
	}
	logger.Debug.Printf("%s push_finished: %s", ctx, update)
	return nil
}

func (s *server) pushRoomUpdates(w http.ResponseWriter, r *http.Request) {
	// TODO: finalize channel properly. Now any error yields to deadlock.
	// IT IS NOT CLEAR HOW how to gracefully finalize channel on errors.
	// now it leads to race conditions when a new channel is created
	httpx.H(authenticated(s.users, func(r *http.Request) (*httpx.Response, error) {
		ctx, err := newContextBuilder(r.Context()).withUser(s, r).withRoom(s, r, "id").build()
		if err != nil {
			return nil, err
		}
		updates := make(chan models.Event)
		var p *models.Player
		if err := ctx.room.Update(func(rm *models.Room) error {
			p = rm.Players[ctx.user.ID]
			if p == nil {
				return httpx.NewError(http.StatusForbidden, "you are not in the room")
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
			select {
			case update := <-updates:
				if err := handlePush(ctx, conn, update); err != nil {
					if errors.Is(err, errChanClosed) {
						return nil, err // terminate the loop only if channel got closed
					}
					logger.Error.Printf("ws %s %s", ctx, err) // continue listening
				}
			case <-time.After(15 * time.Second): // check state periodically
				// logger.Debug.Printf("user_id=%s user_name=%s websocket_ping", ctx.user.ID, ctx.user.Name)
				if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					logger.Error.Printf("ws %s %s", ctx, err)
					// decide how to unsubscribe - race conditions
					// unable to write - close this connection
					// return nil, fmt.Errorf("websocket_ping write error: %w", err)
				}
				// no need to read - browser does not automatically send a response
			}
		}
	}))(w, r)
}

func (s *server) shuffle(r *http.Request) (*httpx.Response, error) {
	ctx, err := newContextBuilder(r.Context()).withUser(s, r).withRoom(s, r, "id").build()
	if err != nil {
		return nil, err
	}
	var notifyThem models.PlayerList
	if err := ctx.room.Update(func(rm *models.Room) error {
		if rm.Players[ctx.user.ID] == nil {
			return httpx.NewError(http.StatusForbidden, "you are not in the room")
		}
		ctx.room.Shuffle()
		for _, p := range rm.Players {
			if p.ID == ctx.user.ID {
				continue // do not update yourseld
			}
			notifyThem = append(notifyThem, p)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	// notify others
	// push updates: potentially long operation - check
	notifyThem.NotifyAll(models.Refresh)
	return httpx.Redirect(fmt.Sprintf("/rooms/%s", ctx.room.ID)), nil
}

func (s *server) takeCard(r *http.Request) (*httpx.Response, error) {
	ctx, err := newContextBuilder(r.Context()).withUser(s, r).withRoom(s, r, "id").build()
	if err != nil {
		return nil, err
	}
	curUser, room := ctx.user, ctx.room
	var updated models.TableItem
	var notifyThem models.PlayerList
	if err := room.Update(func(rm *models.Room) error {
		if rm.Players[curUser.ID] == nil {
			return httpx.NewError(http.StatusForbidden, "you are not in the room")
		}
		req := map[string]int{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			return err
		}
		id, found := req["id"]
		if !found {
			return nil
		}
		item := rm.Items.Get(id)
		if item == nil {
			return httpx.NewError(http.StatusBadRequest, "bad item id")
		}
		// only cards can be taken
		if !item.Is(models.CardClass) {
			return nil
		}
		if item.IsOwned() {
			return nil // already taken
		}
		item.OwnerID = curUser.ID.String()
		updated = *item
		for _, p := range rm.Players {
			if p.ID == curUser.ID {
				continue // do not update yourseld
			}
			notifyThem = append(notifyThem, p)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	updated.Side = models.Face
	// push updates: potentially long operation - check
	notifyThem.NotifyAll(models.UpdateAll)
	return httpx.JSON(http.StatusOK, map[string]*models.TableItem{"updated": &updated}), nil
}

func (s *server) profile(r *http.Request) (*httpx.Response, error) {
	sess, err := getUserFromSession(r, s.users)
	if err != nil {
		return nil, err
	}
	ctx := newContextBuilder(r.Context()).withUser(s, r)
	logger.Info.Printf("%s", ctx)
	return httpx.Render(http.StatusOK, profile, m{
		"Retpath":  sanitizeRetpath(r.URL.Query().Get("ret_path")),
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
	if name == "" {
		return httpx.String(http.StatusBadRequest, "empty name"), nil
	}
	if err := s.users.Update(sess.user.ID, func(u *models.User) error {
		u.Name = name
		sess.user = u
		return nil
	}); err != nil {
		return nil, err
	}
	lastNameCookie := newLastName(time.Now(), name)
	if retPath := sanitizeRetpath(r.URL.Query().Get("ret_path")); retPath != "" {
		return httpx.Redirect(retPath).SetCookie(lastNameCookie), nil
	}
	return httpx.Render(
		http.StatusOK,
		profile,
		m{"Username": sess.user.Name},
		lastNameCookie)
}

func updateRoom(ctx *Context, r *http.Request) error {
	curUser, room := ctx.user, ctx.room
	if room.Players[curUser.ID] == nil {
		return httpx.NewError(http.StatusForbidden, "you are not in the room")
	}
	b, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}
	logger.Debug.Printf("update %s %s", ctx, string(b))
	var src models.TableItem
	if err := json.Unmarshal(b, &src); err != nil {
		return err
	}
	dest := room.Items.Get(src.ID)
	if dest == nil {
		return httpx.NewError(http.StatusBadRequest, "bad item id")
	}
	if dest.Class != src.Class {
		return httpx.NewError(http.StatusBadRequest, "attempt to update readonly field .Class")
	}
	dest.X = src.X
	dest.Y = src.Y
	dest.Side = src.Side
	return nil
}

func (s *server) updateRoom(r *http.Request) (*httpx.Response, error) {
	ctx, err := newContextBuilder(r.Context()).withUser(s, r).withRoom(s, r, "id").build()
	if err != nil {
		return nil, err
	}
	curUser, room := ctx.user, ctx.room
	var notifyThem models.PlayerList
	if err := room.Update(func(rm *models.Room) error {
		if err := updateRoom(ctx, r); err != nil {
			return err
		}
		// collect players to push updates to
		// push itself must happen outside room lock in order to avoid deadlocks
		for _, p := range room.Players {
			if p.ID == curUser.ID {
				continue // do not update yourseld
			}
			notifyThem = append(notifyThem, p)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	// push updates: potentially long operation - check
	notifyThem.NotifyAll(models.UpdateAll)
	return httpx.JSON(http.StatusOK, m{}), nil
}

func (s *server) joinRoom(r *http.Request) (*httpx.Response, error) {
	ctx, err := newContextBuilder(r.Context()).withUser(s, r).withRoom(s, r, "id").build()
	if err != nil {
		return nil, err
	}
	var notifyThem models.PlayerList
	if err := ctx.room.Update(func(rm *models.Room) error {
		hasJoined := rm.Players[ctx.user.ID] != nil
		if hasJoined {
			return nil
		}
		logger.Debug.Printf("players_joind=%d", len(rm.Players))
		if len(rm.Players) > maxPlayers {
			return httpx.NewError(http.StatusForbidden, "this room is full")
		}
		rm.Join(ctx.user)
		for _, p := range rm.Players {
			if p.ID == ctx.user.ID {
				continue // do not update yourseld
			}
			notifyThem = append(notifyThem, p)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	// push updates: potentially long operation - check
	// notifyThem.NotifyAll(models.PlayerJoined)
	notifyThem.NotifyAll(models.UpdateAll)
	return httpx.Redirect(fmt.Sprintf("/rooms/%s", ctx.room.ID)), nil
}

func (s *server) renderRoom(r *http.Request) (*httpx.Response, error) {
	ctx, err := newContextBuilder(r.Context()).withUser(s, r).withRoom(s, r, "id").build()
	if err != nil {
		return nil, err
	}
	curUser, room := ctx.user, ctx.room
	errRedirect := errors.New("redirect")
	players := []*models.Player{}
	if err := room.ReadLock(func(rm *models.Room) error {
		if rm.Players[curUser.ID] == nil {
			return errRedirect
		}
		for _, v := range rm.Players {
			players = append(players, v)
		}
		return nil
	}); err != nil {
		if err == errRedirect {
			return httpx.Redirect(fmt.Sprintf("/rooms/%s/join", room.ID)), nil
		}
		return nil, err
	}
	return httpx.Render(http.StatusOK, pokerTable, m{
		"Players": players,
		// "Retpath":  fmt.Sprintf("/rooms/%s", room.ID),
		"RoomID":   room.ID,
		"Username": curUser.Name,
	})
}

func getRoomState(curUser *models.User, room *models.Room) (*models.Room, error) {
	var roomCopy *models.Room
	if err := room.ReadLock(func(rm *models.Room) error {
		if rm.Players[curUser.ID] == nil {
			return httpx.NewError(http.StatusForbidden, "you are not in the room")
		}
		var err error
		// deep copy the current room - items must be modified
		// as their content differes for different users due to ownership
		roomCopy, err = rm.DeepCopy()
		return err
	}); err != nil {
		return nil, err
	}
	for _, it := range roomCopy.Items {
		if it.IsOwnedBy(curUser.ID) && it.Is(models.CardClass) {
			it.Side = models.Face // owners always see their cards
		}
		isOwnedBySomeoneElse := it.IsOwned() && !it.IsOwnedBy(curUser.ID)
		if isOwnedBySomeoneElse && it.Is(models.CardClass) {
			it.Side = models.Cover // if a card is owned by someone, others always see their card cover
		}
	}
	return roomCopy, nil
}

func (s *server) roomState(r *http.Request) (*httpx.Response, error) {
	ctx, err := newContextBuilder(r.Context()).withUser(s, r).withRoom(s, r, "id").build()
	if err != nil {
		return nil, err
	}
	roomCopy, err := getRoomState(ctx.user, ctx.room)
	if err != nil {
		return nil, err
	}
	return httpx.JSON(http.StatusOK, roomCopy), nil
}

func (s *server) newRoom(r *http.Request) (*httpx.Response, error) {
	sess, err := getUserFromSession(r, s.users)
	if err != nil {
		return nil, err
	}
	curUser := sess.user
	logger.Info.Printf("user_id=%s action=room_created", curUser.ID)

	room := models.NewRoom(uuid.New(), 50).StartGame()
	s.rooms.Set(room.ID, room)
	room.Join(curUser)

	return httpx.Redirect(fmt.Sprintf("/rooms/%s", room.ID)), nil
}

func (s *server) newUser(r *http.Request) (*httpx.Response, error) {
	logger.Debug.Printf("newUser.begin user_count=%d", s.users.Len())
	sess := &session{}
	cookie, err := r.Cookie("session")
	if err != nil || cookie.Value == "" {
		// Cookie not found or empty: create and set a new one
		name := "Anon" + randomString()
		if ln, err := r.Cookie("last_name"); err == nil {
			if ln.Value != "" {
				name = ln.Value
			}
		}
		// old, err := getUserFromSession(r, s.fetchUser)
		// if err!=nil && old.{
		// }
		now := time.Now()
		u := &models.User{ID: uuid.New(), Name: name}
		s.users.Set(u.ID, u)
		sess = &session{UserID: u.ID, CreatedAt: now, Name: u.Name}
		cookie := newSessionCookie(now, sess.toCookie())
		logger.Info.Printf("user_registered user_id=%s name=%s", u.ID, u.Name)
		return httpx.Redirect("/").
			SetCookie(cookie).
			SetCookie(newLastName(now, name)), nil
	}
	logger.Info.Printf("newUser cookie=%s", cookie.Value)
	return httpx.String(http.StatusBadRequest,
		"non-empty cookies: clear cookies before registering a new user"), nil
}

func (s *server) index(r *http.Request) (*httpx.Response, error) {
	logger.Debug.Printf("index.begin user_count=%d", s.users.Len())
	sess := &session{}
	cookie, err := r.Cookie("session")
	if err != nil || cookie.Value == "" {
		// no session cookie
		return httpx.Redirect("/users/new"), nil
	}
	logger.Debug.Printf("cookie_exists: session=%s", cookie.Value)
	if err := sess.parseFromCookie(cookie.Value); err != nil {
		logger.Info.Printf("bad cookie: %s", err)
		return httpx.Redirect("/users/new").
			SetCookie(newEmptySession()), nil
	}
	curUser, found := s.users.Get(sess.UserID)
	if !found {
		return httpx.Redirect("/users/new").
			SetCookie(newEmptySession()), nil
	}
	logger.Info.Printf("%s / session=%s user_id=%s", r.Method, cookie.Value, curUser.ID)

	return httpx.Render(http.StatusOK, index, m{
		"Username": curUser.Name,
	})
}

func getUserFromSession(r *http.Request, users models.UserMap) (*session, error) {
	sess := &session{}
	cookie, err := r.Cookie("session")
	if err != nil {
		return sess, nil // no cookie - return new empty session
	}
	if err := sess.parseFromCookie(cookie.Value); err != nil {
		return nil, err
	}
	u, found := users.Get(sess.UserID)
	if found {
		sess.user = u
	}
	return sess, nil
}

func authenticated(users models.UserMap, f httpx.RequestHandler) httpx.RequestHandler {
	return func(r *http.Request) (*httpx.Response, error) {
		sess, err := getUserFromSession(r, users)
		if err != nil {
			return nil, err
		}
		if sess.user == nil {
			return httpx.String(http.StatusUnauthorized), nil
		}
		return f(r)
	}
}

// TODO: decide what to do with abandoned rooms
// TODO_FEAT: open owned cards by the owner
// TODO_FEAT: redirect back when non-regged enters the room
func main() {
	s := &server{
		endpoint: ":8080",
		rooms:    models.NewRoomMapSyncronized(),
		users:    models.NewUserMapSyncronized(),
	}
	auth := func(f httpx.RequestHandler) httpx.RequestHandler {
		return authenticated(s.users, f)
	}
	redirectIfNoAuth := func(url string, f httpx.RequestHandler) httpx.RequestHandler {
		return func(r *http.Request) (*httpx.Response, error) {
			resp, err := auth(f)(r)
			if err != nil {
				return nil, err
			}
			if resp.Code() == http.StatusUnauthorized {
				return httpx.Redirect(url), nil
			}
			return resp, nil
		}
	}

	r := mux.NewRouter()
	r.HandleFunc("/", httpx.H(s.index)).Methods("GET")
	r.HandleFunc("/rooms/new", httpx.H(auth(s.newRoom)))
	r.HandleFunc("/rooms/{id:[a-z0-9-]+}",
		httpx.H(redirectIfNoAuth("/", s.renderRoom))).Methods("GET")
	r.HandleFunc("/rooms/{id:[a-z0-9-]+}/state",
		httpx.H(auth(s.roomState))).Methods("GET")
	r.HandleFunc("/rooms/{id:[a-z0-9-]+}/join",
		httpx.H(auth(s.joinRoom))).Methods("GET")
	r.HandleFunc("/rooms/{id:[a-z0-9-]+}/update",
		httpx.H(auth(s.updateRoom))).Methods("POST")
	r.HandleFunc("/rooms/{id:[a-z0-9-]+}/take_card",
		httpx.H(auth(s.takeCard))).Methods("POST")
	r.HandleFunc("/rooms/{id:[a-z0-9-]+}/listen",
		s.pushRoomUpdates).Methods("GET")
	r.HandleFunc("/rooms/{id:[a-z0-9-]+}/shuffle",
		httpx.H(auth(s.shuffle))).Methods("GET")

	r.HandleFunc("/users/new", httpx.H(s.newUser))
	r.HandleFunc("/users/profile",
		httpx.H(auth(s.profile))).
		Methods("GET")
	r.HandleFunc("/users/profile",
		httpx.H(auth(s.updateProfile))).
		Methods("POST")

	http.Handle("/", r)

	http.Handle("/static/",
		http.StripPrefix("/static/", http.FileServer(http.Dir("./web/static"))))

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
