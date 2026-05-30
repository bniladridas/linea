package search

import "testing"

func TestShouldSearch(t *testing.T) {
	tests := []struct {
		name    string
		message string
		want    bool
	}{
		{name: "latest", message: "what is the latest Go release", want: true},
		{name: "web", message: "search the web for Linea", want: true},
		{name: "ordinary chat", message: "help me write a function", want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ShouldSearch(test.message); got != test.want {
				t.Fatalf("ShouldSearch(%q) = %v, want %v", test.message, got, test.want)
			}
		})
	}
}

func TestQueryFromMessage(t *testing.T) {
	tests := []struct {
		name    string
		message string
		want    string
	}{
		{name: "search", message: "search OpenAI", want: "OpenAI"},
		{name: "web search", message: "search the web for OpenAI", want: "OpenAI"},
		{name: "polite web search", message: "please search the web for Go release", want: "Go release"},
		{name: "plain latest", message: "what is the latest Go release", want: "what is the latest Go release"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := QueryFromMessage(test.message); got != test.want {
				t.Fatalf("QueryFromMessage(%q) = %q, want %q", test.message, got, test.want)
			}
		})
	}
}
