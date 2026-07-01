// Package job implements bee job commands.
package job

import (
	"database/sql"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/hyhy2001/bee/internal/api"
	"github.com/hyhy2001/bee/internal/cli"
	"github.com/hyhy2001/bee/internal/db"
	"github.com/hyhy2001/bee/internal/session"
	"github.com/hyhy2001/bee/plugins/controller"
)

type controlledAgentGrant struct {
	AgentName string
	GrantID   string
}

func getProfileName(database *sql.DB) string {
	name, _ := session.GetActiveProfileName(database)
	return name
}

func getText(client *api.Client, path string) (string, error) {
	resp, err := client.Do(nil, "GET", path, nil, "")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("GET %s: HTTP %d: %s", path, resp.StatusCode, strings.TrimSpace(string(b)))
	}
	b, err := io.ReadAll(resp.Body)
	return string(b), err
}

func xmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	return s
}

// buildEmailPublisherXML builds the email-ext publisher block.
func buildEmailPublisherXML(email, emailCond string, keywords []string, emailRegex string) string {
	if email == "" {
		return ""
	}
	var triggers []string
	switch emailCond {
	case "failed", "always", "custom", "":
		triggers = append(triggers, `      <hudson.plugins.emailext.plugins.trigger.FailureTrigger><email><defaultSubject>$DEFAULT_SUBJECT</defaultSubject><defaultContent>$DEFAULT_CONTENT</defaultContent><attachmentsPattern></attachmentsPattern><attachBuildLog>false</attachBuildLog><compressBuildLog>false</compressBuildLog><replyTo>$DEFAULT_REPLYTO</replyTo><contentType>default</contentType></email></hudson.plugins.emailext.plugins.trigger.FailureTrigger>`)
	}
	if emailCond == "success" || emailCond == "always" || emailCond == "custom" {
		triggers = append(triggers, `      <hudson.plugins.emailext.plugins.trigger.SuccessTrigger><email><defaultSubject>$DEFAULT_SUBJECT</defaultSubject><defaultContent>$DEFAULT_CONTENT</defaultContent><attachmentsPattern></attachmentsPattern><attachBuildLog>false</attachBuildLog><compressBuildLog>false</compressBuildLog><replyTo>$DEFAULT_REPLYTO</replyTo><contentType>default</contentType></email></hudson.plugins.emailext.plugins.trigger.SuccessTrigger>`)
	}
	presend := "$DEFAULT_PRESEND_SCRIPT"
	if len(keywords) > 0 || emailRegex != "" {
		presend = buildEmailPresendScript(keywords, emailRegex)
	}
	return `    <hudson.plugins.emailext.ExtendedEmailPublisher plugin="email-ext">` +
		`<recipientList>` + xmlEscape(email) + `</recipientList>` +
		`<configuredTriggers>` + strings.Join(triggers, "") + `</configuredTriggers>` +
		`<contentType>default</contentType>` +
		`<defaultSubject>$DEFAULT_SUBJECT</defaultSubject>` +
		`<defaultContent>$DEFAULT_CONTENT</defaultContent>` +
		`<attachmentsPattern></attachmentsPattern>` +
		`<presendScript>` + xmlEscape(presend) + `</presendScript>` +
		`<postsendScript>$DEFAULT_POSTSEND_SCRIPT</postsendScript>` +
		`<attachBuildLog>false</attachBuildLog>` +
		`<compressBuildLog>false</compressBuildLog>` +
		`<replyTo>$DEFAULT_REPLYTO</replyTo>` +
		`<from></from>` +
		`<saveOutput>false</saveOutput>` +
		`<disabled>false</disabled>` +
		`</hudson.plugins.emailext.ExtendedEmailPublisher>`
}

// buildEmailPresendScript generates the Groovy presend script for keyword/regex filtering.
func buildEmailPresendScript(keywords []string, emailRegex string) string {
	kwLiterals := make([]string, len(keywords))
	for i, k := range keywords {
		kwLiterals[i] = `"` + strings.ReplaceAll(k, `"`, `\"`) + `"`
	}
	regexLit := "null"
	if emailRegex != "" {
		regexLit = `"` + strings.ReplaceAll(emailRegex, `"`, `\"`) + `"`
	}
	return strings.Join([]string{
		`def _bee_raw = ''`,
		`try { if (binding?.hasVariable('build') && build != null) { _bee_raw = build.getLog(Integer.MAX_VALUE).join('\n') ?: '' } } catch (Throwable _bee_ignore) {}`,
		`def _bee_keywords = [` + strings.Join(kwLiterals, ", ") + `]`,
		`def _bee_regex = ` + regexLit,
		`def _bee_kw_match = _bee_keywords.any { _bee_raw.toLowerCase().contains(it.toLowerCase()) }`,
		`def _bee_regex_match = _bee_regex != null && (_bee_raw ==~ ('(?is).*(' + _bee_regex + ').*'))`,
		`def _bee_has_kw = !_bee_keywords.isEmpty()`,
		`def _bee_has_rx = (_bee_regex != null)`,
		`if ((_bee_has_kw && !_bee_kw_match) || (_bee_has_rx && !_bee_regex_match)) { cancel = true }`,
	}, "\n")
}

