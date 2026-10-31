package db

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"AI-DB/internal/config"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type mongoDriver struct {
	client   *mongo.Client
	cfg      config.Connection
	mu       sync.Mutex
	activeDB string
}

func openMongo(ctx context.Context, c config.Connection) (Driver, error) {
	port := c.Port
	if port == 0 {
		port = 27017
	}
	uri := fmt.Sprintf("mongodb://%s:%d", c.Host, port)
	opts := options.Client().ApplyURI(uri).SetTimeout(30 * time.Second)
	if c.User != "" {
		opts.SetAuth(options.Credential{
			Username:   c.User,
			Password:   c.Password,
			AuthSource: mongoAuthSource(c),
		})
	}
	client, err := mongo.Connect(opts)
	if err != nil {
		return nil, err
	}
	pingCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx, nil); err != nil {
		_ = client.Disconnect(context.Background())
		return nil, err
	}
	dbName := strings.TrimSpace(c.Database)
	if dbName == "" {
		dbName = "test"
	}
	return &mongoDriver{client: client, cfg: c, activeDB: dbName}, nil
}

// mongoAuthSource picks the authentication database. Explicit AuthSource wins;
// otherwise default to admin (typical for root / Atlas-style users).
func mongoAuthSource(c config.Connection) string {
	if s := strings.TrimSpace(c.AuthSource); s != "" {
		return s
	}
	return "admin"
}

func (d *mongoDriver) Type() config.ConnType { return config.ConnMongo }

func (d *mongoDriver) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	return d.client.Ping(ctx, nil)
}

func (d *mongoDriver) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return d.client.Disconnect(ctx)
}

func (d *mongoDriver) SetActiveSchema(schema string) error {
	schema = strings.TrimSpace(schema)
	if schema == "" {
		return nil
	}
	d.mu.Lock()
	d.activeDB = schema
	d.mu.Unlock()
	return nil
}

func (d *mongoDriver) dbName() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.activeDB != "" {
		return d.activeDB
	}
	if d.cfg.Database != "" {
		return d.cfg.Database
	}
	return "test"
}

func (d *mongoDriver) Execute(ctx context.Context, query string, maxRows int, allowWrites bool) (*QueryResult, error) {
	if err := EnsureMongoReadOnly(query, allowWrites); err != nil {
		return nil, err
	}
	if maxRows <= 0 {
		maxRows = 500
	}
	op, err := parseMongoQuery(query)
	if err != nil {
		return nil, err
	}
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	switch op.Name {
	case "use":
		d.mu.Lock()
		d.activeDB = op.Database
		d.mu.Unlock()
		return &QueryResult{
			Columns:    []string{"message"},
			Rows:       [][]string{{fmt.Sprintf("switched to db %s", op.Database)}},
			RowCount:   1,
			DurationMs: timed(start),
			Message:    "OK",
		}, nil
	case "listDatabases":
		return d.execListDatabases(ctx, start)
	case "listCollections":
		return d.execListCollections(ctx, start, maxRows)
	case "find":
		return d.execFind(ctx, start, op, maxRows)
	case "findOne":
		return d.execFindOne(ctx, start, op)
	case "aggregate":
		return d.execAggregate(ctx, start, op, maxRows)
	case "countDocuments":
		return d.execCount(ctx, start, op)
	case "estimatedDocumentCount":
		return d.execEstimatedCount(ctx, start, op)
	case "getIndexes":
		return d.execGetIndexes(ctx, start, op)
	case "insertOne":
		return d.execInsertOne(ctx, start, op)
	case "insertMany":
		return d.execInsertMany(ctx, start, op)
	case "updateOne", "updateMany":
		return d.execUpdate(ctx, start, op)
	case "deleteOne", "deleteMany":
		return d.execDelete(ctx, start, op)
	case "drop":
		return d.execDrop(ctx, start, op)
	default:
		return nil, fmt.Errorf("unsupported mongo op: %s", op.Name)
	}
}

func (d *mongoDriver) coll(name string) *mongo.Collection {
	return d.client.Database(d.dbName()).Collection(name)
}

