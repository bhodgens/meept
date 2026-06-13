package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// PiperVoice represents a voice in the Piper voices catalog.
type PiperVoice struct {
	Name     string `json:"name"`
	Quality  string `json:"quality"`
	Language string `json:"language"`
	Size     string `json:"size"`
	URL      string `json:"url"`
	Status   string `json:"status,omitempty"` // "installed" or "available"
}

func newTTSCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tts",
		Short: "Text-to-speech management",
		Long:  `Manage Text-to-Speech (TTS) voices and configuration.`,
	}

	cmd.AddCommand(newTTSVoicesCmd())

	return cmd
}

func newTTSVoicesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "voices",
		Short: "Manage TTS voices",
		Long:  `List, download, and remove Piper TTS voices.`,
	}

	cmd.AddCommand(newTTSVoicesListCmd())
	cmd.AddCommand(newTTSVoicesDownloadCmd())
	cmd.AddCommand(newTTSVoicesRemoveCmd())

	return cmd
}

func newTTSVoicesListCmd() *cobra.Command {
	var format string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List available voices",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTTSVoicesList(format)
		},
	}

	cmd.Flags().StringVar(&format, "format", "text", "Output format: text or json")
	return cmd
}

func newTTSVoicesDownloadCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "download <voice>",
		Short: "Download a voice",
		Args:  cobra.ExactArgs(1),
		Example: `  meept tts voices download danny-medium
  meept tts voices download en_US-lessac-medium`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTTSVoicesDownload(cmd.Context(), args[0])
		},
	}
	return cmd
}

func newTTSVoicesRemoveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove <voice>",
		Short: "Remove a downloaded voice",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTTSVoicesRemove(args[0])
		},
	}
	return cmd
}

// runTTSVoicesList lists available Piper voices, combining the builtin curated
// suggestions with any voices actually installed in the local voice directory.
// Each voice is annotated with a status: "installed" if the .onnx model is
// present on disk, "available" otherwise.
func runTTSVoicesList(format string) error {
	catalog := builtinVoiceCatalog()
	installed := scanInstalledVoices()
	voices := mergeVoiceList(catalog, installed)

	if format == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(voices)
	}

	// Text format
	fmt.Printf("%-30s %-10s %-10s %-10s %s\n", "VOICE", "QUALITY", "LANGUAGE", "STATUS", "SIZE")
	fmt.Println(strings.Repeat("-", 82))
	for _, v := range voices {
		fmt.Printf("%-30s %-10s %-10s %-10s %s\n", v.Name, v.Quality, v.Language, v.Status, v.Size)
	}

	return nil
}

// runTTSVoicesDownload downloads a voice.
func runTTSVoicesDownload(ctx context.Context, voiceName string) error {
	if strings.ContainsAny(voiceName, "/\\:") {
		return fmt.Errorf("invalid voice name: contains path separators")
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("getting home directory: %w", err)
	}

	voiceDir := filepath.Join(homeDir, ".meept", "tts", "voices")
	if err := os.MkdirAll(voiceDir, 0o755); err != nil {
		return fmt.Errorf("creating voice directory: %w", err)
	}

	modelPath := filepath.Join(voiceDir, voiceName+".onnx")
	configPath := filepath.Join(voiceDir, voiceName+".onnx.json")

	// Build URLs from Piper voices repository
	baseURL := "https://huggingface.co/rhasspy/piper-voices/resolve/main/"
	modelURL := baseURL + voiceName + "/" + voiceName + ".onnx"
	configURL := baseURL + voiceName + "/" + voiceName + ".onnx.json"

	fmt.Printf("Downloading voice '%s'...\n", voiceName)

	// Download model
	fmt.Printf("  Downloading model (%s)...\n", modelURL)
	if err := downloadFile(ctx, modelURL, modelPath); err != nil {
		return fmt.Errorf("downloading model: %w", err)
	}

	// Download config
	fmt.Printf("  Downloading config (%s)...\n", configURL)
	if err := downloadFile(ctx, configURL, configPath); err != nil {
		return fmt.Errorf("downloading config: %w", err)
	}

	fmt.Printf("Voice '%s' downloaded successfully to %s\n", voiceName, voiceDir)
	return nil
}

