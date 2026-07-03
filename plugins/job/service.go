// Package job — exported service layer for TUI and other consumers.
package job

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"bee/internal/api"
)

// JobDTO is the exported flat job list entry.
type JobDTO struct {
	Class       string
	Name        string
	URL         string
	Color       string
	Description string
	Buildable   bool
	LastBuild   *BuildRef
}

// BuildRef is a lightweight build reference embedded in JobDTO.
type BuildRef struct {
	Number int
	Result string
}

// BuildDTO is the exported build detail.
type BuildDTO struct {
	Number    int
	Result    string
	Building  bool
	Duration  int64
	Timestamp int64
}

// JobPathSegments converts "folder/job" to "folder/job/job" for Jenkins REST.
func JobPathSegments(name string) string {
	parts := strings.Split(name, "/")
	escaped := make([]string, len(parts))
	for i, p := range parts {
		escaped[i] = url.PathEscape(p)
	}
	return strings.Join(escaped, "/job/")
}

// MapColor translates a Jenkins color string to a human-readable status label.
func MapColor(color string) string {
	running := strings.HasSuffix(color, "_anime")
	base := strings.TrimSuffix(color, "_anime")
	m := map[string]string{
		"blue": "OK", "red": "FAIL", "yellow": "WARN",
		"aborted": "ABORTED", "notbuilt": "NEW", "disabled": "DISABLED",
	}
	state, ok := m[base]
	if !ok {
		if base != "" {
			state = strings.ToUpper(base)
		} else {
			state = "UNKNOWN"
		}
	}
	if running {
		return state + " (Run)"
	}
	return state
}

// JobType returns a short two-letter type label for a Jenkins class string.
func JobType(class string) string {
	if strings.Contains(class, "WorkflowJob") || strings.Contains(class, "Pipeline") {
		return "PL"
	}
	if strings.Contains(class, "Folder") {
		return "FD"
	}
	if strings.Contains(class, "FreeStyle") || strings.Contains(class, "Project") {
		return "FS"
	}
	return "?"
}

// ListJobs fetches all jobs from the active controller. Pass a non-nil
// database to read/populate the cache; pass nil to always hit the network.
func ListJobs(ctx context.Context, database *sql.DB, client *api.Client) ([]JobDTO, error) {
	tree := "jobs[_class,name,url,color,description,buildable,lastBuild[number,result,url]]"
	var raw struct {
		Jobs []struct {
			Class       string `json:"_class"`
			Name        string `json:"name"`
			URL         string `json:"url"`
			Color       string `json:"color"`
			Description string `json:"description"`
			Buildable   bool   `json:"buildable"`
			LastBuild   *struct {
				Number int    `json:"number"`
				Result string `json:"result"`
			} `json:"lastBuild"`
		} `json:"jobs"`
	}
	if err := client.GetJSONCached(ctx, database, "/api/json?tree="+url.QueryEscape(tree), "jobs.list", &raw); err != nil {
		return nil, err
	}
	out := make([]JobDTO, 0, len(raw.Jobs))
	for _, j := range raw.Jobs {
		dto := JobDTO{
			Class: j.Class, Name: j.Name, URL: j.URL,
			Color: j.Color, Description: j.Description, Buildable: j.Buildable,
		}
		if j.LastBuild != nil {
			dto.LastBuild = &BuildRef{Number: j.LastBuild.Number, Result: j.LastBuild.Result}
		}
		out = append(out, dto)
	}
	return out, nil
}

