package githubclient

import (
	"context"
	"fmt"
	"sort"
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

//func getUserByLogin(login string) *github.User

func (gh *Client) GetUsersByLocation(location string) ([]*github.User, error) {
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

		sem := make(chan bool, 10)
		var wg sync.WaitGroup
		var mu = &sync.Mutex{}
		for _, u := range result.Users {
			login := *(u).Login
			wg.Add(1)
			sem <- true
			//logrus.Debug("Spawning Users get go Routine")
			go func(login string) {
				defer func() {
					//		logrus.Debug("Ending Users get go Routine")
					<-sem
					wg.Done()
				}()
				user, _, err := gh.clientRest.Users.Get(gh.ctx, login)
				if err != nil {
					logrus.Error(err)
				}
				mu.Lock()
				users = append(users, user)
				mu.Unlock()
			}(login)
		}
		wg.Wait()
	}
	fmt.Println(users)
	//Sort users by public_repos
	sort.SliceStable(users, func(i, j int) bool {
		return *(users)[i].PublicRepos > *(users)[j].PublicRepos
	})
	return users, nil
}
