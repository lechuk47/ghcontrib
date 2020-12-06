package cache

import (
	"context"
	"errors"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/go-redsync/redsync/v4"
	"github.com/go-redsync/redsync/v4/redis/goredis/v8"

	"github.com/sirupsen/logrus"
)

//Cache is the interface that the app has to implement to use the cache
type Cache interface {
	GetKey(ctx context.Context, key string) (interface{}, error)
	SetKey(ctx context.Context, ttl time.Duration, key string, value interface{}) error
	SetLock(ctx context.Context, key string) error
	ReleaseLock(key string) error
	Push(ctx context.Context, ttl time.Duration, key string, values ...string) error
	GetRange(ctx context.Context, key string, items int64) ([]string, error)
	Exists(ctx context.Context, key string) (int64, error)
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

//GetKey gets the value of a key from the cache
func (r RedisCache) GetKey(ctx context.Context, key string) (interface{}, error) {
	value, err := r.client.Get(ctx, key).Result()
	if err != nil {
		return nil, err
	} else {
		return value, nil
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
			logrus.Debug("Returning Nil")
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
func (r RedisCache) SetKey(ctx context.Context, ttl time.Duration, key string, value interface{}) error {
	err := r.client.Set(ctx, key, value, ttl).Err()
	if err != nil {
		return err
	}
	return nil
}

//Push push values to a redis list
func (r RedisCache) Push(ctx context.Context, ttl time.Duration, key string, values ...string) error {
	_ = r.client.Del(ctx, key)
	if val, err := r.client.LPush(ctx, key, values).Result(); err != nil {
		return err
	} else {
		logrus.WithFields(logrus.Fields{
			"insertedItems": val,
		}).Debug("Pushing to the cache")
	}

	logrus.WithField("expiration", ttl).Debug("Setting key expiration")
	if _, err := r.client.Expire(ctx, key, ttl).Result(); err != nil {
		return err
	}
	return nil
}

//GetRange gets a range of values from redis
func (r RedisCache) GetRange(ctx context.Context, key string, items int64) ([]string, error) {
	value, err := r.client.LRange(ctx, key, 0, items).Result()
	logrus.WithFields(logrus.Fields{
		"key":   key,
		"start": 1,
		"stop":  items,
		"got":   len(value),
	}).Debug("GetRange")

	if err != nil {
		return nil, err
	}
	return value, nil
}

//Exists check if a key exists in redis
func (r RedisCache) Exists(ctx context.Context, key string) (int64, error) {
	value, err := r.client.Exists(ctx, key).Result()
	logrus.WithFields(logrus.Fields{
		"key":   key,
		"value": value,
	}).Debug("Checking if key exists in the cache")
	if err != nil {
		logrus.Error(err)
		return 0, err
	} else {
		return value, err
	}
}
