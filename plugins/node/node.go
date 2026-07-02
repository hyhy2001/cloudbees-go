// Package node implements bee node commands.
package node

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/spf13/cobra"

	"bee/internal/api"
	"bee/internal/cache"
	"bee/internal/cli"
	"bee/internal/db"
	"bee/internal/session"
	"bee/plugins/controller"
)

const defaultJavaPath = "/usr/local/java/openjdk-19.0.2-7/bin/java"

// nodeDTO is the lightweight list view of a node.
type nodeDTO struct {
	DisplayName    string `json:"displayName"`
	Description    string `json:"description"`
	Offline        bool   `json:"offline"`
	NumExecutors   int    `json:"numExecutors"`
	AssignedLabels []struct {
		Name string `json:"name"`
	} `json:"assignedLabels"`
}

func nodeSeg(name string) string { return url.PathEscape(name) }

func getProfileName(database *sql.DB) string {
	name, _ := session.GetActiveProfileName(database)
	return name
}

// listNodes fetches all nodes from /computer/api/json.
func listNodes(database *sql.DB, client *api.Client) ([]nodeDTO, error) {
	var result struct {
		Computer []nodeDTO `json:"computer"`
	}
	tree := "computer[displayName,description,offline,numExecutors,assignedLabels[name]]"
	if err := client.GetJSONCached(nil, database, "/computer/api/json?tree="+url.QueryEscape(tree), "nodes.list", &result); err != nil {
		return nil, err
	}
	return result.Computer, nil
}

// getNodeOffline fetches the offline state of a single node.
func getNodeOffline(client *api.Client, name string) (bool, error) {
	var result struct {
		Offline bool `json:"offline"`
	}
	if err := client.GetJSON(nil, "/computer/"+nodeSeg(name)+"/api/json?tree=offline", &result); err != nil {
		return false, err
	}
	return result.Offline, nil
}

func nodeLabels(n nodeDTO) string {
	parts := make([]string, 0, len(n.AssignedLabels))
	for _, l := range n.AssignedLabels {
		if l.Name != "" && l.Name != n.DisplayName {
			parts = append(parts, l.Name)
		}
	}
	return strings.Join(parts, " ")
}

func xmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	return s
}

