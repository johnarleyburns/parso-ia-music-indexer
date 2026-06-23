package ia

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type SimpleListEntry struct {
	Identifier string `json:"identifier"`
	Title      string `json:"title,omitempty"`
}

type SimpleListPatch struct {
	Op      string          `json:"op"`
	Parent  string          `json:"parent"`
	List    string          `json:"list"`
	Notes   json.RawMessage `json:"notes,omitempty"`
}

func AddToList(ctx context.Context, client *http.Client, creds *IACredentials, parentID, listName, childID string) error {
	return postSimpleListPatch(ctx, client, creds, childID, SimpleListPatch{
		Op:     "set",
		Parent: parentID,
		List:   listName,
	})
}

func RemoveFromList(ctx context.Context, client *http.Client, creds *IACredentials, parentID, listName, childID string) error {
	return postSimpleListPatch(ctx, client, creds, childID, SimpleListPatch{
		Op:     "delete",
		Parent: parentID,
		List:   listName,
	})
}

func ListItems(ctx context.Context, client *http.Client, parentID, listName string) ([]SimpleListEntry, error) {
	query := url.Values{}
	query.Set("q", fmt.Sprintf("simplelists__%s:%s", listName, parentID))
	query.Set("fl[]", "identifier,title")
	query.Set("rows", "1000")
	query.Set("output", "json")
	query.Set("sort[]", "addeddate desc")

	apiURL := "https://archive.org/advancedsearch.php?" + query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("list items request: %w", err)
	}

	resp, err := DoWithRetry(ctx, client, req)
	if err != nil {
		return nil, fmt.Errorf("list items: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Response struct {
			Docs []struct {
				Identifier string `json:"identifier"`
				Title      string `json:"title"`
			} `json:"docs"`
		} `json:"response"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode list items: %w", err)
	}

	entries := make([]SimpleListEntry, len(result.Response.Docs))
	for i, doc := range result.Response.Docs {
		entries[i] = SimpleListEntry{Identifier: doc.Identifier, Title: doc.Title}
	}

	return entries, nil
}

func ListUserLists(ctx context.Context, client *http.Client, parentID string) ([]string, error) {
	query := url.Values{}
	query.Set("q", fmt.Sprintf("simplelists__catchall:%s", parentID))
	query.Set("fl[]", "simplelists")
	query.Set("rows", "1000")
	query.Set("output", "json")

	apiURL := "https://archive.org/advancedsearch.php?" + query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("list user lists request: %w", err)
	}

	resp, err := DoWithRetry(ctx, client, req)
	if err != nil {
		return nil, fmt.Errorf("list user lists: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Response struct {
			Docs []struct {
				SimpleLists []string `json:"simplelists"`
			} `json:"docs"`
		} `json:"response"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode user lists: %w", err)
	}

	seen := make(map[string]bool)
	var lists []string
	for _, doc := range result.Response.Docs {
		for _, sl := range doc.SimpleLists {
			parts := strings.SplitN(sl, ":", 2)
			if len(parts) == 2 && strings.HasPrefix(parts[1], parentID) {
				name := strings.TrimPrefix(sl, parts[0]+":")
				if name == parentID && !seen[parts[0]] {
					seen[parts[0]] = true
					lists = append(lists, parts[0])
				}
			}
		}
	}

	return lists, nil
}

func NewAuthenticatedClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
	}
}

func postSimpleListPatch(ctx context.Context, client *http.Client, creds *IACredentials, childID string, patch SimpleListPatch) error {
	urlStr := fmt.Sprintf("https://archive.org/metadata/%s", childID)

	body := map[string]interface{}{
		"-target": "simplelists",
		"-patch":  patch,
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal patch: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, urlStr, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("patch request: %w", err)
	}

	req.Header.Set("Authorization", creds.AuthHeader())
	req.Header.Set("Content-Type", "application/json")

	resp, err := DoWithRetry(ctx, client, req)
	if err != nil {
		return fmt.Errorf("simplelist patch: %w", err)
	}
	resp.Body.Close()

	return nil
}
