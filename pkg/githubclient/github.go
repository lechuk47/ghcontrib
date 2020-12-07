package githubclient

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/go-github/v32/github"
	"github.com/sirupsen/logrus"
	"golang.org/x/oauth2"
)

//Client is a struct to hold the Client
type Client struct {
	ctx            context.Context
	clientRest     *github.Client
	rateLimitError *github.RateLimitError
	rateLimitMutex sync.Mutex
}

//NewClient returns a github client
func NewClient(ctx context.Context, token string) *Client {
	var clientRest *github.Client
	if token != "" {
		ts := oauth2.StaticTokenSource(
			&oauth2.Token{AccessToken: token},
		)
		auth := oauth2.NewClient(ctx, ts)
		clientRest = github.NewClient(auth)
	} else {
		clientRest = github.NewClient(nil)

	}

	return &Client{
		ctx:        ctx,
		clientRest: clientRest,
	}
}

//Checks if a Github API RateLimit is Active
func (gh *Client) CheckRateLimit() bool {
	if gh.rateLimitError != nil {
		now := time.Now()
		waitSecs := gh.rateLimitError.Rate.Reset.Sub(now).Seconds()
		if waitSecs > 0 {
			return true
		}
	}
	return false
}

//Sets a current RateLimit or disables it by setting Nul
//Ratelimit is enebled when the api returns it
//Every Request checks wether a rateLimit is active and current
//If the RateLimit is expired is removed setting it to nil
func (gh *Client) setRateLimit(err *github.RateLimitError) {
	gh.rateLimitMutex.Lock()
	gh.rateLimitError = err
	gh.rateLimitMutex.Unlock()
}

//GetRateLimitError Returns the Rate limit error
func (gh Client) GetRateLimitError() error {
	return gh.rateLimitError
}

//GetUsersByLocation performs a Search API request to find all users by the paramter location
//Then runs the getUserDispatcher function to get all user details concurrently
func (gh *Client) GetUsersByLocation(ctx context.Context, location string, items int) ([]*github.User, error) {
	// Control Rate Limit
	if ok := gh.CheckRateLimit(); ok {
		logrus.Debug("RateLimitError Set, Discarting API Requests until RateLimit expiration")
		logrus.Error(gh.rateLimitError)
		return nil, gh.rateLimitError
	} else {
		gh.setRateLimit(nil)
	}

	var users = make([]*github.User, 0)
	opts := &github.SearchOptions{ListOptions: github.ListOptions{Page: 1, PerPage: items}, Sort: "repos"}
	q := fmt.Sprintf("location:%s type:user", location)

	logrus.Debug("Invoking Github Search API")
	result, resp, err := gh.clientRest.Search.Users(gh.ctx, q, opts)

	if _, ok := err.(*github.RateLimitError); ok {
		logrus.Error(err)
		gh.setRateLimit(err.(*github.RateLimitError))
		return nil, err
	} else if err != nil {
		logrus.Error(err)
		return nil, err
	}

	logrus.WithFields(logrus.Fields{
		"pages":         resp.LastPage,
		"nextPage":      resp.NextPage,
		"returnedUsers": len(result.Users),
		"location":      location,
		"Limit":         resp.Rate.Limit,
		"Remaining":     resp.Rate.Remaining,
		"Reset":         resp.Rate.Reset,
	}).Debug("Github Search API Response")

	if len(result.Users) > 0 {
		users, err = gh.getUsersDispatcher(ctx, result.Users)
		if _, ok := err.(*github.RateLimitError); ok {
			logrus.Error(err)
			gh.setRateLimit(err.(*github.RateLimitError))
			return nil, err
		} else if err != nil {
			return nil, err
		}
	}
	return users, nil
}

//Manages the logic of the getUserWorkers and returns the final result slice with all the user details.
func (gh *Client) getUsersDispatcher(ctx context.Context, users []*github.User) ([]*github.User, error) {
	select {
	case <-ctx.Done():
		return nil, errors.New("getUsersDispatcher Context canceled")
	default:
		var queue = make(chan string)
		var errors = make(chan error, 1)
		var done = make(chan bool, 1)
		var results = make(chan *github.User)
		var wg sync.WaitGroup

		// Ctx withCancel to cancel goroutines if needed
		ctx, cancel := context.WithCancel(ctx)

		// Launch some workers
		// Be careful with RateLimit
		for i := 0; i < 5; i++ {
			wg.Add(1)
			go gh.getUsersWorker(ctx, queue, results, errors, &wg)
		}

		// Enqueue users to get the details on the queue channel
		go func() {
			for _, u := range users {
				logrus.WithFields(logrus.Fields{
					"user": *(u).Login,
				}).Debug("getUsersDispatcher Send user to process queue")
				queue <- *(u).Login
			}
			close(queue)
			wg.Wait()
			// Send a bool the the done channel to notify all work is done
			done <- true
		}()

		// Get the results or error from a the goroutines
		var users []*github.User
		for {
			select {
			case <-done:
				cancel()
				return users, nil
			case e := <-errors:
				// If a goroutine returns an error cancel the context and return the error retourned
				cancel()
				return nil, e
			case r := <-results:
				// Add a result to the results slice
				users = append(users, r)
			}
		}
	}
}

//Function runs as a goroutine concurrently to get the user details (number of repos)
//Sincronization is made with channels
func (gh *Client) getUsersWorker(ctx context.Context, queue <-chan string, results chan *github.User, errors chan<- error, wg *sync.WaitGroup) {
	defer func() { logrus.Debug("getUsersWorker finished"); wg.Done() }()
	for {
		select {
		case <-ctx.Done():
			logrus.Debug("getUsersWorker Context canceled")
		case user, ok := <-queue:
			if !ok {
				logrus.Debug("getUsersWorker queue channel closed, terminating")
				return
			}
			logrus.WithFields(logrus.Fields{
				"user": user,
			}).Debug("getUsersWorker Invoking Github Users API")
			userDetails, resp, err := gh.clientRest.Users.Get(ctx, user)
			if err != nil {
				logrus.Error(err)
				// This will trigger Cancel in the dispatcher
				// A Retry could be issued here depending on the error returned
				errors <- err
			}
			select {
			case <-ctx.Done():
				logrus.Debug("getUsersWorker Context canceled")
				return
			default:
				logrus.WithFields(logrus.Fields{
					"Limit":     resp.Rate.Limit,
					"Remaining": resp.Rate.Remaining,
					"Reset":     resp.Rate.Reset,
				}).Debug("getUsersWorker Github RateLimit")
				results <- userDetails
			}
		}
	}
}
