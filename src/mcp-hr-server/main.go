package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"

	_ "github.com/go-sql-driver/mysql"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

var (
	db   *sql.DB
	dbMu sync.RWMutex
)

// --- 工具 1: 获取数据库 Schema ---
func listSchemaHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	dbMu.RLock()
	defer dbMu.RUnlock()

	if db == nil {
		return mcp.NewToolResultError("尚未连接数据库，请先使用 connect_database 工具连接"), nil
	}

	rows, err := db.Query("SHOW TABLES")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Error listing tables: %v", err)), nil
	}
	defer rows.Close()

	type columnInfo struct {
		Field   string `json:"field"`
		Type    string `json:"type"`
		Null    string `json:"null"`
		Key     string `json:"key"`
		Default string `json:"default"`
		Extra   string `json:"extra"`
	}
	type tableInfo struct {
		TableName string       `json:"table_name"`
		Columns   []columnInfo `json:"columns"`
	}
	var schema []tableInfo

	for rows.Next() {
		var tableName string
		if err := rows.Scan(&tableName); err != nil {
			continue
		}

		cols, err := db.Query(fmt.Sprintf("DESCRIBE %s", tableName))
		if err != nil {
			continue
		}

		var columns []columnInfo
		for cols.Next() {
			var col columnInfo
			var null, key, default_, extra sql.NullString
			if err := cols.Scan(&col.Field, &col.Type, &null, &key, &default_, &extra); err != nil {
				continue
			}
			if null.Valid {
				col.Null = null.String
			}
			if key.Valid {
				col.Key = key.String
			}
			if default_.Valid {
				col.Default = default_.String
			}
			if extra.Valid {
				col.Extra = extra.String
			}
			columns = append(columns, col)
		}
		cols.Close()

		schema = append(schema, tableInfo{
			TableName: tableName,
			Columns:   columns,
		})
	}

	jsonBytes, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Error marshaling schema: %v", err)), nil
	}
	return mcp.NewToolResultText(string(jsonBytes)), nil
}

// --- 工具 2: 执行 SQL 查询 ---
func queryHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args, ok := request.Params.Arguments.(map[string]interface{})
	if !ok {
		return mcp.NewToolResultError("Invalid arguments format"), nil
	}

	query, ok := args["query"].(string)
	if !ok {
		return mcp.NewToolResultError("Query argument missing"), nil
	}

	log.Printf("Executing SQL: %s", query)

	dbMu.RLock()
	defer dbMu.RUnlock()

	if db == nil {
		return mcp.NewToolResultError("尚未连接数据库，请先使用 connect_database 工具连接"), nil
	}

	queryTrimmed := strings.TrimSpace(query)
	queryUpper := strings.ToUpper(queryTrimmed)

	if strings.HasPrefix(queryUpper, "SELECT") || strings.HasPrefix(queryUpper, "SHOW") ||
		strings.HasPrefix(queryUpper, "DESCRIBE") || strings.HasPrefix(queryUpper, "DESC") ||
		strings.HasPrefix(queryUpper, "EXPLAIN") {
		rows, err := db.Query(query)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("SQL Error: %v", err)), nil
		}
		defer rows.Close()

		columns, _ := rows.Columns()
		result := []map[string]interface{}{}

		for rows.Next() {
			values := make([]interface{}, len(columns))
			valuePtrs := make([]interface{}, len(columns))
			for i := range values {
				valuePtrs[i] = &values[i]
			}

			rows.Scan(valuePtrs...)

			rowMap := make(map[string]interface{})
			for i, col := range columns {
				var v interface{}
				val := values[i]
				b, ok := val.([]byte)
				if ok {
					v = string(b)
				} else {
					v = val
				}
				rowMap[col] = v
			}
			result = append(result, rowMap)
		}

		jsonBytes, _ := json.Marshal(result)
		return mcp.NewToolResultText(string(jsonBytes)), nil
	}

	result, err := db.Exec(query)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("SQL Error: %v", err)), nil
	}

	affected, _ := result.RowsAffected()
	lastID, _ := result.LastInsertId()
	response := fmt.Sprintf("操作成功，影响 %d 行数据", affected)
	if lastID > 0 {
		response = fmt.Sprintf("操作成功，影响 %d 行数据，最后插入ID: %d", affected, lastID)
	}
	log.Printf("Write result: %s", response)
	return mcp.NewToolResultText(response), nil
}

