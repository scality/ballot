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
	"time"

	"github.com/go-zookeeper/zk"
	logrus "github.com/sirupsen/logrus"
)

var (
	errNodeWatchClosed = errors.New("watch channel on znode was closed")

	defaultMaxRandomWaitDuration = 2 * time.Second
	defaultHeartbeatInterval     = 60 * time.Second
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

func (e *ZooKeeperElection) createProposalNode() error {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "(unknown)"
	}

	candidateInfo := map[string]string{
		"candidateId":  e.candidateID,
		"hostname":     hostname,
		"pid":          fmt.Sprint(os.Getpid()),
		"proposalTime": time.Now().String(),
		// TODO add session timeout
	}

	data, err := json.Marshal(candidateInfo)
	if err != nil {
		data = []byte(fmt.Sprintf(`{"candidateId": "%s"}`, e.candidateID))
	}

	ownPath, err := e.zk.CreateNodeSequenceEphemeral(e.basePath+"/proposal-", data)
	if err != nil {
		return fmt.Errorf("zk create proposal znode: %w", err)
	}

	e.log.Debugf("own proposal zk node %s", ownPath)
	e.proposalNodePath = ownPath

	return nil
}

func (e *ZooKeeperElection) deleteProposalNode() error {
	err := e.zk.Delete(e.proposalNodePath)
	if err != nil {
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

	for _, n := range nodes {
		nodePath := path.Join(e.basePath, n)
		if nodePath == e.proposalNodePath {
			break
		}

		previousNode = nodePath
		rank++
	}

	e.log.Infof("am number %d in line to become leader", rank)

	return previousNode, nil
}

func (e *ZooKeeperElection) waitForNodeDeletion(ctx context.Context, node string) error {
	watch, err := e.zk.Watch(node)
	if err != nil {
		return fmt.Errorf("node watch %s: %w", node, err)
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

		isLeader := prev == ""

		if isLeader {
			e.log.Debug("am leader, continuing")

			break
		} else {
			e.log.Info("waiting for leader to resign")

			err = e.waitForNodeDeletion(ctx, prev)
			if err != nil {
				return fmt.Errorf("leader election follower wait: %w", err)
			}

			e.log.Debug("redoing election")
		}
	}

	return nil
}

func (e *ZooKeeperElection) Resign(ctx context.Context) error {
	err := e.deleteProposalNode()
	e.proposalNodePath = ""

	if err != nil {
		return fmt.Errorf("resign: %w", err)
	}

	e.log.Info("successfully resigned")

	return nil
}
