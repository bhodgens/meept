package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// PiperVoice represents a voice in the Piper voices catalog.
type PiperVoice struct {
	Name     string `json:"name"`
	Quality  string `json:"quality"`
	Language string `json:"language"`
	Size     string `json:"size"`
	URL      string `json:"url"`
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
			return runTTSVoicesDownload(args[0])
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

// runTTSVoicesList lists available Piper voices.
func runTTSVoicesList(format string) error {
	// Fetch voices catalog from Piper repository
	voices, err := fetchVoicesCatalog()
	if err != nil {
		return fmt.Errorf("fetching voices catalog: %w", err)
	}

	if format == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(voices)
	}

	// Text format
	fmt.Printf("%-30s %-10s %-10s %s\n", "VOICE", "QUALITY", "LANGUAGE", "SIZE")
	fmt.Println(strings.Repeat("-", 70))
	for _, v := range voices {
		fmt.Printf("%-30s %-10s %-10s %s\n", v.Name, v.Quality, v.Language, v.Size)
	}

	return nil
}

// runTTSVoicesDownload downloads a voice.
func runTTSVoicesDownload(voiceName string) error {
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
	if err := downloadFile(modelURL, modelPath); err != nil {
		return fmt.Errorf("downloading model: %w", err)
	}

	// Download config
	fmt.Printf("  Downloading config (%s)...\n", configURL)
	if err := downloadFile(configURL, configPath); err != nil {
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

// fetchVoicesCatalog fetches the list of available Piper voices.
// This is a simplified implementation - in production, this would fetch from the Piper API.
func fetchVoicesCatalog() ([]PiperVoice, error) {
	// Common voices available from Piper
	voices := []PiperVoice{
		{Name: "danny-medium", Quality: "medium", Language: "en-US", Size: "~80MB", URL: "https://huggingface.co/rhasspy/piper-voices"},
		{Name: "en_US-lessac-high", Quality: "high", Language: "en-US", Size: "~120MB", URL: "https://huggingface.co/rhasspy/piper-voices"},
		{Name: "en_US-lessac-medium", Quality: "medium", Language: "en-US", Size: "~70MB", URL: "https://huggingface.co/rhasspy/piper-voices"},
		{Name: "en_US-lessac-low", Quality: "low", Language: "en-US", Size: "~30MB", URL: "https://huggingface.co/rhasspy/piper-voices"},
		{Name: "en_GB-alan-medium", Quality: "medium", Language: "en-GB", Size: "~70MB", URL: "https://huggingface.co/rhasspy/piper-voices"},
		{Name: "de_DE-thorsten-medium", Quality: "medium", Language: "de-DE", Size: "~70MB", URL: "https://huggingface.co/rhasspy/piper-voices"},
		{Name: "fr_FR-siwis-medium", Quality: "medium", Language: "fr-FR", Size: "~70MB", URL: "https://huggingface.co/rhasspy/piper-voices"},
		{Name: "es_ES-davefx-medium", Quality: "medium", Language: "es-ES", Size: "~70MB", URL: "https://huggingface.co/rhasspy/piper-voices"},
	}

	return voices, nil
}

// downloadFile downloads a file from URL to destPath.
func downloadFile(url, destPath string) error {
	resp, err := http.Get(url)
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

	_, err = io.Copy(out, resp.Body)
	return err
}
