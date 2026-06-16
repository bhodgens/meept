// Package bad contains test fixtures for the mutexio analyzer.
package bad

import (
	"os"
	"sync"
)

type fakeClient struct{}

func (fakeClient) Do()        {}
func (fakeClient) Chat()      {}
func (fakeClient) Publish()   {}
func (fakeClient) Save()      {}
func (fakeClient) Close()     {}
func (*fakeClient) Load()     {}
func (*fakeClient) Persist()  {}

var fc fakeClient
var fcp *fakeClient

// violationDirectUnlock: I/O between Lock and Unlock should flag.
func violationDirectUnlock() {
	var mu sync.Mutex
	mu.Lock()
	fc.Do() // want `mutexio: Do called while holding a mutex`
	mu.Unlock()
}

// violationDeferUnlock: I/O between Lock and deferred Unlock (end of body).
func violationDeferUnlock() {
	var mu sync.Mutex
	mu.Lock()
	defer mu.Unlock()
	fc.Chat() // want `mutexio: Chat called while holding a mutex`
}

// violationMultiple: multiple I/O calls in the lock range should both flag.
func violationMultiple() {
	var mu sync.Mutex
	mu.Lock()
	defer mu.Unlock()
	fc.Publish()      // want `mutexio: Publish called while holding a mutex`
	os.WriteFile("/tmp/x", []byte("y"), 0644) // want `mutexio: WriteFile called while holding a mutex`
}

// cleanPattern: collect under lock, release, then operate is fine.
func cleanPattern() {
	var mu sync.Mutex
	mu.Lock()
	x := fc
	mu.Unlock()
	x.Do()
}

// okNoIO: Lock/Unlock with no I/O between them.
func okNoIO() {
	var mu sync.Mutex
	mu.Lock()
	defer mu.Unlock()
	_ = 1 + 2
}

// okReceiverIsMutex: mu.Unlock() inside lock range shouldn't flag on Close.
func okReceiverIsMutex() {
	var mu sync.Mutex
	mu.Lock()
	defer mu.Unlock()
	_ = mu
}

// violationRWMutex: works on sync.RWMutex too.
func violationRWMutex() {
	var mu sync.RWMutex
	mu.RLock()
	defer mu.RUnlock()
	fcp.Persist() // want `mutexio: Persist called while holding a mutex`
}
