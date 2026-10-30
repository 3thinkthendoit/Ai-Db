package agent

import "testing"

func TestIsLocalLLMBase(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"http://localhost:11434/v1", true},
		{"http://127.0.0.1:8080/v1", true},
		{"https://api.openai.com/v1", false},
		{"https://token.example.com/v1", false},
	}
	for _, c := range cases {
		if got := isLocalLLMBase(c.in); got != c.want {
			t.Fatalf("%s: got %v want %v", c.in, got, c.want)
		}
	}
}
