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

// NodeConfig is the exported, human-readable digest of a node's config.xml —
// launcher type/host/port/remoteDir — for TUI/CLI detail display.
type NodeConfig struct {
	LauncherType    string
	Host            string
	Port            int
	CredID          string
	JavaPath        string
	Availability    string
	InDemandDelay   int
	IdleDelay       int
	RemoteDir       string
	ControlledAgent bool
}

// ParseNodeConfig parses a node's config.xml into a NodeConfig for display.
func ParseNodeConfig(xmlStr string) NodeConfig {
	cfg := parseNodeConfigXML(xmlStr)
	return NodeConfig{
		LauncherType:    cfg.launcherType,
		Host:            cfg.host,
		Port:            cfg.port,
		CredID:          cfg.credID,
		JavaPath:        cfg.javaPath,
		Availability:    cfg.availability,
		InDemandDelay:   cfg.inDemandDelay,
		IdleDelay:       cfg.idleDelay,
		RemoteDir:       cfg.remoteDir,
		ControlledAgent: cfg.controlledAgent,
	}
}

// CreateNodeOpts holds parameters for creating a new permanent agent.
type CreateNodeOpts struct {
	RemoteDir     string
	NumExecutors  int
	Labels        string
	Desc          string
	LauncherType  string // "ssh" | "jnlp"
	Host          string
	Port          int
	CredID        string
	JavaPath      string
	Availability  string // "always" | "demand"
	InDemandDelay int
	IdleDelay     int
}