// buildNodeJSON builds the JSON payload for doCreateItem (form-encoded).
func buildNodeFormPayload(name, remoteDir string, numExecutors int, labels, desc, host string, port int, credID, javaPath, availability string, inDemandDelay, idleDelay int) string {
	type retentionDemand struct {
		StaplerClass string `json:"stapler-class"`
		Class        string `json:"$class"`
		InDemand     int    `json:"inDemandDelay"`
		Idle         int    `json:"idleDelay"`
	}
	type retentionAlways struct {
		StaplerClass string `json:"stapler-class"`
		Class        string `json:"$class"`
	}
	type sshVerify struct {
		StaplerClass string `json:"stapler-class"`
		Class        string `json:"$class"`
	}
	type sshLauncher struct {
		StaplerClass string    `json:"stapler-class"`
		Class        string    `json:"$class"`
		Host         string    `json:"host"`
		Port         int       `json:"port"`
		CredID       string    `json:"credentialsId"`
		JavaPath     string    `json:"javaPath"`
		Verify       sshVerify `json:"sshHostKeyVerificationStrategy"`
	}
	type jnlpLauncher struct {
		StaplerClass string `json:"stapler-class"`
		Class        string `json:"$class"`
	}

	nodeProps := map[string]interface{}{"stapler-class-bag": "true"}

	var payload map[string]interface{}
	if host != "" {
		launcher := sshLauncher{
			StaplerClass: "hudson.plugins.sshslaves.SSHLauncher",
			Class:        "hudson.plugins.sshslaves.SSHLauncher",
			Host:         host, Port: port, CredID: credID, JavaPath: javaPath,
			Verify: sshVerify{
				StaplerClass: "hudson.plugins.sshslaves.verifiers.NonVerifyingKeyVerificationStrategy",
				Class:        "hudson.plugins.sshslaves.verifiers.NonVerifyingKeyVerificationStrategy",
			},
		}
		if availability == "demand" {
			payload = map[string]interface{}{
				"name": name, "nodeDescription": desc, "numExecutors": fmt.Sprintf("%d", numExecutors),
				"remoteFS": remoteDir, "labelString": labels, "mode": "NORMAL",
				"type": "hudson.slaves.DumbSlave", "nodeProperties": nodeProps, "launcher": launcher,
				"retentionStrategy": retentionDemand{
					StaplerClass: "hudson.slaves.RetentionStrategy$Demand",
					Class:        "hudson.slaves.RetentionStrategy$Demand",
					InDemand:     inDemandDelay, Idle: idleDelay,
				},
			}
		} else {
			payload = map[string]interface{}{
				"name": name, "nodeDescription": desc, "numExecutors": fmt.Sprintf("%d", numExecutors),
				"remoteFS": remoteDir, "labelString": labels, "mode": "NORMAL",
				"type": "hudson.slaves.DumbSlave", "nodeProperties": nodeProps, "launcher": launcher,
				"retentionStrategy": retentionAlways{
					StaplerClass: "hudson.slaves.RetentionStrategy$Always",
					Class:        "hudson.slaves.RetentionStrategy$Always",
				},
			}
		}
	} else {
		launcher := jnlpLauncher{
			StaplerClass: "hudson.slaves.JNLPLauncher",
			Class:        "hudson.slaves.JNLPLauncher",
		}
		if availability == "demand" {
			payload = map[string]interface{}{
				"name": name, "nodeDescription": desc, "numExecutors": fmt.Sprintf("%d", numExecutors),
				"remoteFS": remoteDir, "labelString": labels, "mode": "NORMAL",
				"type": "hudson.slaves.DumbSlave", "nodeProperties": nodeProps, "launcher": launcher,
				"retentionStrategy": retentionDemand{
					StaplerClass: "hudson.slaves.RetentionStrategy$Demand",
					Class:        "hudson.slaves.RetentionStrategy$Demand",
					InDemand:     inDemandDelay, Idle: idleDelay,
				},
			}
		} else {
			payload = map[string]interface{}{
				"name": name, "nodeDescription": desc, "numExecutors": fmt.Sprintf("%d", numExecutors),
				"remoteFS": remoteDir, "labelString": labels, "mode": "NORMAL",
				"type": "hudson.slaves.DumbSlave", "nodeProperties": nodeProps, "launcher": launcher,
				"retentionStrategy": retentionAlways{
					StaplerClass: "hudson.slaves.RetentionStrategy$Always",
					Class:        "hudson.slaves.RetentionStrategy$Always",
				},
			}
		}
	}

	jsonBytes, _ := json.Marshal(payload)
	return "name=" + url.QueryEscape(name) +
		"&type=hudson.slaves.DumbSlave" +
		"&json=" + url.QueryEscape(string(jsonBytes))
}

// getNodeConfigXML delegates to the exported service function.
// ponytail: callers that don't have context pass nil; upgrade to context.Context when refactoring commands.

// setXMLElement replaces or inserts a simple XML element.
func setXMLElement(xmlStr, tag, value string) string {
	escaped := xmlEscape(value)
	open := "<" + tag + ">"
	close := "</" + tag + ">"
	if i := strings.Index(xmlStr, open); i >= 0 {
		if j := strings.Index(xmlStr[i:], close); j >= 0 {
			return xmlStr[:i+len(open)] + escaped + xmlStr[i+j:]
		}
	}
	// Insert before root closing tag
	candidates := []string{"</slave>", "</agent>", "</hudson.slaves.DumbSlave>"}
	for _, c := range candidates {
		if k := strings.LastIndex(xmlStr, c); k >= 0 {
			return xmlStr[:k] + "  <" + tag + ">" + escaped + "</" + tag + ">\n" + xmlStr[k:]
		}
	}
	return xmlStr
}