// runTTSVoicesRemove removes a downloaded voice.
func runTTSVoicesRemove(voiceName string) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("getting home directory: %w", err)
	}

	modelPath := filepath.Join(homeDir, ".meept", "tts", "voices", voiceName+".onnx")
	configPath := filepath.Join(homeDir, ".meept", "tts", "voices", voiceName+".onnx.json")

	// Remove model
	if _, err := os.Stat(modelPath); err == nil {
		if err := os.Remove(modelPath); err != nil {
			return fmt.Errorf("removing model: %w", err)
		}
		fmt.Printf("Removed: %s\n", modelPath)
	} else {
		fmt.Printf("Model not found: %s\n", modelPath)
	}

	// Remove config
	if _, err := os.Stat(configPath); err == nil {
		if err := os.Remove(configPath); err != nil {
			return fmt.Errorf("removing config: %w", err)
		}
		fmt.Printf("Removed: %s\n", configPath)
	} else {
		fmt.Printf("Config not found: %s\n", configPath)
	}

	return nil
}

// builtinVoiceCatalog returns a static, curated list of common Piper voices.
// This is NOT a live catalog fetched from the network — it is a hardcoded set
// of well-known voices for quick reference. Use `meept tts voices download`
// to install any voice from the full Piper repository at
// https://huggingface.co/rhasspy/piper-voices.
func builtinVoiceCatalog() []PiperVoice {
	return []PiperVoice{
		{Name: "danny-medium", Quality: "medium", Language: "en-US", Size: "~80MB", URL: "https://huggingface.co/rhasspy/piper-voices"},
		{Name: "en_US-lessac-high", Quality: "high", Language: "en-US", Size: "~120MB", URL: "https://huggingface.co/rhasspy/piper-voices"},
		{Name: "en_US-lessac-medium", Quality: "medium", Language: "en-US", Size: "~70MB", URL: "https://huggingface.co/rhasspy/piper-voices"},
		{Name: "en_US-lessac-low", Quality: "low", Language: "en-US", Size: "~30MB", URL: "https://huggingface.co/rhasspy/piper-voices"},
		{Name: "en_GB-alan-medium", Quality: "medium", Language: "en-GB", Size: "~70MB", URL: "https://huggingface.co/rhasspy/piper-voices"},
		{Name: "de_DE-thorsten-medium", Quality: "medium", Language: "de-DE", Size: "~70MB", URL: "https://huggingface.co/rhasspy/piper-voices"},
		{Name: "fr_FR-siwis-medium", Quality: "medium", Language: "fr-FR", Size: "~70MB", URL: "https://huggingface.co/rhasspy/piper-voices"},
		{Name: "es_ES-davefx-medium", Quality: "medium", Language: "es-ES", Size: "~70MB", URL: "https://huggingface.co/rhasspy/piper-voices"},
	}
}

// ttsVoiceDir returns the local directory where Piper voice models are stored.
func ttsVoiceDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("getting home directory: %w", err)
	}
	return filepath.Join(homeDir, ".meept", "tts", "voices"), nil
}

// scanInstalledVoices scans the local voice directory for installed .onnx
// model files and returns a set of voice names (without the .onnx extension).
// If the directory does not exist or is unreadable, returns an empty set.
func scanInstalledVoices() map[string]bool {
	installed := make(map[string]bool)

	dir, err := ttsVoiceDir()
	if err != nil {
		return installed
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return installed
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".onnx") {
			voiceName := strings.TrimSuffix(name, ".onnx")
			installed[voiceName] = true
		}
	}

	return installed
}

// mergeVoiceList combines the builtin voice catalog with locally installed
// voices. Voices present in both the catalog and the installed set are marked
// "installed". Installed voices not in the catalog are appended with detected
// metadata. Catalog voices not installed are marked "available".
func mergeVoiceList(catalog []PiperVoice, installed map[string]bool) []PiperVoice {
	seen := make(map[string]bool, len(catalog)+len(installed))
	result := make([]PiperVoice, 0, len(catalog)+len(installed))

	for _, v := range catalog {
		if seen[v.Name] {
			continue
		}
		seen[v.Name] = true
		status := "available"
		if installed[v.Name] {
			status = "installed"
		}
		v.Status = status
		result = append(result, v)
	}

	// Append locally installed voices not in the builtin catalog.
	for name := range installed {
		if seen[name] {
			continue
		}
		seen[name] = true
		result = append(result, PiperVoice{
			Name:     name,
			Quality:  "-",
			Language: "-",
			Size:     "-",
			Status:   "installed",
		})
	}

	return result
}

// downloadFile downloads a file from URL to destPath.
func downloadFile(ctx context.Context, url, destPath string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: %s", resp.Status)
	}

	out, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, io.LimitReader(resp.Body, 1<<30)) // 1 GiB limit
	return err
}
