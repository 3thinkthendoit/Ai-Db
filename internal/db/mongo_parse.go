package db

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// mongoOp is a parsed mongosh-lite / verb command.
type mongoOp struct {
	Name       string // find, findOne, aggregate, countDocuments, estimatedDocumentCount, insertOne, insertMany, updateOne, updateMany, deleteOne, deleteMany, drop, listDatabases, listCollections, use
	Database   string // for use
	Collection string
	Filter     bson.M
	Update     bson.M
	Pipeline   bson.A
	Documents  []any
	Limit      int64
	Skip       int64
	Sort       bson.D
	Projection bson.M
	HasLimit   bool
	HasSkip    bool
}

var (
	reDBMethod = regexp.MustCompile(`(?is)^db\.(?:getCollection\(\s*['"]([^'"]+)['"]\s*\)|([A-Za-z_][\w$]*))\s*\.\s*([A-Za-z_][\w]*)\s*\(`)
	reUse      = regexp.MustCompile(`(?is)^use\s+([A-Za-z_][\w$-]*)\s*;?\s*$`)
	reShow     = regexp.MustCompile(`(?is)^show\s+(dbs|databases|collections|tables)\s*;?\s*$`)
	reVerb     = regexp.MustCompile(`(?is)^(find|findOne|aggregate|countDocuments|count|estimatedDocumentCount|getIndexes|insertOne|insertMany|updateOne|updateMany|deleteOne|deleteMany|drop|listDatabases|listCollections)\b`)
)

func parseMongoQuery(query string) (*mongoOp, error) {
	q := strings.TrimSpace(query)
	q = strings.TrimSuffix(q, ";")
	q = strings.TrimSpace(q)
	if q == "" {
		return nil, fmt.Errorf("empty command")
	}

	if m := reUse.FindStringSubmatch(q); m != nil {
		return &mongoOp{Name: "use", Database: m[1]}, nil
	}
	if m := reShow.FindStringSubmatch(q); m != nil {
		switch strings.ToLower(m[1]) {
		case "dbs", "databases":
			return &mongoOp{Name: "listDatabases"}, nil
		default:
			return &mongoOp{Name: "listCollections"}, nil
		}
	}

	if m := reDBMethod.FindStringSubmatchIndex(q); m != nil {
		// groups: 1=getCollection name, 2=bare coll, 3=method; full match ends after '('
		coll := ""
		if m[2] >= 0 {
			coll = q[m[2]:m[3]]
		} else if m[4] >= 0 {
			coll = q[m[4]:m[5]]
		}
		method := strings.ToLower(q[m[6]:m[7]])
		openParen := m[1] - 1
		if openParen < 0 || q[openParen] != '(' {
			return nil, fmt.Errorf("invalid method call")
		}
		args, end, err := extractParenContents(q, openParen)
		if err != nil {
			return nil, err
		}
		rest := strings.TrimSpace(q[end:])
		op, err := buildMongoMethodOp(method, coll, args)
		if err != nil {
			return nil, err
		}
		if err := applyMongoChain(op, rest); err != nil {
			return nil, err
		}
		return op, nil
	}

	// Verb form: find users {...}  /  listDatabases  /  drop users
	if m := reVerb.FindStringSubmatchIndex(q); m != nil {
		name := strings.ToLower(q[m[2]:m[3]])
		rest := strings.TrimSpace(q[m[1]:])
		return parseMongoVerb(name, rest)
	}

	return nil, fmt.Errorf("unsupported mongo command (try db.<coll>.find({...}) or show collections)")
}

