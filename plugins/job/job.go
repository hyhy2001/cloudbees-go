// Package job implements bee job commands.
package job

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"bee/internal/api"
	"bee/internal/cache"
	"bee/internal/cli"
	"bee/internal/db"
	"bee/internal/session"
	"bee/plugins/controller"
	node "bee/plugins/node"
)

type controlledAgentGrant struct {
	AgentName string
	GrantID   string
}

// trunc caps s to n runes, matching the TS list columns' .slice(0, n).
func trunc(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

func getProfileName(database *sql.DB) string {
	name, _ := session.GetActiveProfileName(database)
	return name
}

func getText(client *api.Client, path string) (string, error) {
	resp, err := client.Do(context.Background(), "GET", path, nil, "")
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
			var jobs []JobDTO
			if flagRecursive {
				jobs, err = ListJobsRecursive(cmd.Context(), client, "")
			} else {
				jobs, err = ListJobs(cmd.Context(), database, client)
			}
			if err != nil {
				return err
			}
			profile := getProfileName(database)
			ctrlBase := client.BaseURL

			// jobRow mirrors the TS list row: name[:30], type, color[:14],
			// the bare last-build number (or "-"), description[:30].
			jobRow := func(j JobDTO) []string {
				build := "-"
				if j.LastBuild != nil {
					build = fmt.Sprintf("%d", j.LastBuild.Number)
				}
				return []string{trunc(j.Name, 30), JobType(j.Class), trunc(MapColor(j.Color), 14), build, trunc(j.Description, 30)}
			}

			var rows [][]string
			if flagAll {
				for _, j := range jobs {
					rows = append(rows, jobRow(j))
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
						rows = append(rows, jobRow(j))
					}
				}
				for n := range trackedSet {
					if !serverNames[n] {
						rows = append(rows, []string{trunc(n, 30), "?", "[DELETED]", "-", "[DELETED_ON_SERVER]"})
					}
				}
			}
			cli.Table([]string{"Name", "Type", "Status", "Build#", "Description"}, rows)
			fmt.Printf("  %d job(s)  [FS=Freestyle  PL=Pipeline  FD=Folder]\n", len(rows))
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
			pairs := [][]string{
				{"Name", j.Name},
				{"Type", JobType(j.Class)},
				{"Status", MapColor(j.Color)},
				{"Last Build", lastBuild},
				{"Buildable", fmt.Sprintf("%v", j.Buildable)},
				{"Description", j.Description},
				{"URL", j.URL},
			}
			if s, err := GetJobConfigSummary(cmd.Context(), client, args[0]); err == nil {
				pairs = append(pairs,
					[]string{"Node", s.Node},
					[]string{"Schedule", s.Schedule},
					[]string{"Shell", s.ShellCmd},
					[]string{"Chdir", s.Chdir},
					[]string{"Email", s.Email},
					[]string{"Email Cond", s.EmailCond},
					[]string{"Email Keyword", s.EmailKeyword},
					[]string{"Email Regex", s.EmailRegex},
					[]string{"Params", s.Params},
				)
			}
			cli.KV(pairs)
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
			// Allow "folder/job" shorthand — split into folder + basename.
			if fsFolder == "" {
				if idx := strings.LastIndex(name, "/"); idx >= 0 {
					fsFolder = name[:idx]
					name = name[idx+1:]
				}
			}
			client, err := controller.GetActiveControllerClient(database, dbPath)
			if err != nil {
				return err
			}
			err = CreateFreestyleJob(cmd.Context(), client, CreateFreestyleParams{
				Name: name, Folder: fsFolder,
				Description: fsDesc, Shell: fsShell, Chdir: fsChdir, Node: fsNode,
				Schedule: fsSchedule,
				Email:    fsEmail, EmailCond: fsEmailCond, EmailRegex: fsEmailRegex,
				EmailChanged: cmd.Flags().Changed("email"), EmailCondChanged: cmd.Flags().Changed("email-cond"),
				EmailKeywords: fsEmailKeywords, ParamDefs: fsParamDefs,
			})
			if err != nil {
				return err
			}
			_ = cache.InvalidateResource(database, "job")
			profile := getProfileName(database)
			qualified := name
			if fsFolder != "" {
				qualified = fsFolder + "/" + name
			}
			_ = db.TrackResource(database, "job", qualified, profile, client.BaseURL)
			nodeMsg := ""
			if fsNode != "" {
				nodeMsg = fmt.Sprintf(" on node '%s'", fsNode)
			}
			cli.Success(fmt.Sprintf("Freestyle job '%s' created.%s", qualified, nodeMsg))
			if fsNode == "" {
				cli.Warn("No node assigned — job will run on any available agent.")
			}
			fmt.Printf("  Link: %s/job/%s/\n", strings.TrimRight(client.BaseURL, "/"), strings.ReplaceAll(qualified, "/", "/job/"))
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
			// Allow "folder/job" shorthand — split into folder + basename.
			if plFolder == "" {
				if idx := strings.LastIndex(name, "/"); idx >= 0 {
					plFolder = name[:idx]
					name = name[idx+1:]
				}
			}
			client, err := controller.GetActiveControllerClient(database, dbPath)
			if err != nil {
				return err
			}
			err = CreatePipelineJob(cmd.Context(), client, CreatePipelineParams{
				Name: name, Folder: plFolder,
				Description: plDesc, Script: plScript, Node: plNode,
				Schedule: plSchedule,
				Email:    plEmail, EmailCond: plEmailCond, EmailRegex: plEmailRegex,
				EmailChanged: cmd.Flags().Changed("email"), EmailCondChanged: cmd.Flags().Changed("email-cond"),
				EmailKeywords: plEmailKeywords, ParamDefs: plParamDefs,
			})
			if err != nil {
				return err
			}
			_ = cache.InvalidateResource(database, "job")
			profile := getProfileName(database)
			qualified := name
			if plFolder != "" {
				qualified = plFolder + "/" + name
			}
			_ = db.TrackResource(database, "job", qualified, profile, client.BaseURL)
			nodeMsg := ""
			if plNode != "" {
				nodeMsg = fmt.Sprintf(" on node '%s'", plNode)
			}
			cli.Success(fmt.Sprintf("Pipeline job '%s' created.%s", qualified, nodeMsg))
			fmt.Printf("  Link: %s/job/%s/\n", strings.TrimRight(client.BaseURL, "/"), strings.ReplaceAll(qualified, "/", "/job/"))
			return nil
		},
	}
	pipeline.Flags().StringVar(&plDesc, "description", "", "Job description")
	pipeline.Flags().StringVar(&plScript, "script", "", "Pipeline script (Groovy inline or path)")
	pipeline.Flags().StringVar(&plNode, "node", "", "Restrict to node/label")
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
			if err := CreateFolderJob(cmd.Context(), client, name, fdFolder, fdDesc); err != nil {
				return err
			}
			_ = cache.InvalidateResource(database, "job")
			profile := getProfileName(database)
			qualified := name
			if fdFolder != "" {
				qualified = fdFolder + "/" + name
			}
			_ = db.TrackResource(database, "job", qualified, profile, client.BaseURL)
			cli.Success(fmt.Sprintf("Folder '%s' created.", qualified))
			fmt.Printf("  Link: %s/job/%s/\n", strings.TrimRight(client.BaseURL, "/"), strings.ReplaceAll(qualified, "/", "/job/"))
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
				label := fmt.Sprintf("job '%s'", args[0])
				if len(args) > 1 {
					label = fmt.Sprintf("%d jobs", len(args))
				}
				if !cli.Confirm(fmt.Sprintf("Delete %s? [y/N] ", label)) {
					cli.Info("Cancelled.")
					return nil
				}
			}
			client, err := controller.GetActiveControllerClient(database, dbPath)
			if err != nil {
				return err
			}
			for _, name := range args {
				if err := client.PostForm(cmd.Context(), "/job/"+JobPathSegments(name)+"/doDelete", nil); err != nil {
					cli.Warn(fmt.Sprintf("Could not delete job '%s' on server: %s", name, err))
					cli.Info("Proceeding with local removal anyway.")
				} else {
					cli.Success(fmt.Sprintf("Job '%s' deleted from server.", name))
				}
				profile := getProfileName(database)
				_ = db.UntrackResource(database, "job", name, profile, client.BaseURL)
				cli.Success(fmt.Sprintf("Job '%s' removed from local database.", name))
			}
			_ = cache.InvalidateResource(database, "job")
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
			_ = cache.InvalidateResource(database, "job")
			profile := getProfileName(database)
			_ = db.TrackResource(database, "job", dst, profile, client.BaseURL)
			cli.Success(fmt.Sprintf("Job '%s' cloned to '%s'.", src, dst))
			fmt.Printf("  Link: %s/job/%s/\n", strings.TrimRight(client.BaseURL, "/"), strings.ReplaceAll(dst, "/", "/job/"))
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
			_ = cache.InvalidateResource(database, "job")
			base := src
			if idx := strings.LastIndex(src, "/"); idx >= 0 {
				base = src[idx+1:]
			}
			destLabel := dst
			qualified := dst + "/" + base
			if dst == "." || dst == "" {
				destLabel = "/"
				qualified = base
			}
			profile := getProfileName(database)
			_ = db.TrackResource(database, "job", qualified, profile, client.BaseURL)
			cli.Success(fmt.Sprintf("Job '%s' moved to '%s' as '%s'.", src, destLabel, qualified))
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
			jobs, _ := ListJobs(cmd.Context(), database, client)
			serverNames := map[string]bool{}
			for _, j := range jobs {
				serverNames[j.Name] = true
			}
			trackedNow, _ := db.ListTracked(database, "job", profile, client.BaseURL)
			trackedSet := map[string]bool{}
			for _, n := range trackedNow {
				trackedSet[n] = true
			}
			for _, name := range args {
				if !serverNames[name] {
					cli.Error(fmt.Sprintf("Job '%s' not found on server. Skipping.", name))
					continue
				}
				if trackedSet[name] {
					cli.Info(fmt.Sprintf("Job '%s' is already tracked.", name))
					continue
				}
				_ = db.TrackResource(database, "job", name, profile, client.BaseURL)
				cli.Success(fmt.Sprintf("Tracked job '%s'.", name))
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
			trackedNow, _ := db.ListTracked(database, "job", profile, client.BaseURL)
			trackedSet := map[string]bool{}
			for _, n := range trackedNow {
				trackedSet[n] = true
			}
			for _, name := range args {
				if !trackedSet[name] {
					cli.Info(fmt.Sprintf("Job '%s' is not in Mine.", name))
					continue
				}
				_ = db.UntrackResource(database, "job", name, profile, client.BaseURL)
				cli.Success(fmt.Sprintf("Removed job '%s' from Mine.", name))
			}
			return nil
		},
	}
}

