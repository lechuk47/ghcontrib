package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/google/go-github/v32/github"
	"github.com/gorilla/mux"
	"github.com/jpiriz/ghcontrib/pkg/cache"
	"github.com/jpiriz/ghcontrib/pkg/githubclient"
	"github.com/sirupsen/logrus"
)

type App struct {
	listenAddr     string
	cache          *cache.RedisCache
	ghClient       *githubclient.Client
	ghworkers      chan bool
	runningQueries []string
	rqMutex        *sync.Mutex
}

func NewApp(listenAddr string, cache *cache.RedisCache, ghClient *githubclient.Client) App {
	return App{
		listenAddr: listenAddr,
		cache:      cache,
		ghClient:   ghClient,
		rqMutex:    &sync.Mutex{},
		ghworkers:  make(chan bool, 30), // 30 requests / minute
	}
}

//StartServer starts the Server
func (app App) StartServer() {
	r := mux.NewRouter().StrictSlash(false)
	r.HandleFunc("/top/{location}", app.topContributorsHandler)
	//r.HandleFunc("/top/{location}", app.testHandler)
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

type beingCachedError struct {
	key string
}

func (e beingCachedError) Error() string {
	return fmt.Sprintf("%s is Being cached", e.key)
}

func retry(attempts int, f func() ([]*github.User, error)) ([]*github.User, error) {
	users, err := f()
	if err != nil {
		if err != err.(*beingCachedError) {
			return nil, err
		} else {
			if attempts--; attempts > 0 {
				logrus.Debug("Retrying")
				time.Sleep(3 * time.Second)
				return retry(attempts, f)
			}
			return nil, err
		}
	} else {
		return users, nil
	}
}

func (app App) testHandler(w http.ResponseWriter, r *http.Request) {
	app.rqMutex.Lock()
	fmt.Println("Locked")
	time.Sleep(5 * time.Second)
	app.rqMutex.Unlock()
	fmt.Println("HI")
}

func (app *App) topContributorsHandler(w http.ResponseWriter, r *http.Request) {
	location := mux.Vars(r)["location"]
	items, err := strconv.Atoi(r.URL.Query().Get("items"))
	if err != nil {
		items = 10
	}

	users, err := retry(10, func() ([]*github.User, error) {
		users, err := app.queryTopUsersByLocation(r.Context(), location, items)
		if err != nil {
			return nil, err
		} else {
			return users, nil
		}
	})

	if _, ok := err.(*beingCachedError); ok {
		logrus.Debug("Key is being cached too long...")
	} else if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	} else {
		json.NewEncoder(w).Encode(users)
	}
}

func (app App) keyInSlice(key string) bool {
	for _, b := range app.runningQueries {
		if b == key {
			return true
		}
	}
	return false
}

func (app *App) removeFromSlice(key string) {
	var newslice []string
	for _, b := range app.runningQueries {
		if b != key {
			newslice = append(newslice, key)
		}
	}
	app.runningQueries = newslice
}

//queryTopUsersByLocation tries to get the requested location in the cache
//If not succeds it issues a github api request to get the data and sets it in the cache
func (app *App) queryTopUsersByLocation(ctx context.Context, location string, items int) ([]*github.User, error) {
	var key = strings.ToUpper(location)
	users := make([]*github.User, 0)

	users, err := app.cache.GetKey(ctx, key)
	if err == redis.Nil {
		logrus.WithFields(logrus.Fields{
			"key": key,
		}).Info("Cache Miss")

		// Prevent a burst of the same query
		app.rqMutex.Lock()
		logrus.Debug("Inside lock")
		if app.keyInSlice(location) {
			logrus.Debug("location exists in lock")
			app.rqMutex.Unlock()
			return nil, &beingCachedError{key: location}
		} else {
			logrus.Debug("Set location in lock")
			app.runningQueries = append(app.runningQueries, location)
			app.rqMutex.Unlock()
			defer func() {
				logrus.Debug("Removing key")
				app.removeFromSlice(location)
			}()

		}

		// What if there are a burst of the same requests at a time??
		// All requests query the API and feeds the cache
		res, err := app.runGithubAPIRequest(ctx, location)
		if err != nil {
			return nil, err
		} else {
			users = append(users, res...)
		}
		err = app.cache.SetKey(ctx, key, users, 3600*time.Second)
		if err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	} else {
		logrus.WithFields(logrus.Fields{
			"key": key,
		}).Info("Cache Hit")
	}

	if len(users) < items {
		items = len(users)
	}
	return users[:items], nil
}

// This function controls the concurrent invocations to the Github Search Api
// Github has a rate limit of 30 requests per minute so we limit the channel to 30
// If there are no positions left, the function returns an error to the user
func (app App) runGithubAPIRequest(ctx context.Context, location string) ([]*github.User, error) {
	logrus.Info("Getting data from github api")
	res, err := app.ghClient.GetUsersByLocation(ctx, location)
	if err != nil {
		return nil, err
	} else {
		return res, nil
	}
}
