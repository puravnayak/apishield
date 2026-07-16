package cache

import (
	"testing"
)

func TestNewRedisClientParsing(t *testing.T) {
	// 1. Test normal host:port format
	rdb1, err := NewRedisClient("localhost:6379")
	if err != nil {
		t.Fatalf("unexpected error for standard host:port: %v", err)
	}
	if rdb1.Options().Addr != "localhost:6379" {
		t.Errorf("expected Addr to be localhost:6379, got %s", rdb1.Options().Addr)
	}

	// 2. Test redis:// URL format
	rdb2, err := NewRedisClient("redis://:secret_pass@localhost:6379/1")
	if err != nil {
		t.Fatalf("unexpected error for redis:// URL: %v", err)
	}
	if rdb2.Options().Addr != "localhost:6379" {
		t.Errorf("expected Addr to be localhost:6379, got %s", rdb2.Options().Addr)
	}
	if rdb2.Options().Password != "secret_pass" {
		t.Errorf("expected password to be secret_pass, got %s", rdb2.Options().Password)
	}
	if rdb2.Options().DB != 1 {
		t.Errorf("expected DB index to be 1, got %d", rdb2.Options().DB)
	}

	// 3. Test rediss:// URL format (secure SSL/TLS connection)
	rdb3, err := NewRedisClient("rediss://:secure_pass@localhost:6379/2")
	if err != nil {
		t.Fatalf("unexpected error for rediss:// URL: %v", err)
	}
	if rdb3.Options().Addr != "localhost:6379" {
		t.Errorf("expected Addr to be localhost:6379, got %s", rdb3.Options().Addr)
	}
	if rdb3.Options().TLSConfig == nil {
		t.Errorf("expected TLSConfig to be configured/populated for rediss:// scheme")
	}
}
