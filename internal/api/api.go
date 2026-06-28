// Package api implements Ollama-compatible HTTP endpoints
// that route requests through the Mycelium's Three Ravens.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/aaronrdavis/mycelium-api/internal/config"
	"github.com/aaronrdavis/mycelium-api/internal/llamaserver"
	"github.com/aaronrdavis/mycelium-api/internal/node"
	"github.com/aaronrdavis/mycelium-api/internal/queue"
	"github.com/aaronrdavis/mycelium-api/internal/rpc"
	"github.com/aaronrdavis/mycelium-api/internal/routing"
)

// Server is the Mycelium API HTTP server.
type Server struct {
	Config       *config.Config
	Router       *routing.Router
	Manager      *node.Manager
	LlamaManager *llamaserver.Manager
	QueueManager *queue.Manager
}

// NewServer creates an API server.
func NewServer(cfg *config.Config, router *routing.Router, mgr *node.Manager, lm *llamaserver.Manager, qm *queue.Manager) *Server {
	return &Server{
		Config:       cfg,
		Router:       router,
		Manager:      mgr,
		LlamaManager: lm,
		QueueManager: qm,
	}
}

// --- Ollama-compatible types ---

type GenerateRequest struct {
	Model   string `json:"model"`
	Prompt  string `json:"prompt"`
	Stream  *bool  `json:"stream,omitempty"`
	Profile string `json:"profile,omitempty"`
}

type ChatRequest struct {
	Model    string        `json:"model"`
	Messages []ChatMessage `json:"messages"`
	Stream   *bool         `json:"stream,omitempty"`
	Profile  string        `json:"profile,omitempty"`
}

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type GenerateResponse struct {
	Model     string `json:"model"`
	CreatedAt string `json:"created_at"`
	Response  string `json:"response"`
	Done      bool   `json:"done"`
	Profile   string `json:"profile,omitempty"`
	Node      string `json:"node,omitempty"`
}

type ChatResponse struct {
	Model     string      `json:"model"`
	CreatedAt string      `json:"created_at"`
	Message   ChatMessage `json:"message"`
	Done      bool        `json:"done"`
	Profile   string      `json:"profile,omitempty"`
	Node      string      `json:"node,omitempty"`
}

type TagsResponse struct {
	Models []ModelEntry `json:"models"`
}

type ModelEntry struct {
	Name       string   `json:"name"`
	Model      string   `json:"model"`
	ModifiedAt string   `json:"modified_at"`
	Size       int64    `json:"size"`
	Digest     string   `json:"digest"`
	Details    ModelDetail `json:"details"`
}