// buildParametersPropertyXML builds the <properties> block for build parameters.
// paramDefs is a slice of "NAME" or "NAME=default" strings.
func buildParametersPropertyXML(paramDefs []string) string {
	if len(paramDefs) == 0 {
		return `  <properties/>`
	}
	var params []string
	for _, pd := range paramDefs {
		name, def, _ := strings.Cut(pd, "=")
		params = append(params, `      <hudson.model.StringParameterDefinition>`+
			`<name>`+xmlEscape(strings.TrimSpace(name))+`</name>`+
			`<defaultValue>`+xmlEscape(def)+`</defaultValue>`+
			`<trim>false</trim>`+
			`</hudson.model.StringParameterDefinition>`)
	}
	return `  <properties><hudson.model.ParametersDefinitionProperty>` +
		`<parameterDefinitions>` + strings.Join(params, "") + `</parameterDefinitions>` +
		`</hudson.model.ParametersDefinitionProperty></properties>`
}

// buildFreestyleXML builds a Freestyle config.xml with optional email and params.
func buildFreestyleXML(desc, shellCmd, node, schedule, email, emailCond string, emailKeywords []string, emailRegex string, paramDefs []string) string {
	var sb strings.Builder
	sb.WriteString(`<?xml version='1.1' encoding='UTF-8'?><project>`)
	sb.WriteString(`<description>` + xmlEscape(desc) + `</description>`)
	if node != "" {
		sb.WriteString(`<assignedNode>` + xmlEscape(node) + `</assignedNode><canRoam>false</canRoam>`)
	} else {
		sb.WriteString(`<canRoam>true</canRoam>`)
	}
	sb.WriteString(`<disabled>false</disabled>`)
	sb.WriteString(`<blockBuildWhenDownstreamBuilding>false</blockBuildWhenDownstreamBuilding>`)
	sb.WriteString(`<blockBuildWhenUpstreamBuilding>false</blockBuildWhenUpstreamBuilding>`)
	if schedule != "" {
		sb.WriteString(`<triggers><hudson.triggers.TimerTrigger><spec>` + xmlEscape(schedule) + `</spec></hudson.triggers.TimerTrigger></triggers>`)
	} else {
		sb.WriteString(`<triggers/>`)
	}
	sb.WriteString(`<concurrentBuild>false</concurrentBuild>`)
	sb.WriteString(buildParametersPropertyXML(paramDefs))
	sb.WriteString(`<builders><hudson.tasks.Shell><command>` + xmlEscape(shellCmd) + `</command></hudson.tasks.Shell></builders>`)
	emailXML := buildEmailPublisherXML(email, emailCond, emailKeywords, emailRegex)
	if emailXML != "" {
		sb.WriteString(`<publishers>` + emailXML + `</publishers>`)
	} else {
		sb.WriteString(`<publishers/>`)
	}
	sb.WriteString(`<buildWrappers/></project>`)
	return sb.String()
}

// buildPipelineXML builds a Pipeline config.xml with optional email and params.
func buildPipelineXML(desc, script, schedule, email, emailCond string, emailKeywords []string, emailRegex string, paramDefs []string) string {
	var sb strings.Builder
	sb.WriteString(`<?xml version='1.1' encoding='UTF-8'?><flow-definition plugin="workflow-job">`)
	sb.WriteString(`<description>` + xmlEscape(desc) + `</description>`)
	sb.WriteString(`<keepDependencies>false</keepDependencies>`)
	sb.WriteString(buildParametersPropertyXML(paramDefs))
	if schedule != "" {
		sb.WriteString(`<triggers><hudson.triggers.TimerTrigger><spec>` + xmlEscape(schedule) + `</spec></hudson.triggers.TimerTrigger></triggers>`)
	} else {
		sb.WriteString(`<triggers/>`)
	}
	sb.WriteString(`<definition class="org.jenkinsci.plugins.workflow.cps.CpsFlowDefinition" plugin="workflow-cps">`)
	sb.WriteString(`<script>` + xmlEscape(script) + `</script><sandbox>true</sandbox></definition>`)
	emailXML := buildEmailPublisherXML(email, emailCond, emailKeywords, emailRegex)
	if emailXML != "" {
		sb.WriteString(`<publishers>` + emailXML + `</publishers>`)
	} else {
		sb.WriteString(`<publishers/>`)
	}
	sb.WriteString(`<disabled>false</disabled></flow-definition>`)
	return sb.String()
}

