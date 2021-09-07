// Copyright 2021 Scality, Inc
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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path"
	"sort"
	"strings"
	"sync"

	"time"

	"github.com/go-zookeeper/zk"
	logrus "github.com/sirupsen/logrus"
)

var (
	errMustReelect     = errors.New("proposals stale, must reelect")
	errNodeWatchClosed = errors.New("watch channel on znode was closed")
)

const (
	defaultMaxRandomWaitDuration = 2 * time.Second
	defaultHeartbeatInterval     = 30 * time.Second
)

type ZooKeeperElection struct {
	candidateID           string
	log                   *logrus.Entry
	basePath              string
	proposalNodePath      string
	sessionTimeout        time.Duration
	maxRandomWaitDuration time.Duration
	heartbeatInterval     time.Duration
	zk                    zkClient
	leaderResignChan      chan struct{}
	mu                    sync.Mutex
}

func NewZooKeeperElection(servers []string, electionPath string, candidateID string, sessionTimeout time.Duration, debug bool, l *logrus.Entry) (*ZooKeeperElection, error) {
	zlog := l.WithField("name", "election-zk-client")

	zk, err := connectZkClient(servers, sessionTimeout, debug, zlog)
	if err != nil {
		return nil, fmt.Errorf("zookeeper connection: %w", err)
	}

	return newElectionWithZkClient(zk, electionPath, candidateID, sessionTimeout, zlog), nil
}

func newElectionWithZkClient(zk zkClient, electionPath string, candidateID string, sessionTimeout time.Duration, l *logrus.Entry) *ZooKeeperElection {
	return &ZooKeeperElection{
		zk:                    zk,
		basePath:              electionPath,
		candidateID:           candidateID,
		sessionTimeout:        sessionTimeout,
		maxRandomWaitDuration: defaultMaxRandomWaitDuration,
		heartbeatInterval:     defaultHeartbeatInterval,
		log:                   l,
	}
}

func (e *ZooKeeperElection) serializeCandidateInfo() []byte {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "(unknown)"
	}

	candidateInfo := map[string]string{
		"candidateId":    e.candidateID,
		"hostname":       hostname,
		"pid":            fmt.Sprint(os.Getpid()),
		"proposalTime":   time.Now().String(),
		"sessionTimeout": e.sessionTimeout.String(),
	}

	data, err := json.Marshal(candidateInfo)
	if err != nil {
		e.log.Warn("could not serialize candidate info", err)

		return []byte(fmt.Sprintf(`{"candidateId": "%s"}`, e.candidateID))
	}

	return data
}

func (e *ZooKeeperElection) serializeLeaderInfoUnlocked() []byte {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "(unknown)"
	}

	leaderInfo := map[string]string{
		"proposalNode":   e.proposalNodePath,
		"candidateId":    e.candidateID,
		"hostname":       hostname,
		"pid":            fmt.Sprint(os.Getpid()),
		"lastSeenTime":   time.Now().String(),
		"sessionTimeout": e.sessionTimeout.String(),
	}

	data, err := json.Marshal(leaderInfo)
	if err != nil {
		e.log.Warn("could not serialize leader info", err)

		return []byte(fmt.Sprintf(`{"proposalNode": "%s"}`, e.proposalNodePath))
	}

	return data
}

func (e *ZooKeeperElection) createProposalNode() error {
	ownPath, err := e.zk.CreateNodeSequenceEphemeral(e.basePath+"/proposal-", e.serializeCandidateInfo())
	if err != nil {
		return fmt.Errorf("zk create proposal znode: %w", err)
	}

	e.lock("createProposalNode")
	defer e.unlock("createProposalNode")

	e.log.Debugf("own proposal zk node %s", ownPath)
	e.proposalNodePath = ownPath

	return nil
}

func (e *ZooKeeperElection) deleteProposalNodeUnlocked() error {
	err := e.zk.Delete(e.proposalNodePath)
	if err != nil {
		if errors.Is(err, zk.ErrNoNode) {
			e.log.Debugf("own proposal node %s already gone while trying to delete", e.proposalNodePath)

			return nil
		}

		return fmt.Errorf("delete own proposal node: %s: %w", e.proposalNodePath, err)
	}

	e.log.Debugf("successfully deleted own proposal node %s", e.proposalNodePath)

	return nil
}

