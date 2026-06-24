// Package queue implements an async job queue for the Mycelium.
// Long-running inference requests are submitted as jobs, processed
// by a background worker, and results are retrieved by job ID.
//
// This enables the "Muninn" pattern: slow nodes (Crow, Wren, Ember)
// that can't serve real-time requests can grind through background
// jobs at their own pace. The synchronous path (Huginn) handles
// real-time queries on the GPU; the async path (Muninn) handles
// deep work across the full mesh.
package queue

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/aaronrdavis/mycelium-api/internal/llamaserver"
	"github.com/aaronrdavis/mycelium-api/internal/node"
)

// JobStatus represents the lifecycle state of a job.
type JobStatus string

const (
	StatusQueued    JobStatus = "queued"
	StatusRunning   JobStatus = "running"
	StatusComplete  JobStatus = "complete"
	StatusFailed    JobStatus = "failed"
)

// Job is a single async inference request.
type Job struct {
	ID        string    `json:"id"`
	Status    JobStatus `json:"status"`
	Model     string    `json:"model"`
	Prompt    string    `json:"prompt"`
	Profile   string    `json:"profile,omitempty"`
	Response  string    `json:"response,omitempty"`
	Error     string    `json:"error,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	StartedAt time.Time `json:"started_at,omitempty"`
	DoneAt    time.Time `json:"done_at,omitempty"`
	Tokens    int       `json:"tokens,omitempty"`
	TokPerSec float64   `json:"tok_per_sec,omitempty"`
	Node      string    `json:"node,omitempty"`
}

// SubmitRequest is the body for POST /api/submit.
type SubmitRequest struct {
	Model   string `json:"model"`
	Prompt  string `json:"prompt"`
	Profile string `json:"profile,omitempty"`
}

// Manager runs the async job queue.
type Manager struct {
	mu           sync.Mutex
	jobs         map[string]*Job
	queue        []*Job
	llamaManager *llamaserver.Manager
	nodeManager  *node.Manager
	nextID       int
	workerCtx    context.Context
	workerCancel context.CancelFunc
	wg           sync.WaitGroup
}

// NewManager creates a job queue manager.
func NewManager(llamaMgr *llamaserver.Manager, nodeMgr *node.Manager) *Manager {
	return &Manager{
		jobs:         make(map[string]*Job),
		llamaManager: llamaMgr,
		nodeManager:  nodeMgr,
	}
}

// Start launches the background worker.
func (m *Manager) Start(ctx context.Context) {
	m.workerCtx, m.workerCancel = context.WithCancel(ctx)
	m.wg.Add(1)
	go m.worker()
}

// Stop shuts down the worker.
func (m *Manager) Stop() {
	if m.workerCancel != nil {
		m.workerCancel()
	}
	m.wg.Wait()
}

// Submit adds a job to the queue and returns it immediately.
func (m *Manager) Submit(req SubmitRequest) *Job {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.nextID++
	jobID := fmt.Sprintf("job-%d", m.nextID)

	job := &Job{
		ID:        jobID,
		Status:    StatusQueued,
		Model:     req.Model,
		Prompt:    req.Prompt,
		Profile:   req.Profile,
		CreatedAt: time.Now(),
	}

	m.jobs[jobID] = job
	m.queue = append(m.queue, job)

	log.Printf("[queue] submitted %s (model=%s, prompt=%d chars)", jobID, req.Model, len(req.Prompt))
	return job
}

// GetJob retrieves a job by ID.
func (m *Manager) GetJob(id string) (*Job, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	job, ok := m.jobs[id]
	return job, ok
}

// ListJobs returns all jobs (most recent first, max 100).
func (m *Manager) ListJobs() []*Job {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := make([]*Job, 0, len(m.jobs))
	for _, job := range m.jobs {
		result = append(result, job)
	}
	return result
}

// QueueDepth returns the number of queued (not yet running) jobs.
func (m *Manager) QueueDepth() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	count := 0
	for _, job := range m.queue {
		if job.Status == StatusQueued {
			count++
		}
	}
	return count
}

// worker processes jobs from the queue.
func (m *Manager) worker() {
	defer m.wg.Done()

	for {
		select {
		case <-m.workerCtx.Done():
			return
		default:
		}

		job := m.dequeue()
		if job == nil {
			time.Sleep(2 * time.Second)
			continue
		}

		m.processJob(job)
	}
}

// dequeue removes and returns the next queued job (FIFO).
func (m *Manager) dequeue() *Job {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i, job := range m.queue {
		if job.Status == StatusQueued {
			job.Status = StatusRunning
			job.StartedAt = time.Now()
			// Remove from queue
			m.queue = append(m.queue[:i], m.queue[i+1:]...)
			return job
		}
	}
	return nil
}

// processJob runs a single inference job through llama-server.
func (m *Manager) processJob(job *Job) {
	log.Printf("[queue] processing %s", job.ID)

	if m.llamaManager == nil || !m.llamaManager.IsHealthy() {
		m.completeJob(job, "", "llama-server not available", 0, 0)
		return
	}

	// Build llama-server completion request
	completionReq := map[string]interface{}{
		"prompt":      job.Prompt,
		"n_predict":   256,
		"temperature": 0.8,
		"stream":      false,
	}

	body, _ := json.Marshal(completionReq)
	targetURL := fmt.Sprintf("%s/completion", m.llamaManager.BaseURL())

	// Use a fresh transport each time to avoid connection reuse EOF
	transport := &http.Transport{
		DisableKeepAlives: true,
	}
	client := &http.Client{
		Timeout:   600 * time.Second, // 10 min — async, no rush
		Transport: transport,
	}

	req, err := http.NewRequest("POST", targetURL, bytes.NewReader(body))
	if err != nil {
		m.completeJob(job, "", fmt.Sprintf("request error: %v", err), 0, 0)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Connection", "close")

	resp, err := client.Do(req)
	if err != nil {
		m.completeJob(job, "", fmt.Sprintf("llama-server error: %v", err), 0, 0)
		return
	}
	defer resp.Body.Close()

	var llmResp struct {
		Content string `json:"content"`
		Timings struct {
			PredictedPerSecond float64 `json:"predicted_per_second"`
			PredictedN         int     `json:"predicted_n"`
		} `json:"timings"`
		Stop bool `json:"stop"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&llmResp); err != nil {
		m.completeJob(job, "", fmt.Sprintf("parse error: %v", err), 0, 0)
		return
	}

	nodeName := "llama-server-rpc"
	m.completeJob(job, llmResp.Content, "", llmResp.Timings.PredictedN, llmResp.Timings.PredictedPerSecond)
	job.Node = nodeName

	log.Printf("[queue] completed %s (%d tokens, %.1f tok/s, %s)",
		job.ID, llmResp.Timings.PredictedN, llmResp.Timings.PredictedPerSecond,
		time.Since(job.StartedAt).Round(time.Second))
}

// completeJob finalizes a job with results or error.
func (m *Manager) completeJob(job *Job, response, errMsg string, tokens int, tps float64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	job.DoneAt = time.Now()
	job.Tokens = tokens
	job.TokPerSec = tps

	if errMsg != "" {
		job.Status = StatusFailed
		job.Error = errMsg
		log.Printf("[queue] failed %s: %s", job.ID, errMsg)
	} else {
		job.Status = StatusComplete
		job.Response = response
	}
}