// buildFolderXML builds a minimal Folder config.xml.
func buildFolderXML(desc string) string {
	return `<?xml version='1.1' encoding='UTF-8'?>` +
		`<com.cloudbees.hudson.plugins.folder.Folder>` +
		`<description>` + xmlEscape(desc) + `</description>` +
		`<views><hudson.model.AllView><owner reference="../../.."/><name>All</name><filterExecutors>false</filterExecutors><filterQueue>false</filterQueue></hudson.model.AllView></views>` +
		`<viewsTabBar class="hudson.views.DefaultViewsTabBar"/>` +
		`<healthMetrics/>` +
		`</com.cloudbees.hudson.plugins.folder.Folder>`
}

// listControlledAgents parses the HTML at /job/{folder}/controlled-slaves/.
func listControlledAgents(client *api.Client, folderName string) ([]controlledAgentGrant, error) {
	parts := strings.Split(folderName, "/")
	escaped := make([]string, len(parts))
	for i, p := range parts {
		escaped[i] = "job/" + url.PathEscape(p)
	}
	folderPath := strings.Join(escaped, "/")

	html, err := getText(client, "/"+folderPath+"/controlled-slaves/")
	if err != nil {
		return nil, nil // plugin not installed = empty
	}

	var grants []controlledAgentGrant
	// Find each delete link containing grantId
	search := html
	for {
		i := strings.Index(search, `grantsById/`)
		if i < 0 {
			break
		}
		rest := search[i+len(`grantsById/`):]
		j := strings.IndexAny(rest, `/"`)
		if j < 0 {
			break
		}
		grantID := rest[:j]
		// Look backwards for agent name in /computer/{name}/
		before := search[:i]
		agentName := ""
		if k := strings.LastIndex(before, "/computer/"); k >= 0 {
			after := before[k+len("/computer/"):]
			if end := strings.IndexByte(after, '/'); end >= 0 {
				agentName, _ = url.PathUnescape(after[:end])
			}
		}
		grants = append(grants, controlledAgentGrant{AgentName: agentName, GrantID: grantID})
		search = rest[j:]
	}
	return grants, nil
}

// Register wires up the job command group.
func Register(root *cobra.Command, database *sql.DB, dbPath string) {
	grp := &cobra.Command{
		Use:   "job",
		Short: "Manage CloudBees jobs (Freestyle, Pipelines, Folders) and their builds",
	}
	grp.AddCommand(
		jobListCmd(database, dbPath),
		jobGetCmd(database, dbPath),
		jobCreateCmd(database, dbPath),
		jobDeleteCmd(database, dbPath),
		jobCopyCmd(database, dbPath),
		jobMoveCmd(database, dbPath),
		jobTrackCmd(database, dbPath),
		jobUntrackCmd(database, dbPath),
		jobRunCmd(database, dbPath),
		jobStopCmd(database, dbPath),
		jobLogCmd(database, dbPath),
		jobStatusCmd(database, dbPath),
		jobUpdateCmd(database, dbPath),
		jobListAgentsCmd(database, dbPath),
		jobApproveAgentCmd(database, dbPath),
		jobRemoveAgentCmd(database, dbPath),
	)
	root.AddCommand(grp)
}

func jobListCmd(database *sql.DB, dbPath string) *cobra.Command {
	var flagAll, flagRecursive bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List jobs on the controller",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := controller.GetActiveControllerClient(database, dbPath)
			if err != nil {
				return err
			}
			jobs, err := ListJobs(cmd.Context(), client)
			if err != nil {
				return err
			}
			profile := getProfileName(database)
			ctrlBase := client.BaseURL

			var rows [][]string
			if flagAll {
				for _, j := range jobs {
					lastBuild := "-"
					if j.LastBuild != nil {
						lastBuild = fmt.Sprintf("#%d %s", j.LastBuild.Number, j.LastBuild.Result)
					}
					rows = append(rows, []string{j.Name, JobType(j.Class), MapColor(j.Color), lastBuild, j.Description})
				}
			} else {
				tracked, _ := db.ListTracked(database, "job", profile, ctrlBase)
				trackedSet := map[string]bool{}
				for _, n := range tracked {
					trackedSet[n] = true
				}
				serverNames := map[string]bool{}
				for _, j := range jobs {
					serverNames[j.Name] = true
				}
				for _, j := range jobs {
					if trackedSet[j.Name] {
						lastBuild := "-"
						if j.LastBuild != nil {
							lastBuild = fmt.Sprintf("#%d %s", j.LastBuild.Number, j.LastBuild.Result)
						}
						rows = append(rows, []string{j.Name, JobType(j.Class), MapColor(j.Color), lastBuild, j.Description})
					}
				}
				for n := range trackedSet {
					if !serverNames[n] {
						rows = append(rows, []string{n, "?", "[DELETED]", "-", "[DELETED_ON_SERVER]"})
					}
				}
			}
			_ = flagRecursive // ponytail: recursive folder listing
			cli.Table([]string{"Name", "Type", "Status", "Last Build", "Description"}, rows)
			fmt.Printf("  %d job(s)\n", len(rows))
			return nil
		},
	}
	cmd.Flags().BoolVar(&flagAll, "all", false, "Show all jobs (default: only mine)")
	cmd.Flags().BoolVar(&flagRecursive, "recursive", false, "Descend into folders")
	return cmd
}

