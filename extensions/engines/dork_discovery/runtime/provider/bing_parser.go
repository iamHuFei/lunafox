package provider

import (
	"encoding/base64"
	"net/url"
	"strings"

	"golang.org/x/net/html"
)

// ParseBingResults extracts organic hits from a Bing results page. Extraction
// is deliberately lenient: every b_algo block that yields no usable URL counts
// as an anomaly instead of failing the page, so Bing markup drift degrades to
// an observable anomaly count rather than a broken engine.
func ParseBingResults(body string) ([]Hit, int) {
	root, err := html.Parse(strings.NewReader(body))
	if err != nil {
		return nil, 0
	}
	hits := make([]Hit, 0, 16)
	anomalies := 0
	for _, node := range findNodes(root, isBingResultBlock) {
		hit, ok := parseBingResultBlock(node)
		if !ok {
			anomalies++
			continue
		}
		hits = append(hits, hit)
	}
	return hits, anomalies
}

// ParseBingNextPageURL extracts the pagination href Bing rendered for the
// next page (carrying the FPIG session fingerprint). Empty when the page has
// no further results.
func ParseBingNextPageURL(body string) string {
	root, err := html.Parse(strings.NewReader(body))
	if err != nil {
		return ""
	}
	var nextURL string
	for _, node := range findNodes(root, func(node *html.Node) bool {
		return node.Type == html.ElementNode && node.Data == "a"
	}) {
		href := attrValue(node, "href")
		if href == "" {
			continue
		}
		parsed, err := url.Parse(href)
		if err != nil || !strings.HasPrefix(parsed.Path, "/search") {
			continue
		}
		query := parsed.Query()
		first, hasFirst := query["first"]
		if !hasFirst || len(first) == 0 || first[0] == "1" {
			continue
		}
		if nextURL == "" {
			nextURL = href
		}
	}
	return nextURL
}

func isBingResultBlock(node *html.Node) bool {
	return node.Type == html.ElementNode && node.Data == "li" && hasTokenInClass(node, "b_algo")
}

func parseBingResultBlock(block *html.Node) (Hit, bool) {
	anchor := findFirst(block, func(node *html.Node) bool {
		return node.Type == html.ElementNode && node.Data == "h2"
	})
	var href, title string
	if anchor != nil {
		if link := findFirst(anchor, func(node *html.Node) bool {
			return node.Type == html.ElementNode && node.Data == "a"
		}); link != nil {
			href = attrValue(link, "href")
			title = nodeText(link)
		}
	}
	resolved := resolveBingURL(href)
	if resolved == "" {
		// The cite line carries a display-trimmed host/path hint; it repairs
		// scheme-less anchors but never overrides a working href.
		cite := findFirst(block, func(node *html.Node) bool {
			return node.Type == html.ElementNode && node.Data == "cite"
		})
		if cite != nil {
			resolved = repairSchemelessURL(strings.TrimSpace(nodeText(cite)))
		}
	}
	if resolved == "" {
		return Hit{}, false
	}
	parsed, err := url.Parse(resolved)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return Hit{}, false
	}
	if isBingSelfLink(parsed) {
		return Hit{}, false
	}
	return Hit{URL: parsed.String(), Title: strings.TrimSpace(title)}, true
}

// resolveBingURL unwraps Bing's click-tracking redirects. Bing wraps outbound
// links as /ck/a?...&u=a1<base64url(payload)> where the payload is the real
// URL (historically prefixed "a1" is part of the armor, not the payload).
func resolveBingURL(href string) string {
	href = strings.TrimSpace(href)
	if href == "" {
		return ""
	}
	if strings.HasPrefix(href, "//") {
		href = "https:" + href
	}
	parsed, err := url.Parse(href)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return ""
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "www.bing.com" || host == "cn.bing.com" || host == "bing.com" {
		if strings.HasPrefix(parsed.Path, "/ck/") {
			if encoded := parsed.Query().Get("u"); strings.HasPrefix(encoded, "a1") {
				// Bing emits unpadded base64url; restore the padding before
				// decoding (a length ≡ 1 mod 4 is undecodable by construction).
				armored := strings.TrimPrefix(encoded, "a1")
				if len(armored)%4 == 1 {
					return ""
				}
				if len(armored)%4 != 0 {
					armored += strings.Repeat("=", 4-len(armored)%4)
				}
				if decoded, err := base64.URLEncoding.DecodeString(armored); err == nil {
					candidate := string(decoded)
					if parsedCandidate, err := url.Parse(candidate); err == nil &&
						(parsedCandidate.Scheme == "http" || parsedCandidate.Scheme == "https") &&
						parsedCandidate.Host != "" {
						return parsedCandidate.String()
					}
				}
			}
			// Some variants put the target in u without the a1 armor.
			if plain := parsed.Query().Get("u"); strings.HasPrefix(plain, "http") {
				return plain
			}
		}
		return ""
	}
	return parsed.String()
}

// repairSchemelessURL rebuilds cite hints like "example.com › path" or
// "https://example.com/path" (possibly with display ellipses) into URLs.
func repairSchemelessURL(hint string) string {
	hint = strings.TrimSpace(hint)
	if hint == "" {
		return ""
	}
	if strings.HasPrefix(hint, "http://") || strings.HasPrefix(hint, "https://") {
		if index := strings.Index(hint, " …"); index > 0 {
			hint = hint[:index]
		}
		return hint
	}
	// Display form separates path segments with "›"; only the host survives.
	if space := strings.IndexAny(hint, " ›"); space > 0 {
		hint = hint[:space]
	}
	if strings.Contains(hint, "/") || !strings.Contains(hint, ".") {
		return ""
	}
	return "https://" + hint
}

func isBingSelfLink(parsed *url.URL) bool {
	host := strings.ToLower(parsed.Hostname())
	return host == "bing.com" || strings.HasSuffix(host, ".bing.com") ||
		host == "microsoft.com" || strings.HasSuffix(host, ".microsoft.com") ||
		host == "msn.com" || strings.HasSuffix(host, ".msn.com")
}

func findNodes(root *html.Node, match func(*html.Node) bool) []*html.Node {
	var found []*html.Node
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if match(node) {
			found = append(found, node)
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return found
}

func findFirst(root *html.Node, match func(*html.Node) bool) *html.Node {
	if root == nil {
		return nil
	}
	if match(root) {
		return root
	}
	for child := root.FirstChild; child != nil; child = child.NextSibling {
		if found := findFirst(child, match); found != nil {
			return found
		}
	}
	return nil
}

func hasTokenInClass(node *html.Node, token string) bool {
	for _, attr := range node.Attr {
		if attr.Key != "class" {
			continue
		}
		for _, candidate := range strings.Fields(attr.Val) {
			if candidate == token {
				return true
			}
		}
	}
	return false
}

func attrValue(node *html.Node, key string) string {
	for _, attr := range node.Attr {
		if attr.Key == key {
			return attr.Val
		}
	}
	return ""
}

func nodeText(node *html.Node) string {
	if node == nil {
		return ""
	}
	var builder strings.Builder
	var walk func(*html.Node)
	walk = func(current *html.Node) {
		if current.Type == html.TextNode {
			builder.WriteString(current.Data)
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return builder.String()
}
