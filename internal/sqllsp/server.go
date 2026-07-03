package sqllsp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"

	polyglot "github.com/tobilg/polyglot/packages/go"
	"renart/internal/web/sqlformat"
)

type Server struct {
	// mu guards engine, docs, and graph, which are replaced or mutated when
	// documents change and when the workspace graph is reloaded.
	mu            sync.RWMutex
	engine        *Engine
	docs          map[URI]TextDocumentItem
	graph         CanonicalGraph
	poly          *polyglot.Client
	workspaceRoot string
}

func NewServer(graph CanonicalGraph, poly *polyglot.Client) *Server {
	return &Server{
		engine: NewEngineWithPolyglot(graph, poly),
		docs:   map[URI]TextDocumentItem{},
		poly:   poly,
		graph:  graph,
	}
}

func NewWorkspaceServer(workspaceRoot string, graph CanonicalGraph, poly *polyglot.Client) *Server {
	server := NewServer(graph, poly)
	server.workspaceRoot = workspaceRoot
	return server
}

func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	reader := bufio.NewReader(in)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		payload, err := readMessage(reader)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		if len(payload) == 0 {
			continue
		}
		response, notify, err := s.handle(payload)
		if err != nil {
			return err
		}
		for _, message := range notify {
			if err := writeMessage(out, message); err != nil {
				return err
			}
		}
		if len(response) > 0 {
			if err := writeMessage(out, response); err != nil {
				return err
			}
		}
	}
}

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (s *Server) handle(payload []byte) ([]byte, [][]byte, error) {
	var msg rpcMessage
	if err := json.Unmarshal(payload, &msg); err != nil {
		return nil, nil, err
	}
	if msg.Method == "" {
		return nil, nil, nil
	}
	switch msg.Method {
	case "initialize":
		return marshalRPC(msg.ID, map[string]any{
			"capabilities": map[string]any{
				"textDocumentSync": 1,
				"completionProvider": map[string]any{
					"triggerCharacters": []string{".", "\"", "'"},
				},
				"definitionProvider":         true,
				"hoverProvider":              true,
				"referencesProvider":         true,
				"renameProvider":             true,
				"codeActionProvider":         true,
				"documentFormattingProvider": true,
				"signatureHelpProvider": map[string]any{
					"triggerCharacters":   []string{"(", ","},
					"retriggerCharacters": []string{","},
				},
				"semanticTokensProvider": map[string]any{
					"legend": map[string]any{
						"tokenTypes":     semanticTokenTypes,
						"tokenModifiers": []string{},
					},
					"full": true,
				},
				"documentSymbolProvider":  true,
				"workspaceSymbolProvider": true,
			},
			"serverInfo": map[string]string{"name": "renart-sql-lsp", "version": "dev"},
		}), nil, nil
	case "shutdown":
		return marshalRPC(msg.ID, nil), nil, nil
	case "exit":
		return nil, nil, io.EOF
	case "textDocument/didOpen":
		doc := didOpenDocument{}
		if err := json.Unmarshal(msg.Params, &doc); err != nil {
			return nil, nil, err
		}
		s.setDocument(doc.TextDocument)
		return nil, s.publishDiagnostics(doc.TextDocument.URI), nil
	case "textDocument/didChange":
		change := didChangeDocument{}
		if err := json.Unmarshal(msg.Params, &change); err != nil {
			return nil, nil, err
		}
		if len(change.ContentChanges) > 0 {
			doc := TextDocumentItem{URI: change.TextDocument.URI, Version: change.TextDocument.Version, Text: change.ContentChanges[len(change.ContentChanges)-1].Text}
			s.setDocument(doc)
			return nil, s.publishDiagnostics(doc.URI), nil
		}
	case "workspace/didChangeWatchedFiles":
		if err := s.reloadWorkspaceGraph(); err != nil {
			return nil, s.windowLog("warning", "renart sql-lsp: reload workspace graph failed: "+err.Error()), nil
		}
		return nil, s.publishAllDiagnostics(), nil
	case "textDocument/completion":
		var params positionedDocument
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			return nil, nil, err
		}
		doc, ok := s.document(params.TextDocument.URI)
		if !ok {
			return marshalRPC(msg.ID, []CompletionItem{}), nil, nil
		}
		return marshalRPC(msg.ID, s.currentEngine().Complete(doc, params.Position)), nil, nil
	case "textDocument/definition":
		var params positionedDocument
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			return nil, nil, err
		}
		doc, ok := s.document(params.TextDocument.URI)
		if !ok {
			return marshalRPC(msg.ID, []Location{}), nil, nil
		}
		return marshalRPC(msg.ID, s.currentEngine().Definition(doc, params.Position)), nil, nil
	case "textDocument/hover":
		var params positionedDocument
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			return nil, nil, err
		}
		doc, ok := s.document(params.TextDocument.URI)
		if !ok {
			return marshalRPC(msg.ID, nil), nil, nil
		}
		return marshalRPC(msg.ID, s.currentEngine().Hover(doc, params.Position)), nil, nil
	case "textDocument/signatureHelp":
		var params positionedDocument
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			return nil, nil, err
		}
		doc, ok := s.document(params.TextDocument.URI)
		if !ok {
			return marshalRPC(msg.ID, nil), nil, nil
		}
		return marshalRPC(msg.ID, s.currentEngine().SignatureHelp(doc, params.Position)), nil, nil
	case "textDocument/formatting":
		var params formattingDocument
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			return nil, nil, err
		}
		doc, ok := s.document(params.TextDocument.URI)
		if !ok {
			return marshalRPC(msg.ID, []TextEdit{}), nil, nil
		}
		formatted, err := sqlformat.Format(context.Background(), doc.Text, s.currentEngine().dialectForDocument(doc))
		if err != nil {
			return marshalRPC(msg.ID, []TextEdit{}), nil, nil
		}
		return marshalRPC(msg.ID, []TextEdit{{Range: Range{Start: Position{}, End: PositionAt(doc.Text, len(doc.Text))}, NewText: formatted}}), nil, nil
	case "textDocument/references":
		var params referenceDocument
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			return nil, nil, err
		}
		doc, ok := s.document(params.TextDocument.URI)
		if !ok {
			return marshalRPC(msg.ID, []Location{}), nil, nil
		}
		return marshalRPC(msg.ID, s.currentEngine().WorkspaceReferences(doc, params.Position, s.allDocuments(), params.Context.IncludeDeclaration)), nil, nil
	case "textDocument/rename":
		var params renameDocument
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			return nil, nil, err
		}
		doc, ok := s.document(params.TextDocument.URI)
		if !ok {
			return marshalRPC(msg.ID, nil), nil, nil
		}
		edit, err := s.currentEngine().Rename(doc, params.Position, params.NewName)
		if err != nil {
			// -32803 is the LSP RequestFailed code; clients surface the message
			// to the user instead of silently doing nothing.
			return marshalRPCError(msg.ID, -32803, err.Error()), nil, nil
		}
		return marshalRPC(msg.ID, edit), nil, nil
	case "textDocument/codeAction":
		var params codeActionDocument
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			return nil, nil, err
		}
		doc, ok := s.document(params.TextDocument.URI)
		if !ok {
			return marshalRPC(msg.ID, []CodeAction{}), nil, nil
		}
		return marshalRPC(msg.ID, s.currentEngine().CodeActions(doc)), nil, nil
	case "textDocument/semanticTokens/full":
		var params documentOnly
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			return nil, nil, err
		}
		doc, ok := s.document(params.TextDocument.URI)
		if !ok {
			return marshalRPC(msg.ID, SemanticTokens{}), nil, nil
		}
		return marshalRPC(msg.ID, s.currentEngine().SemanticTokens(doc)), nil, nil
	case "textDocument/documentSymbol":
		var params documentOnly
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			return nil, nil, err
		}
		doc, ok := s.document(params.TextDocument.URI)
		if !ok {
			return marshalRPC(msg.ID, []DocumentSymbol{}), nil, nil
		}
		return marshalRPC(msg.ID, s.currentEngine().DocumentSymbols(doc)), nil, nil
	case "workspace/symbol":
		var params workspaceSymbolParams
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			return nil, nil, err
		}
		return marshalRPC(msg.ID, s.currentEngine().WorkspaceSymbols(params.Query)), nil, nil
	}
	if msg.ID != nil {
		return marshalRPCError(msg.ID, -32601, "method not found"), nil, nil
	}
	return nil, nil, nil
}

