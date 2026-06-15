package google

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestListThreads(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/users/me/threads" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("unexpected auth: %s", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"threads":[{"id":"thread1","snippet":"Hello world"}],"nextPageToken":""}`))
	}))
	defer ts.Close()

	c := &Client{token: "test-token", gmailBaseURL: ts.URL, client: ts.Client()}
	resp, err := c.ListThreads(context.Background(), "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Threads) != 1 {
		t.Fatalf("expected 1 thread, got %d", len(resp.Threads))
	}
	if resp.Threads[0].ID != "thread1" || resp.Threads[0].Snippet != "Hello world" {
		t.Fatalf("unexpected thread: %#v", resp.Threads[0])
	}
}

func TestListThreadsWithQuery(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		if q != "from:user@example.com" {
			t.Fatalf("unexpected query: %s", q)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"threads":[],"nextPageToken":""}`))
	}))
	defer ts.Close()

	c := &Client{token: "t", gmailBaseURL: ts.URL, client: ts.Client()}
	resp, err := c.ListThreads(context.Background(), "from:user@example.com", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Threads) != 0 {
		t.Fatalf("expected 0 threads")
	}
}

func TestGetThread(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/users/me/threads/thread123" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"thread123","snippet":"Email content","historyId":1000}`))
	}))
	defer ts.Close()

	c := &Client{token: "t", gmailBaseURL: ts.URL, client: ts.Client()}
	thread, err := c.GetThread(context.Background(), "thread123")
	if err != nil {
		t.Fatal(err)
	}
	if thread.ID != "thread123" || thread.Snippet != "Email content" {
		t.Fatalf("unexpected thread: %#v", thread)
	}
}

func TestSendMessage(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/users/me/messages/send" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"msg123","threadId":"thread123","snippet":"Sent email","labelIds":["SENT"]}`))
	}))
	defer ts.Close()

	c := &Client{token: "t", gmailBaseURL: ts.URL, client: ts.Client()}
	msg, err := c.SendMessage(context.Background(), "to@example.com", "Subject", "Body text")
	if err != nil {
		t.Fatal(err)
	}
	if msg.ID != "msg123" || msg.ThreadID == "" {
		t.Fatalf("unexpected message: %#v", msg)
	}
}

func TestListEvents(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/calendars/primary/events" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("singleEvents") != "true" {
			t.Fatalf("expected singleEvents=true, got %s", r.URL.Query().Get("singleEvents"))
		}
		if r.URL.Query().Get("timeMin") == "" {
			t.Fatal("expected timeMin to be set")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"items":[{"id":"event1","summary":"Meeting","start":{"dateTime":"2026-06-16T10:00:00Z"},"end":{"dateTime":"2026-06-16T11:00:00Z"},"htmlLink":"https://calendar.google.com/event?eid=event1","creator":{"email":"user@example.com"}}],"nextPageToken":""}`))
	}))
	defer ts.Close()

	c := &Client{token: "t", calendarBaseURL: ts.URL, client: ts.Client()}
	resp, err := c.ListEvents(context.Background(), "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 event, got %d", len(resp.Items))
	}
	if resp.Items[0].Summary != "Meeting" || resp.Items[0].Creator.Email != "user@example.com" {
		t.Fatalf("unexpected event: %#v", resp.Items[0])
	}
}

func TestListEventsCustomCalendar(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/calendars/work@example.com/events" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"items":[],"nextPageToken":""}`))
	}))
	defer ts.Close()

	c := &Client{token: "t", calendarBaseURL: ts.URL, client: ts.Client()}
	resp, err := c.ListEvents(context.Background(), "work@example.com", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Items) != 0 {
		t.Fatalf("expected 0 events")
	}
}

func TestCreateEvent(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/calendars/primary/events" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"event2","summary":"New Event","htmlLink":"https://calendar.google.com/calendar/event?eid=event2","start":{"dateTime":"2026-06-17T14:00:00Z"},"end":{"dateTime":"2026-06-17T15:00:00Z"},"creator":{"email":"user@example.com"}}`))
	}))
	defer ts.Close()

	c := &Client{token: "t", calendarBaseURL: ts.URL, client: ts.Client()}
	ev, err := c.CreateEvent(context.Background(), "New Event", "Description", "2026-06-17T14:00:00Z", "2026-06-17T15:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if ev.Summary != "New Event" || ev.ID == "" {
		t.Fatalf("unexpected event: %#v", ev)
	}
}

func TestListFiles(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/files" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"files":[{"id":"file1","name":"doc.pdf","mimeType":"application/pdf","size":1024,"webViewLink":"https://drive.google.com/file/d/file1/view","createdTime":"2026-01-01T00:00:00Z","modifiedTime":"2026-06-01T00:00:00Z"}],"nextPageToken":""}`))
	}))
	defer ts.Close()

	c := &Client{token: "t", driveBaseURL: ts.URL, client: ts.Client()}
	resp, err := c.ListFiles(context.Background(), "", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(resp.Files))
	}
	if resp.Files[0].Name != "doc.pdf" || resp.Files[0].MimeType != "application/pdf" {
		t.Fatalf("unexpected file: %#v", resp.Files[0])
	}
}

func TestListFilesWithQuery(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		if q != "name contains 'report'" {
			t.Fatalf("unexpected query: %s", q)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"files":[],"nextPageToken":""}`))
	}))
	defer ts.Close()

	c := &Client{token: "t", driveBaseURL: ts.URL, client: ts.Client()}
	resp, err := c.ListFiles(context.Background(), "name contains 'report'", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Files) != 0 {
		t.Fatalf("expected 0 files")
	}
}

func TestSearchFiles(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		if q == "" {
			t.Fatal("expected query parameter")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"files":[{"id":"file2","name":"budget.xlsx","mimeType":"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet","size":4096,"webViewLink":"https://drive.google.com/file/d/file2/view","createdTime":"2026-02-01T00:00:00Z","modifiedTime":"2026-05-01T00:00:00Z"}],"nextPageToken":""}`))
	}))
	defer ts.Close()

	c := &Client{token: "t", driveBaseURL: ts.URL, client: ts.Client()}
	resp, err := c.SearchFiles(context.Background(), "name contains 'budget'")
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(resp.Files))
	}
	if resp.Files[0].ID != "file2" {
		t.Fatalf("unexpected file ID: %s", resp.Files[0].ID)
	}
}

func TestAPIError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"invalid_token","error_description":"Invalid Credentials"}`))
	}))
	defer ts.Close()

	c := &Client{token: "bad", gmailBaseURL: ts.URL, client: ts.Client()}
	_, err := c.ListThreads(context.Background(), "", 10)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Fatalf("expected 401 in error, got: %s", err.Error())
	}
}

func TestGetUserInfo(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("unexpected auth: %s", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"123","email":"user@gmail.com","name":"Test User"}`))
	}))
	defer ts.Close()

	c := &Client{token: "test-token", userinfoBaseURL: ts.URL, client: ts.Client()}
	info, err := c.GetUserInfo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.Email != "user@gmail.com" || info.ID != "123" || info.Name != "Test User" {
		t.Fatalf("unexpected user info: %#v", info)
	}
}

func TestNewClient(t *testing.T) {
	c := NewClient("test-token")
	if c.token != "test-token" {
		t.Fatalf("expected test-token, got %s", c.token)
	}
}
