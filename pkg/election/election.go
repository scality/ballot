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
	defaultReinitResignTimeout   = 5 * time.Second
)

type zkConnectFn func() (zkClient, error)

type ZooKeeperElection struct {
	candidateID           string
	log                   *logrus.Entry
	basePath              string
	proposalNodePath      string
	proposalTime          time.Time
	sessionTimeout        time.Duration
	maxRandomWaitDuration time.Duration
	heartbeatInterval     time.Duration
	zk                    zkClient
	leaderResignChan      chan struct{}
	mu                    sync.Mutex
	connectZK             zkConnectFn
}

func NewZooKeeperElection(servers []string, electionPath string, candidateID string, sessionTimeout time.Duration, debug bool, l *logrus.Entry) (*ZooKeeperElection, error) {
	zlog := l.WithField("name", "election-zk-client")

	connect := func() (zkClient, error) {
		zk, err := connectZkClient(servers, sessionTimeout, debug, zlog)
		if err != nil {
			return nil, fmt.Errorf("zookeeper connection: %w", err)
		}

		return zk, nil
	}

	return newElectionWithZkConnectFn(connect, electionPath, candidateID, sessionTimeout, zlog)
}

func newElectionWithZkConnectFn(connect zkConnectFn, electionPath string, candidateID string, sessionTimeout time.Duration, l *logrus.Entry) (*ZooKeeperElection, error) {
	client, err := connect()
	if err != nil {
		return nil, err
	}

	return &ZooKeeperElection{
		zk:                    client,
		connectZK:             connect,
		basePath:              electionPath,
		candidateID:           candidateID,
		sessionTimeout:        sessionTimeout,
		maxRandomWaitDuration: defaultMaxRandomWaitDuration,
		heartbeatInterval:     defaultHeartbeatInterval,
		log:                   l,
	}, nil
}

func (e *ZooKeeperElection) getCandidateInfoUnlocked(proposalTime time.Time) CandidateInfo {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "(unknown)"
	}

	return CandidateInfo{
		CandidateID:    e.candidateID,
		Hostname:       hostname,
		PID:            fmt.Sprint(os.Getpid()),
		ProposalTime:   proposalTime.String(),
		ProposalNode:   e.proposalNodePath,
		SessionTimeout: e.sessionTimeout.String(),
	}
}

func (e *ZooKeeperElection) serializeCandidateInfo(proposalTime time.Time) []byte {
	e.lock("serializeCandidateInfo")
	ci := e.getCandidateInfoUnlocked(proposalTime)
	e.unlock("serializeCandidateInfo")

	data, err := json.Marshal(ci)
	if err != nil {
		e.log.Warn("could not serialize candidate info", err)

		return []byte(fmt.Sprintf(`{"candidateId": "%s"}`, e.candidateID))
	}

	return data
}

func (e *ZooKeeperElection) serializeLeaderInfoUnlocked() []byte {
	leaderInfo := LeaderInfo{
		CandidateInfo: e.getCandidateInfoUnlocked(e.proposalTime),
		LastSeenTime:  time.Now().String(),
	}

	data, err := json.Marshal(leaderInfo)
	if err != nil {
		e.log.Warn("could not serialize leader info", err)

		return []byte(fmt.Sprintf(`{"proposalNode": "%s"}`, e.proposalNodePath))
	}

	return data
}