func buildLauncherXML(launcherType, host string, port int, credID, javaPath string) string {
	if launcherType == "ssh" {
		switch {
		case port == 0:
			port = 22
		case port < 1:
			port = 1
		case port > 65535:
			port = 65535
		}
		return `  <launcher class="hudson.plugins.sshslaves.SSHLauncher" plugin="ssh-slaves">` + "\n" +
			`    <host>` + xmlEscape(host) + `</host>` + "\n" +
			`    <port>` + fmt.Sprintf("%d", port) + `</port>` + "\n" +
			`    <credentialsId>` + xmlEscape(credID) + `</credentialsId>` + "\n" +
			`    <javaPath>` + xmlEscape(javaPath) + `</javaPath>` + "\n" +
			`    <sshHostKeyVerificationStrategy class="hudson.plugins.sshslaves.verifiers.NonVerifyingKeyVerificationStrategy"/>` + "\n" +
			`  </launcher>`
	}
	return `  <launcher class="hudson.slaves.JNLPLauncher">` + "\n" +
		`    <workDirSettings>` + "\n" +
		`      <disabled>false</disabled>` + "\n" +
		`      <internalDir>remoting</internalDir>` + "\n" +
		`      <failIfWorkDirIsMissing>false</failIfWorkDirIsMissing>` + "\n" +
		`    </workDirSettings>` + "\n" +
		`  </launcher>`
}

func buildRetentionXML(availability string, inDemandDelay, idleDelay int) string {
	if availability == "demand" {
		return `  <retentionStrategy class="hudson.slaves.RetentionStrategy$Demand">` + "\n" +
			`    <inDemandDelay>` + fmt.Sprintf("%d", inDemandDelay) + `</inDemandDelay>` + "\n" +
			`    <idleDelay>` + fmt.Sprintf("%d", idleDelay) + `</idleDelay>` + "\n" +
			`  </retentionStrategy>`
	}
	return `  <retentionStrategy class="hudson.slaves.RetentionStrategy$Always"/>`
}

// swapXMLSubtree replaces a whole <tag ...>...</tag> or <tag .../> block.
func swapXMLSubtree(xmlStr, tag, block string) string {
	// Try paired form first
	openRe := "<" + tag
	closeTag := "</" + tag + ">"
	if i := strings.Index(xmlStr, openRe); i >= 0 {
		// find end of opening tag
		j := strings.Index(xmlStr[i:], ">")
		if j >= 0 {
			if xmlStr[i+j-1] == '/' {
				// self-closing
				return xmlStr[:i] + block + xmlStr[i+j+1:]
			}
			// paired — find closing tag
			if k := strings.Index(xmlStr[i:], closeTag); k >= 0 {
				return xmlStr[:i] + block + xmlStr[i+k+len(closeTag):]
			}
		}
	}
	// Insert before root close
	candidates := []string{"</slave>", "</agent>", "</hudson.slaves.DumbSlave>"}
	for _, c := range candidates {
		if k := strings.LastIndex(xmlStr, c); k >= 0 {
			return xmlStr[:k] + block + "\n" + xmlStr[k:]
		}
	}
	return xmlStr
}

// parseNodeConfig extracts launcher and retention from config.xml for display.
type nodeConfig struct {
	launcherType    string
	host            string
	port            int
	credID          string
	javaPath        string
	availability    string
	inDemandDelay   int
	idleDelay       int
	remoteDir       string
	controlledAgent bool
}

func parseNodeConfigXML(xmlStr string) nodeConfig {
	cfg := nodeConfig{launcherType: "jnlp", port: 22, availability: "always", idleDelay: 1}

	// Launcher type
	if strings.Contains(xmlStr, `SSHLauncher`) {
		cfg.launcherType = "ssh"
	}
	// Retention
	if strings.Contains(xmlStr, `RetentionStrategy$Demand`) {
		cfg.availability = "demand"
	}
	cfg.controlledAgent = strings.Contains(xmlStr, controlledAgentPropTag)

	// Simple text extractions
	extractTag := func(tag string) string {
		open := "<" + tag + ">"
		close := "</" + tag + ">"
		if i := strings.Index(xmlStr, open); i >= 0 {
			start := i + len(open)
			if j := strings.Index(xmlStr[start:], close); j >= 0 {
				return strings.TrimSpace(xmlStr[start : start+j])
			}
		}
		return ""
	}
	cfg.remoteDir = extractTag("remoteFS")
	cfg.host = extractTag("host")
	cfg.credID = extractTag("credentialsId")
	cfg.javaPath = extractTag("javaPath")

	if v := extractTag("port"); v != "" {
		fmt.Sscanf(v, "%d", &cfg.port)
	}
	if v := extractTag("inDemandDelay"); v != "" {
		fmt.Sscanf(v, "%d", &cfg.inDemandDelay)
	}
	if v := extractTag("idleDelay"); v != "" {
		fmt.Sscanf(v, "%d", &cfg.idleDelay)
	}
	return cfg
}

