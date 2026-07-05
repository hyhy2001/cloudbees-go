// Package cred — exported service layer for TUI and other consumers.
package cred

import (
	"context"
	"net/url"
	"strings"

	"bee/internal/api"
)

// CredDTO is the exported credential view.
type CredDTO struct {
	ID          string
	DisplayName string
	TypeName    string
	Scope       string
	Description string
}

// UserStoreSeg returns the Jenkins credential store path segment.
func UserStoreSeg(username, store string) string {
	return getUserSeg(username, store)
}

// ListCredentials fetches credentials from the given store.
func ListCredentials(ctx context.Context, client *api.Client, store, username string) ([]CredDTO, error) {
	seg := getUserSeg(username, store)
	var raw struct {
		Credentials []struct {
			ID          string `json:"id"`
			DisplayName string `json:"displayName"`
			TypeName    string `json:"typeName"`
			Scope       string `json:"scope"`
			Description string `json:"description"`
		} `json:"credentials"`
	}
	if err := client.GetJSON(ctx, seg+"/api/json?tree=credentials[id,typeName,description,scope,displayName]", &raw); err != nil {
		if strings.Contains(err.Error(), "404") {
			return nil, nil
		}
		return nil, err
	}
	out := make([]CredDTO, len(raw.Credentials))
	for i, c := range raw.Credentials {
		scope := c.Scope
		if scope == "" {
			scope = "GLOBAL" // API omits scope; match TS default
		}
		out[i] = CredDTO{ID: c.ID, DisplayName: c.DisplayName, TypeName: c.TypeName, Scope: scope, Description: c.Description}
	}
	return out, nil
}

// DeleteCredential deletes a credential by ID from the given store.
func DeleteCredential(ctx context.Context, client *api.Client, credID, username, store string) error {
	seg := getUserSeg(username, store)
	return client.PostForm(ctx, seg+"/credential/"+url.PathEscape(credID)+"/doDelete", map[string]string{})
}

// GetCredentialXML fetches the config.xml for a credential.
func GetCredentialXML(ctx context.Context, client *api.Client, credID, username, store string) (string, error) {
	return getCredentialXML(client, credID, username, store)
}

// ExtractUsername reads the <username> element from a credential's config.xml,
// or "" when the credential type has none (e.g. SecretText). Jenkins never
// returns the password/secret (write-only), so username is all a detail panel
// can safely show for a Username+Password credential.
func ExtractUsername(xmlStr string) string {
	return extractTagCred(xmlStr, "username")
}

// XMLEscape XML-escapes special characters.
func XMLEscape(s string) string {
	return xmlEscape(s)
}

// SetXMLElement replaces or inserts a simple XML element value.
func SetXMLElement(xmlStr, tag, value string) string {
	return setXMLElement(xmlStr, tag, value)
}

// CreateUsernamePasswordCredential creates a Username+Password credential.
func CreateUsernamePasswordCredential(ctx context.Context, client *api.Client, id, username, password, desc, scope, store, sessionUsername string) error {
	if scope == "" {
		scope = "GLOBAL"
	}
	seg := getUserSeg(sessionUsername, store)
	xml := buildUsernamePasswordXML(id, username, password, desc, scope)
	return client.PostXML(ctx, seg+"/createCredentials", xml)
}

// CreateSecretTextCredential creates a SecretText credential.
func CreateSecretTextCredential(ctx context.Context, client *api.Client, id, secret, desc, scope, store, sessionUsername string) error {
	if scope == "" {
		scope = "GLOBAL"
	}
	seg := getUserSeg(sessionUsername, store)
	xml := buildSecretTextXML(id, secret, desc, scope)
	return client.PostXML(ctx, seg+"/createCredentials", xml)
}

// UpdateCredentialField patches a single XML element value in an existing
// credential's config.xml. Suitable for description, username, etc. (Jenkins
// never echoes passwords, so password updates must come from the original XML
// template rather than patching the existing config).
func UpdateCredentialField(ctx context.Context, client *api.Client, credID, tag, value, sessionUsername, store string) error {
	xmlStr, err := getCredentialXML(client, credID, sessionUsername, store)
	if err != nil {
		return err
	}
	updated := setXMLElement(xmlStr, tag, xmlEscape(value))
	seg := getUserSeg(sessionUsername, store)
	return client.PostXML(ctx, seg+"/credential/"+url.PathEscape(credID)+"/config.xml", updated)
}
