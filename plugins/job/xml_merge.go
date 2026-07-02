// Package job — config.xml merge helpers for `job update`.
//
// job update must patch only the fields the caller explicitly passed,
// leaving everything else in the existing config.xml untouched. This file
// implements that as targeted string surgery (freestyle) or extract-merge-
// rebuild (pipeline, mirroring how its builder already works).
package job

import (
	"fmt"
	"regexp"
	"strings"
)

// FreestyleUpdateFields holds the update-time flag values. A nil pointer
// means "flag not passed — leave this field untouched in the existing XML".
type FreestyleUpdateFields struct {
	Description        *string
	Node               *string
	Shell              *string
	Chdir              *string
	Schedule           *string
	Email              *string
	EmailCond          *string
	EmailKeywords      *[]string
	EmailRegex         *string
	ClearEmailKeywords bool
	ClearEmailRegex    bool
	ParamDefs          *[]string
	ClearParams        bool
}

// PipelineUpdateFields mirrors FreestyleUpdateFields for pipeline jobs.
type PipelineUpdateFields struct {
	Description        *string
	Script             *string
	Schedule           *string
	Email              *string
	EmailCond          *string
	EmailKeywords      *[]string
	EmailRegex         *string
	ClearEmailKeywords bool
	ClearEmailRegex    bool
	ParamDefs          *[]string
	ClearParams        bool
}

// MergeFreestyleConfig patches only the touched fields into the existing
// freestyle config.xml, via targeted string surgery (matches TS behavior:
// unset flags never touch the surrounding XML).
func MergeFreestyleConfig(xmlStr string, f FreestyleUpdateFields) (string, error) {
	const rootClose = "</project>"

	if f.Description != nil {
		xmlStr = replaceOrInsertElement(xmlStr, "description", *f.Description, rootClose)
	}

	if f.Node != nil {
		if *f.Node != "" {
			xmlStr = replaceOrInsertElement(xmlStr, "assignedNode", *f.Node, rootClose)
			xmlStr = replaceOrInsertElement(xmlStr, "canRoam", "false", rootClose)
		} else {
			xmlStr = removeElement(xmlStr, "assignedNode")
			xmlStr = replaceOrInsertElement(xmlStr, "canRoam", "true", rootClose)
		}
	}

	if f.Shell != nil || f.Chdir != nil {
		shell := ""
		if f.Shell != nil {
			shell = *f.Shell
		} else {
			shell = extractShellCommand(xmlStr)
		}
		chdir := ""
		if f.Chdir != nil {
			chdir = *f.Chdir
		}
		if chdir != "" && shell != "" {
			shell = "cd " + chdir + " && " + shell
		}
		xmlStr = patchShellCommand(xmlStr, shell)
	}

	if f.Schedule != nil {
		xmlStr = removeTimerTrigger(xmlStr)
		if *f.Schedule != "" {
			xmlStr = insertTimerTrigger(xmlStr, *f.Schedule, rootClose)
		}
	}

	if emailFieldsTouched(f.Email, f.EmailCond, f.EmailKeywords, f.EmailRegex, f.ClearEmailKeywords, f.ClearEmailRegex) {
		var err error
		xmlStr, err = mergeEmailPublisher(xmlStr, f.Email, f.EmailCond, f.EmailKeywords, f.EmailRegex, f.ClearEmailKeywords, f.ClearEmailRegex, rootClose)
		if err != nil {
			return "", err
		}
	}

	if f.ParamDefs != nil {
		xmlStr = replaceProperties(xmlStr, *f.ParamDefs)
	} else if f.ClearParams {
		xmlStr = replaceProperties(xmlStr, nil)
	}

	return xmlStr, nil
}

