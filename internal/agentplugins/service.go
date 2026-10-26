package agentplugins

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	log "github.com/sirupsen/logrus"
)

// Service provides marketplace features for agent skills and plugins.
type Service struct {
	cfg        config.AgentPluginsConfig
	cfgMu      sync.RWMutex
	sources    map[string]Source
	plugins    map[string]Plugin
	httpClient *http.Client
}

// NewService creates and initializes the Agent Plugins Marketplace service.
func NewService(cfg config.AgentPluginsConfig) *Service {
	s := &Service{
		cfg:        cfg,
		sources:    make(map[string]Source),
		plugins:    make(map[string]Plugin),
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
	s.initDefaults()
	s.loadCustomSources()
	return s
}

func (s *Service) initDefaults() {
	// Default Marketplace Sources
	curatedSource := Source{
		ID:          "curated",
		Name:        "Official & Curated Skills",
		Description: "Official and community validated AI Agent plugins and tools",
		Enabled:     true,
	}
	s.sources[curatedSource.ID] = curatedSource

	cacheSource := Source{
		ID:          "codex-plus-cache",
		Name:        "Codex++ Plugin Mirror",
		Repository:  "BigPizzaV3/CodexPlusPlusPluginCache",
		Description: "High-speed mirror and offline package cache for AI coding skills",
		Enabled:     true,
	}
	s.sources[cacheSource.ID] = cacheSource

	// Seed Curated Plugins
	defaultPlugins := []Plugin{
		{
			ID:          "imagegen",
			Name:        "ImageGen",
			Description: "Generate or edit raster images using AI bitmap models for sprites, mockups, or textures.",
			Author:      "OpenAI / Community",
			Version:     "1.2.0",
			Category:    "skills",
			SourceID:    "curated",
			Tags:        []string{"image", "design", "creative"},
		},
		{
			ID:          "find-docs",
			Name:        "Find Docs",
			Description: "Retrieves up-to-date documentation, API references, and code examples for any library.",
			Author:      "Context7 / Community",
			Version:     "2.0.4",
			Category:    "skills",
			SourceID:    "curated",
			Tags:        []string{"docs", "search", "api"},
		},
		{
			ID:          "pdf",
			Name:        "PDF Toolkit",
			Description: "Read, create, inspect, render, and verify PDF files where visual layout matters.",
			Author:      "OpenAI / Bundled",
			Version:     "26.1025.11240",
			Category:    "tools",
			SourceID:    "curated",
			Tags:        []string{"pdf", "document", "office"},
		},
		{
			ID:          "spreadsheets",
			Name:        "Spreadsheets Toolkit",
			Description: "Create, modify, analyze, and visualize Excel and CSV spreadsheets with formulas and charts.",
			Author:      "OpenAI / Bundled",
			Version:     "26.1025.11240",
			Category:    "tools",
			SourceID:    "curated",
			Tags:        []string{"excel", "csv", "data"},
		},
		{
			ID:          "presentations",
			Name:        "Slide Decks & PPT",
			Description: "Create, read, and edit PowerPoint presentation slide decks.",
			Author:      "OpenAI / Bundled",
			Version:     "26.1025.11240",
			Category:    "tools",
			SourceID:    "curated",
			Tags:        []string{"powerpoint", "slides", "presentation"},
		},
		{
			ID:          "reverse-engineer-anything",
			Name:        "Reverse Engineer Anything (REA)",
			Description: "Analyze and decompile native binaries, managed code, Electron packages, and browser apps.",
			Author:      "Reverse Engineering Labs",
			Version:     "1.5.0",
			Category:    "tools",
			SourceID:    "curated",
			Tags:        []string{"reverse-engineering", "decompilation", "security"},
		},
		{
			ID:          "codex-security",
			Name:        "Codex Security Suite",
			Description: "Deep security audit, attack-path analysis, vulnerability discovery, and patch risk assessment.",
			Author:      "OpenAI Curated",
			Version:     "0.7.22",
			Category:    "skills",
			SourceID:    "curated",
			Tags:        []string{"security", "audit", "patch-risk"},
		},
		{
			ID:          "design-taste-frontend",
			Name:        "Anti-Slop Frontend Taste",
			Description: "Art direction for websites and user interfaces distilled from curated design gallery sites.",
			Author:      "Taste Skill v2",
			Version:     "2.1.0",
			Category:    "skills",
			SourceID:    "curated",
			Tags:        []string{"frontend", "ui", "design", "css"},
		},
	}

	for _, p := range defaultPlugins {
		s.plugins[p.ID] = p
	}
}

func (s *Service) loadCustomSources() {
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	for _, src := range s.cfg.Sources {
		if src.ID != "" {
			s.sources[src.ID] = Source{
				ID:          src.ID,
				Name:        src.Name,
				URL:         src.URL,
				Repository:  src.Repository,
				Description: src.Description,
				Enabled:     true,
			}
		}
	}
}

// ListPlugins returns matching plugins with optional filters.
func (s *Service) ListPlugins(ctx context.Context, query, category, sourceID string) []Plugin {
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()

	query = strings.ToLower(strings.TrimSpace(query))
	category = strings.ToLower(strings.TrimSpace(category))
	sourceID = strings.ToLower(strings.TrimSpace(sourceID))

	var results []Plugin
	for _, p := range s.plugins {
		if sourceID != "" && strings.ToLower(p.SourceID) != sourceID {
			continue
		}
		if category != "" && strings.ToLower(p.Category) != category {
			continue
		}
		if query != "" {
			matched := strings.Contains(strings.ToLower(p.ID), query) ||
				strings.Contains(strings.ToLower(p.Name), query) ||
				strings.Contains(strings.ToLower(p.Description), query)
			if !matched {
				tagMatched := false
				for _, t := range p.Tags {
					if strings.Contains(strings.ToLower(t), query) {
						tagMatched = true
						break
					}
				}
				if !tagMatched {
					continue
				}
			}
		}
		results = append(results, p)
	}
	return results
}

// GetPlugin retrieves a single plugin by its ID.
func (s *Service) GetPlugin(ctx context.Context, id string) (*Plugin, bool) {
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	p, ok := s.plugins[id]
	if !ok {
		return nil, false
	}
	return &p, true
}

// ListSources returns all registered plugin sources.
func (s *Service) ListSources() []Source {
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	var res []Source
	for _, src := range s.sources {
		res = append(res, src)
	}
	return res
}

// AddSource registers a new custom source.
func (s *Service) AddSource(src Source) error {
	if src.ID == "" {
		return fmt.Errorf("source id cannot be empty")
	}
	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()
	s.sources[src.ID] = src
	return nil
}

// FetchRemoteIndex refreshes plugins from a remote registry URL.
func (s *Service) FetchRemoteIndex(ctx context.Context, url string, sourceID string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
	if err != nil {
		return err
	}

	var parsed struct {
		Plugins []Plugin `json:"plugins"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return err
	}

	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()
	for _, p := range parsed.Plugins {
		if p.ID != "" {
			if sourceID != "" {
				p.SourceID = sourceID
			}
			s.plugins[p.ID] = p
		}
	}
	log.Infof("agentplugins: loaded %d plugins from remote source %s", len(parsed.Plugins), sourceID)
	return nil
}