func (e *ZooKeeperElection) createElectionPath() error {
	currentPath := "/"

	for _, part := range strings.Split(e.basePath, "/") {
		currentPath = path.Join(currentPath, part)

		err := e.zk.CreateNode(currentPath)
		if err != nil && !errors.Is(err, zk.ErrNodeExists) {
			return fmt.Errorf("create base election path %s: %w", currentPath, err)
		}
	}

	return nil
}

func (e *ZooKeeperElection) getPreviousProposal() (string, error) {
	e.log.Debugf("listing nodes under %s", e.basePath)

	nodes, err := e.zk.ListChildren(e.basePath)
	if err != nil {
		return "", fmt.Errorf("get all proposals under %s: %w", e.basePath, err)
	}

	sort.Strings(nodes)

	previousNode := ""
	rank := 1
	foundOwn := false

	e.lock("getPreviousProposal")
	defer e.unlock("getPreviousProposal")

	for _, n := range nodes {
		nodePath := path.Join(e.basePath, n)
		if nodePath == e.proposalNodePath {
			foundOwn = true

			break
		}

		previousNode = nodePath
		rank++
	}

	if e.proposalNodePath != "" && !foundOwn {
		e.log.Warnf("cannot get previous proposal, own proposal '%s' disappeared", e.proposalNodePath)

		return "", fmt.Errorf("own proposal node '%s' disappeared: %w", e.proposalNodePath, errMustReelect)
	}

	e.log.Infof("am number %d in line to become leader, proposal %s", rank, e.proposalNodePath)

	return previousNode, nil
}

func (e *ZooKeeperElection) waitForNodeChange(ctx context.Context, node string) error {
	watch, err := e.zk.Watch(node)
	if err != nil {
		return fmt.Errorf("node watch %s: %w", node, err)
	}

	baseWatch, err := e.zk.WatchChildren(e.basePath)
	if err != nil {
		return fmt.Errorf("base path watch %s: %w", e.basePath, err)
	}

	e.log.Debugf("waiting for node %s to disappear", node)

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("node watch %s: %w", node, ctx.Err())

		case ev, ok := <-watch:
			if !ok {
				return fmt.Errorf("node watch on %s was closed: %w", node, errNodeWatchClosed)
			}

			if ev.Type == zk.EventNodeDeleted {
				e.log.Debugf("%s disappeared", node)

				return nil
			}

			e.log.Debugf("ignoring event type %s on node %s", ev.Type, node)

		case ev, ok := <-baseWatch:
			if !ok {
				return fmt.Errorf("node watch on %s was closed: %w", e.basePath, errNodeWatchClosed)
			}

			if ev.Type == zk.EventNodeChildrenChanged {
				e.log.Debugf("%s children changed", e.basePath)

				return nil
			}

			e.log.Debugf("ignoring event type %s on path %s", ev.Type, e.basePath)

		case ev, ok := <-e.zk.Events():
			// Dequeue events from the channel even if we don't use them
			if !ok {
				return fmt.Errorf("zookeeper connection events chan closed")
			}

			e.log.Debugf("connection event %v", ev)

		case <-time.After(e.heartbeatInterval):
			return nil
		}
	}
}

func (e *ZooKeeperElection) randomWait() {
	t := time.Duration(rand.Int63n(int64(e.maxRandomWaitDuration)))
	e.log.Debugf("waiting for %v", t)

	<-time.After(t)
}

const waitSessionSteps = 5

func (e *ZooKeeperElection) waitForZooKeeperSession(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, e.sessionTimeout)
	defer cancel()

	for {
		if e.zk.HasSession() {
			e.log.Debug("zk client has a working session")

			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()

		case <-time.After(e.sessionTimeout / time.Duration(waitSessionSteps)):
			continue
		}
	}
}

