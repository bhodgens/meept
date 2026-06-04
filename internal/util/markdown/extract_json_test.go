package markdown

import "testing"

func TestExtractJSON(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "fenced json",
			content: "Some text\n\n```json\n{\"foo\": \"bar\"}\n```\n",
			want:    `{"foo": "bar"}`,
		},
		{
			name:    "fenced without lang",
			content: "```\n{\"foo\": \"bar\"}\n```",
			want:    `{"foo": "bar"}`,
		},
		{
			name:    "inline json",
			content: `{"foo": "bar"}`,
			want:    `{"foo": "bar"}`,
		},
		{
			name:    "text plus json block",
			content: "Here is the result:\n\n```json\n{\"result\": 42}\n```\n\nMore text.",
			want:    `{"result": 42}`,
		},
		{
			name:    "multiple blocks picks first valid",
			content: "```json\n{\"a\": 1}\n```\n\n```json\n{\"b\": 2}\n```",
			want:    `{"a": 1}`,
		},
		{
			name:    "broken markdown falls back",
			content: "```json\nnot json\n```\n\n{\"valid\": true}",
			want:    `{"valid": true}`,
		},
		{
			name:    "empty input",
			content: "",
			want:    "",
		},
		{
			name:    "invalid json",
			content: "just plain text",
			want:    "",
		},
		{
			name:    "nested code block",
			content: "hello\n\n```json\n{\"safe\": true}\n```",
			want:    `{"safe": true}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := string(ExtractJSON(tt.content))
			if got != tt.want {
				t.Errorf("ExtractJSON() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractJSONArray(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "fenced array",
			content: "```json\n[{\"a\": 1}, {\"b\": 2}]\n```",
			want:    `[{"a": 1}, {"b": 2}]`,
		},
		{
			name:    "object is not array",
			content: "```json\n{\"foo\": \"bar\"}\n```",
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := string(ExtractJSONArray(tt.content))
			if got != tt.want {
				t.Errorf("ExtractJSONArray() = %q, want %q", got, tt.want)
			}
		})
	}
}
