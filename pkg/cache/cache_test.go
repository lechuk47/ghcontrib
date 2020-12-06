package cache

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
)

var (
	s   *miniredis.Miniredis
	c   RedisCache
	ctx context.Context
)

func TestMain(m *testing.M) {
	// call flag.Parse() here if TestMain uses flags
	s, _ = miniredis.Run()
	c = NewRedisCache(s.Addr(), "")
	ctx = context.Background()
	os.Exit(m.Run())
}

func TestSetKey(t *testing.T) {
	err := c.SetKey(ctx, "key", "value", 5*time.Second)
	assert.NoError(t, err)
}

func TestGetKey(t *testing.T) {
	val, err := c.GetKey(ctx, "key")
	assert.NoError(t, err)
	assert.Equal(t, val, "value")
}

func TestGetKeyError(t *testing.T) {
	val, err := c.GetKey(ctx, "key-dont-exist")
	assert.Error(t, err)
	assert.Equal(t, val, nil)
}

func TestPush(t *testing.T) {
	var values = []string{"testvalue1", "testvalue2"}
	err := c.Push(ctx, 30*time.Second, "pushkey", values...)
	assert.NoError(t, err)
}

func TestGetRange(t *testing.T) {
	values, err := c.GetRange(ctx, "pushkey", 5)
	assert.NoError(t, err)
	assert.Equal(t, len(values), 2)
}

func TestGetRangeKeyNotExist(t *testing.T) {
	values2, err := c.GetRange(ctx, "noexistkey", 5)
	assert.NoError(t, err)
	assert.Equal(t, len(values2), 0)
}
