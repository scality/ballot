package supervisord

import (
	"context"
	"fmt"
	"net"
	"net/http"

	"github.com/kolo/xmlrpc"
)

const (
	apiVersion string = "3.0"
)

type ProcessInfo struct {
	Name          string `xmlrpc:"name"`
	Description   string `xmlrpc:"description"`
	Group         string `xmlrpc:"group"`
	Start         int64  `xmlrpc:"start"`
	Stop          int64  `xmlrpc:"stop"`
	Now           int64  `xmlrpc:"now"`
	State         int64  `xmlrpc:"state"`
	StateName     string `xmlrpc:"statename"`
	SpawnErr      string `xmlrpc:"spawnerr"`
	ExitStatus    int64  `xmlrpc:"exitstatus"`
	Logfile       string `xmlrpc:"logfile"`
	StdoutLogfile string `xmlrpc:"stdout_logfile"`
	StderrLogfile string `xmlrpc:"stderr_logfile"`
	PID           int64  `xmlrpc:"pid"`
}

type Client struct {
	RpcClient  *xmlrpc.Client
	ApiVersion string
}

// NewClient creates a new supervisor RPC client.
func NewClient(url string) (client Client, err error) {
	transport := &http.Transport{
		DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
			return net.Dial("unix", url)
		},
	}

	var rpc *xmlrpc.Client
	if rpc, err = xmlrpc.NewClient("http://unix/RPC2", transport); err != nil {
		return
	}

	version := ""
	if err = rpc.Call("supervisor.getAPIVersion", nil, &version); err != nil {
		return
	}
	if version != apiVersion {
		err = fmt.Errorf("want Supervisor API version %s, got %s instead", apiVersion, version)
		return
	}
	client = Client{rpc, version}
	return
}

// Close the client.
func (client Client) Close() error {
	return client.RpcClient.Close()
}

// GetProcessInfo retrieves information for a particular Supervisor process.
func (client Client) GetProcessInfo(name string) (info ProcessInfo, err error) {
	client.RpcClient.Call("supervisor.getProcessInfo", name, &info)
	return
}

// StartProcess tells Supervisor to start the named process.
func (client Client) StartProcess(name string, wait bool) (result bool, err error) {
	err = client.RpcClient.Call("supervisor.startProcess", []interface{}{name, wait}, &result)
	return
}
