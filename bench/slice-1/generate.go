// Package main is the Week 0 benchmark corpus generator for the wiki
// intelligence port (Slice 1 ship gate).
//
// Purpose
// =======
//
// Slice 1 must prove recall@20 >= 85% on a synthetic corpus BEFORE email
// sync pipes thousands of real entities into the wiki. This generator
// produces that corpus deterministically: same seed, same bytes.
//
// Shape
// =====
//
//   - 25 synthetic people across 5 synthetic companies (5 per company)
//   - 8 synthetic projects
//   - 500 artifacts (chat / meeting / email) — 60% single-entity status,
//     20% multi-entity relationships, 10% superseding, 5% contradictions,
//     5% noise (null handling)
//   - 50 queries — 20 status, 15 relationship, 10 multi-hop, 3 counterfactual,
//     2 out-of-scope — each mapped to the exact fact_ids the retriever must
//     return in its top-20 to pass the gate
//
// Determinism
// ===========
//
// All randomness flows through math/rand.New(rand.NewSource(42)). Running
// `go run ./bench/slice-1/generate.go` twice MUST produce byte-identical
// corpus.jsonl and queries.jsonl.
//
// Fact IDs
// ========
//
// fact_id = sha256(artifact_sha + "/" + sentence_offset + "/" +
//
//	norm(subject) + "/" + norm(predicate) + "/" +
//	norm(object))[:16]
//
// (§7.3 of docs/specs/WIKI-SCHEMA.md — we use the canonical helpers
// ComputeFactID + NormalizeForFactID from internal/team/wiki_index.go so
// the corpus stays byte-compatible with the live index.)
//
// Run
// ===
//
//	go run ./bench/slice-1/generate.go
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/team"
)

// ---------------------------------------------------------------------------
// Config
// ---------------------------------------------------------------------------

const (
	seed             int64 = 42
	totalArtifacts         = 500
	totalQueries           = 50
	corpusStartDate        = "2026-01-15T09:00:00Z"
	artifactInterval       = 6 * time.Hour // cadence between artifacts
)

// Distribution of artifact kinds (must sum to totalArtifacts).
var distribution = struct {
	SingleEntity  int // status/observation
	MultiEntity   int // relationship
	Superseding   int // role_at changes, etc.
	Contradiction int
	Noise         int
}{
	SingleEntity:  300,
	MultiEntity:   100,
	Superseding:   50,
	Contradiction: 25,
	Noise:         25,
}

// Query mix (must sum to totalQueries).
var queryMix = struct {
	Status         int
	Relationship   int
	MultiHop       int
	Counterfactual int
	OutOfScope     int
}{
	Status:         20,
	Relationship:   15,
	MultiHop:       10,
	Counterfactual: 3,
	OutOfScope:     2,
}

// ---------------------------------------------------------------------------
// Entity pool — small, reused heavily
// ---------------------------------------------------------------------------

type person struct {
	Slug    string
	Display string
	Company string // company slug
	Role    string // initial role
}

type company struct {
	Slug    string
	Display string
}

type project struct {
	Slug    string
	Display string
	Company string // owner company slug
}

var companies = []company{
	{"acme-corp", "Acme Corp"},
	{"blueshift", "Blueshift"},
	{"dunder-mifflin", "Dunder Mifflin"},
	{"northwind", "Northwind"},
	{"vandelay", "Vandelay Industries"},
}

// 25 people — 5 per company. Names are deliberately synthetic composites.
var people = []person{
	// Acme Corp
	{"sarah-jones", "Sarah Jones", "acme-corp", "account-executive"},
	{"marcus-lee", "Marcus Lee", "acme-corp", "solutions-engineer"},
	{"priya-ramos", "Priya Ramos", "acme-corp", "product-manager"},
	{"diego-park", "Diego Park", "acme-corp", "vp-engineering"},
	{"yuki-abrams", "Yuki Abrams", "acme-corp", "head-of-sales"},
	// Blueshift
	{"theo-nakamura", "Theo Nakamura", "blueshift", "cto"},
	{"lena-okafor", "Lena Okafor", "blueshift", "director-product"},
	{"rafael-cho", "Rafael Cho", "blueshift", "senior-engineer"},
	{"maya-grant", "Maya Grant", "blueshift", "designer"},
	{"ivan-petrov", "Ivan Petrov", "blueshift", "sales-lead"},
	// Dunder Mifflin
	{"harper-quinn", "Harper Quinn", "dunder-mifflin", "regional-manager"},
	{"bruno-salas", "Bruno Salas", "dunder-mifflin", "account-manager"},
	{"esme-walker", "Esme Walker", "dunder-mifflin", "operations-lead"},
	{"kiran-joshi", "Kiran Joshi", "dunder-mifflin", "it-director"},
	{"nora-finch", "Nora Finch", "dunder-mifflin", "cfo"},
	// Northwind
	{"oscar-delmar", "Oscar Delmar", "northwind", "ceo"},
	{"tessa-mori", "Tessa Mori", "northwind", "vp-marketing"},
	{"jonah-pike", "Jonah Pike", "northwind", "partnerships-lead"},
	{"amina-reyes", "Amina Reyes", "northwind", "head-of-people"},
	{"viktor-ng", "Viktor Ng", "northwind", "data-lead"},
	// Vandelay
	{"elena-koch", "Elena Koch", "vandelay", "founder"},
	{"rami-sato", "Rami Sato", "vandelay", "engineering-lead"},
	{"claudia-vega", "Claudia Vega", "vandelay", "customer-success"},
	{"noah-brant", "Noah Brant", "vandelay", "growth-pm"},
	{"lin-waters", "Lin Waters", "vandelay", "finance-lead"},
}

var projects = []project{
	{"q2-pilot", "Q2 Pilot Program", "acme-corp"},
	{"pricing-rework", "Pricing Rework", "blueshift"},
	{"apac-launch", "APAC Launch", "northwind"},
	{"security-audit", "Security Audit", "dunder-mifflin"},
	{"onboarding-v3", "Onboarding V3", "vandelay"},
	{"data-platform", "Data Platform Migration", "acme-corp"},
	{"mobile-revamp", "Mobile Revamp", "blueshift"},
	{"partner-program", "Partner Program", "northwind"},
}

// New roles used by superseding / contradiction artifacts.
var promotionRoles = []string{
	"vp-sales",
	"head-of-product",
	"director-engineering",
	"vp-customer-success",
	"chief-of-staff",
	"head-of-platform",
	"vp-operations",
	"principal-engineer",
}