func jobGetCmd(database *sql.DB, dbPath string) *cobra.Command {
	return &cobra.Command{
		Use:   "get <name>",
		Short: "Show job details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := controller.GetActiveControllerClient(database, dbPath)
			if err != nil {
				return err
			}
			j, err := GetJob(cmd.Context(), client, args[0])
			if err != nil {
				return err
			}
			if j == nil {
				return fmt.Errorf("job '%s' not found", args[0])
			}
			lastBuild := "-"
			if j.LastBuild != nil {
				lastBuild = fmt.Sprintf("#%d %s", j.LastBuild.Number, j.LastBuild.Result)
			}
			cli.KV([][]string{
				{"Name", j.Name},
				{"Type", JobType(j.Class)},
				{"Status", MapColor(j.Color)},
				{"Last Build", lastBuild},
				{"Buildable", fmt.Sprintf("%v", j.Buildable)},
				{"Description", j.Description},
				{"URL", j.URL},
			})
			return nil
		},
	}
}

func jobCreateCmd(database *sql.DB, dbPath string) *cobra.Command {
	create := &cobra.Command{Use: "create", Short: "Create a new job"}

	// shared email/param vars — each subcommand binds its own set
	addEmailFlags := func(cmd *cobra.Command, email, emailCond *string, emailKeywords *[]string, emailRegex *string) {
		cmd.Flags().StringVar(email, "email", "", "Email addresses to notify (comma-separated)")
		cmd.Flags().StringVar(emailCond, "email-cond", "failed", "When to send email: failed|success|always|custom")
		cmd.Flags().StringArrayVar(emailKeywords, "email-keyword", nil, "Send email only if build log contains keyword (repeatable)")
		cmd.Flags().StringVar(emailRegex, "email-regex", "", "Send email only if build log matches regex")
	}
	addParamFlag := func(cmd *cobra.Command, paramDefs *[]string) {
		cmd.Flags().StringArrayVar(paramDefs, "param-def", nil, "Add build parameter NAME or NAME=default (repeatable)")
	}

	// freestyle subcommand
	var fsDesc, fsShell, fsChdir, fsNode, fsSchedule, fsFolder, fsEmail, fsEmailCond, fsEmailRegex string
	var fsEmailKeywords, fsParamDefs []string
	freestyle := &cobra.Command{
		Use:   "freestyle <name>",
		Short: "Create a new Freestyle job",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			client, err := controller.GetActiveControllerClient(database, dbPath)
			if err != nil {
				return err
			}
			shell := fsShell
			if fsChdir != "" && shell != "" {
				shell = "cd " + fsChdir + " && " + shell
			}
			xmlBody := buildFreestyleXML(fsDesc, shell, fsNode, fsSchedule, fsEmail, fsEmailCond, fsEmailKeywords, fsEmailRegex, fsParamDefs)
			path := "/createItem?name=" + url.QueryEscape(name)
			if fsFolder != "" {
				path = "/job/" + JobPathSegments(fsFolder) + path
			}
			if err := client.PostXML(cmd.Context(), path, xmlBody); err != nil {
				return err
			}
			profile := getProfileName(database)
			_ = db.TrackResource(database, "job", name, profile, client.BaseURL)
			cli.Success(fmt.Sprintf("Created freestyle job '%s'", name))
			return nil
		},
	}
	freestyle.Flags().StringVar(&fsDesc, "description", "", "Job description")
	freestyle.Flags().StringVar(&fsShell, "shell", "", "Shell command to run")
	freestyle.Flags().StringVar(&fsChdir, "chdir", "", "Working directory (prepended to --shell)")
	freestyle.Flags().StringVar(&fsNode, "node", "", "Restrict to node/label")
	freestyle.Flags().StringVar(&fsSchedule, "schedule", "", "Cron schedule")
	freestyle.Flags().StringVar(&fsFolder, "folder", "", "Parent folder path")
	addEmailFlags(freestyle, &fsEmail, &fsEmailCond, &fsEmailKeywords, &fsEmailRegex)
	addParamFlag(freestyle, &fsParamDefs)

	// pipeline subcommand
	var plDesc, plScript, plNode, plSchedule, plFolder, plEmail, plEmailCond, plEmailRegex string
	var plEmailKeywords, plParamDefs []string
	pipeline := &cobra.Command{
		Use:   "pipeline <name>",
		Short: "Create a new Pipeline job",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			client, err := controller.GetActiveControllerClient(database, dbPath)
			if err != nil {
				return err
			}
			xmlBody := buildPipelineXML(plDesc, plScript, plSchedule, plEmail, plEmailCond, plEmailKeywords, plEmailRegex, plParamDefs)
			path := "/createItem?name=" + url.QueryEscape(name)
			if plFolder != "" {
				path = "/job/" + JobPathSegments(plFolder) + path
			}
			if err := client.PostXML(cmd.Context(), path, xmlBody); err != nil {
				return err
			}
			profile := getProfileName(database)
			_ = db.TrackResource(database, "job", name, profile, client.BaseURL)
			cli.Success(fmt.Sprintf("Created pipeline job '%s'", name))
			return nil
		},
	}
	pipeline.Flags().StringVar(&plDesc, "description", "", "Job description")
	pipeline.Flags().StringVar(&plScript, "script", "", "Pipeline script (Groovy inline or path)")
	pipeline.Flags().StringVar(&plNode, "node", "", "Restrict to node/label")
	_ = plNode // ponytail: pipeline node injection via script parse
	pipeline.Flags().StringVar(&plSchedule, "schedule", "", "Cron schedule")
	pipeline.Flags().StringVar(&plFolder, "folder", "", "Parent folder path")
	addEmailFlags(pipeline, &plEmail, &plEmailCond, &plEmailKeywords, &plEmailRegex)
	addParamFlag(pipeline, &plParamDefs)

	// folder subcommand
	var fdDesc, fdFolder string
	folderCmd := &cobra.Command{
		Use:   "folder <name>",
		Short: "Create a new Folder",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			client, err := controller.GetActiveControllerClient(database, dbPath)
			if err != nil {
				return err
			}
			xmlBody := buildFolderXML(fdDesc)
			path := "/createItem?name=" + url.QueryEscape(name)
			if fdFolder != "" {
				path = "/job/" + JobPathSegments(fdFolder) + path
			}
			if err := client.PostXML(cmd.Context(), path, xmlBody); err != nil {
				return err
			}
			cli.Success(fmt.Sprintf("Created folder '%s'", name))
			return nil
		},
	}
	folderCmd.Flags().StringVar(&fdDesc, "description", "", "Folder description")
	folderCmd.Flags().StringVar(&fdFolder, "folder", "", "Parent folder path")

	create.AddCommand(freestyle, pipeline, folderCmd)
	return create
}