// MergePipelineConfig extracts the current script/description/email/schedule
// from the existing config.xml, merges in only the touched fields, and
// rebuilds the XML via buildPipelineXML — matching TS's approach for
// pipeline jobs (full rebuild, not string surgery, since pipeline XML is
// small and simple).
func MergePipelineConfig(xmlStr string, f PipelineUpdateFields) (string, error) {
	desc := extractTag(xmlStr, "description")
	if f.Description != nil {
		desc = *f.Description
	}

	script := extractPipelineScript(xmlStr)
	if f.Script != nil {
		script = *f.Script
	}

	schedule := extractSchedule(xmlStr)
	if f.Schedule != nil {
		schedule = *f.Schedule
	}

	email, cond, keywords, regexVal, err := mergeEmailValues(xmlStr, f.Email, f.EmailCond, f.EmailKeywords, f.EmailRegex, f.ClearEmailKeywords, f.ClearEmailRegex)
	if err != nil {
		return "", err
	}

	paramDefs := extractParamDefs(xmlStr)
	if f.ParamDefs != nil {
		paramDefs = *f.ParamDefs
	} else if f.ClearParams {
		paramDefs = nil
	}

	return buildPipelineXML(desc, script, schedule, email, cond, keywords, regexVal, paramDefs), nil
}

// ── Generic element string-surgery (no XML parser — mirrors TS's regex
//    string-surgery so formatting/CDATA of untouched parts is preserved) ──

func replaceOrInsertElement(xmlStr, tag, value, rootCloseTag string) string {
	escaped := xmlEscape(value)
	open := "<" + tag + ">"
	closeTag := "</" + tag + ">"
	if i := strings.Index(xmlStr, open); i >= 0 {
		if j := strings.Index(xmlStr[i:], closeTag); j >= 0 {
			return xmlStr[:i+len(open)] + escaped + xmlStr[i+j:]
		}
	}
	selfClose := "<" + tag + "/>"
	if i := strings.Index(xmlStr, selfClose); i >= 0 {
		return xmlStr[:i] + open + escaped + closeTag + xmlStr[i+len(selfClose):]
	}
	if idx := strings.LastIndex(xmlStr, rootCloseTag); idx >= 0 {
		return xmlStr[:idx] + open + escaped + closeTag + xmlStr[idx:]
	}
	return xmlStr + open + escaped + closeTag
}

func removeElement(xmlStr, tag string) string {
	open := "<" + tag + ">"
	closeTag := "</" + tag + ">"
	if i := strings.Index(xmlStr, open); i >= 0 {
		if j := strings.Index(xmlStr[i:], closeTag); j >= 0 {
			end := i + j + len(closeTag)
			return xmlStr[:i] + xmlStr[end:]
		}
	}
	selfClose := "<" + tag + "/>"
	if i := strings.Index(xmlStr, selfClose); i >= 0 {
		return xmlStr[:i] + xmlStr[i+len(selfClose):]
	}
	return xmlStr
}

func extractTag(xmlStr, tag string) string {
	open := "<" + tag + ">"
	closeTag := "</" + tag + ">"
	i := strings.Index(xmlStr, open)
	if i < 0 {
		return ""
	}
	rest := xmlStr[i+len(open):]
	j := strings.Index(rest, closeTag)
	if j < 0 {
		return ""
	}
	return xmlUnescape(rest[:j])
}

func xmlUnescape(s string) string {
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	s = strings.ReplaceAll(s, "&quot;", `"`)
	s = strings.ReplaceAll(s, "&amp;", "&")
	return s
}

// ── Shell command (3-tier: CDATA, plain, or inject a new <builders> block) ──

func extractShellCommand(xmlStr string) string {
	cdataOpen := "<command><![CDATA["
	if i := strings.Index(xmlStr, cdataOpen); i >= 0 {
		rest := xmlStr[i+len(cdataOpen):]
		if j := strings.Index(rest, "]]></command>"); j >= 0 {
			return rest[:j]
		}
	}
	return extractTag(xmlStr, "command")
}

