package google

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	gmailBaseURL      = "https://gmail.googleapis.com/gmail/v1"
	calendarBaseURL   = "https://www.googleapis.com/calendar/v3"
	driveBaseURL      = "https://www.googleapis.com/drive/v3"
	userinfoBaseURL   = "https://www.googleapis.com/oauth2/v2/userinfo"
)

type Client struct {
	token             string
	gmailBaseURL      string
	calendarBaseURL   string
	driveBaseURL      string
	userinfoBaseURL   string
	client            *http.Client
}

type Thread struct {
	ID        string `json:"id"`
	Snippet   string `json:"snippet"`
	HistoryID uint64 `json:"historyId"`
}

type Message struct {
	ID       string   `json:"id"`
	ThreadID string   `json:"threadId"`
	Snippet  string   `json:"snippet"`
	LabelIDs []string `json:"labelIds"`
	Payload  struct {
		Headers []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"headers"`
	} `json:"payload"`
}

type ThreadListItem struct {
	ID      string `json:"id"`
	Snippet string `json:"snippet"`
}

type ListThreadsResponse struct {
	Threads       []ThreadListItem `json:"threads"`
	NextPageToken string           `json:"nextPageToken"`
}

type eventTime struct {
	DateTime string `json:"dateTime,omitempty"`
	Date     string `json:"date,omitempty"`
}

func (e eventTime) String() string {
	if e.DateTime != "" {
		return e.DateTime
	}
	return e.Date
}

type Event struct {
	ID          string    `json:"id"`
	Summary     string    `json:"summary"`
	Description string    `json:"description"`
	Start       eventTime `json:"start"`
	End         eventTime `json:"end"`
	HTMLLink    string    `json:"htmlLink"`
	Creator  struct {
		Email string `json:"email"`
	} `json:"creator"`
}

type EventListItem struct {
	ID      string    `json:"id"`
	Summary string    `json:"summary"`
	Start   eventTime `json:"start"`
	End     eventTime `json:"end"`
	HTMLLink string  `json:"htmlLink"`
	Creator  struct {
		Email string `json:"email"`
	} `json:"creator"`
}

type ListEventsResponse struct {
	Items         []EventListItem `json:"items"`
	NextPageToken string          `json:"nextPageToken"`
}

type File struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	MimeType     string `json:"mimeType"`
	Size         int64  `json:"size"`
	WebViewLink  string `json:"webViewLink"`
	CreatedTime  string `json:"createdTime"`
	ModifiedTime string `json:"modifiedTime"`
}

type ListFilesResponse struct {
	Files         []File `json:"files"`
	NextPageToken string `json:"nextPageToken"`
}

func NewClient(token string) *Client {
	return &Client{
		token:           token,
		gmailBaseURL:    gmailBaseURL,
		calendarBaseURL: calendarBaseURL,
		driveBaseURL:    driveBaseURL,
		userinfoBaseURL: userinfoBaseURL,
		client:          &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) do(ctx context.Context, baseURL, path string, out any) error {
	u := baseURL + path
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "linea")

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode >= 400 {
		return fmt.Errorf("google api: %s returned %d: %s", path, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	if out != nil {
		return json.Unmarshal(body, out)
	}
	return nil
}

func (c *Client) doPost(ctx context.Context, baseURL, path string, payload, out any) error {
	var bodyReader io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		bodyReader = strings.NewReader(string(data))
	}
	u := baseURL + path
	req, err := http.NewRequestWithContext(ctx, "POST", u, bodyReader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "linea")

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode >= 400 {
		return fmt.Errorf("google api: %s returned %d: %s", path, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	if out != nil {
		return json.Unmarshal(body, out)
	}
	return nil
}

func (c *Client) ListThreads(ctx context.Context, query string, maxResults int) (ListThreadsResponse, error) {
	var resp ListThreadsResponse
	path := "/users/me/threads"
	params := url.Values{}
	if query != "" {
		params.Set("q", query)
	}
	if maxResults > 0 {
		params.Set("maxResults", fmt.Sprintf("%d", maxResults))
	}
	if len(params) > 0 {
		path += "?" + params.Encode()
	}
	return resp, c.do(ctx, c.gmailBaseURL, path, &resp)
}

func (c *Client) GetThread(ctx context.Context, id string) (Thread, error) {
	var thread Thread
	return thread, c.do(ctx, c.gmailBaseURL, fmt.Sprintf("/users/me/threads/%s", url.PathEscape(id)), &thread)
}

func (c *Client) SendMessage(ctx context.Context, to, subject, body string) (Message, error) {
	var msg Message
	encodedBody := base64.StdEncoding.EncodeToString([]byte(body))
	raw := fmt.Sprintf("To: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: base64\r\n\r\n%s", to, subject, encodedBody)
	encoded := base64.RawURLEncoding.EncodeToString([]byte(raw))
	payload := map[string]string{"raw": encoded}
	return msg, c.doPost(ctx, c.gmailBaseURL, "/users/me/messages/send", payload, &msg)
}

func (c *Client) ListEvents(ctx context.Context, calendarID string, maxResults int) (ListEventsResponse, error) {
	var resp ListEventsResponse
	if calendarID == "" {
		calendarID = "primary"
	}
	path := fmt.Sprintf("/calendars/%s/events", url.PathEscape(calendarID))
	params := url.Values{}
	params.Set("timeMin", time.Now().UTC().Format(time.RFC3339))
	params.Set("singleEvents", "true")
	if maxResults > 0 {
		params.Set("maxResults", fmt.Sprintf("%d", maxResults))
	}
	if len(params) > 0 {
		path += "?" + params.Encode()
	}
	return resp, c.do(ctx, c.calendarBaseURL, path, &resp)
}

func (c *Client) CreateEvent(ctx context.Context, summary, description, startTime, endTime string) (Event, error) {
	var event Event
	payload := map[string]any{
		"summary":     summary,
		"description": description,
		"start":       map[string]string{"dateTime": startTime},
		"end":         map[string]string{"dateTime": endTime},
	}
	return event, c.doPost(ctx, c.calendarBaseURL, "/calendars/primary/events", payload, &event)
}

func (c *Client) ListFiles(ctx context.Context, query string, maxResults int) (ListFilesResponse, error) {
	var resp ListFilesResponse
	path := "/files"
	params := url.Values{}
	if query != "" {
		params.Set("q", query)
	}
	if maxResults > 0 {
		params.Set("pageSize", fmt.Sprintf("%d", maxResults))
	}
	if len(params) > 0 {
		path += "?" + params.Encode()
	}
	return resp, c.do(ctx, c.driveBaseURL, path, &resp)
}

func (c *Client) SearchFiles(ctx context.Context, query string) (ListFilesResponse, error) {
	return c.ListFiles(ctx, query, 0)
}

type UserInfo struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
}

func (c *Client) GetUserInfo(ctx context.Context) (*UserInfo, error) {
	var info UserInfo
	return &info, c.do(ctx, c.userinfoBaseURL, "", &info)
}