func jobDeleteCmd(database *sql.DB, dbPath string) *cobra.Command {
	var flagYes bool
	cmd := &cobra.Command{
		Use:   "delete <name...>",
		Short: "Delete one or more jobs",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !flagYes {
				cli.Warn(fmt.Sprintf("Delete %d job(s)? This cannot be undone. Use --yes to confirm.", len(args)))
				return fmt.Errorf("aborted — use --yes to confirm")
			}
			client, err := controller.GetActiveControllerClient(database, dbPath)
			if err != nil {
				return err
			}
			for _, name := range args {
				if err := client.PostForm(cmd.Context(), "/job/"+JobPathSegments(name)+"/doDelete", nil); err != nil {
					cli.Error(fmt.Sprintf("Delete '%s': %v", name, err))
					continue
				}
				profile := getProfileName(database)
				_ = db.UntrackResource(database, "job", name, profile, client.BaseURL)
				cli.Success(fmt.Sprintf("Deleted '%s'", name))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&flagYes, "yes", false, "Skip confirmation prompt")
	return cmd
}

func jobCopyCmd(database *sql.DB, dbPath string) *cobra.Command {
	return &cobra.Command{
		Use:   "copy <source> <destination>",
		Short: "Clone an existing job into a new job",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			src, dst := args[0], args[1]
			client, err := controller.GetActiveControllerClient(database, dbPath)
			if err != nil {
				return err
			}
			params := map[string]string{"name": dst, "mode": "copy", "from": src}
			if err := client.PostForm(cmd.Context(), "/createItem", params); err != nil {
				return err
			}
			profile := getProfileName(database)
			_ = db.TrackResource(database, "job", dst, profile, client.BaseURL)
			cli.Success(fmt.Sprintf("Cloned '%s' → '%s'", src, dst))
			return nil
		},
	}
}

