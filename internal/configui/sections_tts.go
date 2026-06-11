// internal/configui/sections_tts.go
package configui

func buildTTSFields() []Field {
	cfg := loadMainConfigOrFallback()
	s := &cfg.TTS
	p := &s.Piper
	pl := &s.Playback
	b := &s.Behavior

	return []Field{
		NewToggleField("tts.enabled", "enabled", s.Enabled),
		NewSelectField("tts.engine", "engine", s.Engine, []string{"piper", "platform"}),
		NewTextField("tts.voice", "voice", s.Voice),
		NewTextField("tts.voice_path", "voice path", s.VoicePath),
		NewDrilldownField("tts.piper", "piper", []DrilldownItem{
			{Name: "piper", Fields: []Field{
				NewTextField("tts.piper.bin_path", "bin path", p.BinPath),
				NewTextField("tts.piper.model_path", "model path", p.ModelPath),
				NewTextField("tts.piper.config_path", "config path", p.ConfigPath),
				NewTextField("tts.piper.speaker", "speaker", p.Speaker),
			}},
		}),
		NewDrilldownField("tts.playback", "playback", []DrilldownItem{
			{Name: "playback", Fields: []Field{
				NewNumberField("tts.playback.volume", "volume", int(pl.Volume*100)),
				NewNumberField("tts.playback.rate", "rate", int(pl.Rate*100)),
				NewTextField("tts.playback.audio_device", "audio device", pl.AudioDevice),
			}},
		}),
		NewDrilldownField("tts.behavior", "behavior", []DrilldownItem{
			{Name: "behavior", Fields: []Field{
				NewToggleField("tts.behavior.read_own_messages", "read own messages", b.ReadOwnMessages),
				NewToggleField("tts.behavior.interrupt_on_new_msg", "interrupt on new msg", b.InterruptOnNewMsg),
				NewToggleField("tts.behavior.queue_messages", "queue messages", b.QueueMessages),
				NewNumberField("tts.behavior.max_queue_size", "max queue size", b.MaxQueueSize),
			}},
		}),
	}
}