func (d *mongoDriver) execListDatabases(ctx context.Context, start time.Time) (*QueryResult, error) {
	names, err := d.client.ListDatabaseNames(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	rows := make([][]string, 0, len(names))
	for _, n := range names {
		rows = append(rows, []string{n})
	}
	return &QueryResult{
		Columns:    []string{"database"},
		Rows:       rows,
		RowCount:   len(rows),
		DurationMs: timed(start),
		Message:    "OK",
	}, nil
}

func (d *mongoDriver) execListCollections(ctx context.Context, start time.Time, maxRows int) (*QueryResult, error) {
	names, err := d.client.Database(d.dbName()).ListCollectionNames(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	truncated := false
	if len(names) > maxRows {
		names = names[:maxRows]
		truncated = true
	}
	rows := make([][]string, 0, len(names))
	for _, n := range names {
		rows = append(rows, []string{n})
	}
	return &QueryResult{
		Columns:    []string{"collection"},
		Rows:       rows,
		RowCount:   len(rows),
		DurationMs: timed(start),
		Truncated:  truncated,
		Message:    "OK",
	}, nil
}

func (d *mongoDriver) execFind(ctx context.Context, start time.Time, op *mongoOp, maxRows int) (*QueryResult, error) {
	filter := op.Filter
	if filter == nil {
		filter = bson.M{}
	}
	findOpts := options.Find()
	limit := int64(maxRows)
	if op.HasLimit {
		limit = op.Limit
		if limit > int64(maxRows) {
			limit = int64(maxRows)
		}
		if limit < 0 {
			limit = int64(maxRows)
		}
	}
	findOpts.SetLimit(limit)
	if op.HasSkip {
		findOpts.SetSkip(op.Skip)
	}
	if len(op.Sort) > 0 {
		findOpts.SetSort(op.Sort)
	}
	if op.Projection != nil {
		findOpts.SetProjection(op.Projection)
	}
	cur, err := d.coll(op.Collection).Find(ctx, filter, findOpts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	docs, err := decodeCursorDocs(cur, ctx, maxRows+1)
	if err != nil {
		return nil, err
	}
	truncated := false
	if len(docs) > maxRows {
		docs = docs[:maxRows]
		truncated = true
	}
	cols, rows := docsToTable(docs)
	return &QueryResult{
		Columns:    cols,
		Rows:       rows,
		RowCount:   len(rows),
		DurationMs: timed(start),
		Truncated:  truncated,
		Message:    "OK",
	}, nil
}

func (d *mongoDriver) execFindOne(ctx context.Context, start time.Time, op *mongoOp) (*QueryResult, error) {
	filter := op.Filter
	if filter == nil {
		filter = bson.M{}
	}
	opts := options.FindOne()
	if op.Projection != nil {
		opts.SetProjection(op.Projection)
	}
	if len(op.Sort) > 0 {
		opts.SetSort(op.Sort)
	}
	var doc bson.M
	err := d.coll(op.Collection).FindOne(ctx, filter, opts).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return &QueryResult{
			Columns:    []string{"json"},
			Rows:       nil,
			RowCount:   0,
			DurationMs: timed(start),
			Message:    "null",
		}, nil
	}
	if err != nil {
		return nil, err
	}
	cols, rows := docsToTable([]bson.M{doc})
	return &QueryResult{
		Columns:    cols,
		Rows:       rows,
		RowCount:   len(rows),
		DurationMs: timed(start),
		Message:    "OK",
	}, nil
}

func (d *mongoDriver) execAggregate(ctx context.Context, start time.Time, op *mongoOp, maxRows int) (*QueryResult, error) {
	pipe := op.Pipeline
	if pipe == nil {
		pipe = bson.A{}
	}
	cur, err := d.coll(op.Collection).Aggregate(ctx, pipe)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	docs, err := decodeCursorDocs(cur, ctx, maxRows+1)
	if err != nil {
		return nil, err
	}
	truncated := false
	if len(docs) > maxRows {
		docs = docs[:maxRows]
		truncated = true
	}
	cols, rows := docsToTable(docs)
	return &QueryResult{
		Columns:    cols,
		Rows:       rows,
		RowCount:   len(rows),
		DurationMs: timed(start),
		Truncated:  truncated,
		Message:    "OK",
	}, nil
}

func (d *mongoDriver) execCount(ctx context.Context, start time.Time, op *mongoOp) (*QueryResult, error) {
	filter := op.Filter
	if filter == nil {
		filter = bson.M{}
	}
	n, err := d.coll(op.Collection).CountDocuments(ctx, filter)
	if err != nil {
		return nil, err
	}
	return &QueryResult{
		Columns:    []string{"count"},
		Rows:       [][]string{{fmt.Sprintf("%d", n)}},
		RowCount:   1,
		DurationMs: timed(start),
		Message:    "OK",
	}, nil
}

func (d *mongoDriver) execEstimatedCount(ctx context.Context, start time.Time, op *mongoOp) (*QueryResult, error) {
	n, err := d.coll(op.Collection).EstimatedDocumentCount(ctx)
	if err != nil {
		return nil, err
	}
	return &QueryResult{
		Columns:    []string{"count"},
		Rows:       [][]string{{fmt.Sprintf("%d", n)}},
		RowCount:   1,
		DurationMs: timed(start),
		Message:    "OK",
	}, nil
}

func (d *mongoDriver) execGetIndexes(ctx context.Context, start time.Time, op *mongoOp) (*QueryResult, error) {
	cur, err := d.coll(op.Collection).Indexes().List(ctx)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	docs, err := decodeCursorDocs(cur, ctx, 200)
	if err != nil {
		return nil, err
	}
	cols, rows := docsToTable(docs)
	return &QueryResult{
		Columns:    cols,
		Rows:       rows,
		RowCount:   len(rows),
		DurationMs: timed(start),
		Message:    "OK",
	}, nil
}

func (d *mongoDriver) execInsertOne(ctx context.Context, start time.Time, op *mongoOp) (*QueryResult, error) {
	if len(op.Documents) == 0 {
		return nil, fmt.Errorf("insertOne requires a document")
	}
	res, err := d.coll(op.Collection).InsertOne(ctx, op.Documents[0])
	if err != nil {
		return nil, err
	}
	return &QueryResult{
		Columns:    []string{"insertedId"},
		Rows:       [][]string{{stringifyBSONValue(res.InsertedID)}},
		RowCount:   1,
		DurationMs: timed(start),
		Message:    "OK",
	}, nil
}

func (d *mongoDriver) execInsertMany(ctx context.Context, start time.Time, op *mongoOp) (*QueryResult, error) {
	if len(op.Documents) == 0 {
		return nil, fmt.Errorf("insertMany requires documents")
	}
	res, err := d.coll(op.Collection).InsertMany(ctx, op.Documents)
	if err != nil {
		return nil, err
	}
	rows := make([][]string, 0, len(res.InsertedIDs))
	for _, id := range res.InsertedIDs {
		rows = append(rows, []string{stringifyBSONValue(id)})
	}
	return &QueryResult{
		Columns:    []string{"insertedId"},
		Rows:       rows,
		RowCount:   len(rows),
		DurationMs: timed(start),
		Message:    "OK",
	}, nil
}

func (d *mongoDriver) execUpdate(ctx context.Context, start time.Time, op *mongoOp) (*QueryResult, error) {
	filter := op.Filter
	if filter == nil {
		filter = bson.M{}
	}
	var (
		matched, modified int64
		err               error
	)
	if op.Name == "updateOne" {
		res, e := d.coll(op.Collection).UpdateOne(ctx, filter, op.Update)
		err = e
		if res != nil {
			matched, modified = res.MatchedCount, res.ModifiedCount
		}
	} else {
		res, e := d.coll(op.Collection).UpdateMany(ctx, filter, op.Update)
		err = e
		if res != nil {
			matched, modified = res.MatchedCount, res.ModifiedCount
		}
	}
	if err != nil {
		return nil, err
	}
	return &QueryResult{
		Columns:    []string{"matched", "modified"},
		Rows:       [][]string{{fmt.Sprintf("%d", matched), fmt.Sprintf("%d", modified)}},
		RowCount:   1,
		DurationMs: timed(start),
		Message:    "OK",
	}, nil
}

func (d *mongoDriver) execDelete(ctx context.Context, start time.Time, op *mongoOp) (*QueryResult, error) {
	filter := op.Filter
	if filter == nil {
		filter = bson.M{}
	}
	var deleted int64
	var err error
	if op.Name == "deleteOne" {
		res, e := d.coll(op.Collection).DeleteOne(ctx, filter)
		err = e
		if res != nil {
			deleted = res.DeletedCount
		}
	} else {
		res, e := d.coll(op.Collection).DeleteMany(ctx, filter)
		err = e
		if res != nil {
			deleted = res.DeletedCount
		}
	}
	if err != nil {
		return nil, err
	}
	return &QueryResult{
		Columns:    []string{"deleted"},
		Rows:       [][]string{{fmt.Sprintf("%d", deleted)}},
		RowCount:   1,
		DurationMs: timed(start),
		Message:    "OK",
	}, nil
}

func (d *mongoDriver) execDrop(ctx context.Context, start time.Time, op *mongoOp) (*QueryResult, error) {
	if err := d.coll(op.Collection).Drop(ctx); err != nil {
		return nil, err
	}
	return &QueryResult{
		Columns:    []string{"message"},
		Rows:       [][]string{{fmt.Sprintf("dropped %s", op.Collection)}},
		RowCount:   1,
		DurationMs: timed(start),
		Message:    "OK",
	}, nil
}

func decodeCursorDocs(cur *mongo.Cursor, ctx context.Context, limit int) ([]bson.M, error) {
	out := make([]bson.M, 0, 32)
	for cur.Next(ctx) {
		var doc bson.M
		if err := cur.Decode(&doc); err != nil {
			return nil, err
		}
		out = append(out, doc)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	if err := cur.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func docsToTable(docs []bson.M) (cols []string, rows [][]string) {
	if len(docs) == 0 {
		return []string{"json"}, nil
	}
	keySet := map[string]struct{}{}
	for _, doc := range docs {
		for k := range doc {
			keySet[k] = struct{}{}
		}
	}
	keys := make([]string, 0, len(keySet))
	for k := range keySet {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i] == "_id" {
			return true
		}
		if keys[j] == "_id" {
			return false
		}
		return keys[i] < keys[j]
	})
	rows = make([][]string, 0, len(docs))
	for _, doc := range docs {
		row := make([]string, len(keys))
		for i, k := range keys {
			row[i] = stringifyBSONValue(doc[k])
		}
		rows = append(rows, row)
	}
	return keys, rows
}

func stringifyBSONValue(v any) string {
	if v == nil {
		return "NULL"
	}
	switch t := v.(type) {
	case string:
		return t
	case bson.ObjectID:
		return t.Hex()
	case []byte:
		return string(t)
	case time.Time:
		return t.UTC().Format(time.RFC3339Nano)
	case bson.M, bson.D, bson.A, map[string]any, []any:
		b, err := bson.MarshalExtJSON(t, false, false)
		if err != nil {
			return fmt.Sprint(t)
		}
		return string(b)
	default:
		b, err := bson.MarshalExtJSON(t, false, false)
		if err != nil {
			return fmt.Sprint(t)
		}
		// ExtJSON wraps scalars in quotes sometimes; prefer plain Sprint for numbers/bools.
		s := string(b)
		if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
			return s[1 : len(s)-1]
		}
		return s
	}
}

func (d *mongoDriver) ListSchemas(ctx context.Context) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return d.client.ListDatabaseNames(ctx, bson.M{})
}