var newCompanyForMove = []string{
	"blueshift", "acme-corp", "northwind", "vandelay", "dunder-mifflin",
}

// ---------------------------------------------------------------------------
// Sentence patterns — 10 per fact type so prose has real linguistic variety
// ---------------------------------------------------------------------------

// roleStatusPatterns: subject role_at object (person role_at company).
// Placeholders: {name} {role} {company} {date}
var roleStatusPatterns = []string{
	"{name} was promoted to {role} at {company} on {date}.",
	"Heads up — {name} is now {role} at {company} as of {date}.",
	"Confirming that {name} stepped into the {role} role at {company} last week.",
	"{name} officially takes over as {role} at {company} effective {date}.",
	"Per the internal note, {name} is the new {role} at {company} starting {date}.",
	"FYI: {name}'s title at {company} is now {role}. Update your records.",
	"{name} has moved into the {role} seat at {company}. Announced {date}.",
	"The {role} job at {company} is going to {name}. Email went out {date}.",
	"Just met {name} — she is the {role} over at {company} now.",
	"{name} owns the {role} function at {company} as of {date}.",
}

// projectLeadPatterns: subject leads object (person leads project).
var projectLeadPatterns = []string{
	"{name} is leading the {project} project at {company}.",
	"{project} is now under {name}'s umbrella over at {company}.",
	"We've handed {project} to {name} on the {company} side.",
	"{name} owns {project} end-to-end for {company} this quarter.",
	"{name} picked up the {project} initiative — it is her baby now.",
	"The {project} program manager at {company} is {name}.",
	"{name} is the DRI on {project}. Route all updates there.",
	"{project} rolls up to {name} in the {company} org.",
	"{name} has been running point on {project} since Monday.",
	"Put {name} as the owner of {project}. She accepted yesterday.",
}

// projectChampionPatterns: subject champions object (relationship).
var projectChampionPatterns = []string{
	"{name} championed the {project} pilot from day one.",
	"We would not have {project} without {name} pushing it internally.",
	"{name} has been the executive champion for {project} since kickoff.",
	"The reason {project} got funded is {name}. She made the case.",
	"{name} carried {project} through the steering committee — twice.",
	"{project} survived because {name} refused to let it die.",
	"Credit where it is due: {name} got {project} over the line.",
	"{name} is the internal sponsor for {project}. Keep her in the loop.",
	"The {project} initiative has one sponsor and it is {name}.",
	"Shout out to {name} — she is why {project} is still alive.",
}

// worksWithPatterns: relationship between two people.
var worksWithPatterns = []string{
	"{name1} works closely with {name2} on the {project} rollout.",
	"{name1} and {name2} are paired up on {project} this cycle.",
	"Every week {name1} syncs with {name2} to unblock {project}.",
	"{name1} has been partnering with {name2} on {project} since March.",
	"For {project}, the two key people are {name1} and {name2}.",
	"{name1} co-leads {project} alongside {name2}.",
	"{name1} and {name2} own {project} jointly at {company}.",
	"The {project} working group is {name1} and {name2}. Nobody else.",
	"{name1} is the engineering counterpart to {name2} on {project}.",
	"{name1} escalates to {name2} on anything {project} related.",
}

// observationPatterns: one-off observations about a person.
var observationPatterns = []string{
	"{name} mentioned she is focused on {project} through end of quarter.",
	"On the call today, {name} flagged a concern about {project} timelines.",
	"{name} shipped the first draft of {project} specs yesterday.",
	"{name} is the internal go-to for anything {project} related.",
	"{name} pushed back on the {project} scope in the review.",
	"{name} signed off on the {project} budget this morning.",
	"Saw {name} present the {project} roadmap — sharp deck.",
	"{name} has deep context on {project}. Loop her in early.",
	"{name} is blocked on {project} until the security review clears.",
	"{name} is traveling next week but still owns {project} updates.",
}

// companyMovePatterns: person moved to a new company.
var companyMovePatterns = []string{
	"{name} has moved from her old role and is now at {company}.",
	"Update: {name} joined {company} — starts {date}.",
	"{name} left her previous company — she is over at {company} now.",
	"Big news — {name} is with {company} effective {date}.",
	"{name} transitioned to {company} this month. New email, same person.",
	"{name}'s new home is {company}. Reach her through their switchboard.",
	"Making it official — {name} is a {company} employee as of {date}.",
	"{name} signed with {company}. Announcement went out {date}.",
	"{name} is no longer at her old shop — she is a {company} hire now.",
	"{name} has taken a role at {company}. Congrats note went out {date}.",
}

// noisePatterns: artifacts with NO extractable fact (test null handling).
var noisePatterns = []string{
	"Reminder: the office is closed Monday for the holiday. Plan accordingly.",
	"The snack budget got approved. Vote for new options in the shared doc.",
	"Laptop rotations start next week. IT will email you a pickup slot.",
	"Big thanks to whoever fixed the conference room AC. Heroes walk among us.",
	"Happy Friday, team. Go touch some grass this weekend.",
	"Just a heads up — parking garage is down to one entrance through Wednesday.",
	"Weekly all-hands is rescheduled to Thursday. Invite will update.",
	"The vending machine is officially out of the pretzel bites we love. Moment of silence.",
	"If anyone lost a black North Face jacket, it is in the front desk lost and found.",
	"Calendar tip: block focus time for yourself. The meetings will otherwise find you.",
	"Slack channel for the book club is open. No pressure to join but the snacks are good.",
	"Coffee machine is back. Third time this year. Let's hope it holds.",
	"Team lunch Tuesday — sign up by EOD Monday or lose your spot.",
	"Reminder to finish your annual compliance training before the 30th.",
	"The office dog, Bagel, will be in on Thursday. Bring your allergy meds if needed.",
	"Someone left a container of leftovers in the fridge since last Wednesday. Claim or it goes.",
	"The Wi-Fi in the east wing is flaky today. IT is on it.",
	"Do not forget to submit your expense reports before Friday.",
	"Happy hour is tonight at the rooftop. First drink is on the company.",
	"Please silence notifications during the keynote. It gets loud otherwise.",
	"The new badge readers are live. If you get locked out, call facilities.",
	"Quarterly survey goes out tomorrow. Please take ten minutes to fill it out.",
	"Whoever has been borrowing the good whiteboard markers — we see you.",
	"Stand desks are being installed next week. Expect some noise Tuesday morning.",
	"The printer on the third floor is jammed again. Use the one by the kitchen.",
}

