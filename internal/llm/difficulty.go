package llm

// DeriveDifficulty projects an Intent's routing signals into a 1-5 level
// (Contract C11, sovereign-router-parity leaf 07). Pure function: no
// classifier call, no I/O. Values come from the values the dispatcher
// already computed, so there is zero extra inference cost. A caller with
// no Intent or no derivable signal passes inputs that yield 0 -> R7
// inert (a pin created at 0 is never released for difficulty).
//
// The frozen table (leaf 07, locked by Q7):
//
//	requiresPlanning==true               -> level 4
//	mode=="quick_plan"                   -> level 5  (explicit execution phrasing)
//	mode=="spec_plan" && requiresPlan    -> level 4
//	mode=="spec_pair" || mode=="compound" -> level 3
//	otherwise, inputLen<50               -> level 1  (short-input downgrade,
//	                                                 mirrors suggestMode)
//	otherwise                            -> level 2
func DeriveDifficulty(mode string, requiresPlanning bool, inputLen int) int {
	// Row order is the frozen precedence: requiresPlanning first, then
	// quick_plan, then the spec_plan conjunction, then the pair/compound
	// pair, then the short-input downgrade.
	if requiresPlanning {
		// spec_plan && requiresPlanning is level 4 (same as the bare
		// requiresPlanning row); quick_plan outranks it below.
		if mode == "quick_plan" {
			return 5
		}
		return 4
	}
	switch mode {
	case "quick_plan":
		return 5
	case "spec_pair", "compound":
		return 3
	}
	if inputLen < 50 {
		return 1
	}
	return 2
}