func (d *mongoDriver) ListTables(ctx context.Context, schema string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	dbName := schema
	if dbName == "" {
		dbName = d.dbName()
	}
	names, err := d.client.Database(dbName).ListCollectionNames(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	if len(names) > 200 {
		names = names[:200]
	}
	return names, nil
}

func (d *mongoDriver) DescribeTable(ctx context.Context, schema, table string) (*QueryResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	dbName := schema
	if dbName == "" {
		dbName = d.dbName()
	}
	cur, err := d.client.Database(dbName).Collection(table).Indexes().List(ctx)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	docs, err := decodeCursorDocs(cur, ctx, 100)
	if err != nil {
		return nil, err
	}
	cols, rows := docsToTable(docs)
	return &QueryResult{Columns: cols, Rows: rows, RowCount: len(rows), Message: "OK"}, nil
}

func (d *mongoDriver) Explain(ctx context.Context, _ string) (*QueryResult, error) {
	return nil, fmt.Errorf("EXPLAIN is not supported for MongoDB")
}

func (d *mongoDriver) ListObjects(ctx context.Context) ([]ObjectNode, error) {
	names, err := d.ListSchemas(ctx)
	if err != nil {
		return nil, err
	}
	return namesToNodes(names, "database", true), nil
}

func (d *mongoDriver) ExpandObject(ctx context.Context, path []string) ([]ObjectNode, error) {
	if len(path) == 0 {
		return d.ListObjects(ctx)
	}
	if len(path) == 1 {
		names, err := d.ListTables(ctx, path[0])
		if err != nil {
			return nil, err
		}
		// Lazy so collections can drill into sample fields + indexes.
		return namesToNodes(names, "collection", true), nil
	}
	if len(path) == 2 {
		return tableDetailFolders(), nil
	}
	if len(path) == 3 {
		dbName, coll := path[0], path[1]
		switch path[2] {
		case CatColumns:
			return d.listSampleFieldNodes(ctx, dbName, coll)
		case CatIndexes:
			return d.listIndexNodes(ctx, dbName, coll)
		}
	}
	return nil, badObjectPath(path)
}

func (d *mongoDriver) listSampleFieldNodes(ctx context.Context, dbName, coll string) ([]ObjectNode, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var doc bson.M
	err := d.client.Database(dbName).Collection(coll).FindOne(ctx, bson.M{}).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(doc))
	for k := range doc {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i] == "_id" {
			return true
		}
		if keys[j] == "_id" {
			return false
		}
		return keys[i] < keys[j]
	})
	out := make([]ObjectNode, 0, len(keys))
	for _, k := range keys {
		label := k
		if v, ok := doc[k]; ok && v != nil {
			label = k + " · " + roughBSONType(v)
		}
		out = append(out, ObjectNode{Name: label, Kind: "column"})
	}
	return out, nil
}

