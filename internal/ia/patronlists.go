package ia

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type ListResponse struct {
	Success bool      `json:"success"`
	Value   ListValue `json:"value"`
}

type ListValue struct {
	ID          int      `json:"id"`
	ListName    string   `json:"list_name"`
	Description string   `json:"description"`
	IsPrivate   bool     `json:"is_private"`
	DateCreated string   `json:"date_created"`
	DateUpdated string   `json:"date_updated"`
	Members     []Member `json:"members"`
}

type Member struct {
	Identifier string `json:"identifier"`
	MemberID   int    `json:"member_id"`
	DateAdded  string `json:"date_added"`
}

func ListAPIURL(rawURL string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return "", fmt.Errorf("parse url: %w", err)
	}

	segs := strings.Split(strings.Trim(u.Path, "/"), "/")

	var screenname, listID string
	for i, s := range segs {
		switch {
		case strings.HasPrefix(s, "@"):
			screenname = s
		case s == "lists" && i+1 < len(segs):
			listID = segs[i+1]
		}
	}

	if screenname == "" || listID == "" {
		return "", fmt.Errorf("could not find @screenname and list id in path %q", u.Path)
	}
	if _, err := strconv.Atoi(listID); err != nil {
		return "", fmt.Errorf("list id %q is not numeric", listID)
	}

	return fmt.Sprintf("https://archive.org/services/users/%s/lists/%s", screenname, listID), nil
}

func FetchPatronList(ctx context.Context, client *http.Client, rawURL string) (*ListResponse, error) {
	api, err := ListAPIURL(rawURL)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, api, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "parso-ia-list/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", api, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return nil, fmt.Errorf("GET %s: status %d: %s", api, resp.StatusCode, strings.TrimSpace(string(snippet)))
	}

	var out ListResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode json: %w", err)
	}
	if !out.Success {
		return nil, fmt.Errorf("api returned success=false")
	}
	return &out, nil
}