func patchShellCommand(xmlStr, shell string) string {
	cdataOpen := "<command><![CDATA["
	cdataClose := "]]></command>"
	if i := strings.Index(xmlStr, cdataOpen); i >= 0 {
		rest := xmlStr[i+len(cdataOpen):]
		if j := strings.Index(rest, cdataClose); j >= 0 {
			return xmlStr[:i+len(cdataOpen)] + shell + rest[j:]
		}
	}
	if i := strings.Index(xmlStr, "<command>"); i >= 0 {
		rest := xmlStr[i+len("<command>"):]
		if j := strings.Index(rest, "</command>"); j >= 0 {
			return xmlStr[:i+len("<command>")] + xmlEscape(shell) + rest[j:]
		}
	}
	block := "<builders><hudson.tasks.Shell><command>" + xmlEscape(shell) + "</command></hudson.tasks.Shell></builders>"
	if idx := strings.LastIndex(xmlStr, "</project>"); idx >= 0 {
		return xmlStr[:idx] + block + xmlStr[idx:]
	}
	return xmlStr + block
}

// ── Cron schedule (Jenkins supports only one TimerTrigger) ──

func removeTimerTrigger(xmlStr string) string {
	open := "<hudson.triggers.TimerTrigger>"
	closeTag := "</hudson.triggers.TimerTrigger>"
	if i := strings.Index(xmlStr, open); i >= 0 {
		if j := strings.Index(xmlStr[i:], closeTag); j >= 0 {
			end := i + j + len(closeTag)
			return xmlStr[:i] + xmlStr[end:]
		}
	}
	return xmlStr
}

func insertTimerTrigger(xmlStr, schedule, rootCloseTag string) string {
	block := "<hudson.triggers.TimerTrigger><spec>" + xmlEscape(schedule) + "</spec></hudson.triggers.TimerTrigger>"
	if i := strings.Index(xmlStr, "<triggers/>"); i >= 0 {
		return xmlStr[:i] + "<triggers>" + block + "</triggers>" + xmlStr[i+len("<triggers/>"):]
	}
	if i := strings.Index(xmlStr, "<triggers>"); i >= 0 {
		return xmlStr[:i+len("<triggers>")] + block + xmlStr[i+len("<triggers>"):]
	}
	if idx := strings.LastIndex(xmlStr, rootCloseTag); idx >= 0 {
		return xmlStr[:idx] + "<triggers>" + block + "</triggers>" + xmlStr[idx:]
	}
	return xmlStr
}

func extractSchedule(xmlStr string) string {
	open := "<hudson.triggers.TimerTrigger><spec>"
	if i := strings.Index(xmlStr, open); i >= 0 {
		rest := xmlStr[i+len(open):]
		if j := strings.Index(rest, "</spec>"); j >= 0 {
			return xmlUnescape(rest[:j])
		}
	}
	return ""
}

// ── Pipeline script ──

func extractPipelineScript(xmlStr string) string {
	return extractTag(xmlStr, "script")
}

// ── Build parameters (<properties> block is swapped wholesale) ──

func replaceProperties(xmlStr string, paramDefs []string) string {
	newProps := buildParametersPropertyXML(paramDefs)
	if i := strings.Index(xmlStr, "<properties/>"); i >= 0 {
		return xmlStr[:i] + newProps + xmlStr[i+len("<properties/>"):]
	}
	open := "<properties>"
	closeTag := "</properties>"
	if i := strings.Index(xmlStr, open); i >= 0 {
		if j := strings.Index(xmlStr[i:], closeTag); j >= 0 {
			end := i + j + len(closeTag)
			return xmlStr[:i] + newProps + xmlStr[end:]
		}
	}
	return xmlStr
}

func extractParamDefs(xmlStr string) []string {
	var out []string
	const marker = "<hudson.model.StringParameterDefinition>"
	const endMarker = "</hudson.model.StringParameterDefinition>"
	rest := xmlStr
	for {
		i := strings.Index(rest, marker)
		if i < 0 {
			break
		}
		rest = rest[i+len(marker):]
		j := strings.Index(rest, endMarker)
		if j < 0 {
			break
		}
		block := rest[:j]
		rest = rest[j+len(endMarker):]
		name := extractTag(block, "name")
		if name == "" {
			continue
		}
		if def := extractTag(block, "defaultValue"); def != "" {
			out = append(out, name+"="+def)
		} else {
			out = append(out, name)
		}
	}
	return out
}

