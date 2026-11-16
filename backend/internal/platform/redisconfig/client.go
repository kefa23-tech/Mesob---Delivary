package redisconfig

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	dialTimeout     = 10 * time.Second
	readTimeout     = 10 * time.Second
	writeTimeout    = 10 * time.Second
	poolTimeout     = 10 * time.Second
	tcpKeepAlive    = 30 * time.Second
	connectionIdle  = 5 * time.Minute
	connectionLimit = 30 * time.Minute
)

// ParseGoRedisURL parses a Redis URL and applies consistent TLS and network
// settings for application clients. TCP keep-alives help detect stale cloud
// connections, while bounded idle/lifetime settings refresh pooled sockets.
func ParseGoRedisURL(raw string) (*redis.Options, error) {
	options, err := redis.ParseURL(raw)
	if err != nil {
		return nil, fmt.Errorf("parse Redis URL: %w", err)
	}

	options.DialTimeout = dialTimeout
	options.ReadTimeout = readTimeout
	options.WriteTimeout = writeTimeout
	options.PoolTimeout = poolTimeout
	options.ConnMaxIdleTime = connectionIdle
	options.ConnMaxLifetime = connectionLimit
	options.MinIdleConns = 1

	if strings.HasPrefix(strings.ToLower(raw), "rediss://") {
		if options.TLSConfig == nil {
			options.TLSConfig = &tls.Config{}
		} else {
			options.TLSConfig = options.TLSConfig.Clone()
		}
		options.TLSConfig.MinVersion = tls.VersionTLS12
	}

	// go-redis permits a custom dialer; preserve URL TLS settings while choosing
	// an explicit TCP keep-alive interval and bounding initial TLS handshakes.
	dialer := &net.Dialer{Timeout: dialTimeout, KeepAlive: tcpKeepAlive}
	if options.TLSConfig != nil {
		tlsDialer := &tls.Dialer{NetDialer: dialer, Config: options.TLSConfig}
		options.Dialer = func(ctx context.Context, network, address string) (net.Conn, error) {
			ctx, cancel := context.WithTimeout(ctx, dialTimeout)
			defer cancel()
			return tlsDialer.DialContext(ctx, network, address)
		}
	} else {
		options.Dialer = func(ctx context.Context, network, address string) (net.Conn, error) {
			ctx, cancel := context.WithTimeout(ctx, dialTimeout)
			defer cancel()
			return dialer.DialContext(ctx, network, address)
		}
	}

	return options, nil
}