// --- 工具 3: 连接/切换数据库 ---
func connectDatabaseHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args, ok := request.Params.Arguments.(map[string]interface{})
	if !ok {
		return mcp.NewToolResultError("Invalid arguments format"), nil
	}

	dsn, ok := args["dsn"].(string)
	if !ok || strings.TrimSpace(dsn) == "" {
		return mcp.NewToolResultError("DSN 参数缺失，格式: user:password@tcp(host:port)/dbname"), nil
	}

	dsn = strings.TrimSpace(dsn)

	// 测试新连接
	newDB, err := sql.Open("mysql", dsn)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("连接失败: %v", err)), nil
	}
	defer func() {
		if err != nil {
			newDB.Close()
		}
	}()

	if err = newDB.Ping(); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("连接测试失败: %v", err)), nil
	}

	// 切换连接（写锁保护）
	dbMu.Lock()
	oldDB := db
	db = newDB
	dbMu.Unlock()

	// 关闭旧连接
	if oldDB != nil {
		oldDB.Close()
	}

	// 获取数据库信息
	var dbVersion string
	newDB.QueryRow("SELECT VERSION()").Scan(&dbVersion)

	// 获取数据库名
	dbName := dsn
	if idx := strings.LastIndex(dsn, "/"); idx >= 0 {
		dbName = dsn[idx+1:]
	}
	if idx := strings.Index(dbName, "?"); idx >= 0 {
		dbName = dbName[:idx]
	}

	log.Printf("Database switched successfully: %s (MySQL %s)", dbName, dbVersion)
	return mcp.NewToolResultText(fmt.Sprintf(
		"✅ 数据库连接成功！\n- 数据库: %s\n- MySQL版本: %s\n- DSN: %s\n\n现在你可以查询表结构或执行SQL操作了。",
		dbName, dbVersion, maskPassword(dsn),
	)), nil
}

// --- 工具 4: 查看当前连接 ---
func showConnectionHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	dbMu.RLock()
	defer dbMu.RUnlock()

	if db == nil {
		return mcp.NewToolResultText("当前未连接任何数据库"), nil
	}

	var dbVersion string
	db.QueryRow("SELECT VERSION()").Scan(&dbVersion)

	var dbName string
	db.QueryRow("SELECT DATABASE()").Scan(&dbName)

	return mcp.NewToolResultText(fmt.Sprintf(
		"📊 当前数据库连接：\n- 数据库: %s\n- MySQL版本: %s\n- 连接状态: 正常",
		dbName, dbVersion,
	)), nil
}

func maskPassword(dsn string) string {
	// user:password@tcp(host:port)/dbname -> user:***@tcp(host:port)/dbname
	atIdx := strings.Index(dsn, "@")
	colonIdx := strings.Index(dsn, ":")
	if colonIdx >= 0 && atIdx > colonIdx {
		return dsn[:colonIdx+1] + "***" + dsn[atIdx:]
	}
	return dsn
}

func main() {
	dsn := os.Getenv("DSN")
	if dsn == "" {
		dsn = "root:native@tcp(127.0.0.1:3306)/hr_db"
	}

	var err error
	db, err = sql.Open("mysql", dsn)
	if err != nil {
		log.Fatal("Error opening database: ", err)
	}
	if err := db.Ping(); err != nil {
		log.Printf("Warning: Database unreachable at startup: %v", err)
	} else {
		log.Printf("Successfully connected to MySQL: %s", maskPassword(dsn))
	}

	s := server.NewMCPServer(
		"HR-Database-MCP",
		"1.0.0",
	)

	// Tool: read_schema
	s.AddTool(mcp.NewTool("read_schema",
		mcp.WithDescription("获取当前数据库的所有表名和字段结构，这是编写SQL前的必要步骤"),
	), listSchemaHandler)

	// Tool: execute_query
	s.AddTool(mcp.NewTool("execute_query",
		mcp.WithDescription("执行SQL语句 (SELECT, INSERT, UPDATE, DELETE)",
		),
		mcp.WithString("query", mcp.Required(), mcp.Description("要执行的SQL语句")),
	), queryHandler)

	// Tool: connect_database
	s.AddTool(mcp.NewTool("connect_database",
		mcp.WithDescription("连接到新的MySQL数据库，支持动态切换。格式: user:password@tcp(host:port)/dbname"),
		mcp.WithString("dsn",
			mcp.Required(),
			mcp.Description("数据库连接串，格式: user:password@tcp(host:port)/dbname，例如 root:mypass@tcp(192.168.1.100:3306)/mydb"),
		),
	), connectDatabaseHandler)

	// Tool: show_connection
	s.AddTool(mcp.NewTool("show_connection",
		mcp.WithDescription("查看当前数据库连接信息"),
	), showConnectionHandler)

	log.Println("Starting MCP Server on :8080/sse")
	sseServer := server.NewSSEServer(s)
	if err := sseServer.Start(":8080"); err != nil {
		log.Fatal(err)
	}
}
