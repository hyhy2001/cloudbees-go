// Package node — exported service layer for TUI and other consumers.
package node

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"

	"bee/internal/api"
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

// ── Folders Plus controlled-agent handshake ───────────────────────────────────

// CreateFolderRequest creates a controlled-agent request on the folder side.
// Returns the grantId (Request Key) to hand to the agent admin.
// Step 1 of the 5-step handshake.
func CreateFolderRequest(ctx context.Context, client *api.Client, folderName string) (string, error) {
	folderPath := folderPathSegments(folderName)
	loc, err := client.PostFormGetLocation(ctx, "/job/"+folderPath+"/controlled-slaves/requestSubmit",
		map[string]string{"Submit": "Yes"})
	if err != nil {
		return "", fmt.Errorf("folder request: %w", err)
	}
	m := grantIDRe.FindStringSubmatch(loc)
	if m == nil {
		return "", fmt.Errorf("could not extract grantId from Location: %s", loc)
	}
	return m[1], nil
}

// CreateAgentToken creates a new security token on the agent side.
// Returns the tokenId. Step 2 of the handshake.
func CreateAgentToken(ctx context.Context, client *api.Client, nodeName string) (string, error) {
	loc, err := client.PostFormGetLocation(ctx,
		"/computer/"+nodeSeg(nodeName)+"/security-tokens/createSubmit",
		map[string]string{"json": "{}", "Submit": "Yes"})
	if err != nil {
		return "", fmt.Errorf("create token: %w", err)
	}
	m := tokenIDRe.FindStringSubmatch(loc)
	if m == nil {
		return "", fmt.Errorf("could not extract tokenId from Location: %s", loc)
	}
	return m[1], nil
}

// AuthorizeAgentToken authorizes a grant request using the agent's token.
// Returns the Request Secret (hash) to pass to the folder admin. Step 3.
func AuthorizeAgentToken(ctx context.Context, client *api.Client, nodeName, tokenID, grantID string) (string, error) {
	path := "/computer/" + nodeSeg(nodeName) + "/security-tokens/tokensById/" + url.PathEscape(tokenID) + "/authorizeSubmit"
	resp, err := client.Do(ctx, "POST", path, strings.NewReader(
		"_.salt="+url.QueryEscape(grantID)+
			"&json="+url.QueryEscape(`{"salt":"`+grantID+`"}`)+
			"&Submit=Authorize"),
		"application/x-www-form-urlencoded")
	if err != nil {
		return "", fmt.Errorf("authorize token: %w", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	html := string(b)
	// Extract _.hash value from the HTML response
	m := hashRe.FindStringSubmatch(html)
	if m == nil {
		m = hashRe2.FindStringSubmatch(html)
	}
	if m == nil {
		return "", fmt.Errorf("could not find Request Secret (_.hash) in authorize response")
	}
	return m[1], nil
}

// AuthorizeFolderGrant completes the handshake from the folder side. Step 4.
func AuthorizeFolderGrant(ctx context.Context, client *api.Client, folderName, grantID, requestSecret string) error {
	folderPath := folderPathSegments(folderName)
	path := "/job/" + folderPath + "/controlled-slaves/grantsById/" + url.PathEscape(grantID) + "/authorizeSubmit"
	return client.PostForm(ctx, path, map[string]string{
		"_.salt": grantID,
		"_.hash": requestSecret,
		"json":   `{"salt":"` + grantID + `","hash":"` + requestSecret + `"}`,
		"Submit": "Authorize",
	})
}

// DeleteAgentToken deletes a security token on an agent. Used for rollback.
func DeleteAgentToken(ctx context.Context, client *api.Client, nodeName, tokenID string) error {
	return client.PostForm(ctx,
		"/computer/"+nodeSeg(nodeName)+"/security-tokens/tokensById/"+url.PathEscape(tokenID)+"/doDelete",
		map[string]string{"Submit": "Yes"})
}

// RemoveControlledAgentGrant removes a grant from a folder by grantId.
func RemoveControlledAgentGrant(ctx context.Context, client *api.Client, folderName, grantID string) error {
	folderPath := folderPathSegments(folderName)
	return client.PostForm(ctx,
		"/job/"+folderPath+"/controlled-slaves/grantsById/"+url.PathEscape(grantID)+"/doDelete",
		map[string]string{"Submit": "Yes"})
}

// ApproveFolder performs the full 5-step Folders Plus controlled-agent handshake.
// Requires admin rights on both the agent and the folder.
// Rolls back dangling artifacts if any step fails.
func ApproveFolder(ctx context.Context, client *api.Client, nodeName, folderName string) error {
	var grantID, tokenID string
	var err error

	// Step 1+2 could run in parallel but we keep sequential for simplicity and
	// easier rollback tracking.
	if grantID, err = CreateFolderRequest(ctx, client, folderName); err != nil {
		return fmt.Errorf("step 1 (folder request): %w", err)
	}
	if tokenID, err = CreateAgentToken(ctx, client, nodeName); err != nil {
		_ = RemoveControlledAgentGrant(ctx, client, folderName, grantID)
		return fmt.Errorf("step 2 (agent token): %w", err)
	}
	requestSecret, err := AuthorizeAgentToken(ctx, client, nodeName, tokenID, grantID)
	if err != nil {
		_ = DeleteAgentToken(ctx, client, nodeName, tokenID)
		_ = RemoveControlledAgentGrant(ctx, client, folderName, grantID)
		return fmt.Errorf("step 3 (authorize token): %w", err)
	}
	if err = AuthorizeFolderGrant(ctx, client, folderName, grantID, requestSecret); err != nil {
		_ = DeleteAgentToken(ctx, client, nodeName, tokenID)
		_ = RemoveControlledAgentGrant(ctx, client, folderName, grantID)
		return fmt.Errorf("step 4 (authorize grant): %w", err)
	}
	return nil
}

// folderPathSegments converts "folder/sub" → "folder/job/sub" for Jenkins REST.
func folderPathSegments(name string) string {
	parts := strings.Split(name, "/")
	out := make([]string, len(parts))
	for i, p := range parts {
		out[i] = url.PathEscape(p)
	}
	return strings.Join(out, "/job/")
}

// regex helpers for parsing Jenkins redirect Location headers
var (
	grantIDRe = regexp.MustCompile(`grantsById/([^/"'\s?#]+)`)
	tokenIDRe = regexp.MustCompile(`tokensById/([^/"'\s?#]+)`)
	hashRe    = regexp.MustCompile(`name=["']_\.hash["'][^>]*value=["']([0-9a-fA-F]+)["']`)
	hashRe2   = regexp.MustCompile(`value=["']([0-9a-fA-F]{32,})["'][^>]*name=["']_\.hash["']`)
)
