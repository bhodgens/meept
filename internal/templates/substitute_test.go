package templates

import (
	"testing"
)

func TestSubstitute_PositionalArgs(t *testing.T) {
	tests := []struct {
		name string
		body string
		args []string
		want string
	}{
		{
			name: "single positional arg",
			body: "Translate to $1.",
			args: []string{"fr"},
			want: "Translate to fr.",
		},
		{
			name: "two positional args",
			body: "Translate the following text to $1.\n\n$2",
			args: []string{"fr", "hello world"},
			want: "Translate the following text to fr.\n\nhello world",
		},
		{
			name: "out of range positional arg becomes empty",
			body: "First: $1, Second: $2, Third: $3",
			args: []string{"only-one"},
			want: "First: only-one, Second: , Third: ",
		},
		{
			name: "no args with positional placeholders",
			body: "Hello $1 $2",
			args: []string{},
			want: "Hello  ",
		},
		{
			name: "empty args slice",
			body: "No args: $1",
			args: nil,
			want: "No args: ",
		},
		{
			name: "all nine positional args",
			body: "$1 $2 $3 $4 $5 $6 $7 $8 $9",
			args: []string{"a", "b", "c", "d", "e", "f", "g", "h", "i"},
			want: "a b c d e f g h i",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Substitute(tt.body, tt.args)
			if got != tt.want {
				t.Errorf("Substitute() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSubstitute_AllArgs(t *testing.T) {
	tests := []struct {
		name string
		body string
		args []string
		want string
	}{
		{
			name: "all args joined",
			body: "Summarize: $@",
			args: []string{"hello", "world", "foo"},
			want: "Summarize: hello world foo",
		},
		{
			name: "$@ with single arg",
			body: "Content: $@",
			args: []string{"solo"},
			want: "Content: solo",
		},
		{
			name: "$@ with no args is empty",
			body: "Content: $@",
			args: []string{},
			want: "Content: ",
		},
		{
			name: "$@ nil args",
			body: "Content: $@",
			args: nil,
			want: "Content: ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Substitute(tt.body, tt.args)
			if got != tt.want {
				t.Errorf("Substitute() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSubstitute_SliceFromN(t *testing.T) {
	tests := []struct {
		name string
		body string
		args []string
		want string
	}{
		{
			name: "slice from index 2",
			body: "Lang: $1, Text: ${@:2}",
			args: []string{"fr", "hello", "world"},
			want: "Lang: fr, Text: hello world",
		},
		{
			name: "slice from index 1 is same as $@",
			body: "${@:1}",
			args: []string{"a", "b", "c"},
			want: "a b c",
		},
		{
			name: "slice from beyond length is empty",
			body: "Result: ${@:5}",
			args: []string{"a", "b"},
			want: "Result: ",
		},
		{
			name: "slice from index 0 clamped to 1",
			body: "${@:0}",
			args: []string{"a", "b"},
			want: "a b",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Substitute(tt.body, tt.args)
			if got != tt.want {
				t.Errorf("Substitute() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSubstitute_SliceFromNWithLength(t *testing.T) {
	tests := []struct {
		name string
		body string
		args []string
		want string
	}{
		{
			name: "one arg from index 2",
			body: "${@:2:1}",
			args: []string{"fr", "hello", "world"},
			want: "hello",
		},
		{
			name: "two args from index 2",
			body: "${@:2:2}",
			args: []string{"fr", "hello", "world", "foo"},
			want: "hello world",
		},
		{
			name: "length exceeds available args",
			body: "${@:2:10}",
			args: []string{"fr", "hello"},
			want: "hello",
		},
		{
			name: "start beyond length",
			body: "${@:5:2}",
			args: []string{"a", "b"},
			want: "",
		},
		{
			name: "length zero",
			body: "${@:1:0}",
			args: []string{"a", "b"},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Substitute(tt.body, tt.args)
			if got != tt.want {
				t.Errorf("Substitute() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSubstitute_UnrecognizedPatterns(t *testing.T) {
	tests := []struct {
		name string
		body string
		args []string
		want string
	}{
		{
			name: "unrecognized $FOO left as-is",
			body: "$FOO is not replaced",
			args: []string{"ignored"},
			want: "$FOO is not replaced",
		},
		{
			name: "unrecognized ${BAR} left as-is",
			body: "${BAR} stays",
			args: []string{"ignored"},
			want: "${BAR} stays",
		},
		{
			name: "dollar sign alone left as-is",
			body: "Price: $50",
			args: []string{"ignored"},
			want: "Price: $50",
		},
		{
			name: "$0 not a valid positional",
			body: "$0 is left alone",
			args: []string{"ignored"},
			want: "$0 is left alone",
		},
		{
			name: "mixed recognized and unrecognized",
			body: "$1 costs $50 and ${unknown} $@",
			args: []string{"item", "rest"},
			want: "item costs $50 and ${unknown} item rest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Substitute(tt.body, tt.args)
			if got != tt.want {
				t.Errorf("Substitute() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSubstitute_RealWorldTemplates(t *testing.T) {
	tests := []struct {
		name string
		body string
		args []string
		want string
	}{
		{
			name: "summarize template",
			body: "Summarize the following text in 2-3 sentences.\nFocus on the key points and actionable takeaways.\n\n$@",
			args: []string{"Go", "is", "a", "statically", "typed", "language"},
			want: "Summarize the following text in 2-3 sentences.\nFocus on the key points and actionable takeaways.\n\nGo is a statically typed language",
		},
		{
			name: "translate template",
			body: "Translate the following text to $1.\nPreserve the original formatting and tone.\n\n$2",
			args: []string{"French", "Hello, how are you?"},
			want: "Translate the following text to French.\nPreserve the original formatting and tone.\n\nHello, how are you?",
		},
		{
			name: "template with no args",
			body: "Pretty-print and validate the following JSON:\n\n$@",
			args: []string{},
			want: "Pretty-print and validate the following JSON:\n\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Substitute(tt.body, tt.args)
			if got != tt.want {
				t.Errorf("Substitute() = %q, want %q", got, tt.want)
			}
		})
	}
}
