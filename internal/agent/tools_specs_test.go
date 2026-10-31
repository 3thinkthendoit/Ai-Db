package agent

import (
	"testing"

	"AI-DB/internal/config"
)

func toolNames(specs []ToolSpec) map[string]bool {
	out := map[string]bool{}
	for _, s := range specs {
		out[s.Function.Name] = true
	}
	return out
}

func TestToolSpecsByConnType(t *testing.T) {
	redis := NewToolRunner(nil, "c", config.ConnRedis, false, 100).Specs()
	rn := toolNames(redis)
	if !rn["execute_query"] || !rn["redis_keys"] || rn["explain_plan"] || rn["list_schemas"] {
		t.Fatalf("redis specs unexpected: %v", rn)
	}

	mongo := NewToolRunner(nil, "c", config.ConnMongo, false, 100).Specs()
	mn := toolNames(mongo)
	if !mn["execute_query"] || !mn["list_schemas"] || !mn["list_tables"] || !mn["describe_table"] {
		t.Fatalf("mongo missing catalog tools: %v", mn)
	}
	if mn["explain_plan"] || mn["redis_keys"] {
		t.Fatalf("mongo should not expose explain/redis tools: %v", mn)
	}

	sql := NewToolRunner(nil, "c", config.ConnPostgres, false, 100).Specs()
	sn := toolNames(sql)
	if !sn["explain_plan"] || !sn["list_tables"] || sn["redis_keys"] {
		t.Fatalf("postgres specs unexpected: %v", sn)
	}
}
