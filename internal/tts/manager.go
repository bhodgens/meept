package tts

import (
	"context"
	"sync"
)

// Manager manages TTS lifecycle and message routing.
type Manager struct {
	mu       sync.RWMutex
	config   Config
	synth    Synthesizer
	queue    []string
	speaking bool
}

// NewManager creates a new TTS manager with the given configuration.
func NewManager(cfg Config) (*Manager, error) {
	synth, err := NewSynthesizer(cfg)
	if err != nil {
		return nil, err
	}

	return &Manager{
		config: cfg,
		synth:  synth,
		queue:  make([]string, 0, cfg.Behavior.MaxQueueSize),
	}, nil
}

// Speak queues or immediately speaks the given text.
func (m *Manager) Speak(text string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.speaking {
		if m.config.Behavior.InterruptOnNewMsg {
			m.synth.Stop()
			m.speaking = false
		} else if m.config.Behavior.QueueMessages {
			if len(m.queue) >= m.config.Behavior.MaxQueueSize {
				m.queue = m.queue[1:] // Drop oldest
			}
			m.queue = append(m.queue, text)
			return nil
		}
	}

	m.speaking = true
	go func() {
		defer func() {
			m.mu.Lock()
			m.speaking = false
			m.mu.Unlock()
			m.processQueue()
		}()

		ctx := context.Background()
		result, err := m.synth.Synthesize(ctx, text)
		if err != nil {
			logger.Warn("TTS synthesis failed", "error", err)
			return
		}

		if err := m.synth.Play(result.AudioData); err != nil {
			logger.Warn("TTS playback failed", "error", err)
		}
	}()

	return nil
}

// processQueue processes queued messages after current playback completes.
func (m *Manager) processQueue() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.queue) > 0 {
		next := m.queue[0]
		m.queue = m.queue[1:]
		m.speaking = true

		go func() {
			ctx := context.Background()
			result, _ := m.synth.Synthesize(ctx, next)
			m.synth.Play(result.AudioData)
			m.processQueue()
		}()
	}
}

// Stop stops any ongoing speech.
func (m *Manager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.speaking = false
	return m.synth.Stop()
}

// IsSpeaking returns whether the manager is currently speaking.
func (m *Manager) IsSpeaking() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.speaking
}

// CheckAvailable checks if the TTS engine is available.
func (m *Manager) CheckAvailable() error {
	return m.synth.CheckAvailable()
}

// Close releases resources used by the manager.
func (m *Manager) Close() error {
	return m.Stop()
}