func nodeListCmd(database *sql.DB, dbPath string) *cobra.Command {
	var flagAll bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List build agents with online/offline status",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := controller.GetActiveControllerClient(database, dbPath)
			if err != nil {
				return err
			}
			allNodes, err := listNodes(database, client)
			if err != nil {
				return err
			}
			profile := getProfileName(database)
			var nodes []nodeDTO
			if flagAll {
				nodes = allNodes
			} else {
				tracked, _ := db.ListTracked(database, "node", profile, client.BaseURL)
				trackedSet := map[string]bool{}
				for _, n := range tracked {
					trackedSet[n] = true
				}
				serverNames := map[string]bool{}
				for _, n := range allNodes {
					serverNames[n.DisplayName] = true
				}
				for _, n := range allNodes {
					if trackedSet[n.DisplayName] {
						nodes = append(nodes, n)
					}
				}
				for name := range trackedSet {
					if !serverNames[name] {
						nodes = append(nodes, nodeDTO{
							DisplayName: name,
							Offline:     true,
							Description: "[DELETED_ON_SERVER]",
						})
					}
				}
			}

			rows := make([][]string, len(nodes))
			for i, n := range nodes {
				status := "ONLINE"
				if n.Offline {
					status = "OFFLINE"
				}
				labels := nodeLabels(n)
				if len(labels) > 20 {
					labels = labels[:20]
				}
				name := n.DisplayName
				if len(name) > 28 {
					name = name[:28]
				}
				desc := n.Description
				if len(desc) > 25 {
					desc = desc[:25]
				}
				rows[i] = []string{name, status, fmt.Sprintf("%d", n.NumExecutors), labels, desc}
			}
			cli.Table([]string{"Name", "Status", "Executors", "Labels", "Description"}, rows)
			fmt.Printf("  %d node(s)\n", len(nodes))
			return nil
		},
	}
	cmd.Flags().BoolVar(&flagAll, "all", false, "Show all nodes (default: only yours)")
	return cmd
}

func nodeGetCmd(database *sql.DB, dbPath string) *cobra.Command {
	return &cobra.Command{
		Use:   "get <name>",
		Short: "Show node details: status, executors, labels, launcher type, remote dir",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := controller.GetActiveControllerClient(database, dbPath)
			if err != nil {
				return err
			}
			name := args[0]
			var detail struct {
				DisplayName    string `json:"displayName"`
				Offline        bool   `json:"offline"`
				NumExecutors   int    `json:"numExecutors"`
				Description    string `json:"description"`
				AssignedLabels []struct {
					Name string `json:"name"`
				} `json:"assignedLabels"`
			}
			if err := client.GetJSON(cmd.Context(), "/computer/"+nodeSeg(name)+"/api/json", &detail); err != nil {
				return err
			}

			var cfg *nodeConfig
			if xmlStr, err := GetNodeConfigXML(cmd.Context(), client, name); err == nil {
				c := parseNodeConfigXML(xmlStr)
				cfg = &c
			}

			labels := make([]string, 0)
			for _, l := range detail.AssignedLabels {
				if l.Name != "" && l.Name != detail.DisplayName {
					labels = append(labels, l.Name)
				}
			}

			pairs := [][]string{
				{"name", detail.DisplayName},
				{"offline", fmt.Sprintf("%v", detail.Offline)},
				{"executors", fmt.Sprintf("%d", detail.NumExecutors)},
				{"labels", strings.Join(labels, " ")},
				{"description", detail.Description},
			}
			if cfg != nil {
				pairs = append(pairs, []string{"launcher", cfg.launcherType})
				pairs = append(pairs, []string{"remote_dir", cfg.remoteDir})
				if cfg.launcherType == "ssh" {
					pairs = append(pairs, []string{"ssh_host", cfg.host})
					pairs = append(pairs, []string{"ssh_port", fmt.Sprintf("%d", cfg.port)})
					pairs = append(pairs, []string{"cred_id", cfg.credID})
					pairs = append(pairs, []string{"java_path", cfg.javaPath})
				}
				pairs = append(pairs, []string{"availability", cfg.availability})
				if cfg.availability == "demand" {
					pairs = append(pairs, []string{"in_demand_delay", fmt.Sprintf("%dm", cfg.inDemandDelay)})
					pairs = append(pairs, []string{"idle_delay", fmt.Sprintf("%dm", cfg.idleDelay)})
				}
				pairs = append(pairs, []string{"controlled_agent", fmt.Sprintf("%v", cfg.controlledAgent)})
			}
			cli.KV(pairs)
			return nil
		},
	}
}

