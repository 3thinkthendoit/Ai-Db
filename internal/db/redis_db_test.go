package db

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"

	"AI-DB/internal/config"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestParseRedisDBIndex(t *testing.T) {
	cases := []struct {
		in   string
		want int
		ok   bool
	}{
		{"", 0, false},
		{"0", 0, true},
		{"15", 15, true},
		{"db3", 3, true},
		{"DB12", 12, true},
		{" 7 ", 7, true},
		{"nope", 0, false},
		{"-1", 0, false},
	}
	for _, c := range cases {
		got, ok := parseRedisDBIndex(c.in)
		if ok != c.ok || (ok && got != c.want) {
			t.Fatalf("parseRedisDBIndex(%q)=(%d,%v) want (%d,%v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestRedisOptionsDB(t *testing.T) {
	opts := redisOptions(config.Connection{Host: "127.0.0.1", Port: 6379, Database: "ignored"}, 5)
	if opts.DB != 5 {
		t.Fatalf("DB=%d", opts.DB)
	}
	if opts.Addr != "127.0.0.1:6379" {
		t.Fatalf("Addr=%q", opts.Addr)
	}
}

func miniRedisConn(t *testing.T, mr *miniredis.Miniredis, database string) config.Connection {
	t.Helper()
	host, portStr, err := net.SplitHostPort(mr.Addr())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatal(err)
	}
	return config.Connection{
		ID:       "redis-test",
		Type:     config.ConnRedis,
		Host:     host,
		Port:     port,
		Database: database,
	}
}

func TestRedisSwitchDBThenExecuteReadsNewDB(t *testing.T) {
	mr := miniredis.RunT(t)
	ctx := context.Background()

	// Seed DB 0 and DB 1 via dedicated clients (Options.DB).
	c0 := redis.NewClient(&redis.Options{Addr: mr.Addr(), DB: 0})
	t.Cleanup(func() { _ = c0.Close() })
	if err := c0.Set(ctx, "k", "from0", 0).Err(); err != nil {
		t.Fatal(err)
	}
	c1 := redis.NewClient(&redis.Options{Addr: mr.Addr(), DB: 1})
	t.Cleanup(func() { _ = c1.Close() })
	if err := c1.Set(ctx, "k", "from1", 0).Err(); err != nil {
		t.Fatal(err)
	}

	drv, err := openRedis(ctx, miniRedisConn(t, mr, "0"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = drv.Close() })
	d := drv.(*redisDriver)

	res, err := d.Execute(ctx, "GET k", 10, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Rows) != 1 || res.Rows[0][0] != "from0" {
		t.Fatalf("before switch: %+v", res.Rows)
	}

	if err := d.SetActiveSchema("1"); err != nil {
		t.Fatal(err)
	}
	if d.live().Options().DB != 1 {
		t.Fatalf("client Options.DB=%d after switch", d.live().Options().DB)
	}

	// Force pool churn: multiple GETs should still hit DB 1 (not a sticky SELECT on one conn).
	for i := 0; i < 8; i++ {
		res, err = d.Execute(ctx, "GET k", 10, false)
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Rows) != 1 || res.Rows[0][0] != "from1" {
			t.Fatalf("after switch iter %d: %+v", i, res.Rows)
		}
	}
}

func TestRedisExpandObjectDoesNotChangeActiveDB(t *testing.T) {
	mr := miniredis.RunT(t)
	ctx := context.Background()

	c0 := redis.NewClient(&redis.Options{Addr: mr.Addr(), DB: 0})
	t.Cleanup(func() { _ = c0.Close() })
	_ = c0.Set(ctx, "only0", "a", 0)
	c1 := redis.NewClient(&redis.Options{Addr: mr.Addr(), DB: 1})
	t.Cleanup(func() { _ = c1.Close() })
	_ = c1.Set(ctx, "only1", "b", 0)

	drv, err := openRedis(ctx, miniRedisConn(t, mr, "0"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = drv.Close() })
	d := drv.(*redisDriver)

	nodes, err := d.ExpandObject(ctx, []string{"1"})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, n := range nodes {
		if n.Name == "only1" {
			found = true
		}
		if n.Name == "only0" {
			t.Fatalf("expand db1 should not list only0: %+v", nodes)
		}
	}
	if !found {
		t.Fatalf("expand db1 missing only1: %+v", nodes)
	}

	if d.activeIndex() != 0 {
		t.Fatalf("active index changed to %d after expand", d.activeIndex())
	}
	if d.live().Options().DB != 0 {
		t.Fatalf("live Options.DB=%d after expand", d.live().Options().DB)
	}

	res, err := d.Execute(ctx, "GET only0", 10, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Rows) != 1 || res.Rows[0][0] != "a" {
		t.Fatalf("execute after expand should still be DB0: %+v", res.Rows)
	}
}

func TestRedisSetActiveSchemaEmptySelectsZero(t *testing.T) {
	mr := miniredis.RunT(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	drv, err := openRedis(ctx, miniRedisConn(t, mr, "1"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = drv.Close() })
	d := drv.(*redisDriver)
	if err := d.SetActiveSchema(""); err != nil {
		t.Fatal(err)
	}
	if d.activeIndex() != 0 || d.live().Options().DB != 0 {
		t.Fatalf("empty schema should select DB 0, got name=%q opts=%d", d.activeDBName, d.live().Options().DB)
	}
}
