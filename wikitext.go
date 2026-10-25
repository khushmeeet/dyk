package main

import (
	"html"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// A hook is one fact from the archive: its text, starting with "that" and
// ending with "?", and the title of the article it is about.
type hook struct {
	Text    string
	Article string
}

var (
	comment      = regexp.MustCompile(`(?s)<!--.*?-->`)
	hookLine     = regexp.MustCompile(`^\*+\s*(?:\.\.\.|…)\s*(.+)$`)
	ref          = regexp.MustCompile(`(?is)<ref[^>]*/>|<ref[^>]*>.*?</ref>`)
	imageNote    = regexp.MustCompile(`''\s*\((?:[^()]|\([^()]*\))*\)\s*''|\((?:[^()]|\([^()]*\))*\)`)
	pictured     = regexp.MustCompile(`(?i)\b(pictured|depicted|shown|illustrated)\b`)
	link         = regexp.MustCompile(`\[\[([^\[\]]*)\]\]`)
	templateCall = regexp.MustCompile(`\{\{([^{}]*)\}\}`)
	lineBreak    = regexp.MustCompile(`(?i)<br\s*/?>`)
	tag          = regexp.MustCompile(`</?[a-zA-Z][^>]*>`)
	quotes       = regexp.MustCompile(`'{2,}`)
	spaces       = regexp.MustCompile(`[ \t]+`)
	spaceBefore  = regexp.MustCompile(` ([?,;])`)
	namedArg     = regexp.MustCompile(`^\s*[\w-]+\s*$`)
)

// parseArchive returns the hooks on a monthly archive page, and how many
// hook lines it skipped because they couldn't be converted cleanly.
func parseArchive(wikitext string) (hooks []hook, skipped int) {
	wikitext = comment.ReplaceAllString(wikitext, "")
	for _, line := range strings.Split(wikitext, "\n") {
		m := hookLine.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		if h, ok := parseHook(m[1]); ok {
			hooks = append(hooks, h)
		} else {
			skipped++
		}
	}
	return hooks, skipped
}

// parseHook converts one hook from wikitext to plain text, starting after
// the leading "...". It reports false if the hook can't be converted
// cleanly, so it is skipped rather than shown broken.
func parseHook(s string) (hook, bool) {
	s = ref.ReplaceAllString(s, "")
	if strings.Contains(strings.ToLower(s), "<math") {
		return hook{}, false
	}

	// Cards are text-only, so notes such as "(pictured)" go.
	s = imageNote.ReplaceAllStringFunc(s, func(m string) string {
		if pictured.MatchString(m) {
			return ""
		}
		return m
	})

	article := articleTitle(boldLink(s))
	if article == "" {
		return hook{}, false
	}

	s = link.ReplaceAllStringFunc(s, func(m string) string {
		target, label, piped := strings.Cut(m[2:len(m)-2], "|")
		if piped && label != "" {
			return label
		}
		return strings.TrimPrefix(target, ":")
	})

	s = lineBreak.ReplaceAllString(s, " ")
	s = tag.ReplaceAllString(s, "")

	// Expand the innermost templates first, until none are left.
	for strings.Contains(s, "{{") {
		known := true
		s = templateCall.ReplaceAllStringFunc(s, func(m string) string {
			out, ok := expandTemplate(m[2 : len(m)-2])
			known = known && ok
			return out
		})
		if !known || !templateCall.MatchString(s) && strings.Contains(s, "{{") {
			return hook{}, false
		}
	}

	// Two apostrophes are italic, three bold, five both. Four are an
	// apostrophe followed by bold, and any past five are apostrophes too.
	s = quotes.ReplaceAllStringFunc(s, func(q string) string {
		switch n := len(q); {
		case n == 4:
			return "'"
		case n > 5:
			return q[:n-5]
		}
		return ""
	})

	// Anything left that looks like markup means the hook used something
	// this doesn't understand. Single brackets stay: quotes use them for
	// words an editor added, as in "pepp[ed] things up".
	if strings.ContainsAny(s, "{}<>|") || strings.Contains(s, "[[") || strings.Contains(s, "]]") ||
		strings.Contains(s, "''") || strings.Contains(s, "://") {
		return hook{}, false
	}
	// A hook that still mentions its picture, as in "the pictured beetle",
	// doesn't make sense without it.
	if strings.Contains(strings.ToLower(s), "pictured") {
		return hook{}, false
	}

	s = html.UnescapeString(s)
	s = spaces.ReplaceAllString(s, " ")
	s = spaceBefore.ReplaceAllString(s, "$1")
	s = strings.TrimSpace(s)

	if !strings.HasPrefix(s, "that ") && !strings.HasPrefix(s, "that,") || !strings.HasSuffix(s, "?") {
		return hook{}, false
	}
	return hook{Text: s, Article: article}, true
}

// boldLink returns the target of the first link in bold text: the article
// the hook is about.
func boldLink(s string) string {
	bold := false
	inLink := -1 // where the link we're inside starts, or -1
	for i := 0; i < len(s); {
		switch {
		case s[i] == '\'':
			n := 1
			for i+n < len(s) && s[i+n] == '\'' {
				n++
			}
			if n >= 3 {
				bold = !bold
				// Bold that starts inside a link, as in [[A|'''B''']].
				if bold && inLink >= 0 {
					return linkTarget(s[inLink:])
				}
			}
			i += n
		case strings.HasPrefix(s[i:], "[["):
			if bold {
				return linkTarget(s[i:])
			}
			inLink = i
			i += 2
		case strings.HasPrefix(s[i:], "]]"):
			inLink = -1
			i += 2
		default:
			i++
		}
	}
	return ""
}

// linkTarget returns the target of the link at the start of s.
func linkTarget(s string) string {
	end := strings.Index(s, "]]")
	if end < 0 {
		return ""
	}
	target, _, _ := strings.Cut(s[2:end], "|")
	return target
}

// articleTitle turns a link target into the title of the article, as
// Wikipedia shows it.
func articleTitle(target string) string {
	target = html.UnescapeString(target)
	target, _, _ = strings.Cut(target, "#")
	target = strings.ReplaceAll(target, "_", " ")
	target = strings.TrimPrefix(strings.TrimSpace(target), ":")
	target = strings.Join(strings.Fields(target), " ")
	if target == "" || strings.ContainsAny(target, "{}<>[]") {
		return ""
	}
	// Wikipedia capitalises the first letter of every title.
	r, size := utf8.DecodeRuneInString(target)
	return string(unicode.ToUpper(r)) + target[size:]
}

// expandTemplate returns the text a simple formatting template stands for,
// given what's between its braces. It reports false for any template it
// doesn't know.
func expandTemplate(body string) (string, bool) {
	parts := strings.Split(body, "|")
	name := strings.ToLower(strings.TrimSpace(strings.ReplaceAll(parts[0], "_", " ")))
	args := templateArgs(parts[1:])
	first := func() string {
		if len(args) == 0 {
			return ""
		}
		return args[0]
	}
	last := func() string {
		if len(args) == 0 {
			return ""
		}
		return args[len(args)-1]
	}

	switch name {
	case "`s", "'s":
		return "'s", true
	case "`", "'":
		return "'", true
	case `"'`:
		return `"'`, true
	case `'"`, `' "`:
		return `'"`, true
	case "-?":
		return "?", true
	case `-"`:
		return `"`, true
	case "circa":
		return strings.TrimSpace("c. " + first()), true
	case "nbsp":
		return "\u00a0", true
	case "ndash", "en dash":
		return "–", true
	case "mdash", "em dash":
		return "—", true
	case "snd", "spaced ndash", "spnd", "sndash":
		return "\u00a0– ", true
	case "mdashb", "spaced mdash":
		return "\u00a0— ", true
	case "dollarsign", "$":
		return "$", true
	case "okina":
		return "ʻ", true
	case "shy", "zwsp", "wbr", "lrm", "rlm", "anchor":
		return "", true
	case "nowrap", "nobreak", "nobr", "nobold", "noitalic", "small", "smaller", "big", "larger",
		"abbr", "visible anchor", "tooltip", "smallcaps", "sc", "ill", "interlanguage link":
		return first(), true
	case "lang", "transl", "transliteration":
		return last(), true
	}
	return "", false
}

// templateArgs returns a template's positional arguments, dropping named
// ones but keeping those named by their position, such as 1=.
func templateArgs(parts []string) []string {
	var args []string
	for _, p := range parts {
		if k, v, ok := strings.Cut(p, "="); ok && namedArg.MatchString(k) {
			if n, err := strconv.Atoi(strings.TrimSpace(k)); err == nil && n == len(args)+1 {
				args = append(args, v)
			}
			continue
		}
		args = append(args, p)
	}
	return args
}