func buildMongoMethodOp(method, coll, args string) (*mongoOp, error) {
	op := &mongoOp{Collection: coll}
	parts := splitTopLevelArgs(args)
	switch method {
	case "find":
		op.Name = "find"
		f, err := parseBSONMapArg(firstOrEmpty(parts))
		if err != nil {
			return nil, fmt.Errorf("find filter: %w", err)
		}
		op.Filter = f
		if len(parts) > 1 {
			p, err := parseBSONMapArg(parts[1])
			if err != nil {
				return nil, fmt.Errorf("find projection: %w", err)
			}
			op.Projection = p
		}
	case "findone":
		op.Name = "findOne"
		f, err := parseBSONMapArg(firstOrEmpty(parts))
		if err != nil {
			return nil, fmt.Errorf("findOne filter: %w", err)
		}
		op.Filter = f
		if len(parts) > 1 {
			p, err := parseBSONMapArg(parts[1])
			if err != nil {
				return nil, fmt.Errorf("findOne projection: %w", err)
			}
			op.Projection = p
		}
	case "aggregate":
		op.Name = "aggregate"
		pipe, err := parseBSONArrayArg(firstOrEmpty(parts))
		if err != nil {
			return nil, fmt.Errorf("aggregate pipeline: %w", err)
		}
		op.Pipeline = pipe
	case "countdocuments", "count":
		op.Name = "countDocuments"
		f, err := parseBSONMapArg(firstOrEmpty(parts))
		if err != nil {
			return nil, fmt.Errorf("count filter: %w", err)
		}
		op.Filter = f
	case "estimateddocumentcount":
		op.Name = "estimatedDocumentCount"
	case "insertone":
		op.Name = "insertOne"
		doc, err := parseBSONAnyArg(firstOrEmpty(parts))
		if err != nil {
			return nil, fmt.Errorf("insertOne document: %w", err)
		}
		op.Documents = []any{doc}
	case "insertmany":
		op.Name = "insertMany"
		arr, err := parseBSONArrayArg(firstOrEmpty(parts))
		if err != nil {
			return nil, fmt.Errorf("insertMany documents: %w", err)
		}
		op.Documents = []any(arr)
	case "updateone":
		op.Name = "updateOne"
		if len(parts) < 2 {
			return nil, fmt.Errorf("updateOne requires filter and update")
		}
		f, err := parseBSONMapArg(parts[0])
		if err != nil {
			return nil, fmt.Errorf("updateOne filter: %w", err)
		}
		u, err := parseBSONMapArg(parts[1])
		if err != nil {
			return nil, fmt.Errorf("updateOne update: %w", err)
		}
		op.Filter, op.Update = f, u
	case "updatemany":
		op.Name = "updateMany"
		if len(parts) < 2 {
			return nil, fmt.Errorf("updateMany requires filter and update")
		}
		f, err := parseBSONMapArg(parts[0])
		if err != nil {
			return nil, fmt.Errorf("updateMany filter: %w", err)
		}
		u, err := parseBSONMapArg(parts[1])
		if err != nil {
			return nil, fmt.Errorf("updateMany update: %w", err)
		}
		op.Filter, op.Update = f, u
	case "deleteone":
		op.Name = "deleteOne"
		f, err := parseBSONMapArg(firstOrEmpty(parts))
		if err != nil {
			return nil, fmt.Errorf("deleteOne filter: %w", err)
		}
		op.Filter = f
	case "deletemany":
		op.Name = "deleteMany"
		f, err := parseBSONMapArg(firstOrEmpty(parts))
		if err != nil {
			return nil, fmt.Errorf("deleteMany filter: %w", err)
		}
		op.Filter = f
	case "drop":
		op.Name = "drop"
	case "getindexes":
		op.Name = "getIndexes"
	default:
		return nil, fmt.Errorf("unsupported method: %s", method)
	}
	return op, nil
}

func parseMongoVerb(name, rest string) (*mongoOp, error) {
	switch name {
	case "listdatabases":
		return &mongoOp{Name: "listDatabases"}, nil
	case "listcollections":
		return &mongoOp{Name: "listCollections"}, nil
	case "estimateddocumentcount":
		coll, _ := splitFirstToken(rest)
		if coll == "" {
			return nil, fmt.Errorf("estimatedDocumentCount requires collection name")
		}
		return &mongoOp{Name: "estimatedDocumentCount", Collection: coll}, nil
	case "drop":
		coll, _ := splitFirstToken(rest)
		if coll == "" {
			return nil, fmt.Errorf("drop requires collection name")
		}
		return &mongoOp{Name: "drop", Collection: coll}, nil
	case "find", "findone", "aggregate", "countdocuments", "count", "getindexes",
		"insertone", "insertmany", "updateone", "updatemany", "deleteone", "deletemany":
		coll, after := splitFirstToken(rest)
		if coll == "" {
			return nil, fmt.Errorf("%s requires collection name", name)
		}
		return buildMongoMethodOp(name, coll, strings.TrimSpace(after))
	default:
		return nil, fmt.Errorf("unsupported verb: %s", name)
	}
}

