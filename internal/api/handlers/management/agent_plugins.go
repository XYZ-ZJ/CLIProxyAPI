package management

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/agentplugins"
)

// ListAgentPluginMarket returns the list of agent plugins with optional filtering.
func (h *Handler) ListAgentPluginMarket(c *gin.Context) {
	if h.agentPluginSvc == nil {
		c.JSON(http.StatusOK, agentplugins.MarketResponse{})
		return
	}

	query := c.Query("q")
	category := c.Query("category")
	sourceID := c.Query("source")

	plugins := h.agentPluginSvc.ListPlugins(c.Request.Context(), query, category, sourceID)
	sources := h.agentPluginSvc.ListSources()

	c.JSON(http.StatusOK, agentplugins.MarketResponse{
		Sources: sources,
		Plugins: plugins,
		Total:   len(plugins),
	})
}

// GetAgentPlugin returns details for a single agent plugin.
func (h *Handler) GetAgentPlugin(c *gin.Context) {
	if h.agentPluginSvc == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "agent plugins service not initialized"})
		return
	}
	id := c.Param("id")
	plugin, found := h.agentPluginSvc.GetPlugin(c.Request.Context(), id)
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "plugin not found"})
		return
	}
	c.JSON(http.StatusOK, plugin)
}

// ListAgentPluginSources returns configured plugin sources.
func (h *Handler) ListAgentPluginSources(c *gin.Context) {
	if h.agentPluginSvc == nil {
		c.JSON(http.StatusOK, []agentplugins.Source{})
		return
	}
	c.JSON(http.StatusOK, h.agentPluginSvc.ListSources())
}

// PostAgentPluginSource registers a new custom agent plugin source.
func (h *Handler) PostAgentPluginSource(c *gin.Context) {
	if h.agentPluginSvc == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "agent plugins service not initialized"})
		return
	}
	var src agentplugins.Source
	if err := c.ShouldBindJSON(&src); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.agentPluginSvc.AddSource(src); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "source added", "source": src})
}