// listJobsAt fetches jobs under a folder path ("" for root), tree-scoped.
func listJobsAt(ctx context.Context, client *api.Client, prefix string) ([]JobDTO, error) {
	tree := "jobs[_class,name,url,color,description,buildable,lastBuild[number,result,url]]"
	path := "/api/json?tree=" + url.QueryEscape(tree)
	if prefix != "" {
		path = "/job/" + JobPathSegments(prefix) + path
	}
	var raw struct {
		Jobs []struct {
			Class       string `json:"_class"`
			Name        string `json:"name"`
			URL         string `json:"url"`
			Color       string `json:"color"`
			Description string `json:"description"`
			Buildable   bool   `json:"buildable"`
			LastBuild   *struct {
				Number int    `json:"number"`
				Result string `json:"result"`
			} `json:"lastBuild"`
		} `json:"jobs"`
	}
	if err := client.GetJSON(ctx, path, &raw); err != nil {
		return nil, err
	}
	out := make([]JobDTO, 0, len(raw.Jobs))
	for _, j := range raw.Jobs {
		name := j.Name
		if prefix != "" {
			name = prefix + "/" + j.Name
		}
		dto := JobDTO{
			Class: j.Class, Name: name, URL: j.URL,
			Color: j.Color, Description: j.Description, Buildable: j.Buildable,
		}
		if j.LastBuild != nil {
			dto.LastBuild = &BuildRef{Number: j.LastBuild.Number, Result: j.LastBuild.Result}
		}
		out = append(out, dto)
	}
	return out, nil
}

// ListJobsRecursive lists jobs under prefix ("" for root) and descends into
// any folder-class items concurrently. A subfolder that fails to list is
// skipped (contributes no jobs) rather than failing the whole call.
func ListJobsRecursive(ctx context.Context, client *api.Client, prefix string) ([]JobDTO, error) {
	jobs, err := listJobsAt(ctx, client, prefix)
	if err != nil {
		return nil, err
	}

	var folders []string
	for _, j := range jobs {
		if strings.Contains(strings.ToLower(j.Class), "folder") {
			folders = append(folders, j.Name)
		}
	}
	if len(folders) == 0 {
		return jobs, nil
	}

	var wg sync.WaitGroup
	results := make([][]JobDTO, len(folders))
	for i, folderName := range folders {
		wg.Add(1)
		go func(i int, folderName string) {
			defer wg.Done()
			children, err := ListJobsRecursive(ctx, client, folderName)
			if err != nil {
				return
			}
			results[i] = children
		}(i, folderName)
	}
	wg.Wait()

	for _, children := range results {
		jobs = append(jobs, children...)
	}
	return jobs, nil
}

// GetJob fetches a single job. Returns (nil, nil) on 404.
func GetJob(ctx context.Context, client *api.Client, name string) (*JobDTO, error) {
	var raw struct {
		Class       string `json:"_class"`
		Name        string `json:"name"`
		URL         string `json:"url"`
		Color       string `json:"color"`
		Description string `json:"description"`
		Buildable   bool   `json:"buildable"`
		LastBuild   *struct {
			Number int    `json:"number"`
			Result string `json:"result"`
		} `json:"lastBuild"`
	}
	err := client.GetJSON(ctx,
		"/job/"+JobPathSegments(name)+"/api/json?tree=_class,name,url,color,description,buildable,lastBuild[number,result,url]",
		&raw)
	if err != nil {
		if strings.Contains(err.Error(), "404") {
			return nil, nil
		}
		return nil, err
	}
	dto := &JobDTO{
		Class: raw.Class, Name: raw.Name, URL: raw.URL,
		Color: raw.Color, Description: raw.Description, Buildable: raw.Buildable,
	}
	if raw.LastBuild != nil {
		dto.LastBuild = &BuildRef{Number: raw.LastBuild.Number, Result: raw.LastBuild.Result}
	}
	return dto, nil
}

// DeleteJob sends doDelete for the named job.
func DeleteJob(ctx context.Context, client *api.Client, name string) error {
	return client.PostForm(ctx, "/job/"+JobPathSegments(name)+"/doDelete", nil)
}

