package internal

import (
	"context"
	"encoding/json"
	"errors"
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
	listenAddr string
	cache      *cache.RedisCache
	ghClient   *githubclient.Client
	ghworkers  chan bool
}

func NewApp(listenAddr string, cache *cache.RedisCache, ghClient *githubclient.Client) App {
	return App{
		listenAddr: listenAddr,
		cache:      cache,
		ghClient:   ghClient,
		ghworkers:  make(chan bool, 30), // 30 requests / minute
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
		WriteTimeout: 15 * time.Second,
		ReadTimeout:  15 * time.Second,
	}
	logrus.Fatal(srv.ListenAndServe())
}

// Just prints the available endpoints
func usage(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode("/top/{location}?items=10")
}

func (app App) topContributorsHandler(w http.ResponseWriter, r *http.Request) {
	location := mux.Vars(r)["location"]
	items, err := strconv.Atoi(r.URL.Query().Get("items"))
	if err != nil {
		items = 10
	}
	users, err := app.queryTopUsersByLocation(r.Context(), location, items)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	} else {
		json.NewEncoder(w).Encode(users)
	}
}

//queryTopUsersByLocation tries to get the requested location in the cache
//If not succeds it issues a github api request to get the data and sets it in the cache
func (app App) queryTopUsersByLocation(ctx context.Context, location string, items int) ([]*github.User, error) {
	var key = strings.ToUpper(location)
	users := make([]*github.User, 0)
	users, err := app.cache.GetKey(ctx, key)
	if err == redis.Nil {
		logrus.WithFields(logrus.Fields{
			"key": key,
		}).Info("Cache Miss")

		// What if there are a burst of the same requests at a time??
		// All requests query the API and feeds the cache
		res, err := app.runGithubAPIRequest(location)
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
func (app App) runGithubAPIRequest(location string) ([]*github.User, error) {
	logrus.Info("Getting data from github api")

	select {
	case app.ghworkers <- true:
		// do nothing
	default:
		return nil, errors.New("Github Rest API Workers limit reached; Try again in a few minutes")
	}
	var wg sync.WaitGroup
	wg.Add(1)
	var err error
	var res []*github.User

	go func(location string) {
		defer func() {
			wg.Done()
			<-app.ghworkers
		}()
		res, err = app.ghClient.GetUsersByLocation(location)
	}(location)

	wg.Wait()
	if err != nil {
		return nil, err
	} else {
		return res, nil
	}
}
