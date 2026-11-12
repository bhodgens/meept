> **Source repo:** caimlas/uptractor-jobfile (`~/git/uptractor-jobfile`)
> **Source:** branch `claude/uptractor-repo-organization-5jusi4`, `backend/research/voice-coordination-threat-assessment.md`
> **Imported verbatim 2026-11-10** (content below this header is byte-identical to the source).

# Competitive Threat Assessment: Construction-AI Startups Adding Voice Coordination for Small Contractors

*Research date: 2026-10-28. Product context: managed AI voice-agent service for small construction contractors (sole proprietors / small GCs, subcontractor coordination). Sources: TechCrunch, Reuters, PRNewswire, company sites/blogs, YC launch pages. All customer/metrics figures are company-reported.*

---

## 1. Adaptive (adaptive.build)

**What they actually do.** "Agentic accounting platform for construction." Founded 2021 (NYC/Boston) by Calvano, Enriquez, Bradlow. Core product automates construction back-office finance: invoice coding/OCR, AP, billings, WIP reporting, change-order drafting, lien waivers, compliance. Their Series B announcement (Sept 17, 2026, $30M led by Tidemark) describes AI agents working as "named roles on a finance team": field coordinator, cost coding agent, compliance agent, budget intelligence, WIP intelligence.

**Funding & momentum.**
- $6.5M seed led by a16z (July 2022)
- $19M Series A led by Emergence Capital (July 2024); ~280 customers at that point
- **$30M Series B led by Tidemark (Sept 2026); total raised $57M.** Claims 750+ construction companies, $5M–$1B annual revenue range, integrations with QuickBooks through Sage Intacct/Foundation/Acumatica, 40+ accounting-firm partners. Customer count, ARR, and churn are all self-reported.

**Voice-agent relevance — this is the sharpest threat signal of the three.** The Series B blog post explicitly describes agents that go get information that isn't written down: *"a call to a foreman who isn't going to stop and log into anything, a text to a project manager between meetings, an email chasing a vendor for a signature."* That is outbound voice/SMS coordination as an already-shipped or near-shipped capability, embedded inside the accounting workflow their customers pay for daily. Their "field coordinator" agent role is functionally adjacent to phone-based job coordination.

**Segment fit vs. micro-contractors.** Mixed. Originally positioned for SMB GCs without accounting staff (2022 TechCrunch coverage), but the stated customer floor is now **$5M annual revenue** — above the sole-proprietor segment. They serve "general contractors, specialty trades, and real estate developers." Micro-contractors are not the beachhead, but the low end of their range ($5M revenue = still a very small business, often owner-operated) overlaps our ICP's upper edge.

**Crush-risk verdict: HIGH.** Well-capitalized ($57M), agentic-AI-native, already doing outbound contact with field workers, and credibly could bolt inbound call answering onto their "field coordinator" agent within a product cycle. Mitigating factors: voice coordination is a means to their accounting end, not a product they monetize standalone; their pricing and onboarding assume accounting-system integration, which a sole proprietor often lacks.

---

## 2. BuildOps (buildops.com)

**What they actually do.** All-in-one field service management (FSM) SaaS for **commercial** contractors — HVAC, plumbing, mechanical/electrical, fire & life safety. Scheduling, dispatch, project management, service, invoicing, "AI-powered automation." Founded 2018, Los Angeles.

**Funding & momentum.**
- $50M Series B (May 2023) + $36M top-up
- **$127M Series C led by Meritech (March 2025) at $1B valuation** — unicorn; total raised >$250M. Reuters and TechCrunch both covered it. ~1,000 commercial contractor customers (J.H. Kelly, Haynes Mechanical, Dynamic Systems, Baker Electric); revenue roughly doubling 2023→2024; ~375 employees and scaling. Roadmap per the announcement: AI-powered scheduling, predictive maintenance, live project tracking, API architecture, and **strategic acquisitions**.

**Voice-agent relevance.** Indirect but real. Their business runs on dispatch and service calls — the front office of a commercial service contractor answers phones constantly. An AI platform with dispatch at its center is one integration away from AI call intake (see Avoca, below, which does exactly this as a ServiceTitan add-on and reached a $1B valuation). BuildOps' stated priorities (API architecture, acquisitions) make voice intake a plausible build-or-buy. No announced voice product found as of this research.

**Segment fit vs. micro-contractors.** **Poor fit — this is the least direct threat.** BuildOps is explicitly commercial, mid-market leaning, per-user annual-contract pricing, customers running multi-tech service operations. Their own marketing distances them from "residential software." A sole-proprietor contractor is not their buyer. Contrast with ServiceTitan (which started small/residential and moved upmarket) — BuildOps is moving the other direction if anywhere.

**Crush-risk verdict: MODERATE (adjacent-segment spillover), LOW for micro-contractors specifically.** They have the capital and the dispatch-centric product to add voice intake for *their* customers, but those customers are mid-size commercial shops, not our segment. Watching brief rather than urgent.

---

## 3. "Sheer" — could not be identified

