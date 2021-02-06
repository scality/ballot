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
	"bytes"
	"context"
	"errors"
	"strings"
	"time"

	"github.com/go-zookeeper/zk"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/sirupsen/logrus"
)

var errTest = errors.New("testing")

var _ = Describe("Election", func() {
	var ctx context.Context
	var logs *bytes.Buffer

	timeout := 100 * time.Millisecond
	electionJitter := time.Millisecond
	basePath := "/ballot/test/election"
	logger := logrus.New()
	logger.SetLevel(logrus.TraceLevel)
	logger.ReportCaller = true
	l := logrus.NewEntry(logger)

	createElection := func(zk zkClient) *ZooKeeperElection {
		e := newElectionWithZkClient(zk, basePath, "id1", timeout, l)
		e.maxRandomWaitDuration = electionJitter

		return e
	}

	flushTest := func(done Done) {
		close(done)
	}

	BeforeEach(func() {
		ctx = context.Background()
		logs = bytes.NewBuffer(nil)
		logger.Out = logs
	})

	AfterEach(func() {
		t := GinkgoT()
		if t.Failed() && logs.Len() > 0 {
			t.Log("Logs for test failure [", t.Name(), "]:")
			for _, l := range strings.Split(logs.String(), "\n") {
				t.Log(l)
			}
		}
	})

	Describe("BecomeLeader", func() {
		It("should wait until zk is connected", func(done Done) {
			defer flushTest(done)

			zkMock := &mockZkClient{
				returnForHasSession: []bool{false, false, true},
			}

			e := createElection(zkMock)
			err := e.BecomeLeader(ctx)

			Expect(err).NotTo(HaveOccurred())
			Expect(zkMock.calledHasSessionNTimes).To(Equal(3))

			Expect(e.proposalNodePath).To(BeEmpty())
		}, 1)

		It("should interrupt zk session wait if context is canceled", func(done Done) {
			defer flushTest(done)

			ctx, cancel := context.WithCancel(ctx)
			cancel()

			zkMock := &mockZkClient{
				returnForHasSession: []bool{false, false, true},
			}

			e := createElection(zkMock)
			err := e.BecomeLeader(ctx)

			Expect(err).To(MatchError(context.Canceled))
			Expect(zkMock.calledHasSessionNTimes).To(Equal(1))
			Expect(zkMock.calledCreateNodeWith).To(BeEmpty())

			Expect(e.proposalNodePath).To(BeEmpty())
		}, 1)

		It("should create zk base path", func(done Done) {
			defer flushTest(done)

			zk := &mockZkClient{
				returnForHasSession: []bool{true},
			}

			e := createElection(zk)
			err := e.BecomeLeader(ctx)

			Expect(err).NotTo(HaveOccurred())
			Expect(zk.calledCreateNodeWith).To(Equal([]string{"/", "/ballot", "/ballot/test", "/ballot/test/election"}))
		}, 1)

		It("should use pre-created zk base path", func(done Done) {
			defer flushTest(done)

			zkMock := &mockZkClient{
				returnForHasSession:                      []bool{true},
				returnForCreateNode:                      zk.ErrNodeExists,
				returnPathForCreateNodeSequenceEphemeral: basePath + "/p001",
			}

			e := createElection(zkMock)
			err := e.BecomeLeader(ctx)

			Expect(err).NotTo(HaveOccurred())
			Expect(zkMock.calledCreateNodeWith).To(Equal([]string{"/", "/ballot", "/ballot/test", "/ballot/test/election"}))

			Expect(e.proposalNodePath).To(Equal("/ballot/test/election/p001"))
		}, 1)

		It("should bail if unable to create base path", func(done Done) {
			defer flushTest(done)

			zkMock := &mockZkClient{
				returnForHasSession: []bool{true},
				returnForCreateNode: errTest,
			}

			e := createElection(zkMock)
			err := e.BecomeLeader(ctx)

			Expect(err).To(MatchError(errTest))
			Expect(zkMock.calledCreateNodeWith).To(Equal([]string{"/"}))

			Expect(e.proposalNodePath).To(BeEmpty())
		}, 1)

		It("should continue as leader if its proposal is the first", func(done Done) {
			defer flushTest(done)

			zkMock := &mockZkClient{
				returnForHasSession:                      []bool{true},
				returnPathForCreateNodeSequenceEphemeral: basePath + "/p001",
				returnChildrenForListChildren:            [][]string{{"p001", "p002"}},
			}

			e := createElection(zkMock)
			err := e.BecomeLeader(ctx)

			Expect(err).NotTo(HaveOccurred())
			Expect(zkMock.calledListChildrenWith).To(Equal([]string{basePath}))
			Expect(zkMock.calledWatchWith).To(BeEmpty()) // leader won't watch anything

			Expect(e.proposalNodePath).To(Equal("/ballot/test/election/p001"))
		}, 1)

		It("should bail if unable to create proposal node", func(done Done) {
			defer flushTest(done)

			zkMock := &mockZkClient{
				returnForHasSession:                       []bool{true},
				returnErrorForCreateNodeSequenceEphemeral: errTest,
			}

			e := createElection(zkMock)
			err := e.BecomeLeader(ctx)

			Expect(err).To(MatchError(errTest))
			Expect(zkMock.calledListChildrenNTimes).To(BeZero())

			Expect(e.proposalNodePath).To(BeEmpty())
		}, 1)

		It("should wait for previous candidate if not leader", func(done Done) {
			defer flushTest(done)

			becomeLeaderDoneCh := make(chan error)

			zkMock := &mockZkClient{
				returnForHasSession:                      []bool{true},
				returnPathForCreateNodeSequenceEphemeral: basePath + "/p003",
				returnChildrenForListChildren:            [][]string{{"p002", "p003", "p001"}},
				watchingCheckpointCh:                     make(chan struct{}),
				returnChForWatch:                         make(chan zk.Event),
			}

			e := createElection(zkMock)

			go func() {
				defer GinkgoRecover()

				becomeLeaderDoneCh <- e.BecomeLeader(ctx)
			}()

			select {
			case <-zkMock.watchingCheckpointCh:
				Expect(e.proposalNodePath).To(Equal("/ballot/test/election/p003"))

			case <-time.After(timeout):
				Fail("should be watching but is not")
			}

			// errNodeWatchClosed is only returned if the election process is
			// explicitly waiting on the previous-node watch channel
			close(zkMock.returnChForWatch)

			select {
			case err := <-becomeLeaderDoneCh:
				Expect(err).To(MatchError(errNodeWatchClosed))

				// list children and watch should only be called once
				Expect(zkMock.calledListChildrenWith).To(Equal([]string{basePath}))
				Expect(zkMock.calledWatchWith).To(Equal([]string{basePath + "/p002"}))

			case <-time.After(timeout):
				Fail("should have returned from BecomeLeader but has not")
			}
		}, 1)

		It("should continue as leader if previous candidate disappears", func(done Done) {
			defer flushTest(done)

			becomeLeaderDoneCh := make(chan error)

			zkMock := &mockZkClient{
				returnForHasSession:                      []bool{true},
				returnPathForCreateNodeSequenceEphemeral: basePath + "/p003",
				returnChildrenForListChildren: [][]string{
					{"p003", "p002"},
					{"p003"},
				},
				watchingCheckpointCh: make(chan struct{}),
				returnChForWatch:     make(chan zk.Event),
			}

			e := createElection(zkMock)

			go func() {
				defer GinkgoRecover()

				becomeLeaderDoneCh <- e.BecomeLeader(ctx)
			}()

			select {
			case <-zkMock.watchingCheckpointCh:
				Expect(e.proposalNodePath).To(Equal("/ballot/test/election/p003"))

			case <-time.After(timeout):
				Fail("should be watching but is not")
			}

			zkMock.returnChForWatch <- zk.Event{Type: zk.EventNodeDeleted}

			select {
			case err := <-becomeLeaderDoneCh:
				Expect(err).NotTo(HaveOccurred())

				// list children should be called once for the first follower outcome
				// then once when leader quits
				Expect(zkMock.calledListChildrenWith).To(Equal([]string{basePath, basePath}))

				// watch should only be called once
				Expect(zkMock.calledWatchWith).To(Equal([]string{basePath + "/p002"}))

			case <-time.After(timeout):
				Fail("should have returned from BecomeLeader but has not")
			}
		}, 1)

		It("should bail if unable to watch previous candidate", func(done Done) {
			defer flushTest(done)

			becomeLeaderDoneCh := make(chan error)

			zkMock := &mockZkClient{
				returnForHasSession:                      []bool{true},
				returnPathForCreateNodeSequenceEphemeral: basePath + "/p003",
				returnChildrenForListChildren: [][]string{
					{"p002", "p003"},
					{"p003"},
				},
				watchingCheckpointCh: make(chan struct{}),
				returnErrorForWatch:  errTest,
			}

			e := createElection(zkMock)

			go func() {
				defer GinkgoRecover()

				becomeLeaderDoneCh <- e.BecomeLeader(ctx)
			}()

			select {
			case <-zkMock.watchingCheckpointCh:
				Expect(e.proposalNodePath).To(Equal("/ballot/test/election/p003"))

			case <-time.After(timeout):
				Fail("should be watching but is not")
			}

			select {
			case err := <-becomeLeaderDoneCh:
				Expect(err).To(MatchError(errTest))

				// list children and watch should only be called once
				Expect(zkMock.calledListChildrenWith).To(Equal([]string{basePath}))
				Expect(zkMock.calledWatchWith).To(Equal([]string{basePath + "/p002"}))

			case <-time.After(timeout):
				Fail("should have returned from BecomeLeader but has not")
			}
		}, 1)

		It("should bail if context canceled while waiting for leader to yield", func(done Done) {
			defer flushTest(done)

			becomeLeaderDoneCh := make(chan error)

			zkMock := &mockZkClient{
				returnForHasSession:                      []bool{true},
				returnPathForCreateNodeSequenceEphemeral: basePath + "/p003",
				returnChildrenForListChildren: [][]string{
					{"p002", "p003"},
					{"p003"},
				},
				watchingCheckpointCh: make(chan struct{}),
				returnChForWatch:     make(chan zk.Event),
			}

			e := createElection(zkMock)

			ctx, cancel := context.WithCancel(ctx)

			go func() {
				defer GinkgoRecover()

				becomeLeaderDoneCh <- e.BecomeLeader(ctx)
			}()

			select {
			case <-zkMock.watchingCheckpointCh:
				Expect(e.proposalNodePath).To(Equal("/ballot/test/election/p003"))

			case <-time.After(timeout):
				Fail("should be watching but is not")
			}

			cancel()

			select {
			case err := <-becomeLeaderDoneCh:
				Expect(err).To(MatchError(context.Canceled))

				// list children and watch should only be called once
				Expect(zkMock.calledListChildrenWith).To(Equal([]string{basePath}))
				Expect(zkMock.calledWatchWith).To(Equal([]string{basePath + "/p002"}))

			case <-time.After(timeout):
				Fail("should have returned from BecomeLeader but has not")
			}
		}, 1)
	})

	Describe("Resign", func() {
		It("should delete the proposal node from zk", func() {
			proposalNodePath := basePath + "/p003"

			zkMock := &mockZkClient{
				returnForHasSession:                      []bool{true},
				returnPathForCreateNodeSequenceEphemeral: proposalNodePath,
			}
			e := createElection(zkMock)

			err := e.BecomeLeader(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(e.proposalNodePath).To(Equal(proposalNodePath))

			err = e.Resign(ctx)
			Expect(err).NotTo(HaveOccurred())

			Expect(e.proposalNodePath).To(BeEmpty())
			Expect(zkMock.calledDeleteWith).To(Equal([]string{proposalNodePath}))
		})

		It("should return an error if proposal node deletion fails", func() {
			proposalNodePath := basePath + "/p003"

			zkMock := &mockZkClient{
				returnForHasSession:                      []bool{true},
				returnPathForCreateNodeSequenceEphemeral: proposalNodePath,
				returnForDelete:                          errTest,
			}
			e := createElection(zkMock)

			err := e.BecomeLeader(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(e.proposalNodePath).To(Equal(proposalNodePath))

			err = e.Resign(ctx)
			Expect(err).To(MatchError(errTest))

			Expect(e.proposalNodePath).To(BeEmpty())
			Expect(zkMock.calledDeleteWith).To(Equal([]string{proposalNodePath}))
		})
	})
})

type mockZkClient struct {
	calledCreateNodeSequenceEphemeralWithPath []string
	calledCreateNodeSequenceEphemeralWithData [][]byte
	returnPathForCreateNodeSequenceEphemeral  string
	returnErrorForCreateNodeSequenceEphemeral error

	calledCreateNodeWith []string
	returnForCreateNode  error

	calledDeleteWith []string
	returnForDelete  error

	calledListChildrenNTimes      int
	calledListChildrenWith        []string
	returnChildrenForListChildren [][]string
	returnErrorForListChildren    error

	calledWatchWith      []string
	returnChForWatch     chan zk.Event
	returnErrorForWatch  error
	watchingCheckpointCh chan struct{}

	calledHasSessionNTimes int
	returnForHasSession    []bool
}

func (z *mockZkClient) CreateNodeSequenceEphemeral(path string, data []byte) (string, error) {
	z.calledCreateNodeSequenceEphemeralWithPath = append(z.calledCreateNodeSequenceEphemeralWithPath, path)
	z.calledCreateNodeSequenceEphemeralWithData = append(z.calledCreateNodeSequenceEphemeralWithData, data)

	return z.returnPathForCreateNodeSequenceEphemeral, z.returnErrorForCreateNodeSequenceEphemeral
}

func (z *mockZkClient) CreateNode(path string) error {
	z.calledCreateNodeWith = append(z.calledCreateNodeWith, path)

	return z.returnForCreateNode
}

func (z *mockZkClient) Delete(path string) error {
	z.calledDeleteWith = append(z.calledDeleteWith, path)

	return z.returnForDelete
}

func (z *mockZkClient) ListChildren(path string) ([]string, error) {
	i := z.calledListChildrenNTimes
	z.calledListChildrenNTimes++
	z.calledListChildrenWith = append(z.calledListChildrenWith, path)

	err := z.returnErrorForListChildren

	if len(z.returnChildrenForListChildren) == 0 {
		return nil, err
	}

	return z.returnChildrenForListChildren[i], err
}

func (z *mockZkClient) Watch(path string) (<-chan zk.Event, error) {
	z.calledWatchWith = append(z.calledWatchWith, path)

	if z.watchingCheckpointCh != nil {
		close(z.watchingCheckpointCh)
	}

	return z.returnChForWatch, z.returnErrorForWatch
}

func (z *mockZkClient) HasSession() bool {
	ret := z.returnForHasSession[z.calledHasSessionNTimes]

	z.calledHasSessionNTimes++

	return ret
}