// CreateNode creates a new permanent agent via /computer/doCreateItem.
// Mirrors the CLI nodeCreateCmd logic but as a library function.
func CreateNode(ctx context.Context, client *api.Client, name string, opts CreateNodeOpts) error {
	if opts.NumExecutors <= 0 {
		opts.NumExecutors = 1
	}
	if opts.Port <= 0 {
		opts.Port = 22
	}
	host := ""
	if opts.LauncherType == "ssh" {
		host = opts.Host
	}
	body := buildNodeFormPayload(name, opts.RemoteDir, opts.NumExecutors, opts.Labels, opts.Desc,
		host, opts.Port, opts.CredID, opts.JavaPath, opts.Availability, opts.InDemandDelay, opts.IdleDelay)
	resp, err := client.Do(ctx, "POST", "/computer/doCreateItem", strings.NewReader(body), "application/x-www-form-urlencoded")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 && resp.StatusCode != 302 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("create node: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	// If SSH creation failed, retry as JNLP then patch config.xml with SSH launcher.
	if host != "" && (resp.StatusCode == 500 || resp.StatusCode == 400) {
		jnlpBody := buildNodeFormPayload(name, opts.RemoteDir, opts.NumExecutors, opts.Labels, opts.Desc,
			"", 22, "", opts.JavaPath, opts.Availability, opts.InDemandDelay, opts.IdleDelay)
		resp2, err2 := client.Do(ctx, "POST", "/computer/doCreateItem", strings.NewReader(jnlpBody), "application/x-www-form-urlencoded")
		if err2 != nil {
			return err2
		}
		resp2.Body.Close()
		jp := opts.JavaPath
		if jp == "" {
			jp = defaultJavaPath
		}
		xmlStr, xmlErr := GetNodeConfigXML(ctx, client, name)
		if xmlErr == nil {
			sshBlock := buildLauncherXML("ssh", opts.Host, opts.Port, opts.CredID, jp)
			xmlStr = swapXMLSubtree(xmlStr, "launcher", sshBlock)
			_ = client.PostXML(ctx, "/computer/"+nodeSeg(name)+"/config.xml", xmlStr)
		}
	}
	return nil
}

// UpdateNodeOpts holds fields to patch on an existing node. Zero/nil = keep current.
type UpdateNodeOpts struct {
	RemoteDir    *string
	NumExecutors *int
	Labels       *string
	Desc         *string
	// Launcher: all three must be set together to swap launcher type.
	LauncherType *string
	Host         *string
	Port         *int
	CredID       *string
	Availability *string
	InDemandDelay *int
	IdleDelay    *int
	ControlledAgent *bool
}

// UpdateNode patches a node's config.xml with any non-nil fields.
func UpdateNode(ctx context.Context, client *api.Client, name string, opts UpdateNodeOpts) error {
	xmlStr, err := GetNodeConfigXML(ctx, client, name)
	if err != nil {
		return err
	}
	cfg := parseNodeConfigXML(xmlStr)

	if opts.Desc != nil {
		xmlStr = setXMLElement(xmlStr, "description", *opts.Desc)
	}
	if opts.RemoteDir != nil {
		xmlStr = setXMLElement(xmlStr, "remoteFS", *opts.RemoteDir)
	}
	if opts.NumExecutors != nil {
		xmlStr = setXMLElement(xmlStr, "numExecutors", fmt.Sprintf("%d", *opts.NumExecutors))
	}
	if opts.Labels != nil {
		xmlStr = setXMLElement(xmlStr, "label", *opts.Labels)
	}

	launcherTouched := opts.LauncherType != nil || opts.Host != nil || opts.Port != nil || opts.CredID != nil
	if launcherTouched {
		lt := cfg.launcherType
		if opts.LauncherType != nil {
			lt = *opts.LauncherType
		}
		h := cfg.host
		if opts.Host != nil {
			h = *opts.Host
		}
		p := cfg.port
		if opts.Port != nil {
			p = *opts.Port
		}
		cred := cfg.credID
		if opts.CredID != nil {
			cred = *opts.CredID
		}
		jp := cfg.javaPath
		block := buildLauncherXML(lt, h, p, cred, jp)
		xmlStr = swapXMLSubtree(xmlStr, "launcher", block)
	}

	retentionTouched := opts.Availability != nil || opts.InDemandDelay != nil || opts.IdleDelay != nil
	if retentionTouched {
		avail := cfg.availability
		if opts.Availability != nil {
			avail = *opts.Availability
		}
		ind := cfg.inDemandDelay
		if opts.InDemandDelay != nil {
			ind = *opts.InDemandDelay
		}
		idle := cfg.idleDelay
		if opts.IdleDelay != nil {
			idle = *opts.IdleDelay
		}
		block := buildRetentionXML(avail, ind, idle)
		xmlStr = swapXMLSubtree(xmlStr, "retentionStrategy", block)
	}

	if opts.ControlledAgent != nil {
		xmlStr = setControlledAgentXML(xmlStr, *opts.ControlledAgent)
	}

	return client.PostXML(ctx, "/computer/"+nodeSeg(name)+"/config.xml", xmlStr)
}

// ── Folders Plus controlled-agent handshake ───────────────────────────────────

const controlledAgentPropTag = "com.cloudbees.jenkins.plugins.foldersplus.SecurityTokensNodeProperty"

// setControlledAgentXML enables or disables the Folders Plus controlled-agent
// node property in a raw node config.xml string.
func setControlledAgentXML(xmlStr string, enable bool) string {
	propBlock := "  <" + controlledAgentPropTag + ">\n    <acceptTasksWithoutOwningItem>false</acceptTasksWithoutOwningItem>\n  </" + controlledAgentPropTag + ">"
	hasProp := strings.Contains(xmlStr, controlledAgentPropTag)
	if enable && !hasProp {
		if strings.Contains(xmlStr, "<nodeProperties>") {
			return strings.Replace(xmlStr, "<nodeProperties>", "<nodeProperties>\n"+propBlock, 1)
		}
		for _, rootClose := range []string{"</slave>", "</agent>", "</hudson.slaves.DumbSlave>"} {
			if k := strings.LastIndex(xmlStr, rootClose); k >= 0 {
				return xmlStr[:k] + "  <nodeProperties>\n" + propBlock + "\n  </nodeProperties>\n" + xmlStr[k:]
			}
		}
		return xmlStr
	}
	if !enable && hasProp {
		return swapXMLSubtree(xmlStr, controlledAgentPropTag, "")
	}
	return xmlStr
}

// SetControlledAgent enables (or disables) Folders Plus controlled-agent mode
// on a node — step 0 of the handshake, run automatically before requesting a
// folder grant so the agent side is ready to receive tokens.
func SetControlledAgent(ctx context.Context, client *api.Client, nodeName string, enable bool) error {
	xmlStr, err := GetNodeConfigXML(ctx, client, nodeName)
	if err != nil {
		return err
	}
	updated := setControlledAgentXML(xmlStr, enable)
	if updated == xmlStr {
		return nil
	}
	return client.PostXML(ctx, "/computer/"+nodeSeg(nodeName)+"/config.xml", updated)
}

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

	if err = SetControlledAgent(ctx, client, nodeName, true); err != nil {
		return fmt.Errorf("step 0 (enable controlled-agent): %w", err)
	}

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

// ApprovedFolder is one row from a node's security-tokens grant list.
type ApprovedFolder struct {
	TokenID    string
	FolderName string
}

// ListApprovedFolders parses the HTML grant table at
// /computer/<name>/security-tokens/ into (tokenId, folderName) pairs.
func ListApprovedFolders(ctx context.Context, client *api.Client, nodeName string) ([]ApprovedFolder, error) {
	html, err := client.GetHTML(ctx, "/computer/"+nodeSeg(nodeName)+"/security-tokens/")
	if err != nil {
		return nil, err
	}
	var out []ApprovedFolder
	for _, row := range strings.Split(html, "<tr") {
		tm := approvedTokenIDRe.FindStringSubmatch(row)
		fm := approvedFolderRe.FindStringSubmatch(row)
		if tm == nil || fm == nil {
			continue
		}
		folder, err := url.QueryUnescape(strings.ReplaceAll(fm[1], "/job/", "/"))
		if err != nil {
			folder = strings.ReplaceAll(fm[1], "/job/", "/")
		}
		out = append(out, ApprovedFolder{TokenID: tm[1], FolderName: strings.Trim(folder, "/")})
	}
	return out, nil
}

// CheckNodeApprovalForJob warns when a controlled-agent node has no approval
// covering jobPath. Returns "" when the node isn't controlled or the job is
// approved; otherwise a human-readable warning.
func CheckNodeApprovalForJob(ctx context.Context, client *api.Client, nodeName, jobPath string) (string, error) {
	xmlStr, err := GetNodeConfigXML(ctx, client, nodeName)
	if err != nil {
		return "", err
	}
	if !strings.Contains(xmlStr, controlledAgentPropTag) {
		return "", nil
	}
	folders, err := ListApprovedFolders(ctx, client, nodeName)
	if err != nil {
		return "", err
	}
	if len(folders) == 0 {
		return fmt.Sprintf("node %q is controlled but has no approved folders", nodeName), nil
	}
	for _, f := range folders {
		if f.FolderName != "" && strings.HasPrefix(jobPath, f.FolderName) {
			return "", nil
		}
	}
	return fmt.Sprintf("node %q is controlled but job %q is not in any approved folder", nodeName, jobPath), nil
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

	approvedTokenIDRe = regexp.MustCompile(`tokensById/([^/"'\s?#]+)/delete`)
	approvedFolderRe  = regexp.MustCompile(`href="[^"]*/job/([^"?#]+)/"[^>]*>`)
)