**No construction-AI software startup named "Sheer" was found** across multiple targeted searches (TechCrunch, YC directory, funding press, company sites). The name resolves only to:
- **Sheer Enterprises / Sheer Service** — a West Palm Beach commercial GC/facilities firm (actual contractor, not a tech company, founded 1991)
- **Sheer Health** — consumer health-insurance billing AI (not construction)
- **Sheer AI** — a marketing-agency consultancy (not construction)
- **Shepherd** (withshepherd.com / shepherdinsurance.com) — *plausibly what was meant*: AI-native **construction insurance** MGA. $13.5M Series A (Feb 2024, Costanoa), **$42M Series B led by Intact Private Capital (March 2026), $67M total**. 600+ commercial contractors, $400B+ insured value, AI COI/compliance tracking, agentic underwriting roadmap. Insurance and COI-compliance workflows overlap subcontractor coordination, but there is no voice/answering signal and their buyer is brokers/carriers and larger insureds, not micro-contractors. If "Sheer" was intended as Shepherd: **crush-risk LOW for voice coordination.**

Recommendation: confirm the intended company with the source of this list before weighting this row.

---

## 4. Similar fast-moving companies (adjacent threat map)

| Company | What / segment | Funding | Voice signal |
|---|---|---|---|
| **Avoca** | AI voice + workflow agents for service businesses (HVAC, plumbing, auto) | **$125M+ total at $1B valuation, Series B led by Meritech + General Catalyst (Apr 2026)**; 8-figure ARR; ServiceTitan/Nexstar partnerships | **Direct**: 24/7 call answering, booking into CRMs. The proof that AI voice for contractors is a $1B-valuation category — but today aimed at larger home-services operators |
| **Probook** | "AI operating system" for home services built around dispatch (HVAC/plumbing/electrical) | **$40M ($6M seed Sequoia + $34M Series A a16z, June 2026)**; one customer booked 2,542 jobs in a month with zero human intervention (company-reported) | **Direct**: call intake, customer messaging, outbound follow-up inside the platform |
| **Cactus** | 24/7 AI copilot for home-service businesses: answers, qualifies, books, follows up | $7M seed, Wellington + YC (Nov 2025) | **Direct** — whole product is conversational AI call handling for small service businesses |
| **Elyos AI** (YC) | AI support agents for trades (UK): inbound calls, missed-call follow-up, email, job booking | **$13M Series A led by Blackbird (2026), $16M total** | **Direct**; expanding to new agent capabilities + international in 2026 |
| **Chime Labs** (AU) | AI receptionist for tradies | $900K pre-seed (Apr 2026, 500 Global) | **Direct**; micro-scale validation of the exact wedge |
| **Trunk Tools** | AI agents for construction project workflows (document QA, deadline monitoring) | $20M Series A led by Redpoint (2024) | Indirect — enterprise/major GC focus, not voice |
| **Handoff** (YC) | AI back-office agent for *residential* contractors: estimates, invoices, schedules at $149/mo | YC-backed | Indirect today — but the closest existing analog to "AI staff for the solo contractor," and voice is the natural next modality |
| **Foreman / ScoutOut** (YC W2026) | AI project management for contractors: takeoffs, estimates, proposals | YC-backed, 2 people | Indirect |

---

## 5. Honest crush-risk assessment for a managed voice-agent service for small contractors

1. **The category is already validated and already funded at the top.** Avoca at $1B and Probook at $40M prove investors believe AI voice/front-office for contractors is a platform-scale business. The window where "AI answers a contractor's phone" is novel is closed; differentiation must come from *construction-specific depth* (subcontractor coordination, scheduling changes, lien/COI context) rather than the answering itself.

2. **The most dangerous single company is Adaptive, not BuildOps.** Adaptive is small-contractor-friendly in spirit, agentic by design, and already calling/texting field workers as part of its product. If they extend the "field coordinator" agent to inbound call answering and job coordination, they'd have distribution into 750+ contractors on day one. BuildOps, despite 10x the capital, serves commercial mid-market shops and shows no voice product and no micro-contractor motion.

3. **The direct voice-first entrants (Avoca, Probook, Cactus, Elyos) target the tier just above micro.** Home-services operators with fleets and CRMs — the same downmarket pull that took ServiceTitan from small plumbers to enterprise is available to them. Cactus and Elyos explicitly court small teams. Expect them to move down-market within 12–18 months.

4. **Incumbent-category convergence risk:** BuildOps' stated M&A appetite means a voice layer could be *acquired* into an FSM platform overnight, collapsing a standalone voice tool's value proposition for any contractor who also adopts FSM software.

5. **Remaining defensible space for a focused entrant:** sole proprietors who will never buy Adaptive (no accounting system to integrate) or BuildOps (too big/commercial), who need subcontractor phone coordination — not just customer lead capture — handled. That's a narrow but real wedge; the honest risk is that it is a wedge, not a moat: every company above has more capital, more distribution, and the same model-improvement tailwind.

**Overall: MEDIUM-HIGH crush risk over a 12–24 month horizon, concentrated in (a) Adaptive extending its field-coordination agents to voice, and (b) Avoca/Probook-style home-services voice platforms moving down-market. BuildOps is a low-urgency watch; "Sheer" is unverified and likely a mis-naming of Shepherd (low risk).**
