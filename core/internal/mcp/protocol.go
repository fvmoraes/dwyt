package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/fvmoraes/dwyt/internal/log"
)

const ProtocolVersion = "2024-11-05"

type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type JSONRPCResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id,omitempty"`
	Result  interface{} `json:"result,omitempty"`
	Error   *RPCError   `json:"error,omitempty"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type InitializeResult struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    ServerCapabilities `json:"capabilities"`
	ServerInfo      ServerInfo         `json:"serverInfo"`
}

type ServerCapabilities struct {
	Tools *struct{} `json:"tools,omitempty"`
}

type Tool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema InputSchema `json:"inputSchema"`
}

type InputSchema struct {
	Type       string              `json:"type"`
	Properties map[string]Property `json:"properties,omitempty"`
	Required   []string            `json:"required,omitempty"`
}

type Property struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

type CallToolRequest struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

type CallToolResult struct {
	Content []TextContent `json:"content"`
	IsError *bool         `json:"isError,omitempty"`
}

type TextContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func boolPtr(b bool) *bool { return &b }

type ToolHandler func(args map[string]interface{}) (string, error)

type Server struct {
	name     string
	version  string
	tools    []Tool
	handlers map[string]ToolHandler
	reader   *bufio.Reader
	writer   io.Writer
	logFile  string
}

func NewServer(name, version string) *Server {
	logPath := os.Getenv("MCP_LOG")
	if logPath == "" {
		home, _ := os.UserHomeDir()
		logPath = home + "/.dwyt/logs/mcp-" + name + ".log"
	}
	os.MkdirAll(filepath.Dir(logPath), 0755)

	return &Server{
		name:     name,
		version:  version,
		reader:   bufio.NewReader(os.Stdin),
		writer:   os.Stdout,
		handlers: make(map[string]ToolHandler),
	}
}

func (s *Server) RegisterTool(name, description string, props map[string]Property, required []string, handler ToolHandler) {
	s.tools = append(s.tools, Tool{
		Name:        name,
		Description: description,
		InputSchema: InputSchema{
			Type:       "object",
			Properties: props,
			Required:   required,
		},
	})
	s.handlers[name] = handler
}

func (s *Server) Run() error {
	for {
		line, err := s.reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("mcp read error: %w", err)
		}

		var req JSONRPCRequest
		if err := json.Unmarshal(line, &req); err != nil {
			log.Error("mcp invalid json", log.Fields{"line": string(line), "error": err.Error()})
			s.sendError(nil, -32700, "Parse error")
			continue
		}

		s.handleRequest(&req)
	}
}

func (s *Server) handleRequest(req *JSONRPCRequest) {
	switch req.Method {
	case "initialize":
		result := InitializeResult{
			ProtocolVersion: ProtocolVersion,
			Capabilities: ServerCapabilities{
				Tools: &struct{}{},
			},
			ServerInfo: ServerInfo{
				Name:    s.name,
				Version: s.version,
			},
		}
		s.sendResult(req.ID, result)

	case "tools/list":
		s.sendResult(req.ID, map[string]interface{}{
			"tools": s.tools,
		})

	case "tools/call":
		var callReq CallToolRequest
		if err := json.Unmarshal(req.Params, &callReq); err != nil {
			s.sendError(req.ID, -32602, "Invalid params: "+err.Error())
			return
		}

		handler, ok := s.handlers[callReq.Name]
		if !ok {
			s.sendError(req.ID, -32601, "Tool not found: "+callReq.Name)
			return
		}

		text, err := handler(callReq.Arguments)
		if err != nil {
			s.sendResult(req.ID, CallToolResult{
				Content: []TextContent{{Type: "text", Text: "Error: " + err.Error()}},
				IsError: boolPtr(true),
			})
			return
		}

		s.sendResult(req.ID, CallToolResult{
			Content: []TextContent{{Type: "text", Text: text}},
		})

	case "notifications/initialized":
		// No response needed

	case "ping":
		s.sendResult(req.ID, map[string]string{})

	default:
		s.sendError(req.ID, -32601, "Method not found: "+req.Method)
	}
}

func (s *Server) sendResult(id interface{}, result interface{}) {
	if id == nil {
		return
	}
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}
	s.writeResponse(resp)
}

func (s *Server) sendError(id interface{}, code int, message string) {
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &RPCError{Code: code, Message: message},
	}
	s.writeResponse(resp)
}

func (s *Server) writeResponse(resp JSONRPCResponse) {
	data, _ := json.Marshal(resp)
	fmt.Fprintf(s.writer, "%s\n", string(data))
}