// ---------------------------------------------------------------------------
// Artifact + expected-fact shape (what we serialize to corpus.jsonl)
// ---------------------------------------------------------------------------

type expectedTriplet struct {
	Subject   string `json:"subject"`
	Predicate string `json:"predicate"`
	Object    string `json:"object"`
}

type expectedFact struct {
	FactID     string          `json:"fact_id"`
	EntitySlug string          `json:"entity_slug"`
	Triplet    expectedTriplet `json:"triplet"`
	Text       string          `json:"text"`
	// SentenceOffset is the position inside the artifact body (line index).
	SentenceOffset int `json:"sentence_offset"`
	// Supersedes is populated for role-change artifacts.
	Supersedes []string `json:"supersedes,omitempty"`
	// ContradictsWith is populated for contradiction artifacts.
	ContradictsWith []string `json:"contradicts_with,omitempty"`
}

type artifact struct {
	ArtifactID    string         `json:"artifact_id"`
	ArtifactSHA   string         `json:"artifact_sha"`
	Kind          string         `json:"kind"`
	OccurredAt    string         `json:"occurred_at"`
	Body          string         `json:"body"`
	ExpectedFacts []expectedFact `json:"expected_facts"`
}

type query struct {
	QueryID               string   `json:"query_id"`
	Query                 string   `json:"query"`
	QueryClass            string   `json:"query_class"`
	ExpectedFactIDs       []string `json:"expected_fact_ids"`
	ExpectedMinRecallAt20 float64  `json:"expected_min_recall_at_20"`
	// Rationale helps a human reviewer see what the retriever is being
	// asked to surface. Not used by the gate itself.
	Rationale string `json:"rationale,omitempty"`
}

// ---------------------------------------------------------------------------
// Deterministic artifact SHA
// ---------------------------------------------------------------------------

func artifactSHA(i int) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("wuphf-bench-slice-1/artifact/%04d", i)))
	return hex.EncodeToString(h[:])[:16]
}

// ---------------------------------------------------------------------------
// Generator state — tracks the "current truth" of each (subject, predicate)
// so supersedes + contradictions + queries can target real fact IDs.
// ---------------------------------------------------------------------------

// factRef remembers a fact id and the object it was stated against.
type factRef struct {
	FactID string
	Object string
}

type genState struct {
	rng *rand.Rand

	// facts keyed by (subject, predicate) → ordered history of statements.
	// The last entry is the current truth.
	facts map[string][]factRef

	// All facts written, indexed by fact ID (for query validation).
	allFactIDs map[string]expectedFact

	// Companies we have stated any fact about (for queries).
	touchedCompanies map[string]bool

	// Projects championed / led per company — for multi-hop queries.
	projectLeadsByCompany map[string]map[string][]factRef // company → project → factRefs for lead

	// People → championed projects (for multi-hop "who at X championed Y").
	championsByProject map[string][]factRef // project → factRefs

	// Current company per person (for counterfactuals).
	personCompany map[string]string
}

func newGenState(rng *rand.Rand) *genState {
	return &genState{
		rng:                   rng,
		facts:                 map[string][]factRef{},
		allFactIDs:            map[string]expectedFact{},
		touchedCompanies:      map[string]bool{},
		projectLeadsByCompany: map[string]map[string][]factRef{},
		championsByProject:    map[string][]factRef{},
		personCompany:         map[string]string{},
	}
}

func factKey(subject, predicate string) string {
	return subject + "|" + predicate
}

// ---------------------------------------------------------------------------
// Artifact builders
// ---------------------------------------------------------------------------

var kinds = []string{"chat", "meeting", "email"}

func pickKind(rng *rand.Rand) string {
	return kinds[rng.Intn(len(kinds))]
}

// formatDate renders a date in the same prose style across patterns.
// Go reference date is "Mon Jan 2 15:04:05 MST 2006"; the year must be 2006
// or Go treats the token as a different component.
func formatDate(t time.Time) string {
	return t.Format("January 2, 2006")
}

// Build a single-entity status/observation artifact. Fills expected_facts
// with one fact.
func buildSingleEntityArtifact(s *genState, idx int, occurred time.Time) artifact {
	artSHA := artifactSHA(idx)

	// Flip between role status and observation with 2:1 weight.
	choice := s.rng.Intn(3)
	if choice < 2 {
		return buildRoleStatusArtifact(s, idx, occurred, artSHA, false /*supersedes*/)
	}
	return buildObservationArtifact(s, idx, occurred, artSHA)
}

func buildRoleStatusArtifact(s *genState, idx int, occurred time.Time, artSHA string, isSupersede bool) artifact {
	p := people[s.rng.Intn(len(people))]

	role := p.Role
	if isSupersede {
		role = promotionRoles[s.rng.Intn(len(promotionRoles))]
	} else if s.rng.Intn(4) == 0 {
		// 25% chance we introduce a promoted role even without supersede flag —
		// the recall gate cares about retrievability, not truth.
		role = promotionRoles[s.rng.Intn(len(promotionRoles))]
	}

	company := p.Company
	// 15% of the time, have the person move companies instead.
	moved := false
	if !isSupersede && s.rng.Intn(7) == 0 {
		company = newCompanyForMove[s.rng.Intn(len(newCompanyForMove))]
		moved = true
	}

	pattern := roleStatusPatterns[idx%len(roleStatusPatterns)]
	if moved {
		pattern = companyMovePatterns[idx%len(companyMovePatterns)]
	}

	display := p.Display
	companyDisplay := companyDisplayName(company)
	date := formatDate(occurred)

	sentence := applyPattern(pattern, map[string]string{
		"{name}":    display,
		"{role}":    humanizeRole(role),
		"{company}": companyDisplay,
		"{date}":    date,
	})

	predicate := "role_at"
	subject := p.Slug
	object := "company:" + company
	// For a move, we still encode with role_at because the triplet object
	// is the target company — the role change is captured in the text.

	body := wrapBody(s.rng, sentence, occurred)
	offset := findSentenceOffset(body, sentence)

	factID := team.ComputeFactID(artSHA, offset, subject, predicate, object)
	supersedes := []string(nil)
	if isSupersede {
		key := factKey(subject, predicate)
		if prev := s.facts[key]; len(prev) > 0 {
			supersedes = []string{prev[len(prev)-1].FactID}
		}
	}

	ef := expectedFact{
		FactID:         factID,
		EntitySlug:     subject,
		Triplet:        expectedTriplet{Subject: subject, Predicate: predicate, Object: object},
		Text:           sentence,
		SentenceOffset: offset,
		Supersedes:     supersedes,
	}

	// Record in state.
	s.facts[factKey(subject, predicate)] = append(s.facts[factKey(subject, predicate)], factRef{FactID: factID, Object: object})
	s.allFactIDs[factID] = ef
	s.touchedCompanies[company] = true
	s.personCompany[subject] = company

	return artifact{
		ArtifactID:    fmt.Sprintf("art_%04d", idx),
		ArtifactSHA:   artSHA,
		Kind:          pickKind(s.rng),
		OccurredAt:    occurred.Format(time.RFC3339),
		Body:          body,
		ExpectedFacts: []expectedFact{ef},
	}
}

