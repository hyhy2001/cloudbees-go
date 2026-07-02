// Package job — pure list mutators behind the TUI ParamListEditor overlay.
// Framework-free so they unit-test without a TTY (same pattern as schedule.go).
package job

import "strings"

// StringParamDef is one row in the build-parameter editor. Description is a
// UI-only convenience (not persisted to config.xml — Jenkins' XML has no
// field for it), kept here only so the editor can round-trip it in-session.
type StringParamDef struct {
	Name, DefaultValue, Description string
}

// AddParam appends a blank row; returns a new slice.
func AddParam(params []StringParamDef) []StringParamDef {
	return append(append([]StringParamDef{}, params...), StringParamDef{})
}

// RemoveParam drops the row at index (no-op if out of range); returns a new slice.
func RemoveParam(params []StringParamDef, index int) []StringParamDef {
	out := append([]StringParamDef{}, params...)
	if index < 0 || index >= len(out) {
		return out
	}
	return append(out[:index], out[index+1:]...)
}

// UpdateParam patches one field of the row at index; returns a new slice.
func UpdateParam(params []StringParamDef, index int, field, value string) []StringParamDef {
	out := append([]StringParamDef{}, params...)
	if index < 0 || index >= len(out) {
		return out
	}
	switch field {
	case "name":
		out[index].Name = value
	case "defaultValue":
		out[index].DefaultValue = value
	case "description":
		out[index].Description = value
	}
	return out
}

// FinalizeParams drops rows with a blank name and trims names.
func FinalizeParams(params []StringParamDef) []StringParamDef {
	out := make([]StringParamDef, 0, len(params))
	for _, p := range params {
		p.Name = strings.TrimSpace(p.Name)
		if p.Name != "" {
			out = append(out, p)
		}
	}
	return out
}

// ParamDefsToStrings converts to the "NAME"/"NAME=default" format the
// service layer (buildParametersPropertyXML, ParamDefs flags) already uses.
func ParamDefsToStrings(params []StringParamDef) []string {
	out := make([]string, len(params))
	for i, p := range params {
		if p.DefaultValue == "" {
			out[i] = p.Name
		} else {
			out[i] = p.Name + "=" + p.DefaultValue
		}
	}
	return out
}

// ParamDefsFromStrings parses the service layer's "NAME"/"NAME=default"
// format back into editable rows (description is always blank — it isn't
// persisted, so there's nothing to recover it from).
func ParamDefsFromStrings(defs []string) []StringParamDef {
	out := make([]StringParamDef, len(defs))
	for i, d := range defs {
		name, def, _ := strings.Cut(d, "=")
		out[i] = StringParamDef{Name: strings.TrimSpace(name), DefaultValue: def}
	}
	return out
}
