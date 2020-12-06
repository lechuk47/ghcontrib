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
	s *miniredis.Miniredis
)

func TestMain(m *testing.M) {
	// call flag.Parse() here if TestMain uses flags
	s, _ = miniredis.Run()
	os.Exit(m.Run())
}

func TestSetKey(t *testing.T) {
	c := NewRedisCache(s.Addr(), "")
	ctx := context.Background()
	err := c.SetKey(ctx, "key", "value", 5*time.Second)
	assert.NoError(t, err)
}

func TestGetKey(t *testing.T) {
	c := NewRedisCache(s.Addr(), "")
	ctx := context.Background()
	val, err := c.GetKey(ctx, "key")
	assert.NoError(t, err)
	assert.Equal(t, val, "value")
}

func TestGetKeyError(t *testing.T) {
	c := NewRedisCache(s.Addr(), "")
	ctx := context.Background()
	val, err := c.GetKey(ctx, "key-dont-exist")
	assert.Error(t, err)
	assert.Equal(t, val, nil)
}