func buildObservationArtifact(s *genState, idx int, occurred time.Time, artSHA string) artifact {
	p := people[s.rng.Intn(len(people))]
	proj := projects[s.rng.Intn(len(projects))]

	pattern := observationPatterns[idx%len(observationPatterns)]
	sentence := applyPattern(pattern, map[string]string{
		"{name}":    p.Display,
		"{project}": proj.Display,
	})

	subject := p.Slug
	predicate := "involved_in"
	object := "project:" + proj.Slug

	body := wrapBody(s.rng, sentence, occurred)
	offset := findSentenceOffset(body, sentence)

	factID := team.ComputeFactID(artSHA, offset, subject, predicate, object)
	ef := expectedFact{
		FactID:         factID,
		EntitySlug:     subject,
		Triplet:        expectedTriplet{Subject: subject, Predicate: predicate, Object: object},
		Text:           sentence,
		SentenceOffset: offset,
	}

	s.facts[factKey(subject, predicate)] = append(s.facts[factKey(subject, predicate)], factRef{FactID: factID, Object: object})
	s.allFactIDs[factID] = ef

	return artifact{
		ArtifactID:    fmt.Sprintf("art_%04d", idx),
		ArtifactSHA:   artSHA,
		Kind:          pickKind(s.rng),
		OccurredAt:    occurred.Format(time.RFC3339),
		Body:          body,
		ExpectedFacts: []expectedFact{ef},
	}
}

// Multi-entity: person leads/champions a project → emits 2 facts: a project
// relationship + the person's relationship to the company.
func buildMultiEntityArtifact(s *genState, idx int, occurred time.Time) artifact {
	artSHA := artifactSHA(idx)
	p := people[s.rng.Intn(len(people))]
	proj := projects[s.rng.Intn(len(projects))]

	useChampion := s.rng.Intn(2) == 0
	var pattern string
	var predicate string
	if useChampion {
		pattern = projectChampionPatterns[idx%len(projectChampionPatterns)]
		predicate = "champions"
	} else {
		pattern = projectLeadPatterns[idx%len(projectLeadPatterns)]
		predicate = "leads"
	}

	sentence := applyPattern(pattern, map[string]string{
		"{name}":    p.Display,
		"{project}": proj.Display,
		"{company}": companyDisplayName(p.Company),
	})

	body := wrapBody(s.rng, sentence, occurred)
	offset := findSentenceOffset(body, sentence)

	subject := p.Slug
	object := "project:" + proj.Slug
	factID := team.ComputeFactID(artSHA, offset, subject, predicate, object)

	ef := expectedFact{
		FactID:         factID,
		EntitySlug:     subject,
		Triplet:        expectedTriplet{Subject: subject, Predicate: predicate, Object: object},
		Text:           sentence,
		SentenceOffset: offset,
	}
	s.facts[factKey(subject, predicate)] = append(s.facts[factKey(subject, predicate)], factRef{FactID: factID, Object: object})
	s.allFactIDs[factID] = ef

	// Track multi-hop indexes.
	if useChampion {
		s.championsByProject[proj.Slug] = append(s.championsByProject[proj.Slug], factRef{FactID: factID, Object: p.Slug})
	} else {
		byProject := s.projectLeadsByCompany[p.Company]
		if byProject == nil {
			byProject = map[string][]factRef{}
			s.projectLeadsByCompany[p.Company] = byProject
		}
		byProject[proj.Slug] = append(byProject[proj.Slug], factRef{FactID: factID, Object: p.Slug})
	}
	s.touchedCompanies[p.Company] = true
	s.personCompany[subject] = p.Company

	return artifact{
		ArtifactID:    fmt.Sprintf("art_%04d", idx),
		ArtifactSHA:   artSHA,
		Kind:          pickKind(s.rng),
		OccurredAt:    occurred.Format(time.RFC3339),
		Body:          body,
		ExpectedFacts: []expectedFact{ef},
	}
}

// Superseding: pick a person who already has a role_at fact and promote them.
// Falls back to a plain role status if no prior fact exists yet.
func buildSupersedingArtifact(s *genState, idx int, occurred time.Time) artifact {
	// Find a candidate with a prior role_at fact.
	candidates := make([]string, 0, len(people))
	for _, p := range people {
		if len(s.facts[factKey(p.Slug, "role_at")]) > 0 {
			candidates = append(candidates, p.Slug)
		}
	}
	if len(candidates) == 0 {
		// No prior role_at yet — emit a regular one so the query pool is non-empty.
		return buildRoleStatusArtifact(s, idx, occurred, artifactSHA(idx), false)
	}
	sort.Strings(candidates)
	chosen := candidates[s.rng.Intn(len(candidates))]
	// Look up person record.
	var p person
	for _, cand := range people {
		if cand.Slug == chosen {
			p = cand
			break
		}
	}
	// Override company so supersede stays within the same employer by default.
	artSHA := artifactSHA(idx)
	role := promotionRoles[s.rng.Intn(len(promotionRoles))]
	pattern := roleStatusPatterns[idx%len(roleStatusPatterns)]

	sentence := applyPattern(pattern, map[string]string{
		"{name}":    p.Display,
		"{role}":    humanizeRole(role),
		"{company}": companyDisplayName(p.Company),
		"{date}":    formatDate(occurred),
	})

	body := wrapBody(s.rng, sentence, occurred)
	offset := findSentenceOffset(body, sentence)

	subject := p.Slug
	predicate := "role_at"
	object := "company:" + p.Company
	factID := team.ComputeFactID(artSHA, offset, subject, predicate, object)

	// Supersedes previous fact (same key).
	prev := s.facts[factKey(subject, predicate)]
	var supersedes []string
	if len(prev) > 0 {
		supersedes = []string{prev[len(prev)-1].FactID}
	}

	ef := expectedFact{
		FactID:         factID,
		EntitySlug:     subject,
		Triplet:        expectedTriplet{Subject: subject, Predicate: predicate, Object: object},
		Text:           sentence,
		SentenceOffset: offset,
		Supersedes:     supersedes,
	}
	s.facts[factKey(subject, predicate)] = append(s.facts[factKey(subject, predicate)], factRef{FactID: factID, Object: object})
	s.allFactIDs[factID] = ef
	s.personCompany[subject] = p.Company

	return artifact{
		ArtifactID:    fmt.Sprintf("art_%04d", idx),
		ArtifactSHA:   artSHA,
		Kind:          pickKind(s.rng),
		OccurredAt:    occurred.Format(time.RFC3339),
		Body:          body,
		ExpectedFacts: []expectedFact{ef},
	}
}

