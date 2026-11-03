package pubsubstate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client is a thin REST wrapper over the portal's HTTP API, scoped to
// the surface the reconciler needs (vendors / topics / schemas /
// examples). All requests carry the operator API key as a Bearer token.
type Client struct {
	baseURL  *url.URL
	apiKey   string
	http     *http.Client
	dryRunFn func(method, path string)
}

// NewClient constructs a Client. baseURL is the portal root (e.g.
// "https://portal.nova.netbird.nova.example" or
// "http://127.0.0.1:8088"). apiKey is the operator API key.
func NewClient(baseURL, apiKey string) (*Client, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse base URL: %w", err)
	}
	return &Client{
		baseURL: u,
		apiKey:  apiKey,
		http:    &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// SetDryRun installs a callback that's invoked instead of executing
// mutating methods. The callback receives (method, path) for each
// would-be call. Read methods (List, Get) still execute against the live
// portal so the diff is real.
func (c *Client) SetDryRun(fn func(method, path string)) { c.dryRunFn = fn }

// --- vendors ---------------------------------------------------------

type vendorListResp struct {
	Vendors []VendorRow `json:"vendors"`
}

// VendorRow is the public-projection vendor shape returned by GET
// /api/vendors. Mirrors the JSON tags on registry.Vendor minus
// secrets.
type VendorRow struct {
	Prefix      string  `json:"prefix"`
	DisplayName string  `json:"display_name"`
	Synthetic   bool    `json:"synthetic"`
	ApprovedAt  *int64  `json:"approved_at,omitempty"`
	Approver    *string `json:"approver,omitempty"`
}

func (c *Client) ListVendors(ctx context.Context) ([]VendorRow, error) {
	var resp vendorListResp
	if err := c.do(ctx, "GET", "/api/vendors", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Vendors, nil
}

type createVendorReq struct {
	Prefix      string `json:"prefix"`
	DisplayName string `json:"display_name,omitempty"`
	Synthetic   bool   `json:"synthetic,omitempty"`
	AutoApprove bool   `json:"auto_approve,omitempty"`
}

func (c *Client) CreateVendor(ctx context.Context, prefix, displayName string, synthetic, autoApprove bool) error {
	body := createVendorReq{Prefix: prefix, DisplayName: displayName, Synthetic: synthetic, AutoApprove: autoApprove}
	err := c.do(ctx, "POST", "/api/vendors", body, nil)
	if err != nil {
		// 409 conflict = vendor already exists; treat as idempotent OK.
		if isStatusCode(err, http.StatusConflict) {
			return nil
		}
		return err
	}
	return nil
}

type patchVendorReq struct {
	DisplayName string `json:"display_name"`
}

func (c *Client) UpdateVendorDisplayName(ctx context.Context, prefix, displayName string) error {
	return c.do(ctx, "PATCH", "/api/vendors/"+url.PathEscape(prefix), patchVendorReq{DisplayName: displayName}, nil)
}

func (c *Client) DeleteVendor(ctx context.Context, prefix string) error {
	return c.do(ctx, "DELETE", "/api/vendors/"+url.PathEscape(prefix), nil, nil)
}

// --- topics ----------------------------------------------------------

type topicListResp struct {
	Topics []TopicRow `json:"topics"`
}

// TopicRow mirrors topicEntry in the API handlers.
type TopicRow struct {
	ID               int64  `json:"id"`
	VendorPrefix     string `json:"vendor_prefix"`
	KeyPattern       string `json:"key_pattern"`
	Format           string `json:"format"`
	SchemaRef        string `json:"schema_ref"`
	Description      string `json:"description"`
	PublishToStorage bool   `json:"publish_to_storage"`
}

func (c *Client) ListTopics(ctx context.Context, vendor string) ([]TopicRow, error) {
	var resp topicListResp
	if err := c.do(ctx, "GET", "/api/vendors/"+url.PathEscape(vendor)+"/topics", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Topics, nil
}

type createTopicReq struct {
	KeyPattern       string `json:"key_pattern"`
	Format           string `json:"format"`
	SchemaRef        string `json:"schema_ref,omitempty"`
	Description      string `json:"description,omitempty"`
	PublishToStorage bool   `json:"publish_to_storage"`
}

// CreateTopic returns the new topic's ID, or 0 + nil error when the
// topic already existed (409 conflict). Caller can re-list to fetch the
// existing ID in that case.
func (c *Client) CreateTopic(ctx context.Context, vendor string, t TopicDoc) (int64, error) {
	var resp TopicRow
	err := c.do(ctx, "POST", "/api/vendors/"+url.PathEscape(vendor)+"/topics", createTopicReq{
		KeyPattern:       t.KeyPattern,
		Format:           t.Format,
		SchemaRef:        t.SchemaRef,
		Description:      t.Description,
		PublishToStorage: t.PublishToStorage,
	}, &resp)
	if err != nil {
		if isStatusCode(err, http.StatusConflict) {
			return 0, nil
		}
		return 0, err
	}
	return resp.ID, nil
}

type updateTopicReq struct {
	Format           string `json:"format"`
	SchemaRef        string `json:"schema_ref"`
	Description      string `json:"description"`
	PublishToStorage bool   `json:"publish_to_storage"`
}

func (c *Client) UpdateTopic(ctx context.Context, vendor string, id int64, t TopicDoc) error {
	return c.do(ctx, "PATCH",
		fmt.Sprintf("/api/vendors/%s/topics/%d", url.PathEscape(vendor), id),
		updateTopicReq{
			Format:           t.Format,
			SchemaRef:        t.SchemaRef,
			Description:      t.Description,
			PublishToStorage: t.PublishToStorage,
		}, nil)
}

func (c *Client) RetireTopic(ctx context.Context, vendor string, id int64) error {
	return c.do(ctx, "DELETE",
		fmt.Sprintf("/api/vendors/%s/topics/%d", url.PathEscape(vendor), id), nil, nil)
}

// --- schemas ---------------------------------------------------------

type schemaListResp struct {
	Schemas []SchemaRow `json:"schemas"`
}

// SchemaRow mirrors registry.TopicSchema minus the file_path.
type SchemaRow struct {
	ID            int64  `json:"id"`
	TopicID       int64  `json:"topic_id"`
	Version       string `json:"version"`
	Compatibility string `json:"compatibility"`
	Format        string `json:"format"`
	SizeBytes     int64  `json:"size_bytes"`
	SHA256        string `json:"sha256"`
}

func (c *Client) ListSchemas(ctx context.Context, vendor string, topicID int64) ([]SchemaRow, error) {
	var resp schemaListResp
	if err := c.do(ctx, "GET",
		fmt.Sprintf("/api/vendors/%s/topics/%d/schemas", url.PathEscape(vendor), topicID), nil, &resp); err != nil {
		return nil, err
	}
	return resp.Schemas, nil
}

// UploadSchema sends multipart/form-data matching the handler's expected
// shape: version, compatibility, format as form fields and the body as
// the "file" field.
func (c *Client) UploadSchema(ctx context.Context, vendor string, topicID int64, meta SchemaMeta, body []byte) error {
	path := fmt.Sprintf("/api/vendors/%s/topics/%d/schemas", url.PathEscape(vendor), topicID)
	if c.dryRunFn != nil {
		c.dryRunFn("POST", path)
		return nil
	}

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("version", meta.Version)
	_ = mw.WriteField("compatibility", meta.Compatibility)
	_ = mw.WriteField("format", meta.Format)
	fw, err := mw.CreateFormFile("file", meta.Version+"."+schemaExtForFormat(meta.Format))
	if err != nil {
		return fmt.Errorf("create form file: %w", err)
	}
	if _, err := fw.Write(body); err != nil {
		return fmt.Errorf("write form file: %w", err)
	}
	if err := mw.Close(); err != nil {
		return fmt.Errorf("close multipart: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.url(path), &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("upload schema: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		// 409 = (topic, version) already exists; treat as OK (idempotent).
		if resp.StatusCode == http.StatusConflict {
			return nil
		}
		return &HTTPError{Status: resp.StatusCode, Body: string(b)}
	}
	return nil
}

// --- code examples ---------------------------------------------------

type putExampleReq struct {
	Code string `json:"code"`
}

func (c *Client) UpsertCodeExample(ctx context.Context, vendor string, topicID int64, language, code string) error {
	return c.do(ctx, "PUT",
		fmt.Sprintf("/api/vendors/%s/topics/%d/examples/%s",
			url.PathEscape(vendor), topicID, url.PathEscape(language)),
		putExampleReq{Code: code}, nil)
}

func (c *Client) DeleteCodeExample(ctx context.Context, vendor string, topicID int64, language string) error {
	return c.do(ctx, "DELETE",
		fmt.Sprintf("/api/vendors/%s/topics/%d/examples/%s",
			url.PathEscape(vendor), topicID, url.PathEscape(language)), nil, nil)
}

type exampleListResp struct {
	Examples []ExampleRow `json:"examples"`
}

// ExampleRow mirrors codeExampleResponse from the API handler.
type ExampleRow struct {
	TopicID   int64  `json:"topic_id"`
	Language  string `json:"language"`
	Code      string `json:"code"`
	UpdatedAt int64  `json:"updated_at"`
	UpdatedBy string `json:"updated_by"`
}

// ListCodeExamples returns every vendor-uploaded example for one
// topic. Used by the reconciler to plan upserts only on drift.
func (c *Client) ListCodeExamples(ctx context.Context, vendor string, topicID int64) ([]ExampleRow, error) {
	var resp exampleListResp
	if err := c.do(ctx, "GET",
		fmt.Sprintf("/api/vendors/%s/topics/%d/examples", url.PathEscape(vendor), topicID), nil, &resp); err != nil {
		return nil, err
	}
	return resp.Examples, nil
}

// --- transport -------------------------------------------------------

// HTTPError carries the response status + body for non-2xx responses.
type HTTPError struct {
	Status int
	Body   string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.Status, strings.TrimSpace(e.Body))
}

func isStatusCode(err error, code int) bool {
	he := &HTTPError{}
	if as := errorsAs(err, &he); as && he.Status == code {
		return true
	}
	return false
}

// errorsAs is a thin wrapper to avoid importing "errors" twice and to
// keep this file self-contained for the small surface it needs.
func errorsAs(err error, target any) bool {
	if err == nil {
		return false
	}
	type unwrapper interface{ Unwrap() error }
	for err != nil {
		if he, ok := err.(*HTTPError); ok {
			if t, ok := target.(**HTTPError); ok {
				*t = he
				return true
			}
		}
		u, ok := err.(unwrapper)
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

func (c *Client) url(path string) string {
	return c.baseURL.ResolveReference(&url.URL{Path: path}).String()
}

func (c *Client) do(ctx context.Context, method, path string, body any, into any) error {
	mutating := method != "GET" && method != "HEAD"
	if mutating && c.dryRunFn != nil {
		c.dryRunFn(method, path)
		return nil
	}

	var bodyReader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal body: %w", err)
		}
		bodyReader = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.url(path), bodyReader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if into != nil {
		req.Header.Set("Accept", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return &HTTPError{Status: resp.StatusCode, Body: string(b)}
	}
	if into != nil {
		if err := json.NewDecoder(resp.Body).Decode(into); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}
