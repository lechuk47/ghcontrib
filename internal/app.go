package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/go-github/v32/github"
	"github.com/gorilla/mux"
	"github.com/jpiriz/ghcontrib/pkg/cache"
	"github.com/jpiriz/ghcontrib/pkg/githubclient"
	"github.com/sirupsen/logrus"
)

const (
	MaxItems         = 100
	KeyUsersNotFound = "NoUsersFound"
)

//Error type to control when a cache has a key with no users associated
type LocationNotFoundError struct {
	location string
}

func (e LocationNotFoundError) Error() string {
	return fmt.Sprintf("No users found with %s location", e.location)
}

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

// There is a corner case when a location has no users in github
// In that case a key with a special value is stored to prevent issue api requests
// for non-existent locations
func (app App) getCacheItems(ctx context.Context, key string, items int) ([]*github.User, error) {
	cUsers := make([]*github.User, 0)
	if isCached, err := app.cache.Exists(ctx, key); err != nil {
		logrus.Debug("Error getting key from the cache")
		return nil, err
	} else {
		if isCached > 0 {
			if users, err := app.cache.GetRange(ctx, key, MaxItems); err != nil {
				if k, err := app.cache.GetKey(ctx, key); err == nil && k == KeyUsersNotFound {
					logrus.Debug("Cache Key exists, location with no users")
					return cUsers, LocationNotFoundError{key}
				} else {
					logrus.Debug("Error getting data from the cache")
					logrus.Error(err)
					return nil, err
				}
			} else {
				// this could be improved
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

// If users is empty, set a object instead of a list, getCacheItems is aware of this case
// is not possible to add an empty list to redis with LPush
func (app App) setCacheItems(ctx context.Context, key string, users []*github.User) error {
	if len(users) > 0 {
		stringItems := make([]string, 0)
		for _, u := range users {
			s, _ := json.Marshal(u)
			stringItems = append(stringItems, string(s))
		}
		if err := app.cache.Push(ctx, app.cacheObjTTL, key, stringItems...); err != nil {
			return err
		} else {
			return nil
		}
	} else {
		logrus.Debug("Location jas no users, set a special cache key to control it")
		if err := app.cache.SetKey(ctx, app.cacheObjTTL, key, KeyUsersNotFound); err != nil {
			return err
		} else {
			return nil
		}
	}
}

func (app *App) getUsersFromCache(ctx context.Context, location string, items int) ([]*github.User, bool, bool) {
	var cacheHit bool
	var cacheDisabled bool
	users, err := app.getCacheItems(ctx, location, items)
	if err != nil {
		err, ok := err.(LocationNotFoundError)
		if ok {
			logrus.Debug(err)
			logrus.WithField("key", location).Info("Cache Hit with no users")
			cacheHit = true
		} else {
			logrus.Debug("Error using cache, disabling it")
			logrus.Error(err)
			cacheDisabled = true
		}
	} else {
		if len(users) >= items {
			logrus.WithField("key", location).Info("Cache Hit")
			cacheHit = true
		} else {
			logrus.WithField("key", location).Info("Cache Miss")
		}
	}
	return users, cacheHit, cacheDisabled
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
		} else if items > MaxItems { //Hard limit the users
			items = MaxItems
		}
		var users = make([]*github.User, 0)
		cacheDisabled := false
		cacheHit := false
		cacheKey := strings.ToUpper(location)

		//[1] Get Data form the cache
		//users, err = app.getCacheItems(ctx, location, items)
		users, cacheHit, cacheDisabled = app.getUsersFromCache(ctx, location, items)
		if cacheHit == false {
			// If system is under RateLimit, return
			if ok := app.ghClient.CheckRateLimit(); ok {
				logrus.Debug("RateLimitError Set, Discarting API Requests until RateLimit expiration")
				logrus.Error(app.ghClient.GetRateLimitError())
				http.Error(w, app.ghClient.GetRateLimitError().Error(), http.StatusTooManyRequests)
				return
			}

			if cacheDisabled == false {
				// Set Cache Distributed Lock
				if err = app.cache.SetLock(ctx, "mutex-"+cacheKey); err == nil {
					logrus.Debug("Cache Distributed lock acquired")
					defer app.releaseCacheLock("mutex-" + cacheKey)
				}

				// Get data from the cache
				// Another goroutine might has set the data
				users, cacheHit, cacheDisabled = app.getUsersFromCache(ctx, location, items)
			}

			if cacheHit == false { // Get users from the Github API
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
				if err = app.setCacheItems(ctx, location, users); err != nil {
					logrus.Debug("Error Setting cache value")
				}
			}

			// Encode users
			if len(users) > 0 {
				sort.SliceStable(users, func(i, j int) bool {
					return *(users)[i].PublicRepos > *(users)[j].PublicRepos
				})
			}
			if items <= len(users) {
				json.NewEncoder(w).Encode(users[:items])
			} else {
				json.NewEncoder(w).Encode(users)
			}
		}
	}
}
