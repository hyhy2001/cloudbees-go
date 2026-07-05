package ask

import (
	"fmt"
	"regexp"
	"strings"
)

// searchDocs runs FTS5 BM25 search and applies promotion rules and relevance gating.
// gate=true enables the relevance gate; softGate=true falls back to ungated on empty.
func searchDocs(query string, corpus []DocItem, limit int, gate, softGate bool) []DocItem {
	match := buildMatchExpr(query)
	if match == "" || len(corpus) == 0 {
		return nil
	}

	db, err := getCorpusDB(corpus)
	if err != nil {
		return nil
	}

	fetchLimit := limit
	if gate {
		fl := limit * 5
		if fl > len(corpus) {
			fl = len(corpus)
		}
		fetchLimit = fl
	}

	q := fmt.Sprintf(`
		SELECT id, type, title, description, body, source
		  FROM docs
		 WHERE docs MATCH ?
		 ORDER BY bm25(docs, 0.0, 0.0, %g, %g, %g, 0.0)
		 LIMIT ?`, wTitle, wDescription, wBody)

	rows, err := db.Query(q, match, fetchLimit)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var items []DocItem
	for rows.Next() {
		var d DocItem
		if err := rows.Scan(&d.ID, &d.Type, &d.Title, &d.Description, &d.Body, &d.Source); err != nil {
			continue
		}
		items = append(items, d)
	}

	items = applyPromotions(query, items, corpus)

	var final []DocItem
	if gate {
		for _, it := range items {
			if passesRelevanceGate(query, it) {
				final = append(final, it)
			}
		}
		if softGate && len(final) == 0 {
			final = items
		}
	} else {
		final = items
	}

	result := sliceN(final, limit)
	result = applyPostGateInjects(query, result, corpus, limit)
	if len(result) > limit {
		result = result[:limit]
	}
	return result
}