// Contradiction: emits a fact that disagrees with a prior fact for the same
// (subject, predicate). Object differs.
func buildContradictionArtifact(s *genState, idx int, occurred time.Time) artifact {
	// Need a prior role_at to contradict.
	candidates := make([]string, 0, len(people))
	for _, p := range people {
		if len(s.facts[factKey(p.Slug, "role_at")]) > 0 {
			candidates = append(candidates, p.Slug)
		}
	}
	if len(candidates) == 0 {
		return buildRoleStatusArtifact(s, idx, occurred, artifactSHA(idx), false)
	}
	sort.Strings(candidates)
	chosen := candidates[s.rng.Intn(len(candidates))]
	var p person
	for _, cand := range people {
		if cand.Slug == chosen {
			p = cand
			break
		}
	}

	// Pick a DIFFERENT company than whatever was last recorded.
	prev := s.facts[factKey(p.Slug, "role_at")]
	lastObject := ""
	if len(prev) > 0 {
		lastObject = prev[len(prev)-1].Object
	}
	var newCompany string
	for attempts := 0; attempts < 10; attempts++ {
		candidate := newCompanyForMove[s.rng.Intn(len(newCompanyForMove))]
		obj := "company:" + candidate
		if obj != lastObject {
			newCompany = candidate
			break
		}
	}
	if newCompany == "" {
		newCompany = newCompanyForMove[0]
	}

	artSHA := artifactSHA(idx)
	pattern := companyMovePatterns[idx%len(companyMovePatterns)]
	sentence := applyPattern(pattern, map[string]string{
		"{name}":    p.Display,
		"{company}": companyDisplayName(newCompany),
		"{date}":    formatDate(occurred),
	})
	body := wrapBody(s.rng, sentence, occurred)
	offset := findSentenceOffset(body, sentence)

	subject := p.Slug
	predicate := "role_at"
	object := "company:" + newCompany
	factID := team.ComputeFactID(artSHA, offset, subject, predicate, object)

	contradicts := []string{prev[len(prev)-1].FactID}

	ef := expectedFact{
		FactID:          factID,
		EntitySlug:      subject,
		Triplet:         expectedTriplet{Subject: subject, Predicate: predicate, Object: object},
		Text:            sentence,
		SentenceOffset:  offset,
		ContradictsWith: contradicts,
	}

	s.facts[factKey(subject, predicate)] = append(s.facts[factKey(subject, predicate)], factRef{FactID: factID, Object: object})
	s.allFactIDs[factID] = ef
	s.personCompany[subject] = newCompany

	return artifact{
		ArtifactID:    fmt.Sprintf("art_%04d", idx),
		ArtifactSHA:   artSHA,
		Kind:          pickKind(s.rng),
		OccurredAt:    occurred.Format(time.RFC3339),
		Body:          body,
		ExpectedFacts: []expectedFact{ef},
	}
}

// Noise: no extractable fact.
func buildNoiseArtifact(s *genState, idx int, occurred time.Time) artifact {
	artSHA := artifactSHA(idx)
	sentence := noisePatterns[idx%len(noisePatterns)]
	body := wrapBody(s.rng, sentence, occurred)

	return artifact{
		ArtifactID:    fmt.Sprintf("art_%04d", idx),
		ArtifactSHA:   artSHA,
		Kind:          pickKind(s.rng),
		OccurredAt:    occurred.Format(time.RFC3339),
		Body:          body,
		ExpectedFacts: nil, // no facts — null-handling test
	}
}

// ---------------------------------------------------------------------------
// Body wrapping — artifacts look like real chat / email / meeting notes,
// not bare sentences. The fact-bearing sentence still has a deterministic
// line offset so ComputeFactID stays stable.
// ---------------------------------------------------------------------------

var chatOpeners = []string{
	"Hey all —",
	"Quick update:",
	"FYI team,",
	"Flagging this for visibility:",
	"Circulating a note —",
	"Sharing what came out of the sync:",
	"One thing from the huddle:",
}

var chatClosers = []string{
	"— thanks",
	"Let me know if questions.",
	"Appreciate it.",
	"More to come.",
	"Flag if this needs discussion.",
}

// wrapBody puts the fact-bearing sentence on a predictable line (line 1).
// Line 0 is a short opener, line 1 is the fact, line 2 is a closer.
func wrapBody(rng *rand.Rand, sentence string, occurred time.Time) string {
	opener := chatOpeners[rng.Intn(len(chatOpeners))]
	closer := chatClosers[rng.Intn(len(chatClosers))]
	return strings.Join([]string{opener, sentence, closer}, "\n")
}

// findSentenceOffset returns the 0-based line index of `sentence` in `body`.
// wrapBody always places the fact on line 1, so this is really a sanity
// check — but we compute it honestly so the generator stays correct if
// wrapBody evolves.
func findSentenceOffset(body, sentence string) int {
	lines := strings.Split(body, "\n")
	for i, l := range lines {
		if l == sentence {
			return i
		}
	}
	return 1 // fall back to the documented slot
}

// ---------------------------------------------------------------------------
// Pattern helpers
// ---------------------------------------------------------------------------

