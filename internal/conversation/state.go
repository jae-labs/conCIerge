package conversation

import (
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// nonceSeq guarantees unique nonces even when two flows start within the same nanosecond.
var nonceSeq atomic.Int64

// Phase represents the current step in the conversation flow.
type Phase int

const (
	PhaseIdle Phase = iota
	PhaseCategorySelected
	PhaseResourceSelected
	PhaseActionSelected
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
	Label string // display label rendered in the locked replacement
}

// State tracks one flow per assistant thread.
type State struct {
	Phase         Phase
	Category      string
	ResourceType  string
	ActionType    string
	ChannelID     string
	ThreadTS      string // flow key
	UserID        string
	Nonce         string // rejects stale callbacks
	TargetRepo    string // chosen map_entry key, or composite key for membership
	Justification string

	// Dynamic, schema-driven payload.
	DynamicConfig       map[string]any
	DynamicKeys         map[string][]string
	DynamicFileContent  []byte
	DynamicResourceKeys []string

	Messages []TrackedMessage
}

func (s *State) TrackMessage(ts string, kind MessageKind, label string) {
	s.Messages = append(s.Messages, TrackedMessage{TS: ts, Kind: kind, Label: label})
}

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
	states map[string]*State
}

func NewStore() *Store {
	return &Store{states: make(map[string]*State)}
}

func (s *Store) Get(threadTS string) *State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.states[threadTS]
}

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

func (s *Store) Delete(threadTS string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.states, threadTS)
}
