// Package job — shared create/update actions used by both the CLI commands
// and the TUI job flow, so the two surfaces never duplicate create/merge/post
// logic (mirrors the TS CLI+TUI sharing one service layer).
package job

import (
	"context"
	"fmt"
	"net/url"

	"bee/internal/api"
)

// CreateFreestyleParams holds inputs for creating a Freestyle job.
type CreateFreestyleParams struct {
	Name, Folder                    string
	Description, Shell, Chdir, Node string
	Schedule                        string
	Email, EmailCond, EmailRegex    string
	EmailChanged, EmailCondChanged  bool
	EmailKeywords                   []string
	ParamDefs                       []string
}

// CreateFreestyleJob creates a new Freestyle job, erroring if one already
// exists at that name.
func CreateFreestyleJob(ctx context.Context, client *api.Client, p CreateFreestyleParams) error {
	if existing, _ := GetJob(ctx, client, p.Name); existing != nil {
		return fmt.Errorf("job %q already exists", p.Name)
	}
	if err := validateEmailFilterFlags(p.Email, p.EmailChanged, p.EmailKeywords, p.EmailRegex, p.EmailCond, p.EmailCondChanged); err != nil {
		return err
	}
	shell := p.Shell
	if p.Chdir != "" && shell != "" {
		shell = "cd " + p.Chdir + " && " + shell
	}
	xmlBody := buildFreestyleXML(p.Description, shell, p.Node, p.Schedule, p.Email, p.EmailCond, p.EmailKeywords, p.EmailRegex, p.ParamDefs)
	path := "/createItem?name=" + url.QueryEscape(p.Name)
	if p.Folder != "" {
		path = "/job/" + JobPathSegments(p.Folder) + path
	}
	return client.PostXML(ctx, path, xmlBody)
}

// CreatePipelineParams holds inputs for creating a Pipeline job. Script may
// be an inline Groovy script or a path to one — resolved via ResolveScript.
type CreatePipelineParams struct {
	Name, Folder                   string
	Description, Script, Node      string
	Schedule                       string
	Email, EmailCond, EmailRegex   string
	EmailChanged, EmailCondChanged bool
	EmailKeywords                  []string
	ParamDefs                      []string
}

// CreatePipelineJob creates a new Pipeline job, erroring if one already
// exists at that name. Resolves the script, injects the node/agent label,
// auto-detects parameters, validates against Jenkins, then creates.
func CreatePipelineJob(ctx context.Context, client *api.Client, p CreatePipelineParams) error {
	if existing, _ := GetJob(ctx, client, p.Name); existing != nil {
		return fmt.Errorf("job %q already exists", p.Name)
	}
	if err := validateEmailFilterFlags(p.Email, p.EmailChanged, p.EmailKeywords, p.EmailRegex, p.EmailCond, p.EmailCondChanged); err != nil {
		return err
	}
	origScript, err := ResolveScript(p.Script)
	if err != nil {
		return err
	}
	finalScript := injectAgent(origScript, p.Node)
	paramDefs := mergeParamDefs(parseParametersFromScript(finalScript), p.ParamDefs)
	if err := ValidatePipelineScript(ctx, client, origScript); err != nil {
		return err
	}
	xmlBody := buildPipelineXML(p.Description, finalScript, p.Schedule, p.Email, p.EmailCond, p.EmailKeywords, p.EmailRegex, paramDefs)
	path := "/createItem?name=" + url.QueryEscape(p.Name)
	if p.Folder != "" {
		path = "/job/" + JobPathSegments(p.Folder) + path
	}
	return client.PostXML(ctx, path, xmlBody)
}

// CreateFolderJob creates a new Folder job, erroring if one already exists
// at that name. (Named CreateFolderJob to avoid colliding with
// node.CreateFolderRequest / cred package folder helpers.)
func CreateFolderJob(ctx context.Context, client *api.Client, name, folder, description string) error {
	if existing, _ := GetJob(ctx, client, name); existing != nil {
		return fmt.Errorf("job %q already exists", name)
	}
	xmlBody := buildFolderXML(description)
	path := "/createItem?name=" + url.QueryEscape(name)
	if folder != "" {
		path = "/job/" + JobPathSegments(folder) + path
	}
	return client.PostXML(ctx, path, xmlBody)
}

// UpdateFreestyleJob reads the existing config.xml, merges only the touched
// fields in f, and posts it back.
func UpdateFreestyleJob(ctx context.Context, client *api.Client, name string, f FreestyleUpdateFields) error {
	xmlStr, err := GetJobConfigXML(ctx, client, name)
	if err != nil {
		return err
	}
	merged, err := MergeFreestyleConfig(xmlStr, f)
	if err != nil {
		return err
	}
	return client.PostXML(ctx, "/job/"+JobPathSegments(name)+"/config.xml", merged)
}

// UpdatePipelineJob reads the existing config.xml, merges only the touched
// fields in f, and posts it back. Callers must resolve/inject/validate
// f.Script (via ResolveScript/injectAgent/ValidatePipelineScript) before
// calling, same as the CLI update command does.
func UpdatePipelineJob(ctx context.Context, client *api.Client, name string, f PipelineUpdateFields) error {
	xmlStr, err := GetJobConfigXML(ctx, client, name)
	if err != nil {
		return err
	}
	merged, err := MergePipelineConfig(xmlStr, f)
	if err != nil {
		return err
	}
	return client.PostXML(ctx, "/job/"+JobPathSegments(name)+"/config.xml", merged)
}

// ListControlledAgents exposes listControlledAgents for the TUI's
// GrantListOverlay (folder → agents vantage point).
func ListControlledAgents(client *api.Client, folderName string) ([]controlledAgentGrant, error) {
	return listControlledAgents(client, folderName)
}

// ControlledAgentGrant is the exported alias TUI code should reference.
type ControlledAgentGrant = controlledAgentGrant

// TriggerBuild runs a job (no parameters). Use nil for params.
func TriggerBuild(ctx context.Context, client *api.Client, name string, params map[string]string) error {
	path := "/job/" + JobPathSegments(name) + "/build"
	return client.PostForm(ctx, path, nil)
}

// StopBuild stops a specific build number.
func StopBuild(ctx context.Context, client *api.Client, name string, buildNum int) error {
	path := fmt.Sprintf("/job/%s/%d/stop", JobPathSegments(name), buildNum)
	return client.PostForm(ctx, path, nil)
}