// ── Email publisher (the most involved merge — recipients, condition,
//    keyword/regex filters all interact) ──

func emailFieldsTouched(email, cond *string, keywords *[]string, regex *string, clearKw, clearRe bool) bool {
	return email != nil || cond != nil || keywords != nil || regex != nil || clearKw || clearRe
}

// validateEmailFilterFlags enforces the create/update-time rules for email
// filters: keywords/regex require a recipient email, and (on update, where
// emailChanged distinguishes "explicitly touched" from "left at default")
// an explicit --email-cond also requires a recipient email.
func validateEmailFilterFlags(email string, emailChanged bool, keywords []string, regex, emailCond string, condChanged bool) error {
	hasFilter := len(keywords) > 0 || regex != ""
	emailEmpty := strings.TrimSpace(email) == ""

	if hasFilter && emailEmpty {
		if emailChanged {
			return fmt.Errorf("cannot set email filters when removing recipient email")
		}
		return fmt.Errorf("email filters require recipient email. Provide --email")
	}
	if condChanged && emailCond != "" && emailEmpty {
		return fmt.Errorf("email condition requires recipient email. Provide --email")
	}
	if regex != "" {
		if _, err := regexp.Compile(regex); err != nil {
			return fmt.Errorf("invalid --email-regex: %w", err)
		}
	}
	return nil
}

// extractEmailRecipients reads the current <recipientList> value.
func extractEmailRecipients(xmlStr string) string {
	return extractTag(xmlStr, "recipientList")
}

// extractEmailKeywordsRegex parses the keyword/regex literals out of the
// presend Groovy script we generate ourselves (buildEmailPresendScript) —
// safe to round-trip since we control the exact format.
func extractEmailKeywordsRegex(xmlStr string) (keywords []string, regexVal string) {
	const kwMarker = "def _bee_keywords = ["
	if i := strings.Index(xmlStr, kwMarker); i >= 0 {
		rest := xmlStr[i+len(kwMarker):]
		if j := strings.IndexByte(rest, ']'); j >= 0 {
			for _, part := range strings.Split(rest[:j], ",") {
				part = strings.TrimSpace(part)
				part = strings.TrimPrefix(part, `"`)
				part = strings.TrimSuffix(part, `"`)
				part = strings.ReplaceAll(part, `\"`, `"`)
				if part != "" {
					keywords = append(keywords, part)
				}
			}
		}
	}
	const reMarker = "def _bee_regex = "
	if i := strings.Index(xmlStr, reMarker); i >= 0 {
		rest := xmlStr[i+len(reMarker):]
		if j := strings.IndexByte(rest, '\n'); j >= 0 {
			val := strings.TrimSpace(rest[:j])
			if val != "null" {
				val = strings.TrimPrefix(val, `"`)
				val = strings.TrimSuffix(val, `"`)
				regexVal = strings.ReplaceAll(val, `\"`, `"`)
			}
		}
	}
	return keywords, regexVal
}

func extractEmailCond(xmlStr string, keywords []string, regexVal string) string {
	hasFailure := strings.Contains(xmlStr, "FailureTrigger")
	hasSuccess := strings.Contains(xmlStr, "SuccessTrigger")
	hasFilter := len(keywords) > 0 || regexVal != ""
	switch {
	case hasFailure && hasSuccess:
		if hasFilter {
			return "custom"
		}
		return "always"
	case hasSuccess:
		return "success"
	case hasFailure:
		return "failed"
	default:
		return ""
	}
}

