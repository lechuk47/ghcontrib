package cache

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/go-redsync/redsync/v4"
	"github.com/go-redsync/redsync/v4/redis/goredis/v8"

	"github.com/google/go-github/v32/github"
	"github.com/sirupsen/logrus"
)

//Cache is the interface which cache implementations must implement
type Cache interface {
	GetKey(ctx context.Context, key string) ([]*github.User, error)
	SetKey(ctx context.Context, key string, value []*github.User, ttl time.Duration) error
	SetLock(ctx context.Context, key string) error
	ReleaseLock(key string) error
}

//RedisCache is the Implementation of Cache interface for Redis
type RedisCache struct {
	client  *redis.Client
	redsync *redsync.Redsync
}

//NewRedisCache constructs the RedisCache object
func NewRedisCache(addr string, password string) RedisCache {
	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
	})
	rs := redsync.New(goredis.NewPool(client))

	return RedisCache{
		client:  client,
		redsync: rs,
	}
}

//GetKey gets a key from a redis cache
//Returns a slice of pointers to User
func (r RedisCache) GetKey(ctx context.Context, key string) ([]*github.User, error) {
	users, err := r.client.Get(ctx, key).Result()
	if err != nil {
		logrus.Error(err)
		return nil, err
	} else {
		var cUsers = make([]*github.User, 0)
		if users != "null" {
			err = json.Unmarshal([]byte(users), &cUsers)
		}
		return cUsers, err
	}
}

//SetLock sets a distributed lock to the cache
func (r RedisCache) SetLock(ctx context.Context, key string) error {
	select {
	case <-ctx.Done():
		return errors.New("setRedisLock Context canceled")
	default:
		mutex := r.redsync.NewMutex(key)
		if err := mutex.Lock(); err != nil {
			logrus.Debug("Failed to acquire Redis Mutex Lock")
			logrus.Error(err)
			return err
		} else {
			return nil
		}
	}
}

//ReleaseLock release the cache distributed Lock
func (r RedisCache) ReleaseLock(key string) error {
	mutex := r.redsync.NewMutex(key)
	if ok, err := mutex.Unlock(); !ok || err != nil {
		return err
	} else {
		return nil
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