func nodeCreateCmd(database *sql.DB, dbPath string) *cobra.Command {
	var (
		flagRemoteDir string
		flagExecutors int
		flagLabels    string
		flagDesc      string
		flagHost      string
		flagPort      int
		flagCredID    string
		flagJavaPath  string
		flagAvail     string
		flagInDemand  int
		flagIdleDelay int
	)
	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Add a new Permanent Agent — SSH (--host + --cred-id) or JNLP (no --host)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := controller.GetActiveControllerClient(database, dbPath)
			if err != nil {
				return err
			}
			name := args[0]
			// Pre-check: node already exists?
			offline, err := getNodeOffline(client, name)
			if err == nil {
				_ = offline
				cli.Info(fmt.Sprintf("Node '%s' already exists.", name))
				return nil
			}

			body := buildNodeFormPayload(name, flagRemoteDir, flagExecutors, flagLabels, flagDesc,
				flagHost, flagPort, flagCredID, flagJavaPath, flagAvail, flagInDemand, flagIdleDelay)
			resp, err := client.Do(cmd.Context(), "POST", "/computer/doCreateItem", strings.NewReader(body), "application/x-www-form-urlencoded")
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			if resp.StatusCode >= 300 && resp.StatusCode != 302 {
				b, _ := io.ReadAll(resp.Body)
				return fmt.Errorf("create node: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
			}

			if flagAvail != "demand" && flagAvail != "always" {
				cli.Warn(fmt.Sprintf("Unknown --availability '%s'; defaulted to 'always'. Valid: always | demand", flagAvail))
			}

			_ = cache.InvalidateResource(database, "node")
			profile := getProfileName(database)
			_ = db.TrackResource(database, "node", name, profile, client.BaseURL)
			cli.Success(fmt.Sprintf("Node '%s' created.", name))
			fmt.Printf("  Link: %s/computer/%s/\n", strings.TrimRight(client.BaseURL, "/"), name)
			if flagHost != "" {
				fmt.Printf("  SSH Node will auto-connect to %s:%d using cred: '%s'\n", flagHost, flagPort, flagCredID)
				if flagCredID == "" {
					cli.Warn("No SSH credential set — ensure key-based auth is configured on the agent.")
				}
			} else {
				fmt.Printf("  Connect it via: Manage Jenkins -> Nodes -> %s -> Agent command\n", name)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&flagRemoteDir, "remote-dir", "", "Remote working directory on agent")
	_ = cmd.MarkFlagRequired("remote-dir")
	cmd.Flags().IntVar(&flagExecutors, "executors", 1, "Number of executors")
	cmd.Flags().StringVar(&flagLabels, "labels", "", "Space-separated node labels")
	cmd.Flags().StringVar(&flagDesc, "description", "", "Human-readable description")
	cmd.Flags().StringVar(&flagHost, "host", "", "SSH host — omit for JNLP/Inbound agent")
	cmd.Flags().IntVar(&flagPort, "port", 22, "SSH port")
	cmd.Flags().StringVar(&flagCredID, "cred-id", "", "Credential ID for SSH")
	cmd.Flags().StringVar(&flagJavaPath, "java-path", defaultJavaPath, "Path to Java on agent")
	cmd.Flags().StringVar(&flagAvail, "availability", "always", "Retention strategy: always | demand")
	cmd.Flags().IntVar(&flagInDemand, "in-demand-delay", 0, "Minutes before bringing online (demand only)")
	cmd.Flags().IntVar(&flagIdleDelay, "idle-delay", 1, "Minutes idle before going offline (demand only)")
	return cmd
}

func nodeCopyCmd(database *sql.DB, dbPath string) *cobra.Command {
	return &cobra.Command{
		Use:   "copy <source_name> <new_name>",
		Short: "Clone an existing node's configuration into a new node",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := controller.GetActiveControllerClient(database, dbPath)
			if err != nil {
				return err
			}
			srcName, newName := args[0], args[1]
			body := "name=" + url.QueryEscape(newName) + "&mode=copy&from=" + url.QueryEscape(srcName)
			resp, err := client.Do(cmd.Context(), "POST", "/computer/doCreateItem", strings.NewReader(body), "application/x-www-form-urlencoded")
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			if resp.StatusCode >= 300 && resp.StatusCode != 302 {
				b, _ := io.ReadAll(resp.Body)
				return fmt.Errorf("copy node: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
			}
			_ = cache.InvalidateResource(database, "node")
			profile := getProfileName(database)
			_ = db.TrackResource(database, "node", newName, profile, client.BaseURL)
			cli.Success(fmt.Sprintf("Node '%s' created (copied from '%s').", newName, srcName))
			return nil
		},
	}
}

func nodeDeleteCmd(database *sql.DB, dbPath string) *cobra.Command {
	var flagYes bool
	cmd := &cobra.Command{
		Use:   "delete <name> [name...]",
		Short: "Delete (decommission) one or more nodes permanently",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !flagYes {
				label := fmt.Sprintf("node '%s'", args[0])
				if len(args) > 1 {
					label = fmt.Sprintf("%d nodes", len(args))
				}
				fmt.Printf("Delete %s? [y/N] ", label)
				var answer string
				fmt.Scanln(&answer)
				if strings.ToLower(strings.TrimSpace(answer)) != "y" {
					cli.Info("Cancelled.")
					return nil
				}
			}
			client, err := controller.GetActiveControllerClient(database, dbPath)
			if err != nil {
				return err
			}
			profile := getProfileName(database)
			for _, name := range args {
				if err := client.PostForm(cmd.Context(), "/computer/"+nodeSeg(name)+"/doDelete", map[string]string{}); err != nil {
					cli.Error(fmt.Sprintf("Failed to delete '%s': %s", name, err))
					continue
				}
				_ = db.UntrackResource(database, "node", name, profile, client.BaseURL)
				cli.Success(fmt.Sprintf("Node '%s' deleted.", name))
			}
			_ = cache.InvalidateResource(database, "node")
			return nil
		},
	}
	cmd.Flags().BoolVar(&flagYes, "yes", false, "Skip confirmation")
	return cmd
}