type ModelDetail struct {
	ParentModel     string   `json:"parent_model"`
	Format          string   `json:"format"`
	Family          string   `json:"family"`
	Families        []string `json:"families"`
	ParameterSize   string   `json:"parameter_size"`
	QuantizationLevel string `json:"quantization_level"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

type StatusResponse struct {
	Status  string        `json:"status"`
	Uptime  string        `json:"uptime"`
	Nodes   []NodeStatus  `json:"nodes"`
	Routing RoutingStatus `json:"routing"`
}

type NodeStatus struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Status      string `json:"status"`
	Latency     string `json:"latency"`
	Pool        string `json:"pool"`
	Protocol    string `json:"protocol,omitempty"`
	FreeMem     string `json:"free_mem,omitempty"`
	TotalMem    string `json:"total_mem,omitempty"`
	GPUVerified bool   `json:"gpu_verified,omitempty"`
	GPUOnCPU    bool   `json:"gpu_on_cpu,omitempty"`
	GPUModel    string `json:"gpu_model,omitempty"`
	FailCount   int    `json:"fail_count,omitempty"`
	LastError   string `json:"last_error,omitempty"`
}

type RoutingStatus struct {
	Default  string            `json:"default"`
	Profiles map[string]string `json:"profiles"`
}

var startTime = time.Now()

// RegisterRoutes sets up all HTTP handlers.
func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/generate", s.handleGenerate)
	mux.HandleFunc("/api/chat", s.handleChat)
	mux.HandleFunc("/api/tags", s.handleTags)
	mux.HandleFunc("/api/show", s.handleShow)
	mux.HandleFunc("/api/version", s.handleVersion)

	// Mycelium extensions
	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/api/routes", s.handleRoutes)
	mux.HandleFunc("/api/rpc/probe", s.handleRPCProbe)
	mux.HandleFunc("/api/gpu-check", s.handleGPUCheck)

	// Async job queue (Muninn — deep, slow, background)
	mux.HandleFunc("/api/submit", s.handleSubmit)
	mux.HandleFunc("/api/job/", s.handleGetJob)
	mux.HandleFunc("/api/jobs", s.handleListJobs)

	mux.HandleFunc("/", s.handleRoot)
}

func (s *Server) handleGenerate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "failed to read request"})
		return
	}

	var req GenerateRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid request body"})
		return
	}

	if req.Model == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "model is required"})
		return
	}

	// If llama-server is healthy, route directly to it (RPC-distributed inference)
	if s.LlamaManager != nil && s.LlamaManager.IsHealthy() {
		log.Printf("[api] generate: %s -> llama-server (RPC distributed)", profileOrDefault(req.Profile))
		s.proxyToLlamaServer(w, r, body, req)
		return
	}

	stream := req.Stream != nil && *req.Stream

	// Try hedged routing — send to multiple nodes, first response wins
	hedgedResult, err := s.Router.RouteHedged(req.Model, stream, len(req.Prompt), 3)
	if err != nil {
		log.Printf("[api] All Mycelium nodes down, falling back to local Ollama: %v", err)
		s.proxyToLocal(w, r, body)
		return
	}

	if len(hedgedResult.Nodes) == 1 {
		// Single node — no hedging needed
		result := &routing.RouteResult{
			Profile: hedgedResult.Profile,
			Rule:    hedgedResult.Rule,
			Node:    hedgedResult.Nodes[0],
			Model:   hedgedResult.Model,
		}
		log.Printf("[api] generate: %s -> %s -> %s (model: %s)", profileOrDefault(req.Profile), result.Profile, result.Node.Config.Name, result.Model)
		s.routeToNode(w, r, result, body)
		return
	}

	log.Printf("[api] generate: %s -> %s (hedged, %d candidates, model: %s)", profileOrDefault(req.Profile), hedgedResult.Profile, len(hedgedResult.Nodes), hedgedResult.Model)
	s.routeToNodeHedged(w, r, hedgedResult, body)
}

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "failed to read request"})
		return
	}

	var req ChatRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid request body"})
		return
	}

	if req.Model == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "model is required"})
		return
	}

	stream := req.Stream != nil && *req.Stream
	contextLen := 0
	for _, m := range req.Messages {
		contextLen += len(m.Content)
	}

	// Try hedged routing — send to multiple nodes, first response wins
	hedgedResult, err := s.Router.RouteHedged(req.Model, stream, contextLen, 3)
	if err != nil {
		log.Printf("[api] All Mycelium nodes down, falling back to local Ollama: %v", err)
		s.proxyToLocal(w, r, body)
		return
	}

	if len(hedgedResult.Nodes) == 1 {
		result := &routing.RouteResult{
			Profile: hedgedResult.Profile,
			Rule:    hedgedResult.Rule,
			Node:    hedgedResult.Nodes[0],
			Model:   hedgedResult.Model,
		}
		log.Printf("[api] chat: %s -> %s -> %s (model: %s)", profileOrDefault(req.Profile), result.Profile, result.Node.Config.Name, result.Model)
		s.routeToNode(w, r, result, body)
		return
	}

	log.Printf("[api] chat: %s -> %s (hedged, %d candidates, model: %s)", profileOrDefault(req.Profile), hedgedResult.Profile, len(hedgedResult.Nodes), hedgedResult.Model)
	s.routeToNodeHedged(w, r, hedgedResult, body)
}

func (s *Server) handleTags(w http.ResponseWriter, r *http.Request) {
	s.proxyToLocal(w, r, nil)
}

func (s *Server) handleShow(w http.ResponseWriter, r *http.Request) {
	s.proxyToLocal(w, r, nil)
}

func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"version": "0.1.0-mycelium"})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	nodes := s.Manager.AllNodes()
	nodeStatuses := make([]NodeStatus, len(nodes))
	for i, n := range nodes {
		latency := "unknown"
		if n.GetLatency() > 0 {
			latency = n.GetLatency().Round(time.Millisecond).String()
		}
		nodeStatuses[i] = NodeStatus{
			Name:        n.GetName(),
			Type:        string(n.GetType()),
			Status:      string(n.GetStatus()),
			Latency:     latency,
			Pool:        n.GetPool(),
			Protocol:    string(n.Config.Protocol),
			FreeMem:     rpc.FormatMemory(n.GetFreeMem()),
			TotalMem:    rpc.FormatMemory(n.GetTotalMem()),
			GPUVerified: n.GetGPUVerified(),
			GPUOnCPU:    n.GetGPUOnCPU(),
			GPUModel:    n.GetGPUModel(),
			FailCount:   n.GetFailCount(),
			LastError:   n.GetLastError(),
		}
	}

	profiles := map[string]string{
		"huginn": s.Config.Routing.Huginn.Model,
		"muninn": s.Config.Routing.Muninn.Model,
		"skald":  s.Config.Routing.Skald.Model,
	}

	writeJSON(w, http.StatusOK, StatusResponse{
		Status: "running",
		Uptime: time.Since(startTime).Round(time.Second).String(),
		Nodes:  nodeStatuses,
		Routing: RoutingStatus{
			Default:  s.Config.Routing.Default,
			Profiles: profiles,
		},
	})
}

func (s *Server) handleRoutes(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.Config.Routing)
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"name":    "mycelium-api",
		"version": "0.1.0",
		"status":  "running",
	})
}

// proxyToLlamaServer translates Ollama /api/generate format to llama-server's
// /completion endpoint, proxies the request, and translates the response back.
func (s *Server) proxyToLlamaServer(w http.ResponseWriter, r *http.Request, body []byte, req GenerateRequest) {
	// Build llama-server completion request
	completionReq := map[string]interface{}{
		"prompt":      req.Prompt,
		"n_predict":   128,
		"temperature": 0.8,
		"stream":      false,
	}

	completionBody, _ := json.Marshal(completionReq)
	targetURL := fmt.Sprintf("%s/completion", s.LlamaManager.BaseURL())

	client := &http.Client{Timeout: 300 * time.Second}
	proxyReq, err := http.NewRequest("POST", targetURL, bytes.NewReader(completionBody))
	if err != nil {
		writeJSON(w, http.StatusBadGateway, ErrorResponse{Error: fmt.Sprintf("proxy error: %v", err)})
		return
	}
	proxyReq.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(proxyReq)
	if err != nil {
		log.Printf("[api] proxy to llama-server failed: %v", err)
		writeJSON(w, http.StatusBadGateway, ErrorResponse{Error: fmt.Sprintf("llama-server error: %v", err)})
		return
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, ErrorResponse{Error: "failed to read llama-server response"})
		return
	}

	// Parse llama-server response and translate to Ollama format
	var llmResp struct {
		Content string `json:"content"`
		Timings struct {
			PredictedPerSecond float64 `json:"predicted_per_second"`
			PredictedMS         float64 `json:"predicted_ms"`
			PredictedTokens     int     `json:"predicted_n"`
			PromptMS            float64 `json:"prompt_ms"`
			PromptTokens        int     `json:"prompt_n"`
		} `json:"timings"`
		Stop bool `json:"stop"`
	}

	if err := json.Unmarshal(respBody, &llmResp); err != nil {
		// Return raw response if we can't parse
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Mycelium-Profile", "llama-server-rpc")
		w.WriteHeader(resp.StatusCode)
		w.Write(respBody)
		return
	}

	// Build Ollama-compatible response
	ollamaResp := GenerateResponse{
		Model:     req.Model,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		Response:  llmResp.Content,
		Done:      true,
		Profile:   "llama-server-rpc",
		Node:      "distributed",
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Mycelium-Profile", "llama-server-rpc")
	w.Header().Set("X-Mycelium-Node", "distributed")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(ollamaResp)
}

func (s *Server) routeToNode(w http.ResponseWriter, r *http.Request, result *routing.RouteResult, body []byte) {
	targetNode := result.Node

	// Apply model override if the route specifies one
	proxiedBody := body
	if result.Model != "" {
		proxiedBody = overrideModel(body, result.Model)
	}

	// If the node has an Ollama API port, proxy directly
	if targetNode.Config.APIPort > 0 {
		targetURL := fmt.Sprintf("http://%s:%d%s", targetNode.Config.Host, targetNode.Config.APIPort, r.URL.Path)
		if s.proxyToURLWithRetry(w, r, targetURL, proxiedBody, result, targetNode) {
			return // Success or error already written
		}
		// Failed — try fallback
		s.proxyToLocal(w, r, proxiedBody)
		return
	}

	// RPC-only node: use llama-server as the inference frontend
	// llama-server on Hearth handles model loading, RPC nodes handle compute offload
	if s.LlamaManager != nil && s.LlamaManager.IsHealthy() {
		// Proxy to llama-server's OpenAI-compatible API
		targetURL := fmt.Sprintf("%s%s", s.LlamaManager.BaseURL(), r.URL.Path)
		if s.proxyToURLWithRetry(w, r, targetURL, proxiedBody, result, targetNode) {
			return
		}
		// Failed — try fallback
		s.proxyToLocal(w, r, proxiedBody)
		return
	}

	// Fallback: use local Ollama
	fallbackAddr := s.Config.Routing.FallbackLocal
	targetURL := fmt.Sprintf("http://%s%s", fallbackAddr, r.URL.Path)
	s.proxyToURL(w, r, targetURL, proxiedBody, result)
}

// proxyToURLWithRetry proxies a request to the target URL. On success, it
// records the success on the node and returns true. On failure, it records
// the failure on the node and returns false (caller should try fallback).
// If the node has already exceeded MaxFailures, it's skipped immediately.
func (s *Server) proxyToURLWithRetry(w http.ResponseWriter, r *http.Request, targetURL string, body []byte, routeInfo *routing.RouteResult, targetNode *node.Node) bool {
	// Skip node if it's already auto-marked unhealthy from failures
	if targetNode.GetFailCount() >= node.MaxFailures {
		log.Printf("[api] skipping %s (fail_count=%d >= %d)", targetNode.GetName(), targetNode.GetFailCount(), node.MaxFailures)
		return false
	}

	var reqBody io.Reader
	if body != nil {
		reqBody = bytes.NewReader(body)
	} else if r.Body != nil {
		reqBody = r.Body
	}

	proxyReq, err := http.NewRequest(r.Method, targetURL, reqBody)
	if err != nil {
		log.Printf("[api] proxy request creation failed: %v", err)
		targetNode.RecordFailure(fmt.Sprintf("proxy creation: %v", err))
		return false
	}

	for k, vv := range r.Header {
		for _, v := range vv {
			proxyReq.Header.Add(k, v)
		}
	}

	client := &http.Client{Timeout: 300 * time.Second}
	resp, err := client.Do(proxyReq)
	if err != nil {
		log.Printf("[api] proxy to %s failed: %v", targetURL, err)
		targetNode.RecordFailure(err.Error())
		return false
	}
	defer resp.Body.Close()

	// Non-2xx/3xx is a failure
	if resp.StatusCode >= 400 {
		errBody, _ := io.ReadAll(resp.Body)
		errMsg := fmt.Sprintf("HTTP %d from %s: %s", resp.StatusCode, targetURL, string(errBody[:clamp(len(errBody), 200)]))
		log.Printf("[api] upstream error: %s", errMsg)
		targetNode.RecordFailure(errMsg)
		writeJSON(w, resp.StatusCode, ErrorResponse{Error: errMsg})
		return true // We wrote a response, but it's an error — don't retry
	}

	// Success — record it
	targetNode.RecordSuccess()

	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}

	if routeInfo != nil {
		w.Header().Set("X-Mycelium-Profile", string(routeInfo.Profile))
		w.Header().Set("X-Mycelium-Node", routeInfo.Node.Config.Name)
		w.Header().Set("X-Mycelium-Model", routeInfo.Model)
	}

	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
	return true
}

func clamp(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// overrideModel rewrites the "model" field in a JSON request body.
func overrideModel(body []byte, model string) []byte {
	var req map[string]interface{}
	if err := json.Unmarshal(body, &req); err != nil {
		return body // If we can't parse it, pass through unchanged
	}
	req["model"] = model
	newBody, err := json.Marshal(req)
	if err != nil {
		return body
	}
	return newBody
}

func (s *Server) proxyToLocal(w http.ResponseWriter, r *http.Request, body []byte) {
	fallbackAddr := s.Config.Routing.FallbackLocal
	targetURL := fmt.Sprintf("http://%s%s", fallbackAddr, r.URL.Path)
	s.proxyToURL(w, r, targetURL, body, nil)
}

func (s *Server) proxyToURL(w http.ResponseWriter, r *http.Request, targetURL string, body []byte, routeInfo *routing.RouteResult) {
	var reqBody io.Reader
	if body != nil {
		reqBody = bytes.NewReader(body)
	} else if r.Body != nil {
		reqBody = r.Body
	}

	proxyReq, err := http.NewRequest(r.Method, targetURL, reqBody)
	if err != nil {
		log.Printf("[api] proxy request creation failed: %v", err)
		writeJSON(w, http.StatusBadGateway, ErrorResponse{Error: fmt.Sprintf("proxy error: %v", err)})
		return
	}

	for k, vv := range r.Header {
		for _, v := range vv {
			proxyReq.Header.Add(k, v)
		}
	}

	client := &http.Client{Timeout: 300 * time.Second}
	resp, err := client.Do(proxyReq)
	if err != nil {
		log.Printf("[api] proxy to %s failed: %v", targetURL, err)
		writeJSON(w, http.StatusBadGateway, ErrorResponse{Error: fmt.Sprintf("upstream error: %v", err)})
		return
	}
	defer resp.Body.Close()

	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}

	if routeInfo != nil {
		w.Header().Set("X-Mycelium-Profile", string(routeInfo.Profile))
		w.Header().Set("X-Mycelium-Node", routeInfo.Node.Config.Name)
		w.Header().Set("X-Mycelium-Model", routeInfo.Model)
	}

	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func profileOrDefault(p string) string {
	if p != "" {
		return p
	}
	return "auto"
}

// hedgeResponse captures a single node's proxy response for hedged routing.
type hedgeResponse struct {
	NodeName string
	Profile  routing.Profile
	Model    string
	Status   int
	Headers  http.Header
	Body     []byte
	Latency  time.Duration
	Error    error
}

// routeToNodeHedged sends the request to multiple nodes with a delayed-hedge
// strategy. The primary node fires immediately. If no response arrives within
// hedgeDelay, a second request fires to the next node, and so on.
// First successful response wins; remaining in-flight requests are cancelled.
// Adapted from SynapticLlamas' HedgingStrategy (delayed-hedge variant).
func (s *Server) routeToNodeHedged(w http.ResponseWriter, r *http.Request, result *routing.RouteHedgedResult, body []byte) {
	nodes := result.Nodes
	if len(nodes) == 0 {
		s.proxyToLocal(w, r, body)
		return
	}

	// If only one node, no hedging needed
	if len(nodes) == 1 {
		singleResult := &routing.RouteResult{
			Profile: result.Profile,
			Rule:    result.Rule,
			Node:    nodes[0],
			Model:   result.Model,
		}
		s.routeToNode(w, r, singleResult, body)
		return
	}

	hedgeDelay := 300 * time.Millisecond
	maxNodes := len(nodes)
	if maxNodes > 3 {
		maxNodes = 3 // Cap at 3 hedge nodes
	}
	nodes = nodes[:maxNodes]

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	resultsCh := make(chan hedgeResponse, maxNodes)
	var wg sync.WaitGroup

	for i, n := range nodes {
		wg.Add(1)
		go func(idx int, node *node.Node) {
			defer wg.Done()

			// Stagger: primary fires immediately, each subsequent after hedgeDelay
			if idx > 0 {
				select {
				case <-time.After(hedgeDelay):
				case <-ctx.Done():
					return
				}
			}

			// Check if a winner already emerged
			if ctx.Err() != nil {
				return
			}

			// Build the proxy URL for this node
			proxiedBody := body
			if result.Model != "" {
				proxiedBody = overrideModel(body, result.Model)
			}

			targetURL := s.nodeProxyURL(node, r.URL.Path)
			if targetURL == "" {
				// Can't route to this node, skip
				return
			}

			start := time.Now()
			hr := hedgeResponse{
				NodeName: node.GetName(),
				Profile:  result.Profile,
				Model:    result.Model,
			}

			client := &http.Client{Timeout: 300 * time.Second}
			proxyReq, err := http.NewRequestWithContext(ctx, r.Method, targetURL, bytes.NewReader(proxiedBody))
			if err != nil {
				hr.Error = err
				hr.Latency = time.Since(start)
				resultsCh <- hr
				return
			}

			for k, vv := range r.Header {
				for _, v := range vv {
					proxyReq.Header.Add(k, v)
				}
			}

			resp, err := client.Do(proxyReq)
			if err != nil {
				hr.Error = err
				hr.Latency = time.Since(start)
				resultsCh <- hr
				return
			}
			defer resp.Body.Close()

			respBody, err := io.ReadAll(resp.Body)
			if err != nil {
				hr.Error = err
				hr.Latency = time.Since(start)
				resultsCh <- hr
				return
			}

			hr.Status = resp.StatusCode
			hr.Headers = resp.Header
			hr.Body = respBody
			hr.Latency = time.Since(start)
			resultsCh <- hr
		}(i, n)
	}

	// Wait for first successful response
	var winner *hedgeResponse
	var errors []hedgeResponse
	for i := 0; i < maxNodes; i++ {
		hr := <-resultsCh
		if hr.Error == nil && hr.Status >= 200 && hr.Status < 400 {
			winner = &hr
			cancel() // Cancel all in-flight losers
			break
		}
		errors = append(errors, hr)
		// Record failure on the losing node
		for _, n := range nodes {
			if n.GetName() == hr.NodeName {
				if hr.Error != nil {
					n.RecordFailure(hr.Error.Error())
				} else {
					n.RecordFailure(fmt.Sprintf("HTTP %d", hr.Status))
				}
				break
			}
		}
	}

	// Drain remaining results (cancelled nodes)
	go func() {
		wg.Wait()
		close(resultsCh)
	}()

	if winner != nil {
		// Record success on the winning node
		for _, n := range nodes {
			if n.GetName() == winner.NodeName {
				n.RecordSuccess()
				break
			}
		}
		log.Printf("[api] hedged: winner=%s (%s latency), profile=%s, model=%s, cancelled=%d",
			winner.NodeName, winner.Latency.Round(time.Millisecond), winner.Profile, winner.Model, len(errors))
		// Write winner response
		for k, vv := range winner.Headers {
			for _, v := range vv {
				w.Header().Add(k, v)
			}
		}
		w.Header().Set("X-Mycelium-Profile", string(winner.Profile))
		w.Header().Set("X-Mycelium-Node", winner.NodeName)
		w.Header().Set("X-Mycelium-Model", winner.Model)
		w.Header().Set("X-Mycelium-Hedged", "true")
		w.WriteHeader(winner.Status)
		w.Write(winner.Body)
		return
	}

	// All nodes failed
	log.Printf("[api] hedged: all %d nodes failed, falling back to local Ollama", len(errors))
	s.proxyToLocal(w, r, body)
}

// nodeProxyURL returns the proxy URL for a given node, or empty string if
// the node can't be proxied to directly.
func (s *Server) nodeProxyURL(n *node.Node, path string) string {
	if n.Config.APIPort > 0 {
		return fmt.Sprintf("http://%s:%d%s", n.Config.Host, n.Config.APIPort, path)
	}
	// RPC-only node: use llama-server
	if s.LlamaManager != nil && s.LlamaManager.IsHealthy() {
		return fmt.Sprintf("%s%s", s.LlamaManager.BaseURL(), path)
	}
	// Fallback to local Ollama
	fallbackAddr := s.Config.Routing.FallbackLocal
	return fmt.Sprintf("http://%s%s", fallbackAddr, path)
}


// handleSubmit accepts an async inference job.
// POST /api/submit with {"model":"...", "prompt":"...", "profile":"muninn"}
// Returns the job immediately with status "queued".
func (s *Server) handleSubmit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
		return
	}

	if s.QueueManager == nil {
		writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{Error: "async queue not enabled"})
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "failed to read request"})
		return
	}

	var req queue.SubmitRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid request body"})
		return
	}

	if req.Prompt == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "prompt is required"})
		return
	}

	if req.Model == "" {
		req.Model = "default"
	}

	job := s.QueueManager.Submit(req)
	log.Printf("[api] submit: %s (model=%s)", job.ID, job.Model)
	writeJSON(w, http.StatusAccepted, job)
}

// handleGetJob retrieves a job by ID.
// GET /api/job/<id>
func (s *Server) handleGetJob(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
		return
	}

	if s.QueueManager == nil {
		writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{Error: "async queue not enabled"})
		return
	}

	// Extract job ID from path: /api/job/<id>
	jobID := r.URL.Path[len("/api/job/"):]
	if jobID == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "job ID required"})
		return
	}

	job, ok := s.QueueManager.GetJob(jobID)
	if !ok {
		writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "job not found"})
		return
	}

	writeJSON(w, http.StatusOK, job)
}

// handleListJobs lists all jobs.
// GET /api/jobs
func (s *Server) handleListJobs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
		return
	}

	if s.QueueManager == nil {
		writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{Error: "async queue not enabled"})
		return
	}

	jobs := s.QueueManager.ListJobs()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"jobs":       jobs,
		"queue_depth": s.QueueManager.QueueDepth(),
		"total":       len(jobs),
	})
}

// handleRPCProbe probes all RPC nodes and returns their device memory info.
func (s *Server) handleRPCProbe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
		return
	}

	type probeResult struct {
		Addr      string `json:"addr"`
		Connected bool   `json:"connected"`
		FreeMem   string `json:"free_mem,omitempty"`
		TotalMem  string `json:"total_mem,omitempty"`
		Alignment uint64 `json:"alignment,omitempty"`
		MaxSize   string `json:"max_size,omitempty"`
		Latency   string `json:"latency,omitempty"`
		Error     string `json:"error,omitempty"`
	}

	var results []probeResult
	for _, n := range s.Manager.AllNodes() {
		if n.Config.Protocol != config.ProtocolRPC {
			continue
		}
		addr := fmt.Sprintf("%s:%d", n.Config.Host, n.Config.Port)
		client := rpc.NewClient(addr)

		start := time.Now()
		if err := client.Dial(); err != nil {
			results = append(results, probeResult{
				Addr:  addr,
				Error: err.Error(),
			})
			continue
		}

		mem, err := client.GetDeviceMemory()
		if err != nil {
			client.Close()
			results = append(results, probeResult{
				Addr:  addr,
				Error: err.Error(),
			})
			continue
		}

		alignment, _ := client.GetAlignment()
		maxSize, _ := client.GetMaxSize()
		latency := time.Since(start)
		client.Close()

		results = append(results, probeResult{
			Addr:      addr,
			Connected: true,
			FreeMem:   rpc.FormatMemory(mem.Free),
			TotalMem:  rpc.FormatMemory(mem.Total),
			Alignment: alignment,
			MaxSize:   rpc.FormatMemory(maxSize),
			Latency:   latency.Round(time.Millisecond).String(),
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"rpc_nodes": results,
	})
}

// handleGPUCheck runs GPU verification on all GPU-type nodes.
// GET /api/gpu-check — verify all GPU nodes
// GET /api/gpu-check?node=hearth — verify specific node
// POST /api/gpu-check?node=hearth&force_reload=true&model=little-watts — force reload to GPU
func (s *Server) handleGPUCheck(w http.ResponseWriter, r *http.Request) {
	nodeName := r.URL.Query().Get("node")
	forceReload := r.URL.Query().Get("force_reload") == "true"
	modelName := r.URL.Query().Get("model")

	type gpuCheckResult struct {
		Node        string `json:"node"`
		GPUVerified bool   `json:"gpu_verified"`
		GPUOnCPU    bool   `json:"gpu_on_cpu"`
		GPUModel    string `json:"gpu_model,omitempty"`
		Action      string `json:"action,omitempty"`
		Error       string `json:"error,omitempty"`
	}

	var results []gpuCheckResult

	for _, n := range s.Manager.AllNodes() {
		// Skip non-Ollama nodes
		if n.Config.Protocol != config.ProtocolOllama {
			continue
		}
		// Filter by node name if specified
		if nodeName != "" && n.GetName() != nodeName {
			continue
		}
		// Skip non-GPU nodes unless explicitly requested
		if n.Config.Type != config.NodeTypeGPU && nodeName == "" {
			continue
		}

		result := gpuCheckResult{Node: n.GetName()}

		// Run verification
		if err := n.VerifyGPU(); err != nil {
			result.Error = err.Error()
			results = append(results, result)
			continue
		}

		result.GPUVerified = n.GetGPUVerified()
		result.GPUOnCPU = n.GetGPUOnCPU()
		result.GPUModel = n.GetGPUModel()

		// Force reload if requested and model is on CPU
		if forceReload && n.GetGPUOnCPU() {
			modelToReload := modelName
			if modelToReload == "" {
				modelToReload = n.GetGPUModel()
			}
			if modelToReload != "" {
				log.Printf("[gpu-check] forcing reload of %s on %s (was on CPU)", modelToReload, n.GetName())
				if err := n.ForceGPUReload(modelToReload); err != nil {
					result.Action = fmt.Sprintf("force_reload_failed: %v", err)
				} else {
					result.GPUVerified = n.GetGPUVerified()
					result.GPUOnCPU = n.GetGPUOnCPU()
					if n.GetGPUVerified() {
						result.Action = "force_reload_success"
					} else {
						result.Action = "force_reload_still_on_cpu"
					}
				}
			}
		}

		results = append(results, result)
	}

	if len(results) == 0 {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"gpu_nodes": []gpuCheckResult{},
			"message":   "no GPU-type Ollama nodes found",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"gpu_nodes": results,
	})
}
