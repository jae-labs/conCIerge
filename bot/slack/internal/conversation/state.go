package conversation

import (
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// nonceSeq prevents nonce collisions when two flows are created within the same nanosecond.
var nonceSeq atomic.Int64

// Phase represents the current step in the conversation flow.
type Phase int

const (
	PhaseIdle Phase = iota
	PhaseCategorySelected
	PhaseResourceSelected
	PhaseActionSelected
	PhaseWizardStep1
	PhaseWizardStep2
	PhaseWizardStep3
	PhaseConfirming
	PhaseCreatingPR
)

// MessageKind identifies the type of tracked interactive message.
type MessageKind string

const (
	MsgWelcome      MessageKind = "welcome"
	MsgCategory     MessageKind = "category"
	MsgResource     MessageKind = "resource"
	MsgAction       MessageKind = "action"
	MsgConfirmation MessageKind = "confirmation"
	MsgProgress     MessageKind = "progress"
)

// TrackedMessage records a bot message that contains interactive elements.
type TrackedMessage struct {
	TS    string
	Kind  MessageKind
	Label string // display label for locked state
}

// State tracks a single flow's position in the conversation wizard.
// Each assistant thread has at most one active flow.
type State struct {
	Phase          Phase
	Category       string // "github", "cloudflare", "doppler"
	ResourceType   string // "repo", "user_management", "settings"
	ChannelID      string // DM channel
	ThreadTS       string // assistant thread timestamp (flow key)
	UserID         string
	Nonce          string // unique per flow, prevents stale callback execution
	ActionType     string // "add", "delete", "settings"
	TargetRepo     string // repo name for delete/settings flows
	TargetZone     string // dns zone for cloudflare flows
	TargetRecord   string // dns record key for update/delete flows
	Justification  string
	Priority       string // "low", "medium", "high"
	AvailableTeams []string // teams fetched from terraform
	RepoConfig     RepoConfig
	DnsConfig      DnsConfig
	OrgConfig        OrgConfig
	TeamMemberConfig TeamMemberConfig
	AvailableMembers []string // org member usernames fetched from terraform
	Messages         []TrackedMessage
}

// TrackMessage appends a tracked message to the state.
func (s *State) TrackMessage(ts string, kind MessageKind, label string) {
	s.Messages = append(s.Messages, TrackedMessage{TS: ts, Kind: kind, Label: label})
}

// HasMessage checks if a message TS belongs to this flow.
func (s *State) HasMessage(ts string) bool {
	for _, m := range s.Messages {
		if m.TS == ts {
			return true
		}
	}
	return false
}

// Store is a concurrency-safe in-memory store keyed by thread timestamp.
type Store struct {
	mu     sync.Mutex
	states map[string]*State // key = threadTS
}

func NewStore() *Store {
	return &Store{states: make(map[string]*State)}
}

// Get returns the state for a thread, or nil if not found.
func (s *Store) Get(threadTS string) *State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.states[threadTS]
}

// Create starts a new flow for a thread.
func (s *Store) Create(threadTS, channelID, userID string) *State {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := &State{
		Phase:     PhaseIdle,
		ChannelID: channelID,
		ThreadTS:  threadTS,
		UserID:    userID,
		Nonce:     strconv.FormatInt(time.Now().UnixNano(), 36) + strconv.FormatInt(nonceSeq.Add(1), 36),
	}
	s.states[threadTS] = st
	return st
}

// Delete removes a flow.
func (s *Store) Delete(threadTS string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.states, threadTS)
}