func nodeOfflineCmd(database *sql.DB, dbPath string) *cobra.Command {
	var flagReason string
	cmd := &cobra.Command{
		Use:   "offline <name>",
		Short: "Take a node offline (maintenance mode)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := controller.GetActiveControllerClient(database, dbPath)
			if err != nil {
				return err
			}
			name := args[0]
			offline, err := getNodeOffline(client, name)
			if err != nil {
				return err
			}
			if offline {
				cli.Info(fmt.Sprintf("Node '%s' is already offline.", name))
				return nil
			}
			path := "/computer/" + nodeSeg(name) + "/toggleOffline?offlineMessage=" + url.QueryEscape(flagReason)
			if err := client.PostForm(cmd.Context(), path, map[string]string{}); err != nil {
				return err
			}
			cli.Success(fmt.Sprintf("Node '%s' marked offline.", name))
			return nil
		},
	}
	cmd.Flags().StringVar(&flagReason, "reason", "", "Reason for taking offline")
	return cmd
}

func nodeOnlineCmd(database *sql.DB, dbPath string) *cobra.Command {
	return &cobra.Command{
		Use:   "online <name>",
		Short: "Bring a node back online",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := controller.GetActiveControllerClient(database, dbPath)
			if err != nil {
				return err
			}
			name := args[0]
			offline, err := getNodeOffline(client, name)
			if err != nil {
				return err
			}
			if !offline {
				cli.Info(fmt.Sprintf("Node '%s' is already online.", name))
				return nil
			}
			path := "/computer/" + nodeSeg(name) + "/toggleOffline?offlineMessage="
			if err := client.PostForm(cmd.Context(), path, map[string]string{}); err != nil {
				return err
			}
			cli.Success(fmt.Sprintf("Node '%s' brought online.", name))
			return nil
		},
	}
}