func jobMoveCmd(database *sql.DB, dbPath string) *cobra.Command {
	return &cobra.Command{
		Use:   "move <source> <destination-folder>",
		Short: "Move (rename/relocate) a job to a different folder",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			src, dst := args[0], args[1]
			client, err := controller.GetActiveControllerClient(database, dbPath)
			if err != nil {
				return err
			}
			path := "/job/" + JobPathSegments(src) + "/move/move"
			params := map[string]string{"destination": "/" + dst}
			if err := client.PostForm(cmd.Context(), path, params); err != nil {
				return err
			}
			cli.Success(fmt.Sprintf("Moved '%s' to '%s'", src, dst))
			return nil
		},
	}
}

func jobTrackCmd(database *sql.DB, dbPath string) *cobra.Command {
	return &cobra.Command{
		Use:   "track <name...>",
		Short: "Start tracking jobs (add to Mine)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := controller.GetActiveControllerClient(database, dbPath)
			if err != nil {
				return err
			}
			profile := getProfileName(database)
			for _, name := range args {
				_ = db.TrackResource(database, "job", name, profile, client.BaseURL)
				cli.Success(fmt.Sprintf("Tracking '%s'", name))
			}
			return nil
		},
	}
}

func jobUntrackCmd(database *sql.DB, dbPath string) *cobra.Command {
	return &cobra.Command{
		Use:   "untrack <name...>",
		Short: "Stop tracking jobs (remove from Mine)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := controller.GetActiveControllerClient(database, dbPath)
			if err != nil {
				return err
			}
			profile := getProfileName(database)
			for _, name := range args {
				_ = db.UntrackResource(database, "job", name, profile, client.BaseURL)
				cli.Success(fmt.Sprintf("Untracked '%s'", name))
			}
			return nil
		},
	}
}

func jobRunCmd(database *sql.DB, dbPath string) *cobra.Command {
	var params []string
	var wait bool
	var timeout int
	cmd := &cobra.Command{
		Use:   "run <name>",
		Short: "Trigger a build",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			client, err := controller.GetActiveControllerClient(database, dbPath)
			if err != nil {
				return err
			}
			path := "/job/" + JobPathSegments(name) + "/build"
			if len(params) > 0 {
				path = "/job/" + JobPathSegments(name) + "/buildWithParameters"
				parts := make([]string, 0, len(params))
				for _, p := range params {
					parts = append(parts, p)
				}
				path += "?" + strings.Join(parts, "&")
			}
			if err := client.PostForm(cmd.Context(), path, nil); err != nil {
				return err
			}
			cli.Success(fmt.Sprintf("Triggered build for '%s'", name))
			if wait {
				cli.Info(fmt.Sprintf("Waiting for build (timeout: %ds)...", timeout))
				deadline := time.Now().Add(time.Duration(timeout) * time.Second)
				for time.Now().Before(deadline) {
					time.Sleep(3 * time.Second)
					buildNum, err := GetLastBuildNumber(cmd.Context(), client, name)
					if err != nil {
						continue
					}
					b, err := GetBuildDetail(cmd.Context(), client, name, buildNum)
					if err != nil {
						continue
					}
					if !b.Building {
						cli.Success(fmt.Sprintf("Build #%d finished: %s", buildNum, b.Result))
						return nil
					}
				}
				return fmt.Errorf("timeout waiting for build")
			}
			return nil
		},
	}
	cmd.Flags().StringArrayVar(&params, "param", nil, "Build parameter KEY=value (repeatable)")
	cmd.Flags().BoolVar(&wait, "wait", false, "Wait for build to finish")
	cmd.Flags().IntVar(&timeout, "timeout", 300, "Max wait time in seconds")
	return cmd
}

func jobStopCmd(database *sql.DB, dbPath string) *cobra.Command {
	return &cobra.Command{
		Use:   "stop <name> [build-number]",
		Short: "Stop (abort) a running build",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			client, err := controller.GetActiveControllerClient(database, dbPath)
			if err != nil {
				return err
			}
			buildNum := 0
			if len(args) == 2 {
				n, e := strconv.Atoi(args[1])
				if e != nil {
					return fmt.Errorf("invalid build number: %s", args[1])
				}
				buildNum = n
			} else {
				buildNum, err = GetLastBuildNumber(cmd.Context(), client, name)
				if err != nil {
					return err
				}
			}
			path := fmt.Sprintf("/job/%s/%d/stop", JobPathSegments(name), buildNum)
			if err := client.PostForm(cmd.Context(), path, nil); err != nil {
				return err
			}
			cli.Success(fmt.Sprintf("Stopped build #%d of '%s'", buildNum, name))
			return nil
		},
	}
}

