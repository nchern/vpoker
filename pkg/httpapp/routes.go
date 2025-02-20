package httpapp

import (
	"fmt"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/nchern/vpoker/pkg/httpx"
	"github.com/nchern/vpoker/pkg/poker"
)

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

// BindRoutes sets routes and returns a router
func BindRoutes(s *Server) *mux.Router {
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
	r.HandleFunc("/games/{id:[a-z0-9-]+}/update_many",
		httpx.H(auth(tableHandler(s.updateMany)))).Methods("POST")
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
	return r
}
