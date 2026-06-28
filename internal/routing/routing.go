// Package routing implements the Three Ravens routing strategy.
// Huginn: fast local (quick answers).
// Muninn: deep remote (memory-weighted, large model).
// Skald: sacred vocabulary (precise, deterministic).
package routing

import (
	"strings"

	"github.com/aaronrdavis/mycelium-api/internal/config"
	"github.com/aaronrdavis/mycelium-api/internal/node"
)

// Profile identifies which Raven handles a request.
type Profile string

const (
	ProfileHuginn Profile = "huginn"
	ProfileMuninn Profile = "muninn"
	ProfileSkald  Profile = "skald"
)

// RouteResult contains the resolved routing decision.
type RouteResult struct {
	Profile Profile
	Rule    config.RouteRule
	Node    *node.Node
	Model   string
}

// Router maps requests to Raven profiles and selects nodes.
type Router struct {
	Config  *config.Config
	Manager *node.Manager
}

// NewRouter creates a router from config and node manager.
func NewRouter(cfg *config.Config, mgr *node.Manager) *Router {
	return &Router{
		Config:  cfg,
		Manager: mgr,
	}
}

// Classify determines which Raven profile should handle a request.
func (r *Router) Classify(model string, stream bool, contextLength int) Profile {
	lower := strings.ToLower(model)

	switch {
	case strings.Contains(lower, "flash"),
		strings.Contains(lower, "small"),
		strings.Contains(lower, "fast"),
		strings.Contains(lower, "qwen2.5-3b"),
		strings.Contains(lower, "qwen3.5"):
		return ProfileHuginn

	case strings.Contains(lower, "glm-4"),
		strings.Contains(lower, "large"),
		strings.Contains(lower, "deepseek"),
		strings.Contains(lower, "70b"):
		return ProfileMuninn

	case strings.Contains(lower, "sacred"),
		strings.Contains(lower, "skald"),
		strings.Contains(lower, "vocab"),
		strings.Contains(lower, "precise"):
		return ProfileSkald
	}

	if contextLength > 4096 {
		return ProfileMuninn
	}

	if stream {
		return ProfileHuginn
	}

	return Profile(r.Config.Routing.Default)
}

// Route classifies a request and selects a node for it.
func (r *Router) Route(model string, stream bool, contextLength int) (*RouteResult, error) {
	profile := r.Classify(model, stream, contextLength)

	var rule config.RouteRule
	switch profile {
	case ProfileHuginn:
		rule = r.Config.Routing.Huginn
	case ProfileMuninn:
		rule = r.Config.Routing.Muninn
	case ProfileSkald:
		rule = r.Config.Routing.Skald
	default:
		rule = r.Config.Routing.Huginn
	}

	resolvedModel := model
	if rule.Model != "" {
		resolvedModel = rule.Model
	}

	n, err := r.Manager.SelectNode(rule.Pools)
	if err != nil {
		// Try fallback pools
		fallbackPools := r.fallbackPools(profile)
		n, err = r.Manager.SelectNode(fallbackPools)
		if err != nil {
			return nil, err
		}
	}

	return &RouteResult{
		Profile: profile,
		Rule:    rule,
		Node:    n,
		Model:   resolvedModel,
	}, nil
}

// RouteHedged classifies a request and selects multiple nodes for hedged routing.
// Returns up to maxHedge nodes, ordered best-first, plus the routing profile and model.
func (r *Router) RouteHedged(model string, stream bool, contextLength int, maxHedge int) (*RouteHedgedResult, error) {
	profile := r.Classify(model, stream, contextLength)

	var rule config.RouteRule
	switch profile {
	case ProfileHuginn:
		rule = r.Config.Routing.Huginn
	case ProfileMuninn:
		rule = r.Config.Routing.Muninn
	case ProfileSkald:
		rule = r.Config.Routing.Skald
	default:
		rule = r.Config.Routing.Huginn
	}

	resolvedModel := model
	if rule.Model != "" {
		resolvedModel = rule.Model
	}

	nodes, err := r.Manager.SelectNodes(rule.Pools, maxHedge)
	if err != nil {
		// Try fallback pools
		fallbackPools := r.fallbackPools(profile)
		nodes, err = r.Manager.SelectNodes(fallbackPools, maxHedge)
		if err != nil {
			return nil, err
		}
	}

	return &RouteHedgedResult{
		Profile: profile,
		Rule:    rule,
		Nodes:   nodes,
		Model:   resolvedModel,
	}, nil
}

// RouteHedgedResult contains multiple node candidates for hedged routing.
type RouteHedgedResult struct {
	Profile Profile
	Rule    config.RouteRule
	Nodes   []*node.Node
	Model   string
}

func (r *Router) fallbackPools(p Profile) []string {
	switch p {
	case ProfileHuginn:
		return []string{"remote", "edge"}
	case ProfileMuninn:
		return []string{"local", "edge"}
	case ProfileSkald:
		return []string{"remote"}
	default:
		return []string{"local", "remote", "edge"}
	}
}

// String returns a human-readable profile name.
func (p Profile) String() string {
	switch p {
	case ProfileHuginn:
		return "Huginn (fast local)"
	case ProfileMuninn:
		return "Muninn (deep remote)"
	case ProfileSkald:
		return "Skald (precise)"
	default:
		return string(p)
	}
}