func sliceN(s []DocItem, n int) []DocItem {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// findIdx returns the first index where pred is true, -1 otherwise.
func findIdx(items []DocItem, pred func(DocItem) bool) int {
	for i, it := range items {
		if pred(it) {
			return i
		}
	}
	return -1
}

// promoteByID moves the item with given ID to the front (or to position 1 if afterFirst=true).
func promoteID(items []DocItem, id string, afterFirst bool) []DocItem {
	idx := findIdx(items, func(it DocItem) bool { return it.ID == id })
	if idx <= 0 {
		return items
	}
	target := 0
	if afterFirst {
		target = 1
	}
	if idx <= target {
		return items
	}
	item := items[idx]
	items = append(items[:idx], items[idx+1:]...)
	newItems := make([]DocItem, 0, len(items)+1)
	newItems = append(newItems, items[:target]...)
	newItems = append(newItems, item)
	newItems = append(newItems, items[target:]...)
	return newItems
}

// injectOrPromote moves id to the front of result (or fetches it from corpus if
// absent). When afterFirst is true it inserts at position 1 instead of 0,
// preserving an already-promoted top hit — matching the TS splice(promoted?1:0).
func injectOrPromote(result []DocItem, id string, corpus []DocItem, afterFirst bool) []DocItem {
	target := 0
	if afterFirst {
		target = 1
	}
	idx := findIdx(result, func(it DocItem) bool { return it.ID == id })
	if idx < 0 {
		for _, c := range corpus {
			if c.ID == id {
				out := make([]DocItem, 0, len(result)+1)
				out = append(out, result[:min(target, len(result))]...)
				out = append(out, c)
				out = append(out, result[min(target, len(result)):]...)
				return out
			}
		}
		return result
	}
	if idx <= target {
		return result
	}
	item := result[idx]
	out := make([]DocItem, 0, len(result))
	out = append(out, result[:target]...)
	out = append(out, item)
	out = append(out, result[target:idx]...)
	out = append(out, result[idx+1:]...)
	return out
}

// injectOrPromotePrefix promotes the first item whose ID starts with prefix.
func injectOrPromotePrefix(result []DocItem, prefix string, corpus []DocItem, afterFirst bool) []DocItem {
	idx := findIdx(result, func(it DocItem) bool { return strings.HasPrefix(it.ID, prefix) })
	if idx < 0 {
		for _, c := range corpus {
			if strings.HasPrefix(c.ID, prefix) {
				return injectOrPromote(result, c.ID, corpus, afterFirst)
			}
		}
		return result
	}
	return injectOrPromote(result, result[idx].ID, corpus, afterFirst)
}

var intentPatterns = []struct {
	re     *regexp.Regexp
	target string
}{
	{regexp.MustCompile(`(?i)\blog\s+out\b`), "auth.logout"},
	{regexp.MustCompile(`(?i)\btell\s+me\s+about\b`), "concept."},
	{regexp.MustCompile(`(?i)\bswitch\s+(server|controller)\b`), "controller.select"},
	{regexp.MustCompile(`(?i)\bswitch\s+.*\bprofile\b`), "auth.use"},
	{regexp.MustCompile(`(?i)\bspecific\s+profile\b`), "auth.use"},
	{regexp.MustCompile(`(?i)\bswitch\s+(user|account)\b`), "auth.use"},
	{regexp.MustCompile(`(?i)\blogin\s+.*\bprofile\b`), "auth.login"},
	{regexp.MustCompile(`(?i)\bprofile\s+.*\blogin\b`), "auth.login"},
	{regexp.MustCompile(`(?i)\bauth\s+login\b`), "auth.login"},
	{regexp.MustCompile(`(?i)\bcan'?t\s+log\s+in\b`), "troubleshooting.login"},
	{regexp.MustCompile(`(?i)\bwrong\s+password\b`), "troubleshooting.login"},
	{regexp.MustCompile(`(?i)\bhow\s+(do\s+i|to)\s+log\s+in\b`), "concept.login"},
	{regexp.MustCompile(`(?i)\bhow\s+(do\s+i|to)\s+login\b`), "concept.login"},
	{regexp.MustCompile(`(?i)\bget\s+started\b`), "concept.getting-started"},
	{regexp.MustCompile(`(?i)\bwhich\s+(server|controller)\b`), "controller.current"},
	{regexp.MustCompile(`(?i)\bam\s+i\s+(connected|on)\b`), "controller.current"},
	{regexp.MustCompile(`(?i)\bchange\s+(server|controller)\b`), "controller.select"},
	{regexp.MustCompile(`(?i)\b(see|list|show)\s+all\s+servers?\b`), "controller.list"},
	{regexp.MustCompile(`(?i)\ball\s+(my\s+)?servers?\b`), "controller.list"},
	{regexp.MustCompile(`(?i)\bhow\s+(do\s+i|to|can\s+i)\s+(create|make)\s+(a\s+)?pipeline\b`), "concept.create-pipeline"},
	{regexp.MustCompile(`(?i)\b(create|make|add)\s+(a\s+)?pipeline\b`), "job.create.pipeline"},
	{regexp.MustCompile(`(?i)\bnew\s+pipeline\b`), "job.create.pipeline"},
	{regexp.MustCompile(`(?i)\bupdate\s+pipeline\b`), "job.update.pipeline"},
	{regexp.MustCompile(`(?i)\b(make|create|new)\s+job\b`), "job.create"},
	{regexp.MustCompile(`(?i)\bdifference\s+between\b`), "concept.pipeline"},
	{regexp.MustCompile(`(?i)\bfreestyle\s+(vs|and|or)\s+pipeline\b`), "concept.pipeline"},
	{regexp.MustCompile(`(?i)\bapprove\s+agent\b`), "job.approve-agent"},
	{regexp.MustCompile(`(?i)\bremove\s+agent\b`), "job.remove-agent"},
	{regexp.MustCompile(`(?i)\bpipeline\s+job\b`), "concept.pipeline"},
	{regexp.MustCompile(`(?i)\badd\s+(a[n]?\s+)?(new\s+)?(agent|node|machine|build\s+machine)\b`), "node.create"},
	{regexp.MustCompile(`(?i)\bsee\s+all\s+(my\s+)?(agents?|nodes?|machines?)\b`), "node.list"},
	{regexp.MustCompile(`(?i)\b(list|show)\s+all\s+(agents?|nodes?|machines?)\b`), "node.list"},
	{regexp.MustCompile(`(?i)\b(my\s+)?agent\s+is\s+offline\b`), "concept.node-offline"},
	{regexp.MustCompile(`(?i)\bagent\s+(won'?t|cannot|can'?t)\s+connect\b`), "troubleshooting.node-connect"},
	{regexp.MustCompile(`(?i)\bwhat\s+is\s+(a\s+)?controller\b`), "concept.controller"},
	{regexp.MustCompile(`(?i)\bwhat\s+is\s+(a\s+)?job\b`), "concept.what-is-job"},
	{regexp.MustCompile(`(?i)\bwhat\s+is\s+(a[n]?\s+)?(agent|node|build\s+machine)\b`), "concept.what-is-node"},
	{regexp.MustCompile(`(?i)\bcontrolled\s+agent\b`), "concept.controlled-agent"},
	{regexp.MustCompile(`(?i)\bwhat\s+is\s+(a\s+|the\s+)?credential\s+store\b`), "concept.credential-store"},
	{regexp.MustCompile(`(?i)\b(mean|what|explain)\b.*\bcredential\s+store\b`), "concept.credential-store"},
	{regexp.MustCompile(`(?i)\btypes?\s+of\s+credentials?\b`), "concept.credential-types"},
	{regexp.MustCompile(`(?i)\bwhat\s+credentials?\s+(can\s+i\s+)?(store|use|create)\b`), "concept.credential-types"},
	{regexp.MustCompile(`(?i)\badd\s+(a\s+)?(build\s+)?machine\b`), "concept.add-node"},
	{regexp.MustCompile(`(?i)\b(see|list|show)\s+(my\s+)?(saved\s+)?(password|credential|secret)s?\b`), "cred.list"},
	{regexp.MustCompile(`(?i)\bhow\s+(to|do\s+i)\s+run\s+(a\s+)?(job|build)\b`), "job.run"},
	{regexp.MustCompile(`(?i)\b(see|view|check|get)\s+(the\s+)?(build\s+)?(error|log|output|failure)\b`), "job.log"},
	{regexp.MustCompile(`(?i)\bsee\s+(build\s+)?logs?\b`), "job.log"},
	{regexp.MustCompile(`(?i)\b(check|is)\s+(my\s+)?(build|job)\s+(done|finished|complete)\b`), "job.status"},
	{regexp.MustCompile(`(?i)\bbuild\s+(done|finished|complete)\b`), "job.status"},
	{regexp.MustCompile(`(?i)\bhow\s+(to|do\s+i)\s+create\s+node\b`), "node.create"},
	{regexp.MustCompile(`(?i)\bhow\s+(to|do\s+i)\s+update\s+node\b`), "node.update"},
	{regexp.MustCompile(`(?i)\brun\s+.*\bparam.*\bvalue`), "job.run"},
	{regexp.MustCompile(`(?i)\brun\s+job\s+.*\bparam`), "job.run"},
	{regexp.MustCompile(`(?i)\bjob\s+with\s+param`), "concept.build-params"},
	{regexp.MustCompile(`(?i)\bhow\s+(to|do)\s+.*run.*\bparam`), "concept.build-params"},
	{regexp.MustCompile(`(?i)\bcustom\s+param`), "concept.build-params"},
	{regexp.MustCompile(`(?i)\badd\s+(a\s+)?(build\s+)?param(eter)?\b`), "job.update.freestyle"},
	{regexp.MustCompile(`(?i)\bjobs?\s+i\s+care\s+about\b`), "concept.mine-vs-all"},
	{regexp.MustCompile(`(?i)\bonly\s+(show|see|my)\s+jobs?\b`), "concept.mine-vs-all"},
	{regexp.MustCompile(`(?i)\bmine\s+vs\b`), "concept.mine-vs-all"},
	{regexp.MustCompile(`(?i)\borganize\b.*\bjobs?\b`), "concept.folders"},
	{regexp.MustCompile(`(?i)\bjobs?\b.*\binto\s+folders?\b`), "concept.folders"},
	{regexp.MustCompile(`(?i)\b(set|assign|restrict)\s+(a\s+)?job\s+(to\s+)?(run\s+on|use)\b`), "concept.node-labels"},
	{regexp.MustCompile(`(?i)\bspecific\s+(machine|agent|node)\b`), "concept.node-labels"},
	{regexp.MustCompile(`(?i)\bstore\s+(a\s+)?(secret|credential|token|key|password)`), "cred.create"},
	{regexp.MustCompile(`(?i)\bstore\s+vs\b`), "concept.credential-store"},
	{regexp.MustCompile(`(?i)\bcredential\s+.*\bstore\b.*\bvs\b`), "concept.credential-store"},
	{regexp.MustCompile(`(?i)\bsystem\s+store\b.*\buser\s+store\b`), "concept.credential-store"},
	{regexp.MustCompile(`(?i)\bcred\s+list\b`), "cred.list"},
	{regexp.MustCompile(`(?i)\bpipeline\s+.*\bvalidat`), "troubleshooting.pipeline-validate"},
	{regexp.MustCompile(`(?i)\bscript\s+.*\bfailed\b`), "troubleshooting.pipeline-validate"},
	{regexp.MustCompile(`(?i)\bpipeline\s+script\s+is\s+invalid\b`), "troubleshooting.pipeline-validate"},
}

// applyPromotions applies all ranking promotions from corpus.ts to the BM25 result list.
func applyPromotions(query string, items []DocItem, corpus []DocItem) []DocItem {
	qNorm := strings.ToLower(strings.TrimSpace(regexp.MustCompile(`\s+`).ReplaceAllString(query, " ")))
	promoted := false

	// 1. Exact command-path promotion
	exactIdx := findIdx(items, func(it DocItem) bool {
		return it.Type == "command" && strings.ReplaceAll(it.ID, ".", " ") == qNorm
	})
	if exactIdx > 0 {
		items = promoteID(items, items[exactIdx].ID, false)
		promoted = true
	}

	// 2. Expert routing: <group> <verb> pattern
	if !promoted {
		cmdPat := regexp.MustCompile(`\b([a-z]{2,})\.([a-z]{2,})\b|([a-z]{2,})\s+([a-z]{2,})\b`)
		matches := cmdPat.FindAllStringSubmatch(qNorm, -1)
		for _, cm := range matches {
			a, b := cm[1], cm[2]
			if a == "" {
				a, b = cm[3], cm[4]
			}
			targetID := a + "." + b
			cmdIdx := findIdx(items, func(it DocItem) bool { return it.Type == "command" && it.ID == targetID })
			if cmdIdx > 1 {
				item := items[cmdIdx]
				out := make([]DocItem, 0, len(items))
				out = append(out, items[0])
				out = append(out, item)
				out = append(out, items[1:cmdIdx]...)
				out = append(out, items[cmdIdx+1:]...)
				items = out
				promoted = true
				break
			}
		}
	}

	// 3. Intent patterns
	for _, ip := range intentPatterns {
		if ip.re.MatchString(qNorm) {
			target := ip.target
			var idx int
			if strings.HasSuffix(target, ".") {
				idx = findIdx(items, func(it DocItem) bool { return strings.HasPrefix(it.ID, target) })
			} else {
				idx = findIdx(items, func(it DocItem) bool { return it.ID == target })
			}
			cutoff := 0
			if promoted {
				cutoff = 1
			}
			if idx > cutoff {
				if strings.HasSuffix(target, ".") {
					items = injectOrPromotePrefix(items, target, corpus, promoted)
				} else {
					items = injectOrPromote(items, target, corpus, promoted)
				}
				promoted = true
			}
		}
	}

	// Special cases
	if regexp.MustCompile(`(?i)\bunexpectedly\b`).MatchString(qNorm) {
		cutoff := 0
		if promoted {
			cutoff = 1
		}
		idx := findIdx(items, func(it DocItem) bool { return it.ID == "troubleshooting.node-connect" })
		if idx > cutoff {
			items = injectOrPromote(items, "troubleshooting.node-connect", corpus, promoted)
			promoted = true
		}
	}
	if regexp.MustCompile(`(?i)\binstall\b`).MatchString(qNorm) {
		cutoff := 0
		if promoted {
			cutoff = 1
		}
		idx := findIdx(items, func(it DocItem) bool { return it.ID == "concept.login" })
		if idx > cutoff {
			items = injectOrPromote(items, "concept.login", corpus, promoted)
			promoted = true
		}
	}

	// 4. Cross-plugin: "what/all/available commands" — each .list to the front.
	if !promoted && regexp.MustCompile(`\b(what|all|available)\s+commands?\b`).MatchString(qNorm) {
		for _, id := range []string{"job.list", "controller.list", "node.list", "cred.list"} {
			items = injectOrPromote(items, id, corpus, false)
		}
		promoted = true
	}

	// Final overrides
	if regexp.MustCompile(`(?i)\bcred\s+list\b`).MatchString(qNorm) {
		items = injectOrPromote(items, "cred.list", corpus, false)
	}
	if !regexp.MustCompile(`(?i)\bagents?\b`).MatchString(qNorm) &&
		(regexp.MustCompile(`(?i)\bjob\s+list\b`).MatchString(qNorm) ||
			regexp.MustCompile(`(?i)\b(show|list|get)\s+all\s+jobs\b`).MatchString(qNorm) ||
			regexp.MustCompile(`(?i)\ball\s+jobs\b`).MatchString(qNorm)) {
		items = injectOrPromote(items, "job.list", corpus, false)
	}
	if regexp.MustCompile(`(?i)\bnode\s+(update|create)\b`).MatchString(qNorm) || regexp.MustCompile(`(?i)\b(update|create)\s+node\b`).MatchString(qNorm) {
		target := "node.create"
		if regexp.MustCompile(`(?i)\bupdate\b`).MatchString(qNorm) {
			target = "node.update"
		}
		items = injectOrPromote(items, target, corpus, false)
	}
	if regexp.MustCompile(`(?i)\bauth\s+login\b`).MatchString(qNorm) {
		items = injectOrPromote(items, "auth.login", corpus, false)
	}
	if regexp.MustCompile(`(?i)\bhow\s+(do\s+i|to|can\s+i)\s+(create|make)\s+(a\s+)?pipeline\b`).MatchString(qNorm) {
		items = injectOrPromote(items, "concept.create-pipeline", corpus, false)
	}
	if regexp.MustCompile(`(?i)\brun\s+(job\s+)?.*\bparam.*\bvalue`).MatchString(qNorm) || regexp.MustCompile(`(?i)\brun\s+job\s+.*\bparam`).MatchString(qNorm) {
		items = injectOrPromote(items, "job.run", corpus, false)
	}
	if regexp.MustCompile(`(?i)\bstore\b.*\bvs\b`).MatchString(qNorm) || regexp.MustCompile(`(?i)\bvs\b.*\bstore\b`).MatchString(qNorm) {
		items = injectOrPromote(items, "concept.credential-store", corpus, false)
	}
	if regexp.MustCompile(`(?i)\bpipeline\b.*\b(validat|failed|error|invalid)\b`).MatchString(qNorm) {
		items = injectOrPromote(items, "troubleshooting.pipeline-validate", corpus, false)
	}
	if regexp.MustCompile(`(?i)\bbuild\s+failed\b`).MatchString(qNorm) {
		items = injectOrPromote(items, "job.log", corpus, false)
	}
	if regexp.MustCompile(`(?i)\b(check|is)\s+(if\s+)?(my\s+)?(build|job)\s+(is\s+)?(done|finished|complete)\b`).MatchString(qNorm) ||
		regexp.MustCompile(`(?i)\bbuild\s+(done|finished|complete)\b`).MatchString(qNorm) {
		items = injectOrPromote(items, "job.status", corpus, false)
	}
	if regexp.MustCompile(`(?i)\bchange\s+(the\s+)?(server|controller)\b`).MatchString(qNorm) || regexp.MustCompile(`(?i)\bhow\s+(do\s+i|to)\s+change\s+(server|controller)\b`).MatchString(qNorm) {
		items = injectOrPromote(items, "controller.select", corpus, false)
	}
	if regexp.MustCompile(`(?i)\bwhich\s+(server|controller)\b`).MatchString(qNorm) || regexp.MustCompile(`(?i)\bam\s+i\s+(connected|on)\b`).MatchString(qNorm) {
		items = injectOrPromote(items, "controller.current", corpus, false)
	}

	_ = promoted
	return items
}

// applyPostGateInjects handles post-gate specific injections.
func applyPostGateInjects(query string, result []DocItem, corpus []DocItem, limit int) []DocItem {
	qNorm := strings.ToLower(strings.TrimSpace(regexp.MustCompile(`\s+`).ReplaceAllString(query, " ")))

	if (regexp.MustCompile(`(?i)\b(jenkins|cloudbees)\b`).MatchString(qNorm) &&
		regexp.MustCompile(`(?i)\b(switch|change|select|pick|choose)\b`).MatchString(qNorm)) {
		result = injectOrPromote(result, "controller.select", corpus, false)
	}
	if regexp.MustCompile(`(?i)\btypes?\s+of\s+credentials?\b`).MatchString(qNorm) || regexp.MustCompile(`(?i)\bwhat\s+credentials?\s+(can\s+i\s+)?(store|use)\b`).MatchString(qNorm) {
		result = injectOrPromote(result, "concept.credential-types", corpus, false)
	}
	if regexp.MustCompile(`(?i)\bjobs?\s+i\s+care\s+about\b`).MatchString(qNorm) || regexp.MustCompile(`(?i)\bonly\s+(show|see|my)\s+jobs?\b`).MatchString(qNorm) {
		result = injectOrPromote(result, "concept.mine-vs-all", corpus, false)
	}
	if regexp.MustCompile(`(?i)\bchange\s+(the\s+)?(server|controller)\b`).MatchString(qNorm) {
		result = injectOrPromote(result, "controller.select", corpus, false)
	}
	if regexp.MustCompile(`(?i)\bbuild\s+failed\b`).MatchString(qNorm) && regexp.MustCompile(`(?i)\b(see|view|get|show)\b`).MatchString(qNorm) {
		result = injectOrPromote(result, "job.log", corpus, false)
	}
	if regexp.MustCompile(`(?i)\bhow\s+(to|do\s+i)\s+(add|create)\s+(a\s+)?(build\s+)?machine\b`).MatchString(qNorm) {
		result = injectOrPromote(result, "concept.add-node", corpus, false)
	}
	if regexp.MustCompile(`(?i)\bcredentials?\s+(does\s+bee|supported|available|types?)\b`).MatchString(qNorm) || regexp.MustCompile(`(?i)\bwhat\s+credentials?\s+(does|can|bee)\b`).MatchString(qNorm) {
		result = injectOrPromote(result, "concept.credential-types", corpus, false)
	}
	if regexp.MustCompile(`(?i)\bhow\s+(to|do\s+i)\s+run\s+(a\s+)?job\s+with\s+param`).MatchString(qNorm) {
		result = injectOrPromote(result, "concept.build-params", corpus, false)
	}
	if regexp.MustCompile(`(?i)^(make|create|new)\s+job$`).MatchString(qNorm) {
		result = injectOrPromote(result, "job.create", corpus, false)
	}
	if regexp.MustCompile(`(?i)\b(unreachable|cannot\s+connect|can'?t\s+connect)\b`).MatchString(qNorm) && regexp.MustCompile(`(?i)\bnode\b`).MatchString(qNorm) {
		result = injectOrPromote(result, "troubleshooting.node-connect", corpus, false)
	}

	return result
}
