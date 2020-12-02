package githubclient

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/go-github/v32/github"
	"github.com/sirupsen/logrus"
	"golang.org/x/oauth2"
)

//Client is a struct to hold the Client
type Client struct {
	ctx        context.Context
	clientRest *github.Client
	Reset      *github.Timestamp
}

//NewClient returns a github client
func NewClient(ctx context.Context, token string) *Client {
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)
	o2client := oauth2.NewClient(ctx, ts)
	clientRest := github.NewClient(o2client)

	return &Client{
		ctx:        ctx,
		clientRest: clientRest,
	}
}

func (gh *Client) githubUserWorker(ctx context.Context, queue <-chan string, results chan<- *github.User, wg *sync.WaitGroup) {
	defer wg.Done()
	logrus.Debug("Spawning githubUserWorker")
	for user := range queue {
		logrus.WithFields(logrus.Fields{
			"user": user,
		}).Debug("Getting github user")
		userDetails, resp, _ := gh.clientRest.Users.Get(ctx, user)
		logrus.WithFields(logrus.Fields{
			"Limit":     resp.Rate.Limit,
			"Remaining": resp.Rate.Remaining,
			"Reset":     resp.Rate.Reset,
		}).Debug("Github Rate")
		results <- userDetails
	}
}

func (gh *Client) getUserDetails(ctx context.Context, users []*github.User) ([]*github.User, error) {
	// Got a bunch of users
	var queue = make(chan string, 10)
	var results = make(chan *github.User, len(users))
	var wg sync.WaitGroup
	// Spin up the workers

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go gh.githubUserWorker(ctx, queue, results, &wg)
	}

	go func() {
		wg.Wait()
		close(results)
		logrus.Debug("All githubUserWorkers finished")
	}()

	for _, u := range users {
		queue <- *(u).Login
	}
	close(queue)
	var _users []*github.User
	for user := range results {
		_users = append(users, user)
	}
	return _users, nil
}

func (gh *Client) GetUsersByLocation(ctx context.Context, location string) ([]*github.User, error) {
	// Check if we must wait until Reset
	if gh.Reset != nil {
		now := time.Now()
		waitSecs := gh.Reset.Sub(now).Seconds()
		if waitSecs < 0 {
			gh.Reset = nil
		} else {
			return nil, fmt.Errorf("RateLimitExceded; Try again in %d Seconds", int(waitSecs))
		}

	}
	var users []*github.User

	opts := &github.SearchOptions{ListOptions: github.ListOptions{Page: 1, PerPage: 100}, Sort: "repos"}
	q := fmt.Sprintf("location:%s type:user", location)

	result, resp, err := gh.clientRest.Search.Users(gh.ctx, q, opts)
	if _, ok := err.(*github.RateLimitError); ok {
		gh.Reset = &resp.Rate.Reset
		return nil, err
	} else if err != nil {
		return nil, err
	}

	logrus.WithFields(logrus.Fields{
		"pages":     resp.LastPage,
		"nextPage":  resp.NextPage,
		"rateLimit": resp.Rate,
	}).Debug("Query response")
	if len(result.Users) > 0 {
		logrus.WithFields(logrus.Fields{
			"users": len(result.Users),
		}).Debug("Users found")

		logrus.Debug("BEFORE")
		users, err = gh.getUserDetails(ctx, result.Users)
		logrus.Debug("AFTER")
		if err != nil {
			return nil, err
		}
	}
	return users, nil
}