func nodeUpdateCmd(database *sql.DB, dbPath string) *cobra.Command {
	var (
		flagDesc       string
		flagRemoteDir  string
		flagExecutors  int
		flagLabels     string
		flagLauncher   string
		flagHost       string
		flagPort       int
		flagCredID     string
		flagJavaPath   string
		flagAvail      string
		flagInDemand   int
		flagIdleDelay  int
		flagControlled string
	)
	cmd := &cobra.Command{
		Use:   "update <name>",
		Short: "Edit a node's configuration: executors, labels, launcher, SSH host, remote dir, availability",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := controller.GetActiveControllerClient(database, dbPath)
			if err != nil {
				return err
			}
			name := args[0]
			xmlStr, err := GetNodeConfigXML(cmd.Context(), client, name)
			if err != nil {
				return err
			}

			if cmd.Flags().Changed("description") {
				xmlStr = setXMLElement(xmlStr, "description", flagDesc)
			}
			if cmd.Flags().Changed("remote-dir") {
				xmlStr = setXMLElement(xmlStr, "remoteFS", flagRemoteDir)
			}
			if cmd.Flags().Changed("executors") {
				xmlStr = setXMLElement(xmlStr, "numExecutors", fmt.Sprintf("%d", flagExecutors))
			}
			if cmd.Flags().Changed("labels") {
				xmlStr = setXMLElement(xmlStr, "label", flagLabels)
			}

			// Launcher / retention use whole-subtree swap
			current := parseNodeConfigXML(xmlStr)
			launcherTouched := cmd.Flags().Changed("launcher") || cmd.Flags().Changed("host") ||
				cmd.Flags().Changed("port") || cmd.Flags().Changed("cred-id") || cmd.Flags().Changed("java-path")

			if launcherTouched {
				lt := current.launcherType
				if cmd.Flags().Changed("launcher") {
					if flagLauncher == "ssh" || flagLauncher == "jnlp" {
						lt = flagLauncher
					} else {
						cli.Warn(fmt.Sprintf("Unknown --launcher '%s'; ignored. Valid: ssh | jnlp", flagLauncher))
					}
				}
				h := current.host
				if cmd.Flags().Changed("host") {
					h = flagHost
				}
				p := current.port
				if cmd.Flags().Changed("port") {
					p = flagPort
				}
				cred := current.credID
				if cmd.Flags().Changed("cred-id") {
					cred = flagCredID
				}
				jp := current.javaPath
				if cmd.Flags().Changed("java-path") {
					jp = flagJavaPath
				}
				block := buildLauncherXML(lt, h, p, cred, jp)
				xmlStr = swapXMLSubtree(xmlStr, "launcher", block)
			}

			retentionTouched := cmd.Flags().Changed("availability") || cmd.Flags().Changed("in-demand-delay") || cmd.Flags().Changed("idle-delay")
			if retentionTouched {
				avail := current.availability
				if cmd.Flags().Changed("availability") {
					if flagAvail == "always" || flagAvail == "demand" {
						avail = flagAvail
					} else {
						cli.Warn(fmt.Sprintf("Unknown --availability '%s'; ignored. Valid: always | demand", flagAvail))
					}
				}
				ind := current.inDemandDelay
				if cmd.Flags().Changed("in-demand-delay") {
					ind = flagInDemand
				}
				idle := current.idleDelay
				if cmd.Flags().Changed("idle-delay") {
					idle = flagIdleDelay
				}
				block := buildRetentionXML(avail, ind, idle)
				xmlStr = swapXMLSubtree(xmlStr, "retentionStrategy", block)
			}

			if cmd.Flags().Changed("controlled-agent") {
				xmlStr = setControlledAgentXML(xmlStr, flagControlled == "true")
			}

			if err := client.PostXML(cmd.Context(), "/computer/"+nodeSeg(name)+"/config.xml", xmlStr); err != nil {
				return err
			}
			_ = cache.InvalidateResource(database, "node")
			cli.Success(fmt.Sprintf("Node '%s' updated.", name))
			if cmd.Flags().Changed("launcher") && flagLauncher == "ssh" && cmd.Flags().Changed("cred-id") && flagCredID == "" {
				cli.Warn("SSH launcher with no credential set — ensure key-based auth is configured.")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&flagDesc, "description", "", "Human-readable description")
	cmd.Flags().StringVar(&flagRemoteDir, "remote-dir", "", "Remote working directory")
	cmd.Flags().IntVar(&flagExecutors, "executors", 0, "Number of executors")
	cmd.Flags().StringVar(&flagLabels, "labels", "", "Space-separated node labels")
	cmd.Flags().StringVar(&flagLauncher, "launcher", "", "Launch method: ssh | jnlp")
	cmd.Flags().StringVar(&flagHost, "host", "", "SSH host")
	cmd.Flags().IntVar(&flagPort, "port", 22, "SSH port")
	cmd.Flags().StringVar(&flagCredID, "cred-id", "", "Credential ID for SSH")
	cmd.Flags().StringVar(&flagJavaPath, "java-path", "", "Path to Java on agent")
	cmd.Flags().StringVar(&flagAvail, "availability", "", "Retention strategy: always | demand")
	cmd.Flags().IntVar(&flagInDemand, "in-demand-delay", 0, "Minutes before bringing online")
	cmd.Flags().IntVar(&flagIdleDelay, "idle-delay", 1, "Minutes idle before going offline")
	cmd.Flags().StringVar(&flagControlled, "controlled-agent", "", "Enable (true) or disable (false) controlled-agent mode")
	return cmd
}

func nodeTrackCmd(database *sql.DB, dbPath string) *cobra.Command {
	return &cobra.Command{
		Use:   "track <name> [name...]",
		Short: "Start tracking an existing node — pin it to your Mine",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := controller.GetActiveControllerClient(database, dbPath)
			if err != nil {
				return err
			}
			profile := getProfileName(database)
			tracked, _ := db.ListTracked(database, "node", profile, client.BaseURL)
			trackedSet := map[string]bool{}
			for _, n := range tracked {
				trackedSet[n] = true
			}
			for _, name := range args {
				if _, err := getNodeOffline(client, name); err != nil {
					if strings.Contains(err.Error(), "404") {
						cli.Error(fmt.Sprintf("Node '%s' not found on server. Skipping.", name))
					} else {
						cli.Error(fmt.Sprintf("Could not verify node '%s': %s", name, err))
					}
					continue
				}
				if trackedSet[name] {
					cli.Info(fmt.Sprintf("Node '%s' is already tracked.", name))
					continue
				}
				_ = db.TrackResource(database, "node", name, profile, client.BaseURL)
				trackedSet[name] = true
				cli.Success(fmt.Sprintf("Tracked node '%s'.", name))
			}
			return nil
		},
	}
}

