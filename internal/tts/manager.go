package tts

import (
	"context"
	"sync"
)

// Manager manages TTS lifecycle and message routing.
type Manager struct {
	mu            sync.RWMutex
	config        Config
	synth         Synthesizer
	queue         []string
	speaking      bool
	processing    bool // prevents concurrent queue processing
	queueOverflow bool // tracks if items were dropped
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
			m.processing = false
			// Fall through to speak new text
		} else if m.config.Behavior.QueueMessages {
			if len(m.queue) >= m.config.Behavior.MaxQueueSize {
				m.queue = m.queue[1:] // Drop oldest
				m.queueOverflow = true
			}
			m.queue = append(m.queue, text)
			// Start queue processing if not already processing.
			// Set processing=true BEFORE spawning so concurrent Speak
			// callers don't each spawn their own processQueue goroutine.
			if !m.processing {
				m.processing = true
				go m.processQueue()
			}
			return nil
		} else {
			// Neither interrupt nor queue - drop the message
			return nil
		}
	}

	m.speaking = true
	m.processing = true
	go func() {
		defer func() {
			// Synthesis of the immediate message is done; drain any
			// queued messages iteratively (S6-13: no recursive
			// goroutine spawn). processQueue clears m.speaking and
			// m.processing when the queue is empty.
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
// It runs as an iterative loop rather than recursively spawning goroutines,
// avoiding unbounded goroutine growth when many messages are queued
// back-to-back (S6-13). The caller is expected to have set m.processing
// and m.speaking appropriately; this function clears them when done.
func (m *Manager) processQueue() {
	for {
		m.mu.Lock()
		if len(m.queue) == 0 {
			// No more queued messages; stop processing.
			m.processing = false
			m.speaking = false
			m.mu.Unlock()
			return
		}
		// Pop next message.
		next := m.queue[0]
		m.queue = m.queue[1:]
		m.mu.Unlock()

		// Synthesize + play sequentially. Errors are logged but the
		// loop continues to drain remaining queued messages.
		ctx := context.Background()
		result, err := m.synth.Synthesize(ctx, next)
		if err == nil {
			if err := m.synth.Play(result.AudioData); err != nil {
				logger.Warn("TTS playback failed", "error", err)
			}
		} else {
			logger.Warn("TTS synthesis failed", "error", err)
		}
	}
}

// QueueLength returns the number of queued messages.
func (m *Manager) QueueLength() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.queue)
}

// HasOverflow returns true if any queued items were dropped.
func (m *Manager) HasOverflow() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.queueOverflow
}

// ClearOverflow resets the overflow flag.
func (m *Manager) ClearOverflow() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.queueOverflow = false
}

// Stop stops any ongoing speech.
func (m *Manager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.speaking = false
	m.processing = false
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