func (e *ZooKeeperElection) createProposalNode() error {
	proposalTime := time.Now()

	ownPath, err := e.zk.CreateNodeSequenceEphemeral(e.basePath+"/proposal-", e.serializeCandidateInfo(proposalTime))
	if err != nil {
		return fmt.Errorf("zk create proposal znode: %w", err)
	}

	e.lock("createProposalNode")
	defer e.unlock("createProposalNode")

	e.log.Debugf("own proposal zk node %s", ownPath)
	e.proposalNodePath = ownPath
	e.proposalTime = proposalTime

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

	var expectVersion int32

	for {
		prev, err := e.getPreviousProposal()
		if err != nil {
			return fmt.Errorf("leader election: %w", err)
		}

		hasLowestProposal := prev == ""

		if hasLowestProposal {
			e.log.Debug("got the lowest proposal")

			hasActiveLeader, lastSeenVersion := e.hasActiveLeader()
			if hasActiveLeader {
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

			expectVersion = lastSeenVersion

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

	e.log.WithField("expectNodeVersion", expectVersion).Info("am leader, trying to claim")

	expectVersion2, err := e.publishLeaderRole(expectVersion)
	if err != nil {
		return fmt.Errorf("unable to claim leadership (expected version %d, found %d): %w",
			expectVersion, expectVersion2, err)
	}

	e.log.Info("am leader, claim ok, continuing")

	e.publishLeaderRoleBackgroundTask(ctx, expectVersion2)

	return nil
}

func convertMsEpochToTime(ms int64) time.Time {
	return time.Unix(ms/1000, (ms%1000)*1_000_000)
}

func (e *ZooKeeperElection) hasActiveLeader() (bool, int32) {
	e.log.Info("checking if there is an active leader")

	data, stat, err := e.zk.Get(e.basePath)
	if err != nil {
		e.log.Errorf("could not get leader node %s, assuming another leader is active: %v", e.basePath, err)

		// if we can't reach zk, assume there's already a
		// leader, as we don't want to risk a split brain
		// election
		return true, 0
	}

	version := stat.Version
	mtime := convertMsEpochToTime(stat.Mtime)
	since := time.Since(mtime)
	hasActiveLeader := since < e.sessionTimeout && len(data) > 0

	e.log.Debugf("active leader info: hasActiveLeader=%v descr=%s since=%s threshold=%s nodeversion=%d",
		hasActiveLeader, string(data), since.String(), e.sessionTimeout.String(), version)

	if hasActiveLeader {
		e.log.Infof("active leader '%s' was seen %s ago", string(data), since.String())
	}

	return hasActiveLeader, version
}

func (e *ZooKeeperElection) clearLeaderRole(expectVersion int32) {
	e.lock("clearLeaderRole")
	defer e.unlock("clearLeaderRole")

	_, err := e.zk.Set(e.basePath, []byte{}, expectVersion)
	if err != nil && !errors.Is(err, zk.ErrConnectionClosed) {
		// don't log connections closed errors, as
		// connections closed mean that the proposal node will
		// be automatically cleaned up and `/basePath` will have
		// a stale timestamp
		e.log.Errorf("could not clear leader role: %v", err)
	}
}

func (e *ZooKeeperElection) publishLeaderRole(expectVersion int32) (int32, error) {
	e.lock("publishLeaderRole")
	defer e.unlock("publishLeaderRole")

	if e.proposalNodePath == "" {
		e.log.Debug("in the process of resigning, not publishing")

		return 0, nil
	}

	e.log.Debug("publishing leader state")

	foundVersion, err := e.zk.Set(e.basePath, e.serializeLeaderInfoUnlocked(), expectVersion)
	if err != nil {
		return foundVersion, fmt.Errorf("publish leader role: %w", err)
	}

	e.log.Debugf("published leader state expectv=%d newv=%d", expectVersion, foundVersion)

	return foundVersion, nil
}

func (e *ZooKeeperElection) publishLeaderRoleBackgroundTask(ctx context.Context, expectVersion int32) {
	e.lock("claimLeaderRole")
	defer e.unlock("claimLeaderRole")

	e.leaderResignChan = make(chan struct{})
	localExpectVersion := expectVersion

	go func() {
		t := time.NewTicker(e.sessionTimeout / 2)
		defer t.Stop()
		defer e.clearLeaderRole(localExpectVersion)

		for {
			select {
			case <-ctx.Done():
				return

			case <-e.leaderResignChan:
				return

			case <-t.C:
				foundVersion, err := e.publishLeaderRole(localExpectVersion)
				if err != nil {
					e.log.Errorf("leader claim loop: %v", err)
				}

				localExpectVersion = foundVersion
			}
		}
	}()
}

func (e *ZooKeeperElection) Resign(ctx context.Context) error {
	e.lock("Resign")
	close(e.leaderResignChan)
	err := e.deleteProposalNodeUnlocked()
	e.proposalNodePath = ""
	e.proposalTime = time.Time{}
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

func (e *ZooKeeperElection) Reinit() error {
	e.zk.Disconnect()

	client, err := e.connectZK()
	if err != nil {
		return err
	}

	e.zk = client

	return nil
}
