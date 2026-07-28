package ui

import (
	"dhtc/db"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

const mcpProtocolVersion = "2025-06-18"

type mcpRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type mcpResponse struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      any       `json:"id,omitempty"`
	Result  any       `json:"result,omitempty"`
	Error   *mcpError `json:"error,omitempty"`
}

type mcpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type mcpToolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

func (c *Controller) MCP(ctx *gin.Context) {
	if ctx.Request.Method == http.MethodGet {
		ctx.JSON(http.StatusOK, gin.H{
			"status":   "ok",
			"protocol": "mcp",
			"endpoint": "/mcp",
		})
		return
	}

	var req mcpRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, mcpResponse{
			JSONRPC: "2.0",
			Error:   &mcpError{Code: -32700, Message: "invalid JSON-RPC request"},
		})
		return
	}

	if req.JSONRPC == "" {
		req.JSONRPC = "2.0"
	}

	resp := mcpResponse{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "initialize":
		resp.Result = gin.H{
			"protocolVersion": mcpProtocolVersion,
			"capabilities": gin.H{
				"tools": gin.H{},
			},
			"serverInfo": gin.H{
				"name":    "dhtc",
				"version": "1.0.2",
			},
		}
	case "notifications/initialized":
		ctx.Status(http.StatusAccepted)
		return
	case "tools/list":
		resp.Result = gin.H{"tools": mcpTools()}
	case "tools/call":
		result, err := c.handleMCPToolCall(req.Params)
		if err != nil {
			resp.Error = &mcpError{Code: -32602, Message: err.Error()}
		} else {
			resp.Result = result
		}
	default:
		resp.Error = &mcpError{Code: -32601, Message: "method not found"}
	}

	ctx.JSON(http.StatusOK, resp)
}

func mcpTools() []gin.H {
	return []gin.H{
		{
			"name":        "search_torrents",
			"description": "Search indexed torrent metadata by name, info hash, file name, or all fields.",
			"inputSchema": gin.H{
				"type": "object",
				"properties": gin.H{
					"query":      gin.H{"type": "string", "description": "Search text."},
					"key":        gin.H{"type": "string", "enum": []string{"All", "Name", "InfoHash", "Files", "DiscoveredOn"}, "default": "All"},
					"match_type": gin.H{"type": "string", "enum": []string{"contains", "equals", "startswith", "endswith"}, "default": "contains"},
					"limit":      gin.H{"type": "integer", "minimum": 1, "maximum": 100, "default": 10},
				},
				"required": []string{"query"},
			},
		},
		{
			"name":        "latest_torrents",
			"description": "Return the latest indexed torrents.",
			"inputSchema": gin.H{
				"type":       "object",
				"properties": gin.H{"limit": gin.H{"type": "integer", "minimum": 1, "maximum": 100, "default": 10}},
			},
		},
		{
			"name":        "torrent_stats",
			"description": "Return crawler statistics and the indexed torrent count.",
			"inputSchema": gin.H{
				"type": "object",
				"properties": gin.H{
					"interval": gin.H{"type": "string", "enum": []string{"minute", "hour", "day"}, "default": "hour"},
					"limit":    gin.H{"type": "integer", "minimum": 1, "maximum": 100, "default": 24},
				},
			},
		},
		{
			"name":        "category_distribution",
			"description": "Return indexed torrent counts grouped by category.",
			"inputSchema": gin.H{"type": "object", "properties": gin.H{}},
		},
	}
}

func (c *Controller) handleMCPToolCall(raw json.RawMessage) (gin.H, error) {
	var params mcpToolCallParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, err
	}

	switch params.Name {
	case "search_torrents":
		return c.mcpSearch(params.Arguments)
	case "latest_torrents":
		return c.mcpLatest(params.Arguments)
	case "torrent_stats":
		return c.mcpStats(params.Arguments)
	case "category_distribution":
		return c.mcpCategories()
	default:
		return nil, &mcpToolError{"unknown tool: " + params.Name}
	}
}

type mcpToolError struct {
	message string
}

func (e *mcpToolError) Error() string {
	return e.message
}

func (c *Controller) mcpSearch(raw json.RawMessage) (gin.H, error) {
	var args struct {
		Query     string `json:"query"`
		Key       string `json:"key"`
		MatchType string `json:"match_type"`
		Limit     int    `json:"limit"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, err
	}
	if args.Query == "" {
		return nil, &mcpToolError{"query is required"}
	}
	if args.Key == "" {
		args.Key = "All"
	}
	if args.MatchType == "" {
		args.MatchType = "contains"
	}
	args.Limit = clampLimit(args.Limit, 10)

	results, total, err := c.Database.Search(args.Key, args.MatchType, args.Query, args.Limit, 0, db.SearchFilters{})
	if err != nil {
		return nil, err
	}
	return mcpContent(gin.H{"results": results, "total": total}), nil
}

func (c *Controller) mcpLatest(raw json.RawMessage) (gin.H, error) {
	limit := mcpLimit(raw, 10)
	results, total, err := c.Database.GetLatest(limit, 0)
	if err != nil {
		return nil, err
	}
	return mcpContent(gin.H{"results": results, "total": total}), nil
}

func (c *Controller) mcpStats(raw json.RawMessage) (gin.H, error) {
	var args struct {
		Interval string `json:"interval"`
		Limit    int    `json:"limit"`
	}
	_ = json.Unmarshal(raw, &args)
	if args.Interval == "" {
		args.Interval = "hour"
	}
	args.Limit = clampLimit(args.Limit, 24)

	stats, err := c.Database.GetStatsByInterval(args.Interval, args.Limit)
	if err != nil {
		return nil, err
	}
	return mcpContent(gin.H{"stats": stats, "interval": args.Interval, "info_hash_count": c.Database.GetInfoHashCount()}), nil
}

func (c *Controller) mcpCategories() (gin.H, error) {
	dist, err := c.Database.GetCategoryDistribution()
	if err != nil {
		return nil, err
	}
	return mcpContent(dist), nil
}

func mcpLimit(raw json.RawMessage, fallback int) int {
	var args struct {
		Limit int `json:"limit"`
	}
	_ = json.Unmarshal(raw, &args)
	return clampLimit(args.Limit, fallback)
}

func clampLimit(limit, fallback int) int {
	if limit < 1 {
		limit = fallback
	}
	if limit > 100 {
		limit = 100
	}
	return limit
}

func mcpContent(value any) gin.H {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		data = []byte(strconv.Quote("failed to encode result"))
	}
	return gin.H{
		"content": []gin.H{
			{
				"type": "text",
				"text": string(data),
			},
		},
	}
}
