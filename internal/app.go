package internal

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/google/go-github/v32/github"
	"github.com/gorilla/mux"
	"github.com/jpiriz/ghcontrib/pkg/cache"
	"github.com/jpiriz/ghcontrib/pkg/githubclient"
	"github.com/sirupsen/logrus"
)

type App struct {
	listenAddr string
	ghClient   *githubclient.Client
	cache      cache.Cache
}

//NewApp returns a App
func NewApp(listenAddr string, ghClient *githubclient.Client, cache cache.Cache) App {
	return App{
		listenAddr: listenAddr,
		ghClient:   ghClient,
		cache:      cache,
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

		var getFromAPI = false
		var cacheError = false
		var users = make([]*github.User, 0)

		if users, err = app.cache.GetKey(ctx, location); err == nil {
			logrus.WithField("key", location).Info("Cache Hit")
			json.NewEncoder(w).Encode(users)
		} else if err == redis.Nil {

			logrus.WithField("key", location).Info("Cache Miss")
			if err = app.cache.SetLock(ctx, "mutex-"+location); err != nil {
				//Lock Set
				defer func() {
					logrus.Debug("Releasing Cache Lock")
					app.cache.ReleaseLock("mutex-" + location)
				}()
			}
			// We got the Lock, but maybe another thread has set the cache before
			if users, err = app.cache.GetKey(ctx, location); err != nil {
				getFromAPI = true
				if err != redis.Nil {
					logrus.Debug("CacheError; Disable the cache for this request")
					logrus.Error(err)
					cacheError = true
				}
			} else {
				logrus.Debug("Data got from the cache after lock")
			}

			if getFromAPI {
				if users, err = app.ghClient.GetUsersByLocation(ctx, location, 100); err != nil {
					if serr, ok := err.(*github.RateLimitError); ok {
						http.Error(w, serr.Error(), http.StatusTooManyRequests)
					} else {
						http.Error(w, err.Error(), http.StatusInternalServerError)
					}
				} else {
					if cacheError == false {
						logrus.Debug("Setting cache value")
						if err := app.cache.SetKey(ctx, location, users, 300*time.Second); err != nil {
							logrus.Error(err)
						}
					}
				}
			}
			// Encode users
			json.NewEncoder(w).Encode(users)
		}
	}
}
