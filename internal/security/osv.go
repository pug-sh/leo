package security

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// OSV looks up a vulnerability's aliases on osv.dev.
type OSV struct {
	baseURL string
	http    *http.Client
	cache   map[string][]string
}

func NewOSV(baseURL string) *OSV {
	return &OSV{baseURL: baseURL, http: &http.Client{Timeout: 30 * time.Second}, cache: map[string][]string{}}
}

func (o *OSV) Aliases(ctx context.Context, id string) ([]string, error) {
	if aliases, ok := o.cache[id]; ok {
		return aliases, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, o.baseURL+"/v1/vulns/"+url.PathEscape(id), nil)
	if err != nil {
		return nil, err
	}
	resp, err := o.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("osv.dev: %s: %w", id, err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		o.cache[id] = nil
		return nil, nil
	default:
		return nil, fmt.Errorf("osv.dev: %s: %s", id, resp.Status)
	}
	var vuln struct {
		Aliases []string `json:"aliases"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&vuln); err != nil {
		return nil, fmt.Errorf("osv.dev: %s: %w", id, err)
	}
	o.cache[id] = vuln.Aliases
	return vuln.Aliases, nil
}
