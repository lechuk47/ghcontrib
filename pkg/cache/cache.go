package cache

import (
	"context"
	"encoding/json"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/google/go-github/v32/github"
)

//Cache is the interface which cache implementations must implement
type Cache interface {
	GetClient() interface{}
	GetKey(ctx context.Context, key string) (*[]github.User, error)
	SetKey(ctx context.Context, key string, value *[]github.User, ttl time.Duration) error
}

//RedisCache is the Implementation of Cache interface for Redis
type RedisCache struct {
	addr     string
	database int
	password string
	client   *redis.Client
}

//NewRedisCache constructs the RedisCache object
func NewRedisCache(addr string, database int, password string) *RedisCache {
	return &RedisCache{
		database: database,
		client: redis.NewClient(&redis.Options{
			Addr:     addr,
			Password: password,
			DB:       database,
		}),
	}
}

//GetKey gets a key from a redis cache
//Returns a slice of User
func (r RedisCache) GetKey(ctx context.Context, key string) ([]*github.User, error) {
	users, err := r.client.Get(ctx, key).Result()
	if err != nil {
		return nil, err
	} else {
		var cUsers []*github.User
		err = json.Unmarshal([]byte(users), &cUsers)
		if err != nil {
			return nil, err
		}
		return cUsers, nil
	}

}

//SetKey sets a key-value in Redis cache
func (r RedisCache) SetKey(ctx context.Context, key string, users []*github.User, ttl time.Duration) error {
	susers, err := json.Marshal(users)
	if err != nil {
		return err
	}
	err = r.client.Set(ctx, key, string(susers), ttl).Err()
	if err != nil {
		return err
	}
	return nil
}
