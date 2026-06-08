// internal/configui/sections_stt.go
package configui

import "github.com/caimlas/meept/internal/config"

func buildSTTFields() []Field {
	cfg, _ := config.LoadDefault()
	s := &cfg.STT
	w := &s.Whisper
	p := &s.Parakeet
	r := &s.Recording
	return []Field{
		NewToggleField("stt.enabled", "enabled", s.Enabled),
		NewSelectField("stt.engine", "engine", s.Engine, []string{"whisper", "parakeet", "native"}),
		NewTextField("stt.language", "language", s.Language),
		NewToggleField("stt.auto_send", "auto send", s.AutoSend),
		NewDrilldownField("stt.whisper", "whisper", []DrilldownItem{
			{Name: "whisper", Fields: []Field{
				NewTextField("stt.whisper.bin_path", "bin path", w.BinPath),
				NewTextField("stt.whisper.model_path", "model path", w.ModelPath),
				NewNumberField("stt.whisper.threads", "threads", w.Threads),
			}},
		}),
		NewDrilldownField("stt.parakeet", "parakeet", []DrilldownItem{
			{Name: "parakeet", Fields: []Field{
				NewTextField("stt.parakeet.bin_path", "bin path", p.BinPath),
				NewTextField("stt.parakeet.model_path", "model path", p.ModelPath),
			}},
		}),
		NewDrilldownField("stt.recording", "recording", []DrilldownItem{
			{Name: "recording", Fields: []Field{
				NewSelectField("stt.recording.recorder_bin", "recorder", r.RecorderBin, []string{"ffmpeg", "sox"}),
				NewNumberField("stt.recording.sample_rate", "sample rate", r.SampleRate),
				NewNumberField("stt.recording.channels", "channels", r.Channels),
				NewSelectField("stt.recording.format", "format", r.Format, []string{"wav", "flac", "ogg"}),
			}},
		}),
	}
}