func (e *ZooKeeperElection) BecomeLeader(ctx context.Context) error {
	e.randomWait()

	err := e.waitForZooKeeperSession(ctx)
	if err != nil {
		return fmt.Errorf("leader election: %w", err)
	}

	err = e.createElectionPath()
	if err != nil {
		return fmt.Errorf("leader election: %w", err)
	}

	err = e.createProposalNode()
	if err != nil {
		return fmt.Errorf("leader election: %w", err)
	}

	for {
		prev, err := e.getPreviousProposal()
		if err != nil {
			return fmt.Errorf("leader election: %w", err)
		}

		hasLowestProposal := prev == ""

		if hasLowestProposal {
			if e.hasActiveLeader() {
				// It is possible to have the lowest proposal and not be the
				// actual leader, if all the previous ephemeral nodes have been
				// auto-expired and there's still a leader running the process.
				// This can happen when communication between ballot processes
				// and the zookeeper ensemble has been cut for longer than the
				// session timeout.
				sleep := e.sessionTimeout / 2
				e.log.Infof("election is being held while there's an active leader, retrying in %s", sleep.String())
				<-time.After(sleep)

				continue
			}

			e.log.Info("am leader, continuing")

			break
		} else {
			e.log.Info("waiting for leader to resign")

			err = e.waitForNodeChange(ctx, prev)
			if err != nil {
				return fmt.Errorf("leader election follower wait: %w", err)
			}

			e.log.Debug("redoing election")
		}
	}

	e.claimLeaderRole(ctx)

	return nil
}

func convertMsEpochToTime(ms int64) time.Time {
	return time.Unix(ms/1000, (ms%1000)*1_000_000)
}

func (e *ZooKeeperElection) hasActiveLeader() bool {
	e.log.Info("checking if there is an active leader")

	data, stat, err := e.zk.Get(e.basePath)
	if err != nil {
		e.log.Errorf("could not get leader node %s, assuming another leader is active: %v", e.basePath, err)

		// if we can't reach zk, assume there's already a
		// leader, as we don't want to risk a split brain
		// election
		return true
	}

	mtime := convertMsEpochToTime(stat.Mtime)
	since := time.Since(mtime)
	hasActiveLeader := since < e.sessionTimeout && len(data) > 0

	e.log.Debugf("active leader info: hasActiveLeader=%v descr=%s since=%s threshold=%s",
		hasActiveLeader, string(data), since.String(), e.sessionTimeout.String())

	if hasActiveLeader {
		e.log.Infof("active leader '%s' was seen %s ago", string(data), since.String())
	}

	return hasActiveLeader
}

func (e *ZooKeeperElection) clearLeaderRole() {
	e.lock("clearLeaderRole")
	defer e.unlock("clearLeaderRole")

	err := e.zk.Set(e.basePath, []byte{}, -1)
	if err != nil {
		e.log.Errorf("could not clear leader role: %v", err)
	}
}

func (e *ZooKeeperElection) publishLeaderRole() {
	e.lock("publishLeaderRole")
	defer e.unlock("publishLeaderRole")

	if e.proposalNodePath == "" {
		e.log.Debug("in the process of resigning, not publishing")

		return
	}

	e.log.Debug("publishing leader state")

	err := e.zk.Set(e.basePath, e.serializeLeaderInfoUnlocked(), -1)
	if err != nil {
		e.log.Errorf("could not publish leader role: %v", err)

		return
	}
}

func (e *ZooKeeperElection) claimLeaderRole(ctx context.Context) {
	e.lock("claimLeaderRole")
	defer e.unlock("claimLeaderRole")

	e.leaderResignChan = make(chan struct{})

	go func() {
		t := time.NewTicker(e.sessionTimeout / 2)
		defer t.Stop()
		defer e.clearLeaderRole()

		for {
			select {
			case <-ctx.Done():
				return

			case <-e.leaderResignChan:
				return

			case <-t.C:
				e.publishLeaderRole()
			}
		}
	}()
}

func (e *ZooKeeperElection) Resign(ctx context.Context) error {
	e.lock("Resign")
	close(e.leaderResignChan)
	err := e.deleteProposalNodeUnlocked()
	e.proposalNodePath = ""
	e.unlock("Resign")

	if err != nil {
		return fmt.Errorf("resign: %w", err)
	}

	e.log.Info("successfully resigned")

	return nil
}

func (e *ZooKeeperElection) unlock(name string) {
	e.mu.Unlock()
	e.log.Debugf("unlock %s ok", name)
}

func (e *ZooKeeperElection) lock(name string) {
	e.log.Debugf("lock %s...", name)
	e.mu.Lock()
	e.log.Debugf("lock %s ok", name)
}