func jobLogCmd(database *sql.DB, dbPath string) *cobra.Command {
	var flagFollow bool
	cmd := &cobra.Command{
		Use:   "log <name> [build-number]",
		Short: "Show (or stream) build console output",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			client, err := controller.GetActiveControllerClient(database, dbPath)
			if err != nil {
				return err
			}
			buildNum := 0
			if len(args) == 2 {
				n, e := strconv.Atoi(args[1])
				if e != nil {
					return fmt.Errorf("invalid build number: %s", args[1])
				}
				buildNum = n
			} else {
				buildNum, err = GetLastBuildNumber(cmd.Context(), client, name)
				if err != nil {
					return err
				}
			}
			var offset int64
			for {
				text, newOffset, hasMore, err := StreamBuildLog(cmd.Context(), client, name, buildNum, offset)
				if err != nil {
					return err
				}
				if text != "" {
					fmt.Print(text)
				}
				offset = newOffset
				if !hasMore {
					break
				}
				if !flagFollow {
					break
				}
				time.Sleep(time.Second)
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&flagFollow, "follow", "f", false, "Stream log in real time")
	return cmd
}

func jobStatusCmd(database *sql.DB, dbPath string) *cobra.Command {
	var flagCount int
	cmd := &cobra.Command{
		Use:   "status <name>",
		Short: "Show build history for a job",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			client, err := controller.GetActiveControllerClient(database, dbPath)
			if err != nil {
				return err
			}
			builds, err := GetBuildHistory(cmd.Context(), client, name, flagCount)
			if err != nil {
				return err
			}
			rows := make([][]string, len(builds))
			for i, b := range builds {
				dur := fmt.Sprintf("%.0fs", float64(b.Duration)/1000)
				ts := time.Unix(b.Timestamp/1000, 0).Format("2006-01-02 15:04")
				status := b.Result
				if b.Building {
					status = "RUNNING"
				}
				rows[i] = []string{fmt.Sprintf("#%d", b.Number), status, ts, dur}
			}
			cli.Table([]string{"Build", "Result", "Started", "Duration"}, rows)
			return nil
		},
	}
	cmd.Flags().IntVar(&flagCount, "count", 10, "Number of builds to show")
	return cmd
}

func jobUpdateCmd(database *sql.DB, dbPath string) *cobra.Command {
	update := &cobra.Command{Use: "update", Short: "Update an existing job's configuration"}

	addEmailFlags := func(cmd *cobra.Command, email, emailCond *string, emailKeywords *[]string, emailRegex *string, clearKw, clearRe *bool) {
		cmd.Flags().StringVar(email, "email", "", "Add or change email recipients, or '' to remove")
		cmd.Flags().StringVar(emailCond, "email-cond", "", "When to send: failed|success|always|custom")
		cmd.Flags().StringArrayVar(emailKeywords, "email-keyword", nil, "Replace email keyword filters (repeatable)")
		cmd.Flags().StringVar(emailRegex, "email-regex", "", "Replace email regex filter")
		cmd.Flags().BoolVar(clearKw, "clear-email-keywords", false, "Remove all email keyword filters")
		cmd.Flags().BoolVar(clearRe, "clear-email-regex", false, "Remove email regex filter")
	}

	var fsDesc, fsShell, fsChdir, fsNode, fsSchedule, fsEmail, fsEmailCond, fsEmailRegex string
	var fsEmailKeywords, fsParamDefs []string
	var fsClearKw, fsClearRe, fsClearParams bool
	freestyle := &cobra.Command{
		Use:   "freestyle <name>",
		Short: "Update a Freestyle job",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			client, err := controller.GetActiveControllerClient(database, dbPath)
			if err != nil {
				return err
			}
			shell := fsShell
			if fsChdir != "" && shell != "" {
				shell = "cd " + fsChdir + " && " + shell
			}
			emailKws := fsEmailKeywords
			if fsClearKw {
				emailKws = []string{}
			}
			emailRe := fsEmailRegex
			if fsClearRe {
				emailRe = ""
			}
			paramDefs := fsParamDefs
			if fsClearParams {
				paramDefs = []string{}
			}
			xmlBody := buildFreestyleXML(fsDesc, shell, fsNode, fsSchedule, fsEmail, fsEmailCond, emailKws, emailRe, paramDefs)
			return client.PostXML(cmd.Context(), "/job/"+JobPathSegments(name)+"/config.xml", xmlBody)
		},
	}
	freestyle.Flags().StringVar(&fsDesc, "description", "", "Job description")
	freestyle.Flags().StringVar(&fsShell, "shell", "", "Shell command")
	freestyle.Flags().StringVar(&fsChdir, "chdir", "", "Working directory (prepended to --shell)")
	freestyle.Flags().StringVar(&fsNode, "node", "", "Node/label")
	freestyle.Flags().StringVar(&fsSchedule, "schedule", "", "Cron schedule")
	addEmailFlags(freestyle, &fsEmail, &fsEmailCond, &fsEmailKeywords, &fsEmailRegex, &fsClearKw, &fsClearRe)
	freestyle.Flags().StringArrayVar(&fsParamDefs, "param-def", nil, "Add/replace build parameter NAME or NAME=default (repeatable)")
	freestyle.Flags().BoolVar(&fsClearParams, "clear-params", false, "Remove all build parameters")

	var plDesc, plScript, plSchedule, plEmail, plEmailCond, plEmailRegex string
	var plEmailKeywords, plParamDefs []string
	var plClearKw, plClearRe, plClearParams bool
	pipeline := &cobra.Command{
		Use:   "pipeline <name>",
		Short: "Update a Pipeline job",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			client, err := controller.GetActiveControllerClient(database, dbPath)
			if err != nil {
				return err
			}
			emailKws := plEmailKeywords
			if plClearKw {
				emailKws = []string{}
			}
			emailRe := plEmailRegex
			if plClearRe {
				emailRe = ""
			}
			paramDefs := plParamDefs
			if plClearParams {
				paramDefs = []string{}
			}
			xmlBody := buildPipelineXML(plDesc, plScript, plSchedule, plEmail, plEmailCond, emailKws, emailRe, paramDefs)
			return client.PostXML(cmd.Context(), "/job/"+JobPathSegments(name)+"/config.xml", xmlBody)
		},
	}
	pipeline.Flags().StringVar(&plDesc, "description", "", "Job description")
	pipeline.Flags().StringVar(&plScript, "script", "", "Pipeline script")
	pipeline.Flags().StringVar(&plSchedule, "schedule", "", "Cron schedule")
	addEmailFlags(pipeline, &plEmail, &plEmailCond, &plEmailKeywords, &plEmailRegex, &plClearKw, &plClearRe)
	pipeline.Flags().StringArrayVar(&plParamDefs, "param-def", nil, "Add/replace build parameter NAME or NAME=default (repeatable)")
	pipeline.Flags().BoolVar(&plClearParams, "clear-params", false, "Remove all build parameters")

	update.AddCommand(freestyle, pipeline)
	return update
}