func applyMongoChain(op *mongoOp, rest string) error {
	rest = strings.TrimSpace(rest)
	for rest != "" {
		if !strings.HasPrefix(rest, ".") {
			return fmt.Errorf("unexpected trailing input: %s", rest)
		}
		rest = strings.TrimSpace(rest[1:])
		name, afterName, ok := readIdent(rest)
		if !ok {
			return fmt.Errorf("invalid chain call")
		}
		rest = strings.TrimSpace(afterName)
		if !strings.HasPrefix(rest, "(") {
			return fmt.Errorf("expected ( after .%s", name)
		}
		args, end, err := extractParenContents(rest, 0)
		if err != nil {
			return err
		}
		rest = strings.TrimSpace(rest[end:])
		switch strings.ToLower(name) {
		case "limit":
			n, err := strconv.ParseInt(strings.TrimSpace(args), 10, 64)
			if err != nil {
				return fmt.Errorf("limit: %w", err)
			}
			op.Limit, op.HasLimit = n, true
		case "skip":
			n, err := strconv.ParseInt(strings.TrimSpace(args), 10, 64)
			if err != nil {
				return fmt.Errorf("skip: %w", err)
			}
			op.Skip, op.HasSkip = n, true
		case "sort":
			d, err := parseBSONDocOrder(args)
			if err != nil {
				return fmt.Errorf("sort: %w", err)
			}
			op.Sort = d
		case "project", "projection":
			p, err := parseBSONMapArg(args)
			if err != nil {
				return fmt.Errorf("project: %w", err)
			}
			op.Projection = p
		default:
			return fmt.Errorf("unsupported chain method: %s", name)
		}
	}
	return nil
}

func firstOrEmpty(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}

func splitFirstToken(s string) (token, rest string) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", ""
	}
	i := 0
	for i < len(s) && !unicode.IsSpace(rune(s[i])) {
		i++
	}
	return s[:i], strings.TrimSpace(s[i:])
}

func readIdent(s string) (name, rest string, ok bool) {
	if s == "" || !(unicode.IsLetter(rune(s[0])) || s[0] == '_') {
		return "", s, false
	}
	i := 1
	for i < len(s) {
		r := rune(s[i])
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			i++
			continue
		}
		break
	}
	return s[:i], s[i:], true
}

func extractParenContents(s string, openIdx int) (inner string, endExclusive int, err error) {
	if openIdx < 0 || openIdx >= len(s) || s[openIdx] != '(' {
		return "", 0, fmt.Errorf("expected '('")
	}
	depth := 0
	inStr := byte(0)
	esc := false
	for i := openIdx; i < len(s); i++ {
		c := s[i]
		if inStr != 0 {
			if esc {
				esc = false
				continue
			}
			if c == '\\' {
				esc = true
				continue
			}
			if c == inStr {
				inStr = 0
			}
			continue
		}
		switch c {
		case '\'', '"':
			inStr = c
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return s[openIdx+1 : i], i + 1, nil
			}
		}
	}
	return "", 0, fmt.Errorf("unbalanced parentheses")
}

func splitTopLevelArgs(args string) []string {
	args = strings.TrimSpace(args)
	if args == "" {
		return nil
	}
	var out []string
	depth := 0
	inStr := byte(0)
	esc := false
	start := 0
	for i := 0; i < len(args); i++ {
		c := args[i]
		if inStr != 0 {
			if esc {
				esc = false
				continue
			}
			if c == '\\' {
				esc = true
				continue
			}
			if c == inStr {
				inStr = 0
			}
			continue
		}
		switch c {
		case '\'', '"':
			inStr = c
		case '{', '[', '(':
			depth++
		case '}', ']', ')':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, strings.TrimSpace(args[start:i]))
				start = i + 1
			}
		}
	}
	out = append(out, strings.TrimSpace(args[start:]))
	return out
}

