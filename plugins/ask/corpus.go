// Package ask implements the `bee ask` RAG pipeline (BM25 + LM).
package ask

import (
	"database/sql"
	"regexp"
	"strings"

	_ "modernc.org/sqlite"
)

// DocItem is one searchable entry: either a CLI command or a help-fact.
type DocItem struct {
	ID          string
	Type        string // "command" | "doc"
	Title       string
	Description string
	Body        string
	Source      string
}

// ─── BM25 column weights (must match CREATE TABLE column order) ───────────────
const (
	wTitle       = 10.0
	wDescription = 5.0
	wBody        = 1.0
)

// ─── Stopwords ────────────────────────────────────────────────────────────────

var stopWords = map[string]bool{
	"a": true, "an": true, "the": true, "is": true, "am": true, "it": true,
	"its": true, "be": true, "are": true, "was": true, "were": true,
	"been": true, "being": true, "have": true, "has": true, "had": true,
	"do": true, "does": true, "did": true, "doing": true, "will": true,
	"would": true, "could": true, "should": true, "may": true, "might": true,
	"shall": true, "can": true, "need": true, "dare": true, "ought": true,
	"used": true, "i": true, "me": true, "my": true, "we": true, "our": true,
	"you": true, "your": true, "he": true, "she": true, "they": true,
	"them": true, "their": true, "this": true, "that": true, "these": true,
	"those": true, "which": true, "whom": true, "whose": true,
	"how": true, "why": true, "when": true, "where": true, "if": true,
	"then": true, "else": true, "so": true, "as": true, "at": true,
	"by": true, "for": true, "from": true, "in": true, "into": true,
	"of": true, "on": true, "or": true, "and": true, "but": true,
	"not": true, "no": true, "nor": true, "to": true, "up": true,
	"out": true, "with": true, "about": true, "after": true, "before": true,
	"between": true, "through": true, "during": true, "without": true,
	"within": true, "against": true, "along": true, "across": true,
	"behind": true, "beyond": true, "down": true, "off": true, "over": true,
	"under": true, "above": true, "below": true, "per": true, "via": true,
	"come": true, "comes": true, "coming": true,
	"jenkins": true, "name": true,
	"please": true, "guide": true, "tell": true, "show": true, "help": true,
	"explain": true, "describe": true, "teach": true, "want": true,
	"trying": true, "try": true, "let": true, "give": true,
	"find": true, "know": true, "understand": true, "using": true, "use": true,
}

// ─── Synonyms ─────────────────────────────────────────────────────────────────

