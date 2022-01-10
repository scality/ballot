// Copyright 2022 Scality, Inc
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

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	logrus "github.com/sirupsen/logrus"
)

type ZooKeeperElectionClient struct {
	zk       zkClient
	basePath string
	debug    bool
	log      *logrus.Entry
}

func NewZooKeeperElectionClient(servers []string, electionPath string, sessionTimeout time.Duration, debug bool, l *logrus.Entry) (*ZooKeeperElectionClient, error) {
	zlog := l.WithField("name", "election-client-zk-client")

	zk, err := connectZkClient(servers, sessionTimeout, debug, zlog)
	if err != nil {
		return nil, err
	}

	ret := &ZooKeeperElectionClient{
		zk:       zk,
		basePath: electionPath,
		debug:    debug,
		log:      l,
	}

	return ret, nil
}

func (ec ZooKeeperElectionClient) parseTime(s string) time.Time {
	t, err := time.Parse("2006-01-02 15:04:05.999999999 -0700 MST", strings.Split(s, " m=")[0])
	if err != nil {
		ec.log.Warn(err)

		return time.Time{}
	}

	return t
}

func (ec ZooKeeperElectionClient) getDurationSince(s string) time.Duration {
	return time.Now().Sub(ec.parseTime(s))
}

func (ec ZooKeeperElectionClient) Inspect() (*ZooKeeperElectionStatus, error) {
	hasLeaderInfo := true

	data, _, err := ec.zk.Get(ec.basePath)
	if err != nil {
		ec.log.Warnf("get election node: %v", err)

		hasLeaderInfo = false
	}

	leaderInfo := LeaderInfo{}

	if hasLeaderInfo {
		err = json.Unmarshal(data, &leaderInfo)
		if err != nil {
			ec.log.Warnf("parse leader node data: %v", err)

			hasLeaderInfo = false
		}
	}

	proposalNodes, err := ec.zk.ListChildren(ec.basePath)
	if err != nil {
		return nil, fmt.Errorf("list candidates: %w", err)
	}

	foundLeaderInProposalNodes := false

	for _, n := range proposalNodes {
		if ec.basePath+"/"+n == leaderInfo.ProposalNode {
			foundLeaderInProposalNodes = true

			break
		}
	}

	leaderIsOld := false

	if hasLeaderInfo {
		leaderInfo.Notes = []string{"is leader"}

		sessionTimeout, _ := time.ParseDuration(leaderInfo.SessionTimeout)

		lastSeenDuration := ec.getDurationSince(leaderInfo.LastSeenTime)
		leaderInfo.Notes = append(leaderInfo.Notes, fmt.Sprintf("last heartbeat %v ago", lastSeenDuration))

		if lastSeenDuration > sessionTimeout/2 {
			leaderIsOld = true
		}

		if leaderIsOld {
			leaderInfo.Notes = append(leaderInfo.Notes, "last heartbeat is older than the expected publish interval")
		}

		if !foundLeaderInProposalNodes {
			leaderInfo.Notes = append(leaderInfo.Notes, "possibly resigning")
			leaderInfo.Notes = append(leaderInfo.Notes, "election may be in progress")
		}
	}

	sort.Strings(proposalNodes)

	candidates := make([]CandidateInfo, 0, len(proposalNodes))

	for _, node := range proposalNodes {
		var candidateInfo CandidateInfo
		var notes []string

		nodePath := ec.basePath + "/" + node
		isLeader := nodePath == leaderInfo.ProposalNode
		isLowestNode := len(proposalNodes) > 0 && node == proposalNodes[0]

		if isLowestNode {
			if isLeader {
				notes = append(notes, "this proposal node claimed leadership")
			} else {
				notes = append(notes, "should be leader but another candidate is claiming the role")
				notes = append(notes, "possibly waiting for previous leader resignation")
			}
		}

		data, stat, err := ec.zk.Get(nodePath)
		if err != nil {
			ec.log.Warnf("get candidate %s: %v", node, err)

			notes = append(notes, "gone")

			candidates = append(candidates, CandidateInfo{
				CandidateID:  node,
				Notes:        notes,
				ProposalNode: nodePath,
			})

			continue
		}

		err = json.Unmarshal(data, &candidateInfo)
		if err != nil {
			ec.log.Warnf("unmarshal candidate %s: %v", node, err)

			notes = append(notes, "unparseable")

			candidates = append(candidates, CandidateInfo{
				CandidateID:  node,
				Notes:        notes,
				ProposalNode: nodePath,
			})

			continue
		}

		if candidateInfo.CandidateID == leaderInfo.CandidateID {
			notes = append(notes, "this candidate id claimed leadership")

			if leaderIsOld {
				notes = append(notes, "possibly resigning")
			}
		}

		if candidateInfo.ProposalNode == "" {
			candidateInfo.ProposalNode = nodePath
		}

		notes = append(notes, fmt.Sprintf("proposal node created %v ago", ec.getDurationSince(candidateInfo.ProposalTime)))

		drift := convertMsEpochToTime(stat.Mtime).Sub(ec.parseTime(candidateInfo.ProposalTime))
		if drift > time.Millisecond || drift < -time.Millisecond {
			notes = append(notes, fmt.Sprintf("time drift of %v between proposal and zookeeper write", drift))
		}

		candidateInfo.Notes = notes

		candidates = append(candidates, candidateInfo)
	}

	ret := &ZooKeeperElectionStatus{
		Leader:     leaderInfo,
		Candidates: candidates,
	}

	return ret, nil
}
