package db

import "fmt"

// Schema category folder keys (stable; UI localizes labels).
const (
	CatTables      = "tables"
	CatViews       = "views"
	CatProcedures  = "procedures"
	CatFunctions   = "functions"
	CatTriggers    = "triggers"
	CatEvents      = "events"
	CatColumns     = "columns"
	CatIndexes     = "indexes"
	CatSequences   = "sequences"
)

func folderNode(name string) ObjectNode {
	return ObjectNode{Name: name, Kind: "folder", Lazy: true}
}

func mysqlSchemaFolders() []ObjectNode {
	return []ObjectNode{
		folderNode(CatTables),
		folderNode(CatViews),
		folderNode(CatProcedures),
		folderNode(CatFunctions),
		folderNode(CatTriggers),
		folderNode(CatEvents),
	}
}

func postgresSchemaFolders() []ObjectNode {
	return []ObjectNode{
		folderNode(CatTables),
		folderNode(CatViews),
		folderNode(CatFunctions),
		folderNode(CatProcedures),
		folderNode(CatSequences),
	}
}

func tableDetailFolders() []ObjectNode {
	return []ObjectNode{
		folderNode(CatColumns),
		folderNode(CatIndexes),
	}
}

func namesToNodes(names []string, kind string, lazy bool) []ObjectNode {
	out := make([]ObjectNode, 0, len(names))
	for _, n := range names {
		out = append(out, ObjectNode{Name: n, Kind: kind, Lazy: lazy})
	}
	return out
}

func rowsFirstCol(res *QueryResult) []string {
	if res == nil {
		return nil
	}
	out := make([]string, 0, res.RowCount)
	for _, r := range res.Rows {
		if len(r) > 0 {
			out = append(out, r[0])
		}
	}
	return out
}

func badObjectPath(path []string) error {
	return fmt.Errorf("unsupported object path: %v", path)
}
