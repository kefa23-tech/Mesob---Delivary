package redisconfig

import (
	"crypto/tls"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/hibiken/asynq"
)

// ParseAsynqURI uses Asynq's URI parser for TLS and Redis options. Asynq v0.26
// does not copy URI usernames into RedisClientOpt, so preserve that ACL field
// for providers such as Upstash.
func ParseAsynqURI(raw string) (asynq.RedisConnOpt, error) {
	options, err := asynq.ParseRedisURI(raw)
	if err != nil {
		return nil, fmt.Errorf("parse Asynq Redis URI: %w", err)
	}

	clientOptions, ok := options.(asynq.RedisClientOpt)
	if !ok {
		return options, nil
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parse Redis URI credentials: %w", err)
	}
	if parsed.User != nil {
		clientOptions.Username = parsed.User.Username()
	}
	if strings.HasPrefix(strings.ToLower(raw), "rediss://") {
		if clientOptions.TLSConfig == nil {
			clientOptions.TLSConfig = &tls.Config{}
		} else {
			clientOptions.TLSConfig = clientOptions.TLSConfig.Clone()
		}
		clientOptions.TLSConfig.MinVersion = tls.VersionTLS12
	}
	clientOptions.DialTimeout = 10 * time.Second
	clientOptions.ReadTimeout = 10 * time.Second
	clientOptions.WriteTimeout = 10 * time.Second
	clientOptions.PoolSize = 20
	return clientOptions, nil
}
