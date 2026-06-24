// Package api implements Ollama-compatible HTTP endpoints
// that route requests through the Mycelium's Three Ravens.
package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
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
	Name     string `json:"name"`
	Type     string `json:"type"`
	Status   string `json:"status"`
	Latency  string `json:"latency"`
	Pool     string `json:"pool"`
	Protocol string `json:"protocol,omitempty"`
	FreeMem  string `json:"free_mem,omitempty"`
	TotalMem string `json:"total_mem,omitempty"`
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
	result, err := s.Router.Route(req.Model, stream, len(req.Prompt))
	if err != nil {
		log.Printf("[api] All Mycelium nodes down, falling back to local Ollama: %v", err)
		s.proxyToLocal(w, r, body)
		return
	}

	log.Printf("[api] generate: %s -> %s -> %s (model: %s)", profileOrDefault(req.Profile), result.Profile, result.Node.Config.Name, result.Model)
	s.routeToNode(w, r, result, body)
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

	result, err := s.Router.Route(req.Model, stream, contextLen)
	if err != nil {
		log.Printf("[api] All Mycelium nodes down, falling back to local Ollama: %v", err)
		s.proxyToLocal(w, r, body)
		return
	}

	log.Printf("[api] chat: %s -> %s -> %s (model: %s)", profileOrDefault(req.Profile), result.Profile, result.Node.Config.Name, result.Model)
	s.routeToNode(w, r, result, body)
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
			Name:     n.GetName(),
			Type:     string(n.GetType()),
			Status:   string(n.GetStatus()),
			Latency:  latency,
			Pool:     n.GetPool(),
			Protocol: string(n.Config.Protocol),
			FreeMem:  rpc.FormatMemory(n.GetFreeMem()),
			TotalMem: rpc.FormatMemory(n.GetTotalMem()),
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
		s.proxyToURL(w, r, targetURL, proxiedBody, result)
		return
	}

	// RPC-only node: use llama-server as the inference frontend
	// llama-server on Hearth handles model loading, RPC nodes handle compute offload
	if s.LlamaManager != nil && s.LlamaManager.IsHealthy() {
		// Proxy to llama-server's OpenAI-compatible API
		targetURL := fmt.Sprintf("%s%s", s.LlamaManager.BaseURL(), r.URL.Path)
		s.proxyToURL(w, r, targetURL, proxiedBody, result)
		return
	}

	// Fallback: use local Ollama
	fallbackAddr := s.Config.Routing.FallbackLocal
	targetURL := fmt.Sprintf("http://%s%s", fallbackAddr, r.URL.Path)
	s.proxyToURL(w, r, targetURL, proxiedBody, result)
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