func parseBSONMapArg(s string) (bson.M, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "{}" {
		return bson.M{}, nil
	}
	var m bson.M
	if err := tryUnmarshalBSON(s, &m); err != nil {
		return nil, err
	}
	if m == nil {
		m = bson.M{}
	}
	return m, nil
}

func parseBSONArrayArg(s string) (bson.A, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "[]" {
		return bson.A{}, nil
	}
	var a bson.A
	if err := tryUnmarshalBSON(s, &a); err != nil {
		return nil, err
	}
	if a == nil {
		a = bson.A{}
	}
	return a, nil
}

func parseBSONAnyArg(s string) (any, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fmt.Errorf("empty document")
	}
	var v any
	if err := tryUnmarshalBSON(s, &v); err != nil {
		return nil, err
	}
	return v, nil
}

func parseBSONDocOrder(s string) (bson.D, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "{}" {
		return bson.D{}, nil
	}
	// Preserve key order via json.Decoder into map is unordered; use bson.D via ExtJSON.
	var d bson.D
	if err := tryUnmarshalBSON(s, &d); err != nil {
		return nil, err
	}
	return d, nil
}

func tryUnmarshalBSON(s string, dest any) error {
	if err := bson.UnmarshalExtJSON([]byte(s), false, dest); err == nil {
		return nil
	}
	if err := json.Unmarshal([]byte(s), dest); err == nil {
		return nil
	}
	fixed := jsLikeToJSON(s)
	if fixed != s {
		if err := json.Unmarshal([]byte(fixed), dest); err == nil {
			return nil
		}
		if err := bson.UnmarshalExtJSON([]byte(fixed), false, dest); err == nil {
			return nil
		}
	}
	return fmt.Errorf("invalid JSON (use double-quoted keys): %s", truncateRunes(s, 80))
}

// jsLikeToJSON converts light JS object literals: single quotes → double, unquoted keys → quoted.
func jsLikeToJSON(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 16)
	inStr := byte(0)
	esc := false
	for i := 0; i < len(s); {
		c := s[i]
		if inStr != 0 {
			if esc {
				b.WriteByte(c)
				esc = false
				i++
				continue
			}
			if c == '\\' {
				b.WriteByte(c)
				esc = true
				i++
				continue
			}
			if c == inStr {
				b.WriteByte('"')
				inStr = 0
				i++
				continue
			}
			if c == '"' && inStr == '\'' {
				b.WriteString(`\"`)
				i++
				continue
			}
			b.WriteByte(c)
			i++
			continue
		}
		switch c {
		case '\'', '"':
			inStr = c
			b.WriteByte('"')
			i++
		default:
			// Unquoted key: identifier before ':'
			if (c == '{' || c == ',') || (unicode.IsSpace(rune(c))) {
				b.WriteByte(c)
				i++
				// peek ahead for ident:
				j := i
				for j < len(s) && unicode.IsSpace(rune(s[j])) {
					b.WriteByte(s[j])
					j++
				}
				if j < len(s) && (unicode.IsLetter(rune(s[j])) || s[j] == '_' || s[j] == '$') {
					k := j + 1
					for k < len(s) {
						r := rune(s[k])
						if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '$' {
							k++
							continue
						}
						break
					}
					t := k
					for t < len(s) && unicode.IsSpace(rune(s[t])) {
						t++
					}
					if t < len(s) && s[t] == ':' {
						b.WriteByte('"')
						b.WriteString(s[j:k])
						b.WriteByte('"')
						for j2 := k; j2 < t; j2++ {
							b.WriteByte(s[j2])
						}
						i = t
						continue
					}
				}
				i = j
				continue
			}
			b.WriteByte(c)
			i++
		}
	}
	return b.String()
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func mongoOpIsReadOnly(name string) bool {
	switch strings.ToLower(name) {
	case "find", "findone", "aggregate", "countdocuments", "estimateddocumentcount",
		"getindexes", "listdatabases", "listcollections", "use", "show":
		return true
	default:
		return false
	}
}