func jobRunCmd(database *sql.DB, dbPath string) *cobra.Command {
	var params []string
	var wait bool
	var timeout int
	var flagYes bool
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
			if !flagYes {
				if xmlStr, err := GetJobConfigXML(cmd.Context(), client, name); err == nil {
					if nodeName := extractTag(xmlStr, "assignedNode"); nodeName != "" {
						if warning, err := node.CheckNodeApprovalForJob(cmd.Context(), client, nodeName, name); err == nil && warning != "" {
							cli.Warn(warning)
							fmt.Print("Trigger build anyway? [y/N] ")
							var answer string
							fmt.Scanln(&answer)
							if strings.ToLower(strings.TrimSpace(answer)) != "y" {
								cli.Info("Cancelled.")
								return nil
							}
						}
					}
				}
			}
			baseline, _ := GetLastBuildNumber(cmd.Context(), client, name)

			path := "/job/" + JobPathSegments(name) + "/build"
			var formParams map[string]string
			if len(params) > 0 {
				path = "/job/" + JobPathSegments(name) + "/buildWithParameters"
				formParams = make(map[string]string, len(params))
				for _, p := range params {
					key, val, _ := strings.Cut(p, "=")
					formParams[url.QueryEscape(key)] = url.QueryEscape(val)
				}
			}
			if err := client.PostForm(cmd.Context(), path, formParams); err != nil {
				// Jobs with defined parameters require /buildWithParameters even with no values.
				if len(params) == 0 && strings.Contains(err.Error(), "Nothing is submitted") {
					if err2 := client.PostForm(cmd.Context(), "/job/"+JobPathSegments(name)+"/buildWithParameters", nil); err2 != nil {
						return err2
					}
				} else {
					return err
				}
			}
			cli.Success(fmt.Sprintf("Triggered: %s", name))

			buildNum := baseline
			deadline := time.Now().Add(15 * time.Second)
			for time.Now().Before(deadline) {
				time.Sleep(2 * time.Second)
				if n, err := GetLastBuildNumber(cmd.Context(), client, name); err == nil && n > baseline {
					buildNum = n
					break
				}
			}
			if buildNum <= baseline {
				cli.Warn("Timed out waiting for the new build to appear in the queue.")
				return fmt.Errorf("could not determine new build number for '%s'", name)
			}

			if wait {
				cli.Info(fmt.Sprintf("Waiting for build #%d (timeout: %ds)...", buildNum, timeout))
				deadline := time.Now().Add(time.Duration(timeout) * time.Second)
				for time.Now().Before(deadline) {
					time.Sleep(3 * time.Second)
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
	cmd.Flags().IntVar(&timeout, "timeout", 120, "Max wait time in seconds")
	cmd.Flags().BoolVar(&flagYes, "yes", false, "Skip node-approval confirmation prompt")
	return cmd
}

func jobStopCmd(database *sql.DB, dbPath string) *cobra.Command {
	return &cobra.Command{
		Use:   "stop <name> <build-number>",
		Short: "Stop (abort) a running build",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			raw := strings.TrimSpace(args[1])
			n, e := strconv.Atoi(raw)
			if e != nil || strconv.Itoa(n) != raw || n <= 0 {
				return fmt.Errorf("invalid build number: '%s' — must be a positive integer", args[1])
			}
			buildNum := n
			client, err := controller.GetActiveControllerClient(database, dbPath)
			if err != nil {
				return err
			}
			path := fmt.Sprintf("/job/%s/%d/stop", JobPathSegments(name), buildNum)
			if err := client.PostForm(cmd.Context(), path, nil); err != nil {
				return err
			}
			cli.Success(fmt.Sprintf("Stop requested: %s #%d", name, buildNum))
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
		Use:     "status <name>",
		Aliases: []string{"history"},
		Short:   "Show build history for a job",
		Args:    cobra.ExactArgs(1),
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
			if len(builds) == 0 {
				cli.Info("No builds found.")
				return nil
			}
			rows := make([][]string, len(builds))
			for i, b := range builds {
				dur := "-"
				if b.Duration != 0 {
					dur = fmt.Sprintf("%ds", b.Duration/1000)
				}
				ts := "-"
				if b.Timestamp != 0 {
					ts = time.Unix(b.Timestamp/1000, 0).UTC().Format("2006-01-02 15:04")
				}
				result := b.Result
				if result == "" {
					if b.Building {
						result = "RUNNING"
					} else {
						result = "-"
					}
				}
				rows[i] = []string{fmt.Sprintf("%d", b.Number), result, dur, ts}
			}
			cli.Table([]string{"Build#", "Result", "Duration", "Timestamp"}, rows)
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

			f := FreestyleUpdateFields{ClearEmailKeywords: fsClearKw, ClearEmailRegex: fsClearRe, ClearParams: fsClearParams}
			flags := cmd.Flags()
			if flags.Changed("description") {
				f.Description = &fsDesc
			}
			if flags.Changed("node") {
				f.Node = &fsNode
			}
			if flags.Changed("shell") || flags.Changed("chdir") {
				shell := fsShell
				chdir := fsChdir
				f.Shell = &shell
				f.Chdir = &chdir
			}
			if flags.Changed("schedule") {
				f.Schedule = &fsSchedule
			}
			if flags.Changed("email") {
				f.Email = &fsEmail
			}
			if flags.Changed("email-cond") {
				f.EmailCond = &fsEmailCond
			}
			if flags.Changed("email-keyword") {
				f.EmailKeywords = &fsEmailKeywords
			}
			if flags.Changed("email-regex") {
				f.EmailRegex = &fsEmailRegex
			}
			if flags.Changed("param-def") {
				f.ParamDefs = &fsParamDefs
			}

			if err := validateEmailFilterFlags(fsEmail, flags.Changed("email"), fsEmailKeywords, fsEmailRegex, fsEmailCond, flags.Changed("email-cond")); err != nil {
				return err
			}

			if err := UpdateFreestyleJob(cmd.Context(), client, name, f); err != nil {
				return err
			}
			_ = cache.InvalidateResource(database, "job")
			cli.Success(fmt.Sprintf("Freestyle job '%s' updated.", name))
			return nil
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

	var plDesc, plScript, plNode, plSchedule, plEmail, plEmailCond, plEmailRegex string
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

			f := PipelineUpdateFields{ClearEmailKeywords: plClearKw, ClearEmailRegex: plClearRe, ClearParams: plClearParams}
			flags := cmd.Flags()
			if flags.Changed("description") {
				f.Description = &plDesc
			}
			if flags.Changed("script") {
				origScript, err := ResolveScript(plScript)
				if err != nil {
					return err
				}
				finalScript := injectAgent(origScript, plNode)
				if err := ValidatePipelineScript(cmd.Context(), client, origScript); err != nil {
					return err
				}
				f.Script = &finalScript
				if !flags.Changed("param-def") {
					if autoParams := parseParametersFromScript(finalScript); len(autoParams) > 0 {
						f.ParamDefs = &autoParams
					}
				} else {
					merged := mergeParamDefs(parseParametersFromScript(finalScript), plParamDefs)
					f.ParamDefs = &merged
				}
			}
			if flags.Changed("schedule") {
				f.Schedule = &plSchedule
			}
			if flags.Changed("email") {
				f.Email = &plEmail
			}
			if flags.Changed("email-cond") {
				f.EmailCond = &plEmailCond
			}
			if flags.Changed("email-keyword") {
				f.EmailKeywords = &plEmailKeywords
			}
			if flags.Changed("email-regex") {
				f.EmailRegex = &plEmailRegex
			}
			if flags.Changed("param-def") && !flags.Changed("script") {
				f.ParamDefs = &plParamDefs
			}

			if err := validateEmailFilterFlags(plEmail, flags.Changed("email"), plEmailKeywords, plEmailRegex, plEmailCond, flags.Changed("email-cond")); err != nil {
				return err
			}

			if err := UpdatePipelineJob(cmd.Context(), client, name, f); err != nil {
				return err
			}
			_ = cache.InvalidateResource(database, "job")
			cli.Success(fmt.Sprintf("Pipeline job '%s' updated.", name))
			return nil
		},
	}
	pipeline.Flags().StringVar(&plDesc, "description", "", "Job description")
	pipeline.Flags().StringVar(&plScript, "script", "", "Pipeline script (Groovy inline or path)")
	pipeline.Flags().StringVar(&plNode, "node", "", "Restrict to node/label (applied when --script is set)")
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
		Short: "Approve an agent for a Folders Plus controlled-agent folder (5-step handshake)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			folder, agent := args[0], args[1]
			client, err := controller.GetActiveControllerClient(database, dbPath)
			if err != nil {
				return err
			}
			fmt.Printf("  Running handshake: folder='%s' agent='%s'…\n", folder, agent)
			if err := node.ApproveFolder(cmd.Context(), client, agent, folder); err != nil {
				return err
			}
			cli.Success(fmt.Sprintf("Agent '%s' approved for folder '%s'.", agent, folder))
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
			folder, agent := args[0], args[1]
			if !flagYes {
				if !cli.Confirm(fmt.Sprintf("Remove agent '%s' from '%s'? [y/N] ", agent, folder)) {
					cli.Info("Cancelled.")
					return nil
				}
			}
			client, err := controller.GetActiveControllerClient(database, dbPath)
			if err != nil {
				return err
			}
			grants, err := listControlledAgents(client, folder)
			if err != nil {
				return err
			}
			for _, g := range grants {
				if strings.EqualFold(g.AgentName, agent) {
					if err := node.RemoveControlledAgentGrant(cmd.Context(), client, folder, g.GrantID); err != nil {
						return err
					}
					cli.Success(fmt.Sprintf("Agent '%s' removed from '%s'.", agent, folder))
					return nil
				}
			}
			return fmt.Errorf("agent '%s' not found in folder '%s'", agent, folder)
		},
	}
	cmd.Flags().BoolVar(&flagYes, "yes", false, "Skip confirmation")
	return cmd
}
