package cache

import (
	"strings"

	"github.com/redis/go-redis/v9"
)

func NewRedisClient(addr string) (*redis.Client, error) {
	if strings.HasPrefix(addr, "redis://") || strings.HasPrefix(addr, "rediss://") {
		opts, err := redis.ParseURL(addr)
		if err != nil {
			return nil, err
		}
		return redis.NewClient(opts), nil
	}

	return redis.NewClient(&redis.Options{
		Addr: addr,
	}), nil
}
