package redisconfig

import (
	"crypto/tls"
	"testing"
	"time"

	"github.com/hibiken/asynq"
)

func TestParseAsynqURIEnablesTLSAndPreservesACLUsername(t *testing.T) {
	options, err := ParseAsynqURI("rediss://default:secret@redis.example.com:6380/0")
	if err != nil {
		t.Fatal(err)
	}

	clientOptions, ok := options.(asynq.RedisClientOpt)
	if !ok {
		t.Fatalf("expected RedisClientOpt, got %T", options)
	}
	if clientOptions.TLSConfig == nil {
		t.Fatal("expected TLS config for rediss URI")
	}
	if clientOptions.TLSConfig.MinVersion != tls.VersionTLS12 {
		t.Errorf("TLS minimum = %v, want TLS 1.2", clientOptions.TLSConfig.MinVersion)
	}
	if clientOptions.TLSConfig.ServerName != "redis.example.com" {
		t.Errorf("TLS server name = %q, want redis.example.com", clientOptions.TLSConfig.ServerName)
	}
	if clientOptions.Username != "default" {
		t.Errorf("username = %q, want default", clientOptions.Username)
	}
	if clientOptions.Password != "secret" {
		t.Error("password was not parsed")
	}
	if clientOptions.DialTimeout != 10*time.Second || clientOptions.ReadTimeout != 10*time.Second || clientOptions.WriteTimeout != 10*time.Second {
		t.Errorf("unexpected Asynq timeouts: dial=%s read=%s write=%s", clientOptions.DialTimeout, clientOptions.ReadTimeout, clientOptions.WriteTimeout)
	}
	if clientOptions.PoolSize != 20 {
		t.Errorf("pool size = %d, want 20", clientOptions.PoolSize)
	}
}

func TestParseGoRedisURLConfiguresTLSAndNetworkSettings(t *testing.T) {
	options, err := ParseGoRedisURL("rediss://default:secret@redis.example.com:6380/0")
	if err != nil {
		t.Fatal(err)
	}
	if options.TLSConfig == nil || options.TLSConfig.MinVersion != tls.VersionTLS12 {
		t.Fatalf("expected TLS 1.2 minimum, got %#v", options.TLSConfig)
	}
	if options.DialTimeout != 10*time.Second || options.ReadTimeout != 10*time.Second || options.WriteTimeout != 10*time.Second || options.PoolTimeout != 10*time.Second {
		t.Errorf("unexpected Redis timeouts: dial=%s read=%s write=%s pool=%s", options.DialTimeout, options.ReadTimeout, options.WriteTimeout, options.PoolTimeout)
	}
	if options.ConnMaxIdleTime <= 0 || options.ConnMaxLifetime <= options.ConnMaxIdleTime || options.Dialer == nil {
		t.Errorf("expected bounded pooled connections and custom keep-alive dialer, got idle=%s lifetime=%s dialer=%v", options.ConnMaxIdleTime, options.ConnMaxLifetime, options.Dialer != nil)
	}
}
