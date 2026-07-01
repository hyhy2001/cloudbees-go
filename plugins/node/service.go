// Package node — exported service layer for TUI and other consumers.
package node

import (
	"context"
	"io"
	"fmt"
	"net/url"
	"strings"

	"github.com/hyhy2001/bee/internal/api"
)

// NodeDTO is the exported lightweight node view.
type NodeDTO struct {
	DisplayName  string
	Description  string
	Offline      bool
	NumExecutors int
	Labels       string
}

// ListNodes fetches all nodes from /computer/api/json.
func ListNodes(ctx context.Context, client *api.Client) ([]NodeDTO, error) {
	var raw struct {
		Computer []struct {
			DisplayName    string `json:"displayName"`
			Description    string `json:"description"`
			Offline        bool   `json:"offline"`
			NumExecutors   int    `json:"numExecutors"`
			AssignedLabels []struct {
				Name string `json:"name"`
			} `json:"assignedLabels"`
		} `json:"computer"`
	}
	tree := "computer[displayName,description,offline,numExecutors,assignedLabels[name]]"
	if err := client.GetJSON(ctx, "/computer/api/json?tree="+url.QueryEscape(tree), &raw); err != nil {
		return nil, err
	}
	out := make([]NodeDTO, 0, len(raw.Computer))
	for _, n := range raw.Computer {
		parts := make([]string, 0, len(n.AssignedLabels))
		for _, l := range n.AssignedLabels {
			if l.Name != "" && l.Name != n.DisplayName {
				parts = append(parts, l.Name)
			}
		}
		out = append(out, NodeDTO{
			DisplayName:  n.DisplayName,
			Description:  n.Description,
			Offline:      n.Offline,
			NumExecutors: n.NumExecutors,
			Labels:       strings.Join(parts, " "),
		})
	}
	return out, nil
}

// DeleteNode sends doDelete for the named node.
func DeleteNode(ctx context.Context, client *api.Client, name string) error {
	return client.PostForm(ctx, "/computer/"+nodeSeg(name)+"/doDelete", map[string]string{})
}

// ToggleOffline toggles the offline state of a node. Pass reason="" to bring online.
func ToggleOffline(ctx context.Context, client *api.Client, name, reason string) error {
	path := "/computer/" + nodeSeg(name) + "/toggleOffline?offlineMessage=" + url.QueryEscape(reason)
	return client.PostForm(ctx, path, map[string]string{})
}

// GetNodeConfigXML fetches the config.xml for a node.
func GetNodeConfigXML(ctx context.Context, client *api.Client, name string) (string, error) {
	resp, err := client.Do(ctx, "GET", "/computer/"+nodeSeg(name)+"/config.xml", nil, "")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("GET config.xml: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	b, err := io.ReadAll(resp.Body)
	return string(b), err
}
