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
	"fmt"
	"time"

	"github.com/go-zookeeper/zk"
	logrus "github.com/sirupsen/logrus"
)

type zkClient interface {
	CreateNodeSequenceEphemeral(path string, data []byte) (string, error)
	CreateNode(path string) error
	Delete(path string) error
	ListChildren(path string) ([]string, error)
	Watch(path string) (<-chan zk.Event, error)
	HasSession() bool
}

type defaultZkClient struct {
	conn *zk.Conn
}

func connectZkClient(servers []string, sessionTimeout time.Duration, debug bool, l *logrus.Entry) (*defaultZkClient, error) {
	conn, _, err := zk.Connect(servers, sessionTimeout, zk.WithLogger(l), zk.WithLogInfo(debug))
	if err != nil {
		return nil, fmt.Errorf("zookeeper connect: %w", err)
	}

	return &defaultZkClient{
		conn: conn,
	}, nil
}

func (z *defaultZkClient) CreateNodeSequenceEphemeral(path string, data []byte) (string, error) {
	flags := int32(zk.FlagEphemeral | zk.FlagSequence)
	acls := zk.WorldACL(zk.PermAll)

	return z.conn.Create(path, data, flags, acls)
}

func (z *defaultZkClient) CreateNode(path string) error {
	_, err := z.conn.Create(path, nil, 0, zk.WorldACL(zk.PermAll))

	return fmt.Errorf("create: %w", err)
}

func (z *defaultZkClient) Delete(path string) error {
	return z.conn.Delete(path, 0)
}

func (z *defaultZkClient) ListChildren(path string) ([]string, error) {
	children, _, err := z.conn.Children(path)
	if err != nil {
		return nil, fmt.Errorf("list: %w", err)
	}

	return children, nil
}

func (z *defaultZkClient) Watch(path string) (<-chan zk.Event, error) {
	_, _, ch, err := z.conn.GetW(path)
	if err != nil {
		return nil, fmt.Errorf("watch: %w", err)
	}

	return ch, nil
}

func (z *defaultZkClient) HasSession() bool {
	return z.conn.State() == zk.StateHasSession
}
