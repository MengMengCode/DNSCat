package api

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"dnscat/internal/cluster"
	"dnscat/internal/database"
	"dnscat/internal/model"

	"github.com/gin-gonic/gin"
)

type NodeHandler struct {
	clusterMgr *cluster.ClusterManager
}

func NewNodeHandler(cm *cluster.ClusterManager) *NodeHandler {
	return &NodeHandler{clusterMgr: cm}
}

type CreateNodeReq struct {
	Name   string `json:"name" binding:"required"`
	IP     string `json:"ip"`
	Region string `json:"region"`
}

func (h *NodeHandler) ListNodes(c *gin.Context) {
	var nodes []model.Node
	if err := database.DB.Order("id asc").Find(&nodes).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch nodes"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"nodes": nodes, "total": len(nodes)})
}

func (h *NodeHandler) CreateNode(c *gin.Context) {
	var req CreateNodeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid node parameters"})
		return
	}

	b := make([]byte, 16)
	_, _ = rand.Read(b)
	nodeID := fmt.Sprintf("node-%s", hex.EncodeToString(b[:4]))
	secretToken := fmt.Sprintf("tok_%s", hex.EncodeToString(b))

	ip := req.IP
	if ip == "" {
		ip = "0.0.0.0"
	}
	region := req.Region
	if region == "" {
		region = "Global"
	}

	node := model.Node{
		NodeID:        nodeID,
		Name:          req.Name,
		IP:            ip,
		Region:        region,
		SecretToken:   secretToken,
		IsOnline:      false,
		LastHeartbeat: time.Now(),
		// 版本留空：节点还没上报过心跳，此时填任何值都是猜的。
		// 早前预填 "v1.0.0"，导致控制台把从未连上的节点显示成某个具体版本。
		Version:   "",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := database.DB.Create(&node).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create node"})
		return
	}

	c.JSON(http.StatusCreated, node)
}

func (h *NodeHandler) DeleteNode(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	database.DB.Where("id = ?", id).Delete(&model.Node{})
	c.JSON(http.StatusOK, gin.H{"message": "Node deleted successfully"})
}

func (h *NodeHandler) GetInstallScript(c *gin.Context) {
	nodeID := c.Query("node_id")
	var node model.Node
	if err := database.DB.Where("node_id = ?", nodeID).First(&node).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Node not found"})
		return
	}

	host := c.Request.Host
	scheme := "http"
	if c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	masterURL := fmt.Sprintf("%s://%s", scheme, host)

	script := fmt.Sprintf(`#!/usr/bin/env bash
# DnsCat Edge Node One-Click Installer
set -e

echo ">>> Installing DnsCat Edge Node [%s]..."
MASTER_URL="%s"
NODE_ID="%s"
TOKEN="%s"

# Download binary or run via Docker
docker run -d \
  --name dnscat-node \
  --restart always \
  -p 53:53/udp \
  -p 53:53/tcp \
  dnscat/node:latest \
  --master "$MASTER_URL" \
  --node-id "$NODE_ID" \
  --token "$TOKEN" \
  --udp 53 \
  --tcp 53

echo ">>> DnsCat Edge Node [$NODE_ID] successfully started and connected to $MASTER_URL"
`, node.Name, masterURL, node.NodeID, node.SecretToken)

	c.JSON(http.StatusOK, gin.H{
		"script":     script,
		"master_url": masterURL,
		"node_id":    node.NodeID,
		"token":      node.SecretToken,
	})
}

func (h *NodeHandler) SyncSnapshot(c *gin.Context) {
	if h.clusterMgr == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Cluster manager not available"})
		return
	}

	snapshot, err := h.clusterMgr.GetFullSnapshot()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to produce zone snapshot"})
		return
	}

	c.JSON(http.StatusOK, snapshot)
}

func (h *NodeHandler) Heartbeat(c *gin.Context) {
	var req cluster.NodeHeartbeatReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid heartbeat body"})
		return
	}

	clientIP := c.ClientIP()
	if err := h.clusterMgr.RecordHeartbeat(&req, clientIP); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok", "ack_time": time.Now().Unix()})
}
