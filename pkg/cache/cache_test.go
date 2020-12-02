package cache

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"testing"
	"time"

	"github.com/alicebob/miniredis"
	"github.com/elliotchance/redismock/v8"
	"github.com/go-redis/redis/v8"

	"github.com/google/go-github/v32/github"
	"github.com/stretchr/testify/assert"
)

var (
	client   *redis.Client
	key      = "key"
	testUser = "TestUser"
	value    = github.User{
		Login: &testUser,
	}
)

func TestMain(m *testing.M) {
	mr, err := miniredis.Run()
	if err != nil {
		log.Fatalf("Error running minireedis %s", err)
	}

	client = redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	code := m.Run()
	os.Exit(code)
}

func TestSetKey(t *testing.T) {
	log.Println("TestA running")
	ctx := context.Background()
	exp := time.Duration(0)

	r := RedisCache{
		context: ctx,
		client:  client,
	}
	lval := []*github.User{&value}
	err := r.SetKey(key, lval, exp)
	assert.Nil(t, err)
}

func TestGetKey(t *testing.T) {
	ctx := context.Background()
	mock := redismock.NewNiceMock(client)
	mock.On("Get", key).Return(json.Marshal(value))

	r := RedisCache{
		context: ctx,
		client:  client,
	}
	res, err := r.GetKey(key)
	assert.NoError(t, err)
	assert.Equal(t, value, res)
}