func (s *Server) currentEngine() *Engine {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.engine
}

func (s *Server) reloadWorkspaceGraph() error {
	if strings.TrimSpace(s.workspaceRoot) == "" {
		return nil
	}
	graph, err := LoadGraphFromDir(context.Background(), s.workspaceRoot)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.graph = graph
	s.engine = NewEngineWithPolyglot(graph, s.poly)
	return nil
}

func (s *Server) setDocument(doc TextDocumentItem) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.docs[doc.URI] = doc
}

func (s *Server) document(uri URI) (TextDocumentItem, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	doc, ok := s.docs[uri]
	return doc, ok
}

func (s *Server) allDocuments() []TextDocumentItem {
	s.mu.RLock()
	defer s.mu.RUnlock()
	docs := make([]TextDocumentItem, 0, len(s.docs))
	for _, doc := range s.docs {
		docs = append(docs, doc)
	}
	return docs
}

func (s *Server) publishDiagnostics(uri URI) [][]byte {
	doc, ok := s.document(uri)
	if !ok {
		return nil
	}
	engine := s.currentEngine()
	msg := struct {
		JSONRPC string `json:"jsonrpc"`
		Method  string `json:"method"`
		Params  any    `json:"params"`
	}{
		JSONRPC: "2.0",
		Method:  "textDocument/publishDiagnostics",
		Params: map[string]any{
			"uri":         uri,
			"diagnostics": engine.Diagnostics(doc),
		},
	}
	payload, _ := json.Marshal(msg)
	return [][]byte{payload}
}