func nodeUntrackCmd(database *sql.DB, dbPath string) *cobra.Command {
	return &cobra.Command{
		Use:   "untrack <name> [name...]",
		Short: "Remove nodes from your Mine (does not delete from server)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := controller.GetActiveControllerClient(database, dbPath)
			if err != nil {
				return err
			}
			profile := getProfileName(database)
			tracked, _ := db.ListTracked(database, "node", profile, client.BaseURL)
			trackedSet := map[string]bool{}
			for _, n := range tracked {
				trackedSet[n] = true
			}
			for _, name := range args {
				if !trackedSet[name] {
					cli.Info(fmt.Sprintf("Node '%s' is not in Mine.", name))
					continue
				}
				_ = db.UntrackResource(database, "node", name, profile, client.BaseURL)
				delete(trackedSet, name)
				cli.Success(fmt.Sprintf("Removed node '%s' from Mine.", name))
			}
			return nil
		},
	}
}

// Register wires up the node command group.
func Register(root *cobra.Command, database *sql.DB, dbPath string) {
	grp := &cobra.Command{
		Use:   "node",
		Short: "Manage CloudBees build agents, workers, and executor nodes",
	}
	grp.AddCommand(
		nodeListCmd(database, dbPath),
		nodeGetCmd(database, dbPath),
		nodeCreateCmd(database, dbPath),
		nodeCopyCmd(database, dbPath),
		nodeDeleteCmd(database, dbPath),
		nodeOfflineCmd(database, dbPath),
		nodeOnlineCmd(database, dbPath),
		nodeUpdateCmd(database, dbPath),
		nodeTrackCmd(database, dbPath),
		nodeUntrackCmd(database, dbPath),
	)
	root.AddCommand(grp)
}