// mergeEmailValues computes the target email/cond/keywords/regex from the
// current XML plus whichever fields were explicitly touched, applying the
// same validation rules as TS (filters require an email; email-cond
// requires an email).
func mergeEmailValues(xmlStr string, email, cond *string, keywords *[]string, regexPtr *string, clearKw, clearRe bool) (targetEmail, targetCond string, targetKeywords []string, targetRegex string, err error) {
	curEmail := extractEmailRecipients(xmlStr)
	curKeywords, curRegex := extractEmailKeywordsRegex(xmlStr)
	curCond := extractEmailCond(xmlStr, curKeywords, curRegex)

	targetEmail = curEmail
	if email != nil {
		targetEmail = *email
	}
	targetCond = curCond
	if cond != nil {
		targetCond = *cond
	}
	targetKeywords = curKeywords
	if keywords != nil {
		targetKeywords = *keywords
	} else if clearKw {
		targetKeywords = nil
	}
	targetRegex = curRegex
	if regexPtr != nil {
		targetRegex = *regexPtr
	} else if clearRe {
		targetRegex = ""
	}

	if strings.TrimSpace(targetEmail) == "" {
		if len(targetKeywords) > 0 || targetRegex != "" {
			return "", "", nil, "", fmt.Errorf("cannot set email filters when removing recipient email")
		}
		if cond != nil {
			return "", "", nil, "", fmt.Errorf("email condition requires recipient email; provide --email")
		}
		return "", "", nil, "", nil
	}

	if targetRegex != "" {
		if _, e := regexp.Compile(targetRegex); e != nil {
			return "", "", nil, "", fmt.Errorf("invalid --email-regex: %w", e)
		}
	}

	return targetEmail, targetCond, targetKeywords, targetRegex, nil
}

func mergeEmailPublisher(xmlStr string, email, cond *string, keywords *[]string, regexPtr *string, clearKw, clearRe bool, rootClose string) (string, error) {
	targetEmail, targetCond, targetKeywords, targetRegex, err := mergeEmailValues(xmlStr, email, cond, keywords, regexPtr, clearKw, clearRe)
	if err != nil {
		return "", err
	}

	xmlStr = removeEmailPublisher(xmlStr)

	if strings.TrimSpace(targetEmail) == "" {
		return strings.ReplaceAll(xmlStr, "<publishers></publishers>", "<publishers/>"), nil
	}

	emailXML := buildEmailPublisherXML(targetEmail, targetCond, targetKeywords, targetRegex)
	return setPublishersContent(xmlStr, emailXML, rootClose), nil
}

func removeEmailPublisher(xmlStr string) string {
	const openPrefix = "<hudson.plugins.emailext.ExtendedEmailPublisher"
	const closeTag = "</hudson.plugins.emailext.ExtendedEmailPublisher>"
	i := strings.Index(xmlStr, openPrefix)
	if i < 0 {
		return xmlStr
	}
	j := strings.Index(xmlStr[i:], closeTag)
	if j < 0 {
		return xmlStr
	}
	end := i + j + len(closeTag)
	return xmlStr[:i] + xmlStr[end:]
}

func setPublishersContent(xmlStr, content, rootCloseTag string) string {
	if i := strings.Index(xmlStr, "<publishers></publishers>"); i >= 0 {
		return xmlStr[:i] + "<publishers>" + content + "</publishers>" + xmlStr[i+len("<publishers></publishers>"):]
	}
	if i := strings.Index(xmlStr, "<publishers/>"); i >= 0 {
		return xmlStr[:i] + "<publishers>" + content + "</publishers>" + xmlStr[i+len("<publishers/>"):]
	}
	if i := strings.Index(xmlStr, "<publishers>"); i >= 0 {
		return xmlStr[:i+len("<publishers>")] + content + xmlStr[i+len("<publishers>"):]
	}
	if idx := strings.LastIndex(xmlStr, rootCloseTag); idx >= 0 {
		return xmlStr[:idx] + "<publishers>" + content + "</publishers>" + xmlStr[idx:]
	}
	return xmlStr
}
