package internal

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/google/go-github/v32/github"
	"github.com/gorilla/mux"
	"github.com/jpiriz/ghcontrib/pkg/cache"
	"github.com/jpiriz/ghcontrib/pkg/githubclient"
	"github.com/sirupsen/logrus"
)

const (
	MAX_ITEMS = 100
)

type App struct {
	listenAddr  string
	ghClient    *githubclient.Client
	cache       cache.Cache
	cacheObjTTL time.Duration
}

//NewApp returns a App
func NewApp(listenAddr string, ghClient *githubclient.Client, cache cache.Cache, objTTL time.Duration) App {
	return App{
		listenAddr:  listenAddr,
		ghClient:    ghClient,
		cache:       cache,
		cacheObjTTL: objTTL,
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

func (app *App) releaseCacheLock(key string) {
	logrus.WithField("key", key).Debug("Releasing Cache Lock")
	err := app.cache.ReleaseLock(key)
	if err != nil {
		logrus.Debug("Error Releaseing cache lock")
		logrus.Error(err)
	} else {
		logrus.Debug("Cache lock Released")
	}
}

func (app App) getCacheItems(ctx context.Context, location string, items int) ([]*github.User, error) {
	cUsers := make([]*github.User, 0)
	if isCached, err := app.cache.Exists(ctx, location); err != nil {
		logrus.Debug("Error getting key from the cache")
		return nil, err
	} else {
		if isCached > 0 {
			if users, err := app.cache.GetRange(ctx, location, MAX_ITEMS); err != nil {
				logrus.Debug("Error getting range from the cache")
				return nil, err
			} else {
				for _, u := range users {
					var uo github.User
					json.Unmarshal([]byte(u), &uo)
					cUsers = append(cUsers, &uo)
				}
				return cUsers, err
			}
		} else {
			logrus.Debug("Key does not exist in the cache")
			return cUsers, nil
		}
	}
}

func (app App) setCache(ctx context.Context, location string, users []*github.User) error {
	stringItems := make([]string, 0)
	for _, u := range users {
		s, _ := json.Marshal(u)
		stringItems = append(stringItems, string(s))
	}
	if err := app.cache.Push(ctx, app.cacheObjTTL, location, stringItems...); err != nil {
		return err
	} else {
		return nil
	}
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
		} else if items > MAX_ITEMS { //Hard limit the users
			items = MAX_ITEMS
		}
		var users = make([]*github.User, 0)
		cacheDisabled := false
		cacheHit := false
		fetchFromGithub := false

		//[1] Get Data form the cache
		users, err = app.getCacheItems(ctx, location, items)
		if err != nil {
			logrus.Debug("Error using the cache; disabling cache usage")
			logrus.Error(err)
			cacheDisabled = true
			fetchFromGithub = true
		} else {
			if len(users) >= items {
				logrus.WithField("key", location).Info("Cache Hit")
				cacheHit = true
			}
		}

		if ok := app.ghClient.CheckRateLimit(); ok {
			logrus.Debug("RateLimitError Set, Discarting API Requests until RateLimit expiration")
			logrus.Error(app.ghClient.GetRateLimitError())
			http.Error(w, app.ghClient.GetRateLimitError().Error(), http.StatusTooManyRequests)
			return
		}
		//[2] If cache is Alive and we need to get data from Github:
		//    Get a cache lock to prevent other tasks to get the same data
		//    After get the lock, check the cache again
		if cacheDisabled == false && cacheHit == false {
			logrus.Debug("Cache Miss, Getting from the API")
			// Set Cache Distributed Lock
			if err = app.cache.SetLock(ctx, "mutex-"+location); err == nil {
				logrus.Debug("Cache Distributed lock acquired")
				defer app.releaseCacheLock("mutex-" + location)
				// We got the Lock, but maybe another thread has set the cache while this
				// Thread was waiting
				users, err = app.getCacheItems(ctx, location, items)
				if err != nil {
					logrus.Debug("Error using the cache")
					logrus.Error(err)
					cacheDisabled = true
					fetchFromGithub = true
				} else {
					if len(users) >= items {
						logrus.WithField("key", location).Info("Cache Hit")
					} else {
						fetchFromGithub = true
					}
				}
			} else {
				users, err = app.getCacheItems(ctx, location, items)
				if err != nil {
					logrus.Debug("Error using the cache")
					logrus.Error(err)
				} else {
					if len(users) >= items {
						logrus.WithField("key", location).Info("Cache Hit After waiting to cache lock")
					}
				}
			}
		}

		if fetchFromGithub == true { // Get users from the Github API
			if users, err = app.ghClient.GetUsersByLocation(ctx, location, items); err != nil {
				if serr, ok := err.(*github.RateLimitError); ok {
					http.Error(w, serr.Error(), http.StatusTooManyRequests)
				} else {
					http.Error(w, err.Error(), http.StatusInternalServerError)
				}
				return
			}
		}

		if cacheDisabled == false && cacheHit == false {
			logrus.Debug("Setting cache value")
			if err = app.setCache(ctx, location, users); err != nil {
				logrus.Debug("Error Setting cache value")
			}
		}

		// Encode users
		if len(users) > 0 {
			sort.SliceStable(users, func(i, j int) bool {
				return *(users)[i].PublicRepos > *(users)[j].PublicRepos
			})
		} else {
			logrus.Debug("Returning no users")
		}
		if items <= len(users) {
			json.NewEncoder(w).Encode(users[:items])
		} else {
			json.NewEncoder(w).Encode(users)
		}
	}
}