func (s *Server) publishAllDiagnostics() [][]byte {
	s.mu.RLock()
	uris := make([]URI, 0, len(s.docs))
	for uri := range s.docs {
		uris = append(uris, uri)
	}
	s.mu.RUnlock()
	var payloads [][]byte
	for _, uri := range uris {
		payloads = append(payloads, s.publishDiagnostics(uri)...)
	}
	return payloads
}

func (s *Server) windowLog(level, message string) [][]byte {
	msg := struct {
		JSONRPC string `json:"jsonrpc"`
		Method  string `json:"method"`
		Params  any    `json:"params"`
	}{
		JSONRPC: "2.0",
		Method:  "window/logMessage",
		Params: map[string]any{
			"type":    logMessageType(level),
			"message": message,
		},
	}
	payload, _ := json.Marshal(msg)
	return [][]byte{payload}
}

func logMessageType(level string) int {
	switch level {
	case "error":
		return 1
	case "warning":
		return 2
	case "info":
		return 3
	default:
		return 4
	}
}

type didOpenDocument struct {
	TextDocument TextDocumentItem `json:"textDocument"`
}

type didChangeDocument struct {
	TextDocument struct {
		URI     URI `json:"uri"`
		Version int `json:"version,omitempty"`
	} `json:"textDocument"`
	ContentChanges []struct {
		Text string `json:"text"`
	} `json:"contentChanges"`
}

type positionedDocument struct {
	TextDocument struct {
		URI URI `json:"uri"`
	} `json:"textDocument"`
	Position Position `json:"position"`
}

type referenceDocument struct {
	TextDocument struct {
		URI URI `json:"uri"`
	} `json:"textDocument"`
	Position Position `json:"position"`
	Context  struct {
		IncludeDeclaration bool `json:"includeDeclaration"`
	} `json:"context"`
}

type renameDocument struct {
	TextDocument struct {
		URI URI `json:"uri"`
	} `json:"textDocument"`
	Position Position `json:"position"`
	NewName  string   `json:"newName"`
}

type codeActionDocument struct {
	TextDocument struct {
		URI URI `json:"uri"`
	} `json:"textDocument"`
	Range   Range `json:"range"`
	Context struct {
		Diagnostics []Diagnostic `json:"diagnostics"`
	} `json:"context"`
}

type documentOnly struct {
	TextDocument struct {
		URI URI `json:"uri"`
	} `json:"textDocument"`
}

type formattingDocument struct {
	TextDocument struct {
		URI URI `json:"uri"`
	} `json:"textDocument"`
	Options FormattingOptions `json:"options"`
}

type workspaceSymbolParams struct {
	Query string `json:"query"`
}

// maxMessageBytes bounds a single JSON-RPC payload so a malformed or hostile
// Content-Length header cannot trigger an unbounded allocation.
const maxMessageBytes = 64 << 20 // 64 MiB

func readMessage(reader *bufio.Reader) ([]byte, error) {
	contentLength := -1
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			parsed, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				return nil, err
			}
			contentLength = parsed
		}
	}
	if contentLength < 0 {
		return nil, fmt.Errorf("missing Content-Length header")
	}
	if contentLength > maxMessageBytes {
		return nil, fmt.Errorf("Content-Length %d exceeds maximum of %d bytes", contentLength, maxMessageBytes)
	}
	payload := make([]byte, contentLength)
	_, err := io.ReadFull(reader, payload)
	return payload, err
}

func writeMessage(out io.Writer, payload []byte) error {
	_, err := fmt.Fprintf(out, "Content-Length: %d\r\n\r\n%s", len(payload), payload)
	return err
}

func marshalRPC(id any, result any) []byte {
	payload, _ := json.Marshal(rpcMessage{JSONRPC: "2.0", ID: id, Result: result})
	return payload
}

func marshalRPCError(id any, code int, message string) []byte {
	payload, _ := json.Marshal(rpcMessage{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: message}})
	return payload
}

func EncodeMessage(payload []byte) []byte {
	var buffer bytes.Buffer
	_ = writeMessage(&buffer, payload)
	return buffer.Bytes()
}
