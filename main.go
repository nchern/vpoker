package main

import (
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nchern/vpoker/pkg/httpapp"
	"github.com/nchern/vpoker/pkg/logger"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

func handleSignalsLoop(srv *httpapp.Server) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)

	s := <-signals
	logger.Info.Printf("received: %s; saving state...", s)
	if err := srv.SaveState(); err != nil {
		logger.Error.Printf("server.saveState %s", err)
	}
	if s == syscall.SIGTERM || s == os.Interrupt {
		logger.Info.Printf("graceful shutdown: %s", s)
		os.Exit(0)
	}
	os.Exit(1)
}

func saveStateLoop(s *httpapp.Server) {
	const saveStateEvery = 10 * time.Second
	for range time.Tick(saveStateEvery) {
		if err := s.SaveState(); err != nil {
			logger.Error.Printf("saveStateLoop: %s", err)
		}
	}
}

// TODO_TECHDEBT: test new user handler {"/users/new", "GET"}
// TODO_TECHDEBT: test {"/games/%s/kick", "POST"},
// TODO_FEAT: move multiple chips on mobile devices: handle long taps
// TODO_FEAT: handle 4 players
// TODO_TECHDEBT: move images in a separate subfolder /img/
// TODO_TECHDEBT: introduce Table.UpdateBy(userID, ...)
// TODO: add metrics
// TODO: connect metrics to Grafana
// TODO: decide what to do with abandoned tables. Now they not only stay in memory but also
// keep websocket groutines/channels until browser window is not closed
func main() {
	s := httpapp.New()

	if err := s.LoadState(); err != nil {
		logger.Error.Printf("server.loadState %s", err)
	}

	go saveStateLoop(s)
	go handleSignalsLoop(s)

	http.Handle("/", httpapp.BindRoutes(s))
	// handle static files
	http.Handle("/robots.txt",
		http.StripPrefix("/", http.FileServer(http.Dir("./web/"))))
	http.Handle("/static/",
		http.StripPrefix("/static/", http.FileServer(http.Dir("./web/static"))))

	const endpoint = ":8080"
	logger.Info.Printf("Start listening on %s", endpoint)
	must(http.ListenAndServe(endpoint, nil))
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
