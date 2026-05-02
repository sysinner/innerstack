// Copyright 2015 Eryx <evorui at gmail dot com>, All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package stateflow provides a finite state machine for managing resource
// lifecycle operations. It implements state transitions triggered by events
// with associated commands.
package stateflow

import (
	"fmt"
	"log/slog"
	"sync"

	"github.com/sysinner/incore/v2/inapi"
)

// AppStateCommand defines the function signature for app state operations
type AppStateCommand func(replica *inapi.AppReplicaInstance) (string, error)

// AppStateEntry represents a state transition with its associated command
type AppStateEntry struct {
	State   string          // target state after transition
	Command AppStateCommand // command to execute during transition
}

// AppStateWorkflow manages state transitions for app operations
// It implements a finite state machine where transitions are triggered by events
type AppStateWorkflow struct {
	mu sync.RWMutex

	// Transitions maps: current state -> event -> next state command
	Transitions map[string]map[string]*AppStateEntry
}

// NextCommand returns the command for a given state and event transition
// Returns false if no transition is defined for the state/event pair
func (it *AppStateWorkflow) NextCommand(state, event string) (*AppStateEntry, bool) {
	it.mu.RLock()
	defer it.mu.RUnlock()

	if it.Transitions == nil || it.Transitions[state] == nil {
		return nil, false
	}

	cmd, ok := it.Transitions[state][event]
	if !ok {
		slog.Warn(fmt.Sprintf("No transition defined for state/event %s -> %s", state, event))
		return nil, false
	}

	slog.Debug(fmt.Sprintf("Hit workflow transition %s -> %s -> %s", state, event, cmd.State))

	return cmd, true
}

// Register adds a new state transition to the workflow
// currentState: the state from which the transition applies
// event: the event that triggers the transition
// nextState: the target state after the transition
// nextCommand: the command to execute during the transition
func (it *AppStateWorkflow) Register(
	currentState, event, nextState string,
	nextCommand AppStateCommand,
) {
	it.mu.Lock()
	defer it.mu.Unlock()

	if it.Transitions == nil {
		it.Transitions = make(map[string]map[string]*AppStateEntry)
	}

	if it.Transitions[currentState] == nil {
		it.Transitions[currentState] = make(map[string]*AppStateEntry)
	}

	slog.Debug(fmt.Sprintf("reg state transition %s -> %s -> %s",
		currentState, event, nextState))

	it.Transitions[currentState][event] = &AppStateEntry{
		State:   nextState,
		Command: nextCommand,
	}
}
