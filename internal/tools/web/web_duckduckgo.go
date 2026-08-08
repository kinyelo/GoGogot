package web

import (
	"context"
	"fmt"
	"github.com/aspasskiy/gogogot/internal/tools/types"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

func DuckDuckGoSearchTool() types.Tool {
	return types.Tool{
		Name:        "web_search",
		Label:       "Searching the web",
		Description: "Search the web for information using DuckDuckGo. Returns top results with title, URL, and description.",
		DetailFunc: func(input map[string]any) string {
			s, _ := input["query"].(string)
			return s
		},
		Parameters: map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "The search query",
			},
		},
		Required: []string{"query"},
		Handler: func(ctx context.Context, input map[string]any) types.Result {
			return duckDuckGoSearch(ctx, input)
		},
	}
}

func duckDuckGoSearch(ctx context.Context, input map[string]any) types.Result {
	query, err := types.GetString(input, "query")
	if err != nil {
		return types.ErrResult(err)
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	form := url.Values{"q": {query}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://html.duckduckgo.com/html/", strings.NewReader(form.Encode()))
	if err != nil {
		return types.Errf("request error: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; SofieBot/1.0)")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return types.Errf("http error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return types.Errf("DuckDuckGo returned HTTP %d", resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return types.Errf("html parse error: %v", err)
	}

	var sb strings.Builder
	doc.Find(".result").Each(func(i int, s *goquery.Selection) {
		if i >= 5 {
			return
		}

		titleEl := s.Find(".result__title a")
		title := strings.TrimSpace(titleEl.Text())
		href, _ := titleEl.Attr("href")

		snippet := strings.TrimSpace(s.Find(".result__snippet").Text())

		cleanURL := extractDDGURL(href)
		if title == "" || cleanURL == "" {
			return
		}

		fmt.Fprintf(&sb, "%d. %s\n   %s\n   %s\n\n", i+1, title, cleanURL, snippet)
	})

	if sb.Len() == 0 {
		return types.Result{Output: "no results found"}
	}
	return types.Result{Output: sb.String()}
}

func extractDDGURL(href string) string {
	if href == "" {
		return ""
	}
	if strings.HasPrefix(href, "//") {
		href = "https:" + href
	}
	u, err := url.Parse(href)
	if err != nil {
		return href
	}
	if decoded, err := url.QueryUnescape(u.Query().Get("uddg")); err == nil && decoded != "" {
		return decoded
	}
	return href
}