func roughBSONType(v any) string {
	switch v.(type) {
	case string:
		return "string"
	case int32, int64, int, float64, float32:
		return "number"
	case bool:
		return "bool"
	case bson.ObjectID:
		return "ObjectId"
	case time.Time:
		return "date"
	case bson.A, []any:
		return "array"
	case bson.M, bson.D, map[string]any:
		return "object"
	default:
		return "value"
	}
}

func (d *mongoDriver) listIndexNodes(ctx context.Context, dbName, coll string) ([]ObjectNode, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cur, err := d.client.Database(dbName).Collection(coll).Indexes().List(ctx)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	docs, err := decodeCursorDocs(cur, ctx, 200)
	if err != nil {
		return nil, err
	}
	out := make([]ObjectNode, 0, len(docs))
	for _, doc := range docs {
		name, _ := doc["name"].(string)
		if name == "" {
			continue
		}
		label := name
		if key, ok := doc["key"]; ok {
			label = name + " (" + stringifyBSONValue(key) + ")"
		}
		n := ObjectNode{Name: label, Kind: "index"}
		if u, ok := doc["unique"].(bool); ok && u {
			n.Badge = "UNI"
		}
		if name == "_id_" {
			n.Badge = "PK"
		}
		out = append(out, n)
	}
	return out, nil
}