// GetJobConfigXML fetches the raw config.xml for a job.
func GetJobConfigXML(ctx context.Context, client *api.Client, name string) (string, error) {
	resp, err := client.Do(ctx, "GET", "/job/"+JobPathSegments(name)+"/config.xml", nil, "")
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

// ConfigSummary is a best-effort, human-readable digest of a job's config.xml.
// Fields default to "-" when they can't be determined; parse failures never
// fail the calling command.
type ConfigSummary struct {
	Description  string
	Node         string
	Schedule     string
	ShellCmd     string
	Chdir        string
	Email        string
	EmailCond    string
	EmailKeyword string
	EmailRegex   string
	Params       string
}

var pipelineAgentLabelRe = regexp.MustCompile(`agent\s*\{\s*label\s*['"]([^'"]+)['"]`)
var chdirPrefixRe = regexp.MustCompile(`^cd (\S+) && `)

// GetJobConfigSummary fetches and parses a job's config.xml into a
// ConfigSummary. On any error, returns a summary of all "-" values.
func GetJobConfigSummary(ctx context.Context, client *api.Client, name string) (ConfigSummary, error) {
	dash := ConfigSummary{Description: "-", Node: "-", Schedule: "-", ShellCmd: "-", Chdir: "-",
		Email: "-", EmailCond: "-", EmailKeyword: "-", EmailRegex: "-", Params: "-"}

	xmlStr, err := GetJobConfigXML(ctx, client, name)
	if err != nil {
		return dash, nil
	}

	s := dash
	if v := extractTag(xmlStr, "description"); v != "" {
		s.Description = v
	}
	if v := extractSchedule(xmlStr); v != "" {
		s.Schedule = v
	}
	if v := extractEmailRecipients(xmlStr); v != "" {
		s.Email = v
		kw, re := extractEmailKeywordsRegex(xmlStr)
		if cond := extractEmailCond(xmlStr, kw, re); cond != "" {
			s.EmailCond = cond
		}
		if len(kw) > 0 {
			s.EmailKeyword = strings.Join(kw, ",")
		}
		if re != "" {
			s.EmailRegex = re
		}
	}
	if params := extractParamDefs(xmlStr); len(params) > 0 {
		s.Params = strings.Join(params, ",")
	}

	if strings.Contains(xmlStr, "<flow-definition") {
		script := extractPipelineScript(xmlStr)
		if m := pipelineAgentLabelRe.FindStringSubmatch(script); m != nil {
			s.Node = m[1]
		}
	} else {
		if v := extractTag(xmlStr, "assignedNode"); v != "" {
			s.Node = v
		}
		if v := extractShellCommand(xmlStr); v != "" {
			s.ShellCmd = v
			if m := chdirPrefixRe.FindStringSubmatch(v); m != nil {
				s.Chdir = m[1]
			}
		}
	}

	return s, nil
}

// GetBuildHistory returns up to count recent builds for jobName.
func GetBuildHistory(ctx context.Context, client *api.Client, name string, count int) ([]BuildDTO, error) {
	var raw struct {
		Builds []struct {
			Number    int    `json:"number"`
			Result    string `json:"result"`
			Building  bool   `json:"building"`
			Duration  int64  `json:"duration"`
			Timestamp int64  `json:"timestamp"`
		} `json:"builds"`
	}
	path := fmt.Sprintf("/job/%s/api/json?tree=builds[number,result,building,duration,timestamp,url]{0,%d}", JobPathSegments(name), count)
	if err := client.GetJSON(ctx, path, &raw); err != nil {
		return nil, err
	}
	out := make([]BuildDTO, len(raw.Builds))
	for i, b := range raw.Builds {
		out[i] = BuildDTO{Number: b.Number, Result: b.Result, Building: b.Building, Duration: b.Duration, Timestamp: b.Timestamp}
	}
	return out, nil
}

// GetLastBuildNumber returns the number of the most recent build.
func GetLastBuildNumber(ctx context.Context, client *api.Client, name string) (int, error) {
	var raw struct {
		LastBuild *struct {
			Number int `json:"number"`
		} `json:"lastBuild"`
	}
	if err := client.GetJSON(ctx, "/job/"+JobPathSegments(name)+"/api/json?tree=lastBuild[number]", &raw); err != nil {
		return 0, err
	}
	if raw.LastBuild == nil {
		return 0, fmt.Errorf("no builds")
	}
	return raw.LastBuild.Number, nil
}

// GetBuildDetail fetches a single build by number.
func GetBuildDetail(ctx context.Context, client *api.Client, name string, num int) (*BuildDTO, error) {
	var raw struct {
		Number    int    `json:"number"`
		Result    string `json:"result"`
		Building  bool   `json:"building"`
		Duration  int64  `json:"duration"`
		Timestamp int64  `json:"timestamp"`
	}
	if err := client.GetJSON(ctx, fmt.Sprintf("/job/%s/%d/api/json", JobPathSegments(name), num), &raw); err != nil {
		return nil, err
	}
	return &BuildDTO{Number: raw.Number, Result: raw.Result, Building: raw.Building, Duration: raw.Duration, Timestamp: raw.Timestamp}, nil
}

// StreamBuildLog fetches progressive log text using X-Text-Size header.
func StreamBuildLog(ctx context.Context, client *api.Client, name string, buildNum int, start int64) (text string, newOffset int64, hasMore bool, err error) {
	path := fmt.Sprintf("/job/%s/%d/logText/progressiveText?start=%d", JobPathSegments(name), buildNum, start)
	resp, err := client.Do(ctx, "GET", path, nil, "")
	if err != nil {
		return "", start, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return "", start, false, fmt.Errorf("log stream: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	b, _ := io.ReadAll(resp.Body)
	text = string(b)
	newOffset = start + int64(len(b))
	if v := resp.Header.Get("X-Text-Size"); v != "" {
		if n, e := strconv.ParseInt(v, 10, 64); e == nil {
			newOffset = n
		}
	}
	hasMore = resp.Header.Get("X-More-Data") == "true"
	return text, newOffset, hasMore, nil
}

// StreamLastBuildLog fetches progressive log text for the job's last build.
func StreamLastBuildLog(ctx context.Context, client *api.Client, name string, start int64) (text string, newOffset int64, hasMore bool, err error) {
	path := fmt.Sprintf("/job/%s/lastBuild/logText/progressiveText?start=%d", JobPathSegments(name), start)
	resp, err := client.Do(ctx, "GET", path, nil, "")
	if err != nil {
		return "", start, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return "", start, false, fmt.Errorf("log stream: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	b, _ := io.ReadAll(resp.Body)
	text = string(b)
	newOffset = start + int64(len(b))
	if v := resp.Header.Get("X-Text-Size"); v != "" {
		if n, e := strconv.ParseInt(v, 10, 64); e == nil {
			newOffset = n
		}
	}
	hasMore = resp.Header.Get("X-More-Data") == "true"
	return text, newOffset, hasMore, nil
}

// ListJobsInFolder lists jobs directly under folder (non-recursive). Use "" for root.
func ListJobsInFolder(ctx context.Context, client *api.Client, folder string) ([]JobDTO, error) {
	return listJobsAt(ctx, client, folder)
}

// GetPipelineScript fetches the Jenkinsfile/script body for a pipeline job.
// Returns "" when not found (non-pipeline job or no script configured).
func GetPipelineScript(ctx context.Context, client *api.Client, name string) (string, error) {
	xml, err := GetJobConfigXML(ctx, client, name)
	if err != nil {
		return "", err
	}
	// Extract <script>…</script> from the pipeline config XML.
	const open, close = "<script>", "</script>"
	start := strings.Index(xml, open)
	if start < 0 {
		return "", nil
	}
	start += len(open)
	end := strings.Index(xml[start:], close)
	if end < 0 {
		return xml[start:], nil
	}
	return xml[start : start+end], nil
}

// CopyJob copies src to dst (Jenkins copy-from API).
func CopyJob(ctx context.Context, client *api.Client, src, dst string) error {
	path := fmt.Sprintf("/createItem?name=%s&mode=copy&from=%s",
		url.QueryEscape(dst), url.QueryEscape(src))
	resp, err := client.Do(ctx, "POST", path, nil, "")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("copy job: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}

// MoveJob moves src to dst folder using the job-dsl moveJob approach.
// dst is the fully-qualified destination path (folder/newname).
func MoveJob(ctx context.Context, client *api.Client, src, dst string) error {
	path := fmt.Sprintf("/job/%s/move/move?destination=%%2F%s",
		JobPathSegments(src), url.QueryEscape(strings.ReplaceAll(dst, "/", "/")))
	resp, err := client.Do(ctx, "POST", path, nil, "")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("move job: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}
