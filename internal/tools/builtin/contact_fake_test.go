package builtin

// Gate tests for contract C6: shared fake for the contact tool's chain
// seam. The concrete ChainRunner type does not exist yet; the tool's
// constructor takes this interface-shaped seam (the leaf-02 wiring
// injects contact.ChainRunner adapted onto it).

// fakeChainRunner is the injected chain seam: the tool runs records
// through it, never through concrete channels. It records the person /
// message / channels the tool forwarded and returns a pre-rendered
// report body.
type fakeChainRunner struct {
	gotPerson   string
	gotMessage  string
	gotChannels []string
	report      string // rendered ChainReport body the tool must pass through
}

// Run mirrors contact.ChainRunner.Run's argument shape with plain
// strings (the tool carries NO internal/contact import edge, mirroring
// the voice_call tool's no-voice-type rule).
func (f *fakeChainRunner) Run(person, message string, channels []string) string {
	f.gotPerson = person
	f.gotMessage = message
	f.gotChannels = channels
	return f.report
}
