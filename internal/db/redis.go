package db

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"AI-DB/internal/config"

	"github.com/redis/go-redis/v9"
)

type redisDriver struct {
	client *redis.Client
	cfg    config.Connection
}

func openRedis(ctx context.Context, c config.Connection) (Driver, error) {
	port := c.Port
	if port == 0 {
		port = 6379
	}
	dbIndex := 0
	if c.Database != "" {
		if n, err := strconv.Atoi(c.Database); err == nil {
			dbIndex = n
		}
	}
	opts := &redis.Options{
		Addr:         fmt.Sprintf("%s:%d", c.Host, port),
		Password:     c.Password,
		DB:           dbIndex,
		DialTimeout:  8 * time.Second,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}
	if c.User != "" {
		opts.Username = c.User
	}
	client := redis.NewClient(opts)
	d := &redisDriver{client: client, cfg: c}
	if err := d.Ping(ctx); err != nil {
		_ = client.Close()
		return nil, err
	}
	return d, nil
}

func (d *redisDriver) Type() config.ConnType { return config.ConnRedis }

func (d *redisDriver) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	return d.client.Ping(ctx).Err()
}

func (d *redisDriver) Close() error { return d.client.Close() }

func (d *redisDriver) Execute(ctx context.Context, query string, maxRows int, allowWrites bool) (*QueryResult, error) {
	if err := EnsureRedisReadOnly(query, allowWrites); err != nil {
		return nil, err
	}
	if maxRows <= 0 {
		maxRows = 500
	}
	start := time.Now()
	parts := splitRedisArgs(strings.TrimSpace(query))
	if len(parts) == 0 {
		return nil, fmt.Errorf("empty command")
	}
	args := make([]any, len(parts))
	for i, p := range parts {
		args[i] = p
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := d.client.Do(ctx, args...)
	val, err := cmd.Result()
	if err != nil && err != redis.Nil {
		return nil, err
	}
	rows := flattenRedis(val, maxRows)
	truncated := false
	if len(rows) > maxRows {
		rows = rows[:maxRows]
		truncated = true
	}
	return &QueryResult{
		Columns:    []string{"value"},
		Rows:       rows,
		RowCount:   len(rows),
		DurationMs: timed(start),
		Truncated:  truncated,
		Message:    "OK",
	}, nil
}

func flattenRedis(v any, max int) [][]string {
	switch t := v.(type) {
	case nil:
		return [][]string{{"nil"}}
	case string:
		return [][]string{{t}}
	case []byte:
		return [][]string{{string(t)}}
	case int64:
		return [][]string{{strconv.FormatInt(t, 10)}}
	case float64:
		return [][]string{{strconv.FormatFloat(t, 'f', -1, 64)}}
	case bool:
		return [][]string{{strconv.FormatBool(t)}}
	case []any:
		out := make([][]string, 0, len(t))
		for i, item := range t {
			if i >= max {
				break
			}
			out = append(out, []string{stringify(item)})
		}
		return out
	case map[any]any:
		out := make([][]string, 0, len(t))
		i := 0
		for k, val := range t {
			if i >= max {
				break
			}
			out = append(out, []string{fmt.Sprintf("%v = %v", k, val)})
			i++
		}
		return out
	default:
		return [][]string{{fmt.Sprint(t)}}
	}
}

func (d *redisDriver) ListSchemas(ctx context.Context) ([]string, error) {
	return []string{fmt.Sprintf("db%s", d.cfg.Database)}, nil
}

func (d *redisDriver) ListTables(ctx context.Context, _ string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var cursor uint64
	keys := []string{}
	for {
		batch, next, err := d.client.Scan(ctx, cursor, "*", 100).Result()
		if err != nil {
			return nil, err
		}
		keys = append(keys, batch...)
		cursor = next
		if cursor == 0 || len(keys) >= 200 {
			break
		}
	}
	return keys, nil
}

func (d *redisDriver) DescribeTable(ctx context.Context, _, key string) (*QueryResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	t, err := d.client.Type(ctx, key).Result()
	if err != nil {
		return nil, err
	}
	ttl, _ := d.client.TTL(ctx, key).Result()
	return &QueryResult{
		Columns:  []string{"key", "type", "ttl_seconds"},
		Rows:     [][]string{{key, t, strconv.FormatInt(int64(ttl.Seconds()), 10)}},
		RowCount: 1,
	}, nil
}

func (d *redisDriver) Explain(ctx context.Context, _ string) (*QueryResult, error) {
	return nil, fmt.Errorf("EXPLAIN is not supported for Redis")
}

func (d *redisDriver) ListObjects(ctx context.Context) ([]ObjectNode, error) {
	name := "db0"
	if d.cfg.Database != "" {
		name = "db" + d.cfg.Database
	}
	return []ObjectNode{{Name: name, Kind: "database", Lazy: true}}, nil
}

func (d *redisDriver) ExpandObject(ctx context.Context, path []string) ([]ObjectNode, error) {
	if len(path) == 0 {
		return d.ListObjects(ctx)
	}
	if len(path) == 1 {
		keys, err := d.ListTables(ctx, "")
		if err != nil {
			return nil, err
		}
		return namesToNodes(keys, "key", false), nil
	}
	return nil, badObjectPath(path)
}

// RedisGet returns string value helper for agent tools.
func (d *redisDriver) RedisGet(ctx context.Context, key string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return d.client.Get(ctx, key).Result()
}

// RedisType returns key type.
func (d *redisDriver) RedisType(ctx context.Context, key string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return d.client.Type(ctx, key).Result()
}

// RedisKeys scans keys with pattern.
func (d *redisDriver) RedisKeys(ctx context.Context, pattern string, limit int64) ([]string, error) {
	if pattern == "" {
		pattern = "*"
	}
	if limit <= 0 {
		limit = 100
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var cursor uint64
	keys := []string{}
	for {
		batch, next, err := d.client.Scan(ctx, cursor, pattern, 50).Result()
		if err != nil {
			return nil, err
		}
		keys = append(keys, batch...)
		cursor = next
		if cursor == 0 || int64(len(keys)) >= limit {
			break
		}
	}
	if int64(len(keys)) > limit {
		keys = keys[:limit]
	}
	return keys, nil
}