func jobListAgentsCmd(database *sql.DB, dbPath string) *cobra.Command {
	return &cobra.Command{
		Use:   "list-agents <folder>",
		Short: "List agents approved (whitelisted) for a controlled-agent folder",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := controller.GetActiveControllerClient(database, dbPath)
			if err != nil {
				return err
			}
			grants, err := listControlledAgents(client, args[0])
			if err != nil {
				return err
			}
			rows := make([][]string, len(grants))
			for i, g := range grants {
				rows[i] = []string{g.AgentName, g.GrantID}
			}
			cli.Table([]string{"Agent", "Grant ID"}, rows)
			return nil
		},
	}
}

func jobApproveAgentCmd(database *sql.DB, dbPath string) *cobra.Command {
	return &cobra.Command{
		Use:   "approve-agent <folder> <agent>",
		Short: "Approve (whitelist) an agent to run builds from a controlled-agent folder",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			folder, agent := args[0], args[1]
			client, err := controller.GetActiveControllerClient(database, dbPath)
			if err != nil {
				return err
			}
			path := "/job/" + JobPathSegments(folder) + "/controlled-agent/grant"
			params := map[string]string{"agent": agent}
			if err := client.PostForm(cmd.Context(), path, params); err != nil {
				return err
			}
			cli.Success(fmt.Sprintf("Agent '%s' approved for folder '%s'", agent, folder))
			return nil
		},
	}
}

func jobRemoveAgentCmd(database *sql.DB, dbPath string) *cobra.Command {
	var flagYes bool
	cmd := &cobra.Command{
		Use:   "remove-agent <folder> <agent>",
		Short: "Revoke agent approval from a controlled-agent folder",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !flagYes {
				return fmt.Errorf("aborted — use --yes to confirm")
			}
			folder, agent := args[0], args[1]
			client, err := controller.GetActiveControllerClient(database, dbPath)
			if err != nil {
				return err
			}
			grants, err := listControlledAgents(client, folder)
			if err != nil {
				return err
			}
			for _, g := range grants {
				if g.AgentName == agent {
					path := "/job/" + JobPathSegments(folder) + "/controlled-agent/grant/" + url.PathEscape(g.GrantID) + "/remove"
					if err := client.PostForm(cmd.Context(), path, nil); err != nil {
						return err
					}
					cli.Success(fmt.Sprintf("Removed agent '%s' from folder '%s'", agent, folder))
					return nil
				}
			}
			return fmt.Errorf("agent '%s' not found in folder '%s'", agent, folder)
		},
	}
	cmd.Flags().BoolVar(&flagYes, "yes", false, "Skip confirmation")
	return cmd
}
