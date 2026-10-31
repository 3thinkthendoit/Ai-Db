package db

import (
	"testing"
)

func TestParseMongoFindChain(t *testing.T) {
	op, err := parseMongoQuery(`db.users.find({"age":18}).limit(10).skip(2)`)
	if err != nil {
		t.Fatal(err)
	}
	if op.Name != "find" || op.Collection != "users" {
		t.Fatalf("got %+v", op)
	}
	if !op.HasLimit || op.Limit != 10 || !op.HasSkip || op.Skip != 2 {
		t.Fatalf("limit/skip: %+v", op)
	}
	if op.Filter["age"] != float64(18) && op.Filter["age"] != int32(18) && op.Filter["age"] != int64(18) {
		// ExtJSON/json may decode numbers differently
		if n, ok := asInt(op.Filter["age"]); !ok || n != 18 {
			t.Fatalf("filter age=%v (%T)", op.Filter["age"], op.Filter["age"])
		}
	}
}

func asInt(v any) (int64, bool) {
	switch n := v.(type) {
	case int:
		return int64(n), true
	case int32:
		return int64(n), true
	case int64:
		return n, true
	case float64:
		return int64(n), true
	default:
		return 0, false
	}
}

func TestParseMongoGetCollection(t *testing.T) {
	op, err := parseMongoQuery(`db.getCollection("my-coll").find({})`)
	if err != nil {
		t.Fatal(err)
	}
	if op.Collection != "my-coll" || op.Name != "find" {
		t.Fatalf("got %+v", op)
	}
}

func TestParseMongoShowAndUse(t *testing.T) {
	op, err := parseMongoQuery("show collections")
	if err != nil || op.Name != "listCollections" {
		t.Fatalf("show: %+v %v", op, err)
	}
	op, err = parseMongoQuery("use mydb")
	if err != nil || op.Name != "use" || op.Database != "mydb" {
		t.Fatalf("use: %+v %v", op, err)
	}
}

func TestParseMongoVerbFind(t *testing.T) {
	op, err := parseMongoQuery(`find users {"x":1}`)
	if err != nil {
		t.Fatal(err)
	}
	if op.Name != "find" || op.Collection != "users" {
		t.Fatalf("got %+v", op)
	}
}

func TestParseMongoUpdate(t *testing.T) {
	op, err := parseMongoQuery(`db.users.updateOne({"_id":1},{"$set":{"a":2}})`)
	if err != nil {
		t.Fatal(err)
	}
	if op.Name != "updateOne" || op.Update["$set"] == nil {
		t.Fatalf("got %+v", op)
	}
}

func TestParseMongoJSLikeFilter(t *testing.T) {
	op, err := parseMongoQuery(`db.users.find({age: 18})`)
	if err != nil {
		t.Fatal(err)
	}
	if n, ok := asInt(op.Filter["age"]); !ok || n != 18 {
		t.Fatalf("filter=%v", op.Filter)
	}
}

func TestEnsureMongoReadOnly(t *testing.T) {
	if err := EnsureMongoReadOnly(`db.users.find({})`, false); err != nil {
		t.Fatal(err)
	}
	if err := EnsureMongoReadOnly(`db.users.insertOne({"a":1})`, false); err == nil {
		t.Fatal("expected block")
	}
	if err := EnsureMongoReadOnly(`db.users.insertOne({"a":1})`, true); err != nil {
		t.Fatal(err)
	}
	if err := EnsureMongoReadOnly(`not a command!!!`, false); err == nil {
		t.Fatal("expected parse error")
	}
	if MongoQueryMutating(`not a command!!!`) {
		t.Fatal("unparsable should not count as mutating")
	}
	if !MongoQueryMutating(`db.users.insertOne({"a":1})`) {
		t.Fatal("insertOne should be mutating")
	}
	if MongoQueryMutating(`db.users.find({})`) {
		t.Fatal("find should not be mutating")
	}
}

func TestParseMongoAggregate(t *testing.T) {
	op, err := parseMongoQuery(`db.users.aggregate([{"$match":{"a":1}}])`)
	if err != nil {
		t.Fatal(err)
	}
	if op.Name != "aggregate" || len(op.Pipeline) != 1 {
		t.Fatalf("got %+v", op)
	}
}
