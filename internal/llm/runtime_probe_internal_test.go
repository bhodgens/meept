package llm

import "testing"

// TestSpawnCommandBindsPort pins how the duplicate-spawn pre-check decides that
// a spawn command is the thing that binds an endpoint port. Whole-token matching
// is the point: a model path or host that merely contains the digits must not
// arm the check.
func TestSpawnCommandBindsPort(t *testing.T) {
	cases := []struct {
		name  string
		spawn []string
		port  string
		want  bool
	}{
		{
			name:  "llama-server --port form",
			spawn: []string{"llama-server", "--model", "/m/y.gguf", "--port", "8081", "--host", "127.0.0.1"},
			port:  "8081",
			want:  true,
		},
		{
			name:  "mlx_lm --port form",
			spawn: []string{"mlx_lm", "server", "--model", "/m/x", "--port", "8082"},
			port:  "8082",
			want:  true,
		},
		{
			name:  "assignment form",
			spawn: []string{"prompt_router_sidecar.py", "ROUTER_PORT=8082"},
			port:  "8082",
			want:  true,
		},
		{
			name:  "different port",
			spawn: []string{"mlx_lm", "server", "--port", "18081"},
			port:  "8081",
			want:  false,
		},
		{
			name:  "port digits inside a model path do not count",
			spawn: []string{"mlx_lm", "server", "--model", "/models/8081.gguf"},
			port:  "8081",
			want:  false,
		},
		{
			name:  "port digits inside a host do not count",
			spawn: []string{"server", "--host", "127.0.0.1", "--port", "9000"},
			port:  "8081",
			want:  false,
		},
		{
			name:  "no port configured on the command",
			spawn: []string{"sleep", "300"},
			port:  "8081",
			want:  false,
		},
		{
			name:  "empty port never matches",
			spawn: []string{"mlx_lm", "server", "--port", ""},
			port:  "",
			want:  false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := spawnCommandBindsPort(tc.spawn, tc.port); got != tc.want {
				t.Errorf("spawnCommandBindsPort(%v, %q) = %v, want %v", tc.spawn, tc.port, got, tc.want)
			}
		})
	}
}