var synonyms = map[string]string{
	"kill": "stop", "cancel": "stop", "abort": "stop", "terminate": "stop",
	"halt": "stop", "interrupt": "stop",
	"kick": "run", "remove": "delete", "rm": "delete", "revoke": "delete",
	"rotate": "update", "invalidate": "delete", "details": "get",
	"erase": "delete", "destroy": "delete", "expire": "delete", "rid": "delete",
	"options": "flags", "parameters": "flags", "arguments": "flags",
	"make": "create", "add": "create", "new": "create", "save": "create",
	"provision": "create", "register": "create", "setup": "create",
	"build": "job", "pipeline": "job", "project": "job", "folder": "job",
	"trigger": "run", "launch": "run", "execute": "run", "start": "run",
	"clone": "copy", "duplicate": "copy", "rename": "move", "relocate": "move",
	"configure": "update", "edit": "update", "existing": "update",
	"modify": "update", "change": "update",
	"monitor": "track", "watch": "track", "tail": "log", "stream": "log",
	"output": "log", "history": "status", "recent": "status", "runs": "status",
	"results": "status", "track": "track", "tracking": "track", "pin": "track",
	"what": "concept", "explain": "concept", "define": "concept",
	"meaning": "concept", "mine": "mine",
	"authenticate": "login", "connect": "login", "disconnect": "logout",
	"logged": "login", "expired": "login",
	"agent": "node", "slave": "node", "worker": "node", "executor": "node",
	"machine": "node", "decommission": "delete", "inspect": "get",
	"label": "labels", "maintenance": "offline", "disable": "offline",
	"suspend": "offline", "pause": "offline", "shutdown": "offline",
	"deactivate": "offline", "enable": "online", "resume": "online",
	"restore": "online", "activate": "online",
	"secret": "credential", "token": "credential", "password": "credential",
	"key": "credential", "apikey": "credential", "api": "credential",
	"cert": "credential", "certificate": "credential",
	"denied": "403", "forbidden": "403",
	"disconnecting": "connect", "connecting": "connect", "unreachable": "connect",
	"signin": "login", "sign-in": "login", "logon": "login",
	"log-out": "logout", "signout": "logout", "sign-out": "logout",
	"signoff": "logout", "logout": "logout", "out": "logout",
	"account": "profile", "user": "profile", "switch": "use",
	"whoami": "profiles",
	"master": "controller", "instance": "controller", "jenkins": "controller",
	"cloudbees": "controller", "bee": "overview",
	"list": "list", "view": "list", "display": "list", "see": "list",
	"get": "get", "fetch": "get", "look": "get",
	"env": "environment", "var": "variable", "vars": "variable",
	"envvar": "environment", "install": "setup",
	"tui": "ui", "interactive": "ui", "search": "ask",
	"logs": "log", "done": "status", "finished": "status",
	"organize": "folder", "group": "folder",
	"passwords": "credential", "secrets": "credential",
	"whitelist": "approve", "grant": "approve",
	"docs": "ask", "notification": "email", "notify": "email",
	"count": "status", "parameter": "param", "params": "param",
	"creds": "cred", "store": "store", "choose": "select", "pick": "select",
	"directory": "dir", "slot": "executor", "slots": "executor",
	"scope": "credential", "assign": "node", "cache": "ttl",
}

// reservedTokens must never be remapped by generated synonyms.
var reservedTokens = map[string]bool{
	"create": true, "update": true, "delete": true, "list": true, "get": true,
	"run": true, "stop": true, "copy": true, "move": true, "log": true,
	"track": true, "untrack": true, "status": true, "select": true, "use": true,
	"login": true, "logout": true, "info": true,
	"job": true, "node": true, "credential": true, "controller": true,
	"auth": true, "profile": true, "environment": true, "troubleshooting": true,
	"concept": true, "pipeline": true, "folder": true, "multibranch": true,
	"show": true, "view": true, "set": true, "change": true, "edit": true,
	"add": true, "remove": true, "find": true, "search": true,
	"open": true, "close": true, "start": true, "end": true, "begin": true,
	"finish": true, "install": true, "configure": true, "enable": true,
	"disable": true, "suspend": true, "resume": true, "restart": true,
	"reload": true, "refresh": true, "save": true, "load": true,
	"import": true, "export": true, "upload": true, "download": true,
	"sync": true, "backup": true, "restore": true, "clean": true, "clear": true,
	"reset": true, "init": true, "setup": true, "deploy": true,
}

// expandToken returns the synonym-expanded form of a lowercase token.
func expandToken(t string) string {
	if v, ok := synonyms[t]; ok {
		return v
	}
	return t
}

// bigrams catches two-token phrases where one token is a stopword.
var bigrams = map[string]string{
	"log out":  "logout",
	"sign out": "logout",
	"sign in":  "login",
	"log in":   "login",
	"set up":   "setup",
	"sign up":  "register",
}

var nonAlphaNum = regexp.MustCompile(`[^a-z0-9]+`)