func applyPattern(pattern string, vars map[string]string) string {
	out := pattern
	// Sort keys so iteration order is deterministic (even though Replace
	// is order-independent for non-overlapping placeholders, we keep the
	// discipline).
	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		out = strings.ReplaceAll(out, k, vars[k])
	}
	return out
}

func humanizeRole(slug string) string {
	parts := strings.Split(slug, "-")
	caps := map[string]string{
		"vp":  "VP",
		"cto": "CTO",
		"ceo": "CEO",
		"cfo": "CFO",
		"pm":  "PM",
		"it":  "IT",
		"dri": "DRI",
	}
	// Roles like "vp-sales" → "VP of Sales"; "head-of-product" stays as
	// "Head of Product"; "account-executive" → "Account Executive".
	for i, p := range parts {
		if c, ok := caps[p]; ok {
			parts[i] = c
			continue
		}
		if len(p) == 0 {
			continue
		}
		// Lowercase connector words that shouldn't be title-cased.
		if p == "of" || p == "the" || p == "and" {
			parts[i] = p
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	// Insert "of" after the leading title-word when the role is a single
	// two-part slug like "vp-sales" or "director-engineering" — but NOT
	// when the slug already contains "of" (e.g. "head-of-product").
	hasOf := false
	for _, p := range parts {
		if strings.EqualFold(p, "of") {
			hasOf = true
			break
		}
	}
	if !hasOf && len(parts) == 2 {
		// Only "VP of X" and "Director of X" read naturally. "Principal
		// Engineer", "Senior Engineer", "Chief Of Staff" stay as-is.
		titleWords := map[string]bool{
			"VP":       true,
			"Director": true,
			"Head":     true,
		}
		if titleWords[parts[0]] {
			return parts[0] + " of " + parts[1]
		}
	}
	return strings.Join(parts, " ")
}

func companyDisplayName(slug string) string {
	for _, c := range companies {
		if c.Slug == slug {
			return c.Display
		}
	}
	return slug
}

func personDisplay(slug string) string {
	for _, p := range people {
		if p.Slug == slug {
			return p.Display
		}
	}
	return slug
}

func projectDisplay(slug string) string {
	for _, p := range projects {
		if p.Slug == slug {
			return p.Display
		}
	}
	return slug
}

// ---------------------------------------------------------------------------
// Query generation — each query maps to the real fact_ids in allFactIDs.
// ---------------------------------------------------------------------------

func buildQueries(s *genState) []query {
	queries := make([]query, 0, totalQueries)

	// Status queries: "What does X do?" / "Who is X?" / "Where does X work?"
	// Map to the MOST RECENT role_at fact for person X plus any relationship
	// facts that share the subject.
	statusTemplates := []struct {
		q     string
		class string
	}{
		{"What does %s do?", "status"},
		{"Who is %s?", "status"},
		{"Where does %s work?", "status"},
		{"What is %s's current role?", "status"},
	}
	statusPeople := shufflePeople(s.rng, queryMix.Status)
	for i := 0; i < queryMix.Status; i++ {
		p := statusPeople[i]
		tmpl := statusTemplates[i%len(statusTemplates)]
		q := fmt.Sprintf(tmpl.q, p.Display)

		// Expected: the LAST role_at fact (most recent truth) plus any
		// non-contradicted observation/champions/leads facts for this person.
		expected := expectedFactsForPerson(s, p.Slug, []string{"role_at", "involved_in", "leads", "champions"}, true)
		queries = append(queries, query{
			QueryID:               fmt.Sprintf("q_%03d", len(queries)+1),
			Query:                 q,
			QueryClass:            tmpl.class,
			ExpectedFactIDs:       expected,
			ExpectedMinRecallAt20: 0.85,
			Rationale:             fmt.Sprintf("Most recent role_at + involvement facts for %s.", p.Slug),
		})
	}

	// Relationship queries: "Who works with X?" / "Who leads Y?" /
	//                      "Who champions Y?"
	relProjects := shuffleProjects(s.rng, queryMix.Relationship)
	for i := 0; i < queryMix.Relationship; i++ {
		proj := relProjects[i]
		var q, class string
		var expected []string
		switch i % 3 {
		case 0:
			q = fmt.Sprintf("Who leads %s?", proj.Display)
			class = "relationship"
			expected = expectedFactsForProjectPredicate(s, proj.Slug, "leads")
		case 1:
			q = fmt.Sprintf("Who champions %s?", proj.Display)
			class = "relationship"
			expected = expectedFactsForProjectPredicate(s, proj.Slug, "champions")
		case 2:
			q = fmt.Sprintf("Who is involved in %s?", proj.Display)
			class = "relationship"
			expected = expectedFactsForProjectAnyPredicate(s, proj.Slug, []string{"leads", "champions", "involved_in"})
		}
		if expected == nil {
			expected = []string{}
		}
		queries = append(queries, query{
			QueryID:               fmt.Sprintf("q_%03d", len(queries)+1),
			Query:                 q,
			QueryClass:            class,
			ExpectedFactIDs:       expected,
			ExpectedMinRecallAt20: 0.85,
			Rationale:             fmt.Sprintf("Facts about %s project.", proj.Slug),
		})
	}

	// Multi-hop queries: "Who at <company> championed <project>?"
	// Expected: intersect champions(project) with role_at(person)==company.
	multihopCompanies := shuffleCompanies(s.rng, queryMix.MultiHop)
	for i := 0; i < queryMix.MultiHop; i++ {
		c := multihopCompanies[i]
		// Pick a project that has any champion at company c.
		proj := pickProjectForMultiHop(s, c.Slug)
		var q, rationale string
		var expected []string
		if proj != "" {
			q = fmt.Sprintf("Who at %s championed the %s project?", c.Display, projectDisplay(proj))
			expected = expectedMultiHop(s, c.Slug, proj)
			rationale = fmt.Sprintf("Intersect champions(%s) with role_at==%s.", proj, c.Slug)
		} else {
			// Fallback: a general "who at company leads any project" — still
			// answerable from the corpus.
			q = fmt.Sprintf("Who at %s is leading a project right now?", c.Display)
			expected = expectedLeadsAtCompany(s, c.Slug)
			rationale = fmt.Sprintf("Leads facts for people whose current role_at is %s.", c.Slug)
		}
		if expected == nil {
			expected = []string{}
		}
		queries = append(queries, query{
			QueryID:               fmt.Sprintf("q_%03d", len(queries)+1),
			Query:                 q,
			QueryClass:            "multi_hop",
			ExpectedFactIDs:       expected,
			ExpectedMinRecallAt20: 0.85,
			Rationale:             rationale,
		})
	}

	// Counterfactual queries — what if X hadn't happened.
	counterPeople := shufflePeople(s.rng, queryMix.Counterfactual)
	for i := 0; i < queryMix.Counterfactual; i++ {
		p := counterPeople[i]
		q := fmt.Sprintf("What would have happened if %s had not taken her current role?", p.Display)
		expected := expectedFactsForPerson(s, p.Slug, []string{"role_at"}, true)
		queries = append(queries, query{
			QueryID:               fmt.Sprintf("q_%03d", len(queries)+1),
			Query:                 q,
			QueryClass:            "counterfactual",
			ExpectedFactIDs:       expected,
			ExpectedMinRecallAt20: 0.85,
			Rationale:             fmt.Sprintf("Current role_at facts for %s — retriever should surface even if reasoning would refuse.", p.Slug),
		})
	}

	// Out-of-scope queries: no expected facts, retriever should refuse.
	oosQueries := []string{
		"What is the weather on Mars today?",
		"Explain the plot of a novel that has never been written.",
	}
	for i := 0; i < queryMix.OutOfScope; i++ {
		queries = append(queries, query{
			QueryID:               fmt.Sprintf("q_%03d", len(queries)+1),
			Query:                 oosQueries[i%len(oosQueries)],
			QueryClass:            "general",
			ExpectedFactIDs:       []string{},
			ExpectedMinRecallAt20: 1.0, // vacuously true when expected is empty
			Rationale:             "Out of scope — retriever should return nothing; expected set empty.",
		})
	}

	return queries
}

// shufflePeople returns min(n, len(people)) distinct people in deterministic
// shuffled order.
func shufflePeople(rng *rand.Rand, n int) []person {
	idx := rng.Perm(len(people))
	if n > len(people) {
		n = len(people)
	}
	out := make([]person, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, people[idx[i]])
	}
	return out
}

func shuffleProjects(rng *rand.Rand, n int) []project {
	idx := rng.Perm(len(projects))
	out := make([]project, 0, n)
	for i := 0; i < n && i < len(idx); i++ {
		out = append(out, projects[idx[i]])
	}
	// If caller wants more than we have, cycle.
	for len(out) < n {
		out = append(out, projects[idx[len(out)%len(projects)]])
	}
	return out
}

func shuffleCompanies(rng *rand.Rand, n int) []company {
	idx := rng.Perm(len(companies))
	out := make([]company, 0, n)
	for i := 0; i < n && i < len(idx); i++ {
		out = append(out, companies[idx[i]])
	}
	for len(out) < n {
		out = append(out, companies[idx[len(out)%len(companies)]])
	}
	return out
}

// expectedFactsForPerson returns fact IDs for the given person subject across
// one or more predicates. If currentOnly is true, only the LAST fact per
// predicate (the current truth) is included — reflecting how status queries
// should prefer the freshest answer.
func expectedFactsForPerson(s *genState, slug string, predicates []string, currentOnly bool) []string {
	out := []string{}
	for _, pred := range predicates {
		history := s.facts[factKey(slug, pred)]
		if len(history) == 0 {
			continue
		}
		if currentOnly && pred == "role_at" {
			out = append(out, history[len(history)-1].FactID)
			continue
		}
		for _, ref := range history {
			out = append(out, ref.FactID)
		}
	}
	sort.Strings(out)
	return dedupe(out)
}

func expectedFactsForProjectPredicate(s *genState, projSlug, predicate string) []string {
	out := []string{}
	for key, history := range s.facts {
		if !strings.HasSuffix(key, "|"+predicate) {
			continue
		}
		for _, ref := range history {
			if ref.Object == "project:"+projSlug {
				out = append(out, ref.FactID)
			}
		}
	}
	sort.Strings(out)
	return dedupe(out)
}

func expectedFactsForProjectAnyPredicate(s *genState, projSlug string, preds []string) []string {
	out := []string{}
	wanted := map[string]bool{}
	for _, p := range preds {
		wanted[p] = true
	}
	for key, history := range s.facts {
		parts := strings.SplitN(key, "|", 2)
		if len(parts) != 2 {
			continue
		}
		if !wanted[parts[1]] {
			continue
		}
		for _, ref := range history {
			if ref.Object == "project:"+projSlug {
				out = append(out, ref.FactID)
			}
		}
	}
	sort.Strings(out)
	return capExpected(dedupe(out))
}

// pickProjectForMultiHop returns a project slug that has at least one
// champion whose current role_at is the given company, or "" if none.
func pickProjectForMultiHop(s *genState, companySlug string) string {
	type pair struct {
		Project string
		FactID  string
	}
	candidates := []string{}
	for proj, refs := range s.championsByProject {
		for _, r := range refs {
			if s.personCompany[r.Object] == companySlug {
				candidates = append(candidates, proj)
				break
			}
		}
	}
	if len(candidates) == 0 {
		return ""
	}
	sort.Strings(candidates)
	return candidates[s.rng.Intn(len(candidates))]
}

func expectedMultiHop(s *genState, companySlug, projSlug string) []string {
	out := []string{}
	for _, r := range s.championsByProject[projSlug] {
		if s.personCompany[r.Object] != companySlug {
			continue
		}
		out = append(out, r.FactID)
		// Include the person's current role_at fact too — multi-hop retrievers
		// should surface both sides of the join.
		history := s.facts[factKey(r.Object, "role_at")]
		if len(history) > 0 {
			out = append(out, history[len(history)-1].FactID)
		}
	}
	sort.Strings(out)
	return capExpected(dedupe(out))
}

func expectedLeadsAtCompany(s *genState, companySlug string) []string {
	out := []string{}
	for key, history := range s.facts {
		if !strings.HasSuffix(key, "|leads") {
			continue
		}
		parts := strings.SplitN(key, "|", 2)
		if len(parts) != 2 {
			continue
		}
		subject := parts[0]
		if s.personCompany[subject] != companySlug {
			continue
		}
		for _, ref := range history {
			out = append(out, ref.FactID)
		}
	}
	sort.Strings(out)
	return dedupe(out)
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// expectedSetCap is the per-query ceiling on |expected| so recall@20 stays a
// solvable metric. Queries whose raw expected set would exceed this cap get
// deterministically truncated — we sort ascending (done by every caller) and
// keep the first expectedSetCap IDs. See RESULTS.md for the rationale.
const expectedSetCap = 20

// capExpected truncates a pre-sorted expected-set slice to at most
// expectedSetCap entries. The truncation is deterministic: because callers
// sort ascending before dedup, the same seed produces the same kept slice.
// Micro-recall is unaffected (both numerator and denominator shrink together
// for capped queries); the per-query pass-rate metric becomes honest again.
func capExpected(in []string) []string {
	if len(in) <= expectedSetCap {
		return in
	}
	return in[:expectedSetCap]
}

// ---------------------------------------------------------------------------
// Driver
// ---------------------------------------------------------------------------

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "generate:", err)
		os.Exit(1)
	}
}

