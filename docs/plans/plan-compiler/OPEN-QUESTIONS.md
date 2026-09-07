# OPEN-QUESTIONS — plan-compiler tree

Errata and spec-drift findings recorded during execution. Rules win over
leaf-table literals; dialect doc wins over leaf docs per the leaf's own
conflict rule. Never silently reconciled.

## 1. DependsOn semantics: phase ordinals (RESOLVED)

Leaf 02's Task 4 sketch said `DependsOn [0]` for golden 8.2; the dialect
doc §8.2 walkthrough (normative) says `{1}`. Resolution per conflict
rule: **phase ordinals (1-based sequence numbers)**. Implemented in
eab9db01; CompileSealed emits ordinals. Conversion to agent.PlanPhaseSpec
(leaf 04) must keep ordinals — agent.PlanPhaseSpec.DependsOn semantics at
strategic.go:76 are 0-based indices, so leaf 04's mapping subtracts 1.
Verify against parsePhaseOutput's existing remap code when wiring.

## 2. Self-consume message wording gap (SPEC WORDING, unfixed in code)

Dialect doc error class for consume-ordering says "produced by a later
phase", but §3's rule covers same-phase self-consume too. Class fires for
both (only ordering class exists). Message string kept verbatim per the
slave-to-spec rule; if the wording bothers anyone, fix the DIALECT DOC
(first) and the compiler message in the same commit.

## 3. Golden 8.1 vs 8.2 DependsOn — both by ordinal

8.1 phase 2 consumes avatar-store from phase 1 ⇒ DependsOn [1] (ordinal).
8.2 both consumers get [1]. Leaf-sketch "[0]" values are stale phrasing;
tables/tests pin ordinals.

## 4. Tree emission sizing disambiguation (leaf 03, accepted)

stepsPerLeaf: n ≤ MaxLeavesPerPhase ⇒ one leaf; else ceil(n/cap) over
exactly cap leaves. Without clause 1 the "2 steps ⇒ flat" pin contradicts
literal ceil. Gate: tree iff any phase > cap steps, OR intent > budget,
OR total > 6. Recorded here so Contract D's prose ("steps > MaxLeaves
* steps-per-leaf") reads as satisfied by this interpretation.

## 5. Cycle detection unreachable-but-implemented

Contract B requires it; the dialect's consume-before-produce rule makes
artifact-edge cycles unconstructible in valid input. Implemented anyway
(defense in depth for future dialect versions). Cost: one DFS, ~30 LOC.
