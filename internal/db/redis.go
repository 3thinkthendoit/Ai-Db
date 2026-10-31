package db

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"AI-DB/internal/config"

	"github.com/redis/go-redis/v9"
)

type redisDriver struct {
	mu     sync.Mutex
	client *redis.Client
	cfg    config.Connection
	// activeDBName is the UI label for the selected logical DB (e.g. "0", "3").
	activeDBName string
}

func openRedis(ctx context.Context, c config.Connection) (Driver, error) {
	dbIndex := 0
	if n, ok := parseRedisDBIndex(c.Database); ok {
		dbIndex = n
	}
	client, err := openRedisClient(c, dbIndex)
	if err != nil {
		return nil, err
	}
	d := &redisDriver{client: client, cfg: c, activeDBName: strconv.Itoa(dbIndex)}
	if err := d.Ping(ctx); err != nil {
		_ = client.Close()
		return nil, err
	}
	return d, nil
}

func redisOptions(c config.Connection, dbIndex int) *redis.Options {
	port := c.Port
	if port == 0 {
		port = 6379
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
	return opts
}

func openRedisClient(c config.Connection, dbIndex int) (*redis.Client, error) {
	return redis.NewClient(redisOptions(c, dbIndex)), nil
}

func (d *redisDriver) Type() config.ConnType { return config.ConnRedis }

func (d *redisDriver) live() *redis.Client {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.client
}

func (d *redisDriver) Ping(ctx context.Context) error {
	c := d.live()
	if c == nil {
		return fmt.Errorf("redis client not open")
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	return c.Ping(ctx).Err()
}

func (d *redisDriver) Close() error {
	d.mu.Lock()
	c := d.client
	d.client = nil
	d.mu.Unlock()
	if c == nil {
		return nil
	}
	return c.Close()
}

// SetActiveSchema switches the open client to another Redis logical DB (Options.DB).
// Callers (Manager) must ensure no in-flight queries — the live client is replaced.
func (d *redisDriver) SetActiveSchema(schema string) error {
	idx, ok := parseRedisDBIndex(schema)
	if !ok {
		// Empty / omitted → DB 0 (connection form allows blank).
		if strings.TrimSpace(schema) == "" {
			idx = 0
		} else {
			return fmt.Errorf("invalid redis db index: %q", schema)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return d.switchDB(ctx, idx)
}

// switchDB replaces the live client with one bound to Options.DB=idx.
// Never use SELECT on a pooled Client — it only sticks to one connection.
func (d *redisDriver) switchDB(ctx context.Context, idx int) error {
	if idx < 0 {
		return fmt.Errorf("invalid redis db index: %d", idx)
	}
	d.mu.Lock()
	if d.activeDBName == strconv.Itoa(idx) && d.client != nil {
		d.mu.Unlock()
		return nil
	}
	cfg := d.cfg
	d.mu.Unlock()

	next, err := openRedisClient(cfg, idx)
	if err != nil {
		return err
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := next.Ping(pingCtx).Err(); err != nil {
		_ = next.Close()
		return fmt.Errorf("switch redis db %d: %w", idx, err)
	}

	d.mu.Lock()
	old := d.client
	d.client = next
	d.activeDBName = strconv.Itoa(idx)
	d.cfg.Database = strconv.Itoa(idx)
	d.mu.Unlock()
	if old != nil {
		_ = old.Close()
	}
	return nil
}

// withDB runs fn on the live client when idx matches the active DB; otherwise a temporary
// client (object-tree browse must not change where Execute runs, nor SELECT a pooled conn).
func (d *redisDriver) withDB(ctx context.Context, idx int, fn func(*redis.Client) error) error {
	d.mu.Lock()
	active := d.activeIndexLocked()
	live := d.client
	cfg := d.cfg
	d.mu.Unlock()
	if idx == active {
		if live == nil {
			return fmt.Errorf("redis client not open")
		}
		return fn(live)
	}
	tmp, err := openRedisClient(cfg, idx)
	if err != nil {
		return err
	}
	defer tmp.Close()
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := tmp.Ping(pingCtx).Err(); err != nil {
		return fmt.Errorf("open redis db %d: %w", idx, err)
	}
	return fn(tmp)
}

func (d *redisDriver) activeIndex() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.activeIndexLocked()
}

func (d *redisDriver) activeIndexLocked() int {
	if idx, ok := parseRedisDBIndex(d.activeDBName); ok {
		return idx
	}
	if idx, ok := parseRedisDBIndex(d.cfg.Database); ok {
		return idx
	}
	return 0
}

// parseRedisDBIndex accepts "0", "3", "db0", "DB12". Empty → not ok (caller may default to 0).
func parseRedisDBIndex(schema string) (int, bool) {
	s := strings.TrimSpace(strings.ToLower(schema))
	if s == "" {
		return 0, false
	}
	if strings.HasPrefix(s, "db") {
		s = strings.TrimPrefix(s, "db")
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

func (d *redisDriver) Execute(ctx context.Context, query string, maxRows int, allowWrites bool) (*QueryResult, error) {
	if err := EnsureRedisReadOnly(query, allowWrites); err != nil {
		return nil, err
	}
	if maxRows <= 0 {
		maxRows = 500
	}
	c := d.live()
	if c == nil {
		return nil, fmt.Errorf("redis client not open")
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
	cmd := c.Do(ctx, args...)
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
	c := d.live()
	if c == nil {
		return nil, fmt.Errorf("redis client not open")
	}
	n := databaseCount(ctx, c)
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, strconv.Itoa(i))
	}
	return out, nil
}

// databaseCount reads CONFIG GET databases; on denial uses INFO keyspace (max used index+1)
// floored at 16 so empty high-number DBs still appear in the picker when CONFIG is allowed
// to fail but keyspace shows activity beyond the default 16.
func databaseCount(ctx context.Context, c *redis.Client) int {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	m, err := c.ConfigGet(ctx, "databases").Result()
	if err == nil {
		if raw, ok := m["databases"]; ok {
			if n, e := strconv.Atoi(raw); e == nil && n > 0 && n <= 1024 {
				return n
			}
		}
	}
	n := 16
	if info, err := c.Info(ctx, "keyspace").Result(); err == nil {
		for _, line := range strings.Split(info, "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "db") {
				continue
			}
			rest := strings.TrimPrefix(line, "db")
			i := strings.IndexByte(rest, ':')
			if i <= 0 {
				continue
			}
			if idx, e := strconv.Atoi(rest[:i]); e == nil && idx+1 > n && idx+1 <= 1024 {
				n = idx + 1
			}
		}
	}
	return n
}

func (d *redisDriver) ListTables(ctx context.Context, _ string) ([]string, error) {
	c := d.live()
	if c == nil {
		return nil, fmt.Errorf("redis client not open")
	}
	return scanKeys(ctx, c, "*", 200)
}

func scanKeys(ctx context.Context, c *redis.Client, pattern string, limit int) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var cursor uint64
	keys := []string{}
	for {
		batch, next, err := c.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return nil, err
		}
		keys = append(keys, batch...)
		cursor = next
		if cursor == 0 || len(keys) >= limit {
			break
		}
	}
	if len(keys) > limit {
		keys = keys[:limit]
	}
	return keys, nil
}

func (d *redisDriver) DescribeTable(ctx context.Context, _, key string) (*QueryResult, error) {
	c := d.live()
	if c == nil {
		return nil, fmt.Errorf("redis client not open")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	t, err := c.Type(ctx, key).Result()
	if err != nil {
		return nil, err
	}
	ttl, _ := c.TTL(ctx, key).Result()
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
	names, err := d.ListSchemas(ctx)
	if err != nil {
		return nil, err
	}
	nodes := make([]ObjectNode, 0, len(names))
	for _, name := range names {
		nodes = append(nodes, ObjectNode{Name: name, Kind: "database", Lazy: true})
	}
	return nodes, nil
}

func (d *redisDriver) ExpandObject(ctx context.Context, path []string) ([]ObjectNode, error) {
	if len(path) == 0 {
		return d.ListObjects(ctx)
	}
	if len(path) == 1 {
		want, ok := parseRedisDBIndex(path[0])
		if !ok {
			return nil, fmt.Errorf("invalid redis db: %q", path[0])
		}
		var out []ObjectNode
		err := d.withDB(ctx, want, func(c *redis.Client) error {
			keys, err := scanKeys(ctx, c, "*", 200)
			if err != nil {
				return err
			}
			nodes, err := keysWithTypeBadges(ctx, c, keys)
			if err != nil {
				return err
			}
			out = nodes
			return nil
		})
		return out, err
	}
	return nil, badObjectPath(path)
}

func keysWithTypeBadges(ctx context.Context, c *redis.Client, keys []string) ([]ObjectNode, error) {
	out := make([]ObjectNode, 0, len(keys))
	if len(keys) == 0 {
		return out, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	pipe := c.Pipeline()
	cmds := make([]*redis.StatusCmd, len(keys))
	for i, k := range keys {
		cmds[i] = pipe.Type(ctx, k)
	}
	_, _ = pipe.Exec(ctx)
	for i, k := range keys {
		n := ObjectNode{Name: k, Kind: "key"}
		if typ, err := cmds[i].Result(); err == nil && typ != "" && typ != "none" {
			n.Badge = typ
		}
		out = append(out, n)
	}
	return out, nil
}

// RedisGet returns string value helper for agent tools.
func (d *redisDriver) RedisGet(ctx context.Context, key string) (string, error) {
	c := d.live()
	if c == nil {
		return "", fmt.Errorf("redis client not open")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return c.Get(ctx, key).Result()
}

// RedisType returns key type.
func (d *redisDriver) RedisType(ctx context.Context, key string) (string, error) {
	c := d.live()
	if c == nil {
		return "", fmt.Errorf("redis client not open")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return c.Type(ctx, key).Result()
}

// RedisKeys scans keys with pattern.
func (d *redisDriver) RedisKeys(ctx context.Context, pattern string, limit int64) ([]string, error) {
	if pattern == "" {
		pattern = "*"
	}
	if limit <= 0 {
		limit = 100
	}
	c := d.live()
	if c == nil {
		return nil, fmt.Errorf("redis client not open")
	}
	return scanKeys(ctx, c, pattern, int(limit))
}
