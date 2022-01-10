// Copyright 2021-2022 Scality, Inc
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package election

import "context"

type Election interface {
	BecomeLeader(ctx context.Context) error
	Resign(ctx context.Context) error
	Reinit() error
}

type CandidateInfo struct {
	CandidateID    string `json:"candidateId,omitempty" yaml:"id,omitempty"`
	Hostname       string `json:"hostname,omitempty" yaml:"host,omitempty"`
	PID            string `json:"pid,omitempty" yaml:"pid,omitempty"`
	ProposalTime   string `json:"proposalTime,omitempty" yaml:"proposalTime,omitempty"`
	ProposalNode   string `json:"proposalNode,omitempty" yaml:"proposalNode,omitempty"`
	SessionTimeout string `json:"sessionTimeout,omitempty" yaml:"sessionTimeout,omitempty"`

	Notes []string `json:"notes,omitempty" yaml:"notes,omitempty"`
}

type LeaderInfo struct {
	CandidateInfo `json:",inline" yaml:",inline"`
	LastSeenTime  string `json:"lastSeenTime,omitempty" yaml:"lastSeenTime,omitempty"`
}
