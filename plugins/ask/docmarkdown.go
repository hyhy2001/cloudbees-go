package ask

// Embedded documentation corpus for `bee ask`. Docs under docs/ are baked
// into the binary via go:embed. Only used when BEE_ASK_INCLUDE_DOC_CHUNKS=1
// (dev/fallback — the live command tree + help facts cover normal use).
import (
	"embed"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
)

//go:embed docs
var docsFS embed.FS

// DocChunk is one heading-delimited section of an embedded markdown file.
type DocChunk struct {
	ID      string // "source#slug"
	Source  string
	Heading string
	Body    string
}

var headingRe = regexp.MustCompile(`^(#{1,2})\s+(.+?)\s*$`)
var fenceRe = regexp.MustCompile("^\\s*```")
var slugNonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(text string) string {
	s := slugNonAlnum.ReplaceAllString(strings.ToLower(text), "-")
	return strings.Trim(s, "-")
}

// chunkMarkdown splits one markdown document into chunks at "#"/"##" headings.
// Fenced code blocks are tracked so a "# comment" inside a shell example isn't
// mistaken for a heading. Deeper headings ("###"+) stay within their parent.
func chunkMarkdown(source, content string) []DocChunk {
	lines := strings.Split(content, "\n")
	var chunks []DocChunk
	usedSlugs := make(map[string]bool)
	inFence := false
	heading := ""
	var buf []string

	flush := func() {
		body := strings.TrimSpace(strings.Join(buf, "\n"))
		buf = nil
		if body == "" {
			return
		}
		slug := ""
		if heading != "" {
			slug = slugify(heading)
		}
		if slug == "" {
			slug = "section-" + itoa(len(chunks))
		}
		if usedSlugs[slug] {
			n := 2
			for usedSlugs[slug+"-"+itoa(n)] {
				n++
			}
			slug = slug + "-" + itoa(n)
		}
		usedSlugs[slug] = true
		chunks = append(chunks, DocChunk{ID: source + "#" + slug, Source: source, Heading: heading, Body: body})
	}

	for _, line := range lines {
		if fenceRe.MatchString(line) {
			inFence = !inFence
			buf = append(buf, line)
			continue
		}
		if !inFence {
			if m := headingRe.FindStringSubmatch(line); m != nil {
				flush()
				heading = m[2]
				continue
			}
		}
		buf = append(buf, line)
	}
	flush()
	return chunks
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

var (
	_docChunks     []DocChunk
	_docChunksOnce sync.Once
)

// buildDocChunks walks docs/ (embedded) and chunks every .md file, sorted by
// path for stable ordering (fs.WalkDir doesn't guarantee it across platforms,
// though embed.FS does — sorted anyway for a deterministic corpus).
func buildDocChunks() []DocChunk {
	_docChunksOnce.Do(func() {
		var paths []string
		_ = walkEmbedDir(docsFS, "docs", &paths)
		sort.Strings(paths)
		for _, p := range paths {
			raw, err := docsFS.ReadFile(p)
			if err != nil {
				continue
			}
			source := strings.TrimPrefix(p, "docs/")
			_docChunks = append(_docChunks, chunkMarkdown(source, string(raw))...)
		}
	})
	return _docChunks
}

func walkEmbedDir(fsys embed.FS, dir string, out *[]string) error {
	entries, err := fsys.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		p := dir + "/" + e.Name()
		if e.IsDir() {
			if err := walkEmbedDir(fsys, p, out); err != nil {
				return err
			}
			continue
		}
		if strings.HasSuffix(e.Name(), ".md") {
			*out = append(*out, p)
		}
	}
	return nil
}

// includeDocChunks mirrors TS's opts.includeDocChunks ?? env gate.
func includeDocChunks() bool {
	return os.Getenv("BEE_ASK_INCLUDE_DOC_CHUNKS") == "1"
}