func run() error {
	rng := rand.New(rand.NewSource(seed))
	state := newGenState(rng)

	// Build an ordered plan of artifact kinds so we can interleave them
	// while respecting the distribution. We need supersedes + contradictions
	// to come AFTER we have some role_at facts on the books, so we
	// front-load single-entity and multi-entity artifacts.
	plan := make([]string, 0, totalArtifacts)
	// Phase 1: single + multi mixed 3:1 for the first 80 artifacts so state
	// fills up before we start superseding.
	for i := 0; i < 80; i++ {
		if i%4 == 0 {
			plan = append(plan, "multi")
		} else {
			plan = append(plan, "single")
		}
	}
	// Remaining quotas.
	remaining := map[string]int{
		"single":        distribution.SingleEntity - 60, // 60 already consumed in phase 1
		"multi":         distribution.MultiEntity - 20,  // 20 already consumed in phase 1
		"supersede":     distribution.Superseding,
		"contradiction": distribution.Contradiction,
		"noise":         distribution.Noise,
	}
	// Deterministic interleave: cycle through kinds by weight.
	kindOrder := []string{"single", "multi", "supersede", "contradiction", "noise"}
	for sumRemaining(remaining) > 0 {
		for _, k := range kindOrder {
			if remaining[k] > 0 {
				plan = append(plan, k)
				remaining[k]--
			}
		}
	}
	if len(plan) != totalArtifacts {
		return fmt.Errorf("plan length %d != totalArtifacts %d", len(plan), totalArtifacts)
	}

	// Generate artifacts.
	startTime, err := time.Parse(time.RFC3339, corpusStartDate)
	if err != nil {
		return fmt.Errorf("parse start date: %w", err)
	}

	artifacts := make([]artifact, 0, totalArtifacts)
	occurred := startTime
	for i, kind := range plan {
		var a artifact
		switch kind {
		case "single":
			a = buildSingleEntityArtifact(state, i, occurred)
		case "multi":
			a = buildMultiEntityArtifact(state, i, occurred)
		case "supersede":
			a = buildSupersedingArtifact(state, i, occurred)
		case "contradiction":
			a = buildContradictionArtifact(state, i, occurred)
		case "noise":
			a = buildNoiseArtifact(state, i, occurred)
		default:
			return fmt.Errorf("unknown kind %q", kind)
		}
		artifacts = append(artifacts, a)
		occurred = occurred.Add(artifactInterval)
	}

	queries := buildQueries(state)

	// Validate: every fact_id in queries maps to a real fact in the corpus.
	if err := validate(artifacts, queries); err != nil {
		return fmt.Errorf("validate: %w", err)
	}

	// Write files.
	here := benchDir()
	corpusPath := filepath.Join(here, "corpus.jsonl")
	queriesPath := filepath.Join(here, "queries.jsonl")
	if err := writeJSONL(corpusPath, artifacts); err != nil {
		return fmt.Errorf("write corpus: %w", err)
	}
	if err := writeJSONL(queriesPath, queries); err != nil {
		return fmt.Errorf("write queries: %w", err)
	}

	// Summary.
	info, _ := os.Stat(corpusPath)
	var corpusSize int64
	if info != nil {
		corpusSize = info.Size()
	}
	if corpusSize > 2*1024*1024 {
		return fmt.Errorf("corpus.jsonl is %d bytes, exceeds 2 MB budget", corpusSize)
	}
	factCount := 0
	for _, a := range artifacts {
		factCount += len(a.ExpectedFacts)
	}
	fmt.Printf("wrote %d artifacts, %d facts, corpus size %d bytes\n", len(artifacts), factCount, corpusSize)
	fmt.Printf("wrote %d queries\n", len(queries))
	return nil
}

