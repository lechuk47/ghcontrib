package internal

import (
	"encoding/json"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/google/go-github/v32/github"
	"github.com/gorilla/mux"
	"github.com/jpiriz/ghcontrib/pkg/githubclient"
	"github.com/sirupsen/logrus"
)

type App struct {
	listenAddr string
	ghClient   *githubclient.Client
	rqMutex    *sync.Mutex
}

func NewApp(listenAddr string, ghClient *githubclient.Client) App {
	return App{
		listenAddr: listenAddr,
		ghClient:   ghClient,
	}
}

//StartServer starts the Server
func (app App) StartServer() {
	r := mux.NewRouter().StrictSlash(false)
	r.HandleFunc("/top/{location}", app.topContributorsHandler)
	r.NotFoundHandler = http.HandlerFunc(usage)
	srv := &http.Server{
		Handler:      r,
		Addr:         app.listenAddr,
		WriteTimeout: 60 * time.Second,
		ReadTimeout:  60 * time.Second,
	}
	logrus.Fatal(srv.ListenAndServe())
}

// Just prints the available endpoints
func usage(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode("/top/{location}?items=10")
}

// Handler that executes the topContributors function
func (app *App) topContributorsHandler(w http.ResponseWriter, r *http.Request) {
	logrus.Info("Serving topContributors Request")
	ctx := r.Context()
	select {
	case <-ctx.Done():
		logrus.Debug("topContributorsHandler Context canceled")
	default:
		location := mux.Vars(r)["location"]
		items, err := strconv.Atoi(r.URL.Query().Get("items"))
		if err != nil {
			items = 10
		} else if items > 100 { //Hard limit the users
			items = 100
		}

		users := make([]*github.User, 0)
		if users, err = app.ghClient.GetUsersByLocation(ctx, location, items); err != nil {
			if serr, ok := err.(*github.RateLimitError); ok {
				http.Error(w, serr.Error(), http.StatusTooManyRequests)
			} else {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
		} else {
			json.NewEncoder(w).Encode(users)
		}
	}
}