// buildMatchExpr tokenises query, drops stopwords, expands synonyms, and
// returns an FTS5 MATCH expression like `"job"* OR "run"* OR "create"*`.
// Returns "" when nothing survives (caller skips the search).
func buildMatchExpr(query string) string {
	lower := strings.ToLower(query)
	raw := nonAlphaNum.Split(lower, -1)
	tokens := make([]string, 0, len(raw))
	for _, t := range raw {
		if t != "" {
			tokens = append(tokens, t)
		}
	}

	joined := strings.Join(tokens, " ")
	var expanded []string
	for bigram, expansion := range bigrams {
		if strings.Contains(joined, bigram) {
			expanded = append(expanded, expansion)
		}
	}

	for _, t := range tokens {
		if stopWords[t] {
			continue
		}
		expanded = append(expanded, t)
		syn := expandToken(t)
		if syn != t {
			expanded = append(expanded, syn)
		}
	}

	// deduplicate, preserve order
	seen := make(map[string]bool)
	var unique []string
	for _, t := range expanded {
		if !seen[t] {
			seen[t] = true
			unique = append(unique, t)
		}
	}

	if len(unique) == 0 {
		return ""
	}
	parts := make([]string, len(unique))
	for i, t := range unique {
		parts[i] = `"` + t + `"*`
	}
	return strings.Join(parts, " OR ")
}

// contentTokens returns query tokens for the relevance gate (mirrors buildMatchExpr tokenisation).
func contentTokens(query string) []string {
	lower := strings.ToLower(query)
	raw := nonAlphaNum.Split(lower, -1)
	var out []string
	for _, t := range raw {
		if t != "" && !stopWords[t] {
			out = append(out, t)
		}
	}
	return out
}

// wordStartMatch checks whether token starts a word in blob (same semantics as FTS5 prefix match).
func wordStartMatch(blob, token string) bool {
	if token == "" {
		return false
	}
	from := 0
	for {
		idx := strings.Index(blob[from:], token)
		if idx < 0 {
			return false
		}
		idx += from
		if idx == 0 {
			return true
		}
		prev := blob[idx-1]
		if (prev < 'a' || prev > 'z') && (prev < '0' || prev > '9') {
			return true
		}
		from = idx + 1
	}
}

const gateCovMin = 0.6

// passesRelevanceGate returns true when the item's content covers enough of the query tokens.
func passesRelevanceGate(query string, item DocItem) bool {
	blob := strings.ToLower(item.Title + " " + item.Description + " " + item.Body)
	toks := contentTokens(query)
	if len(toks) == 0 {
		return false
	}
	matched := 0
	for _, t := range toks {
		if wordStartMatch(blob, t) || wordStartMatch(blob, expandToken(t)) {
			matched++
		}
	}
	cov := float64(matched) / float64(len(toks))
	if cov < gateCovMin {
		return false
	}
	if len(toks) == 1 {
		return matched >= 1
	}
	return matched >= 2
}

// ─── In-memory FTS5 corpus ────────────────────────────────────────────────────

type corpusCache struct {
	db    *sql.DB
	items []DocItem
}

var _corpusCache *corpusCache

func getCorpusDB(corpus []DocItem) (*sql.DB, error) {
	if _corpusCache != nil && corpusEqual(_corpusCache.items, corpus) {
		return _corpusCache.db, nil
	}
	if _corpusCache != nil {
		_ = _corpusCache.db.Close()
		_corpusCache = nil
	}

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return nil, err
	}
	_, err = db.Exec(`CREATE VIRTUAL TABLE docs USING fts5(
		id          UNINDEXED,
		type        UNINDEXED,
		title,
		description,
		body,
		source      UNINDEXED,
		tokenize = 'unicode61'
	)`)
	if err != nil {
		db.Close()
		return nil, err
	}
	stmt, err := db.Prepare("INSERT INTO docs(id,type,title,description,body,source) VALUES(?,?,?,?,?,?)")
	if err != nil {
		db.Close()
		return nil, err
	}
	defer stmt.Close()
	for _, d := range corpus {
		if _, err = stmt.Exec(d.ID, d.Type, d.Title, d.Description, d.Body, d.Source); err != nil {
			db.Close()
			return nil, err
		}
	}
	_corpusCache = &corpusCache{db: db, items: corpus}
	return db, nil
}

// corpusEqual uses pointer equality — the corpus slice must be the same reference to hit cache.
func corpusEqual(a, b []DocItem) bool {
	if len(a) != len(b) {
		return false
	}
	if len(a) == 0 {
		return true
	}
	// pointer identity via first element address
	return &a[0] == &b[0]
}