func sumRemaining(m map[string]int) int {
	total := 0
	for _, v := range m {
		total += v
	}
	return total
}

func validate(arts []artifact, qs []query) error {
	seenArt := map[string]bool{}
	seenFact := map[string]bool{}
	for _, a := range arts {
		if seenArt[a.ArtifactID] {
			return fmt.Errorf("duplicate artifact_id %s", a.ArtifactID)
		}
		seenArt[a.ArtifactID] = true
		for _, f := range a.ExpectedFacts {
			if f.FactID == "" {
				return fmt.Errorf("empty fact_id in %s", a.ArtifactID)
			}
			seenFact[f.FactID] = true
		}
	}
	for _, q := range qs {
		for _, fid := range q.ExpectedFactIDs {
			if !seenFact[fid] {
				return fmt.Errorf("query %s references missing fact_id %s", q.QueryID, fid)
			}
		}
	}
	return nil
}

func writeJSONL(path string, rows interface{}) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)

	switch v := rows.(type) {
	case []artifact:
		for _, r := range v {
			if err := enc.Encode(r); err != nil {
				return err
			}
		}
	case []query:
		for _, r := range v {
			if err := enc.Encode(r); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unsupported row type")
	}
	return nil
}

// benchDir returns the directory of this generator file regardless of where
// `go run` is invoked from. We rely on the fact that the generator lives at
// bench/slice-1/ relative to the module root.
func benchDir() string {
	// Start from the current working directory. If CWD already ends in
	// bench/slice-1 (e.g. `go run .`), write there; otherwise resolve the
	// module-relative path.
	wd, err := os.Getwd()
	if err != nil {
		return "bench/slice-1"
	}
	if filepath.Base(wd) == "slice-1" && filepath.Base(filepath.Dir(wd)) == "bench" {
		return wd
	}
	candidate := filepath.Join(wd, "bench", "slice-1")
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	// Last resort — crawl up until we find go.mod, then append bench/slice-1.
	dir := wd
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Join(dir, "bench", "slice-1")
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "bench/slice-1"
}
