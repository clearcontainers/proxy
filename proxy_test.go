// Copyright (c) 2016,2017 Intel Corporation
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"encoding/json"
	"flag"
	"net"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/clearcontainers/proxy/api"
	goapi "github.com/clearcontainers/proxy/client"
	"github.com/containers/virtcontainers/hyperstart/mock"

	hyperapi "github.com/hyperhq/runv/hyperstart/api/json"
	"github.com/stretchr/testify/assert"
)

type testRig struct {
	t  *testing.T
	wg sync.WaitGroup

	// hyperstart mocking
	Hyperstart      *mock.Hyperstart
	ctlPath, ioPath string

	// Control if we start the proxy in the test process or as a separate
	// process
	proxyFork bool

	// proxy, in process
	proxy      *proxy
	protocol   *protocol
	proxyConns []net.Conn // sockets used by proxy to communicate with Client

	// client
	Client *goapi.Client

	// fd leak detection
	detector          *FdLeakDetector
	startFds, stopFds *FdSnapshot
}

func newTestRig(t *testing.T) *testRig {
	proto := newProtocol()
	proto.HandleCommand(api.CmdRegisterVM, registerVM)
	proto.HandleCommand(api.CmdAttachVM, attachVM)
	proto.HandleCommand(api.CmdUnregisterVM, unregisterVM)
	proto.HandleCommand(api.CmdHyper, hyper)
	proto.HandleCommand(api.CmdConnectShim, connectShim)
	proto.HandleCommand(api.CmdDisconnectShim, disconnectShim)

	return &testRig{
		t:        t,
		protocol: proto,
		proxy:    newProxy(),
		detector: NewFdLeadDetector(),
	}
}

func (rig *testRig) Start() {
	var err error

	rig.startFds, err = rig.detector.Snapshot()
	assert.Nil(rig.t, err)

	initLogging()
	flag.Parse()

	// Start hyperstart go routine
	rig.Hyperstart = mock.NewHyperstart(rig.t)
	rig.Hyperstart.Start()

	// Explicitly send READY message from hyperstart mock
	rig.wg.Add(1)
	go func() {
		rig.Hyperstart.SendMessage(int(hyperapi.INIT_READY), []byte{})
		rig.wg.Done()
	}()

	// Client object that can be used to issue proxy commands
	clientConn := rig.ServeNewClient()
	rig.Client = goapi.NewClient(clientConn.(*net.UnixConn))
}

func (rig *testRig) Stop() {
	var err error

	rig.Client.Close()

	for _, conn := range rig.proxyConns {
		conn.Close()
	}

	rig.Hyperstart.Stop()

	rig.wg.Wait()

	if rig.proxy != nil {
		rig.proxy.wg.Wait()
	}

	// We shouldn't have leaked a fd between the beginning of Start() and
	// the end of Stop().
	rig.stopFds, err = rig.detector.Snapshot()
	assert.Nil(rig.t, err)

	assert.True(rig.t,
		rig.detector.Compare(os.Stdout, rig.startFds, rig.stopFds))
}

// ServeNewClient simulate a new client connecting to the proxy. It returns the
// net.Conn that represents client-side connection to the proxy.
func (rig *testRig) ServeNewClient() net.Conn {
	clientConn, proxyConn, err := Socketpair()
	assert.Nil(rig.t, err)
	rig.proxyConns = append(rig.proxyConns, proxyConn)
	rig.wg.Add(1)
	go func() {
		rig.proxy.serveNewClient(rig.protocol, proxyConn)
		rig.wg.Done()
	}()

	return clientConn
}

const testContainerID = "0987654321"

func TestRegisterVM(t *testing.T) {
	rig := newTestRig(t)
	rig.Start()

	// Register new VM.
	ctlSocketPath, ioSocketPath := rig.Hyperstart.GetSocketPaths()
	ret, err := rig.Client.RegisterVM(testContainerID, ctlSocketPath, ioSocketPath, nil)
	assert.Nil(t, err)
	assert.NotNil(t, ret)
	// We haven't asked for I/O tokens
	assert.Equal(t, "", ret.IO.URL)
	assert.Equal(t, 0, len(ret.IO.Tokens))

	// A new RegisterVM message with the same containerID should error out.
	_, err = rig.Client.RegisterVM(testContainerID, "fooCtl", "fooIo", nil)
	assert.NotNil(t, err)

	// RegisterVM should register a new vm object.
	proxy := rig.proxy
	proxy.Lock()
	vm := proxy.vms[testContainerID]
	proxy.Unlock()

	assert.NotNil(t, vm)
	assert.Equal(t, testContainerID, vm.containerID)

	// This test shouldn't send anything to hyperstart.
	msgs := rig.Hyperstart.GetLastMessages()
	assert.Equal(t, 0, len(msgs))

	rig.Stop()
}

func TestUnregisterVM(t *testing.T) {
	rig := newTestRig(t)
	rig.Start()

	// Register new VM
	ctlSocketPath, ioSocketPath := rig.Hyperstart.GetSocketPaths()
	_, err := rig.Client.RegisterVM(testContainerID, ctlSocketPath, ioSocketPath, nil)
	assert.Nil(t, err)

	// UnregisterVM with a bad containerID.
	err = rig.Client.UnregisterVM("foo")
	assert.NotNil(t, err)

	// Bye!
	err = rig.Client.UnregisterVM(testContainerID)
	assert.Nil(t, err)

	// A second UnregisterVM (client not attached anymore) should return an
	// error.
	err = rig.Client.UnregisterVM(testContainerID)
	assert.NotNil(t, err)

	// UnregisterVM should unregister the vm object
	proxy := rig.proxy
	proxy.Lock()
	vm := proxy.vms[testContainerID]
	proxy.Unlock()
	assert.Nil(t, vm)

	// This test shouldn't send anything to hyperstart
	msgs := rig.Hyperstart.GetLastMessages()
	assert.Equal(t, 0, len(msgs))

	rig.Stop()
}

func TestAttachVM(t *testing.T) {
	rig := newTestRig(t)
	rig.Start()

	// Register new VM
	ctlSocketPath, ioSocketPath := rig.Hyperstart.GetSocketPaths()
	_, err := rig.Client.RegisterVM(testContainerID, ctlSocketPath, ioSocketPath, nil)
	assert.Nil(t, err)

	// Attaching to an unknown VM should return an error
	_, err = rig.Client.AttachVM("foo", nil)
	assert.NotNil(t, err)

	// Attaching to an existing VM should work. To test we are effectively
	// attached, we issue an UnregisterVM that would error out if not
	// attached.
	ret, err := rig.Client.AttachVM(testContainerID, nil)
	assert.Nil(t, err)
	// We haven't asked for I/O tokens
	assert.Equal(t, "", ret.IO.URL)
	assert.Equal(t, 0, len(ret.IO.Tokens))

	err = rig.Client.UnregisterVM(testContainerID)
	assert.Nil(t, err)

	// This test shouldn't send anything with hyperstart
	msgs := rig.Hyperstart.GetLastMessages()
	assert.Equal(t, 0, len(msgs))

	rig.Stop()
}

func TestHyperPing(t *testing.T) {
	rig := newTestRig(t)
	rig.Start()

	ctlSocketPath, ioSocketPath := rig.Hyperstart.GetSocketPaths()
	_, err := rig.Client.RegisterVM(testContainerID, ctlSocketPath, ioSocketPath, nil)
	assert.Nil(t, err)

	// Send ping and verify we have indeed received the message on the
	// hyperstart side. Ping is somewhat interesting because it's a case of
	// an hyper message without data.
	err = rig.Client.Hyper("ping", nil)
	assert.Nil(t, err)

	msgs := rig.Hyperstart.GetLastMessages()
	assert.Equal(t, 1, len(msgs))

	msg := msgs[0]
	assert.Equal(t, hyperapi.INIT_PING, int(msg.Code))
	assert.Equal(t, 0, len(msg.Message))

	rig.Stop()
}

func TestHyperStartpod(t *testing.T) {
	rig := newTestRig(t)
	rig.Start()

	// Register new VM
	ctlSocketPath, ioSocketPath := rig.Hyperstart.GetSocketPaths()
	_, err := rig.Client.RegisterVM(testContainerID, ctlSocketPath, ioSocketPath, nil)
	assert.Nil(t, err)

	// Send startopd and verify we have indeed received the message on the
	// hyperstart side. startpod is interesting because it's a case of an
	// hyper message with JSON data.
	startpod := hyperapi.Pod{
		Hostname: "testhostname",
		ShareDir: "rootfs",
	}
	err = rig.Client.Hyper("startpod", &startpod)
	assert.Nil(t, err)

	msgs := rig.Hyperstart.GetLastMessages()
	assert.Equal(t, 1, len(msgs))

	msg := msgs[0]
	assert.Equal(t, hyperapi.INIT_STARTPOD, int(msg.Code))
	received := hyperapi.Pod{}
	err = json.Unmarshal(msg.Message, &received)
	assert.Nil(t, err)
	assert.Equal(t, startpod.Hostname, received.Hostname)
	assert.Equal(t, startpod.ShareDir, received.ShareDir)

	rig.Stop()
}

func TestRegisterVMAllocateTokens(t *testing.T) {
	rig := newTestRig(t)
	rig.Start()

	// Register new VM, asking for tokens
	ctlSocketPath, ioSocketPath := rig.Hyperstart.GetSocketPaths()
	ret, err := rig.Client.RegisterVM(testContainerID, ctlSocketPath, ioSocketPath,
		&goapi.RegisterVMOptions{NumIOStreams: 2})
	assert.Nil(t, err)
	assert.NotNil(t, ret)
	assert.True(t, strings.HasPrefix(ret.IO.URL, "unix://"))
	assert.Equal(t, 2, len(ret.IO.Tokens))

	// This test shouldn't send anything to hyperstart.
	msgs := rig.Hyperstart.GetLastMessages()
	assert.Equal(t, 0, len(msgs))

	rig.Stop()
}

func TestAttachVMAllocateTokens(t *testing.T) {
	rig := newTestRig(t)
	rig.Start()

	// Register new VM
	ctlSocketPath, ioSocketPath := rig.Hyperstart.GetSocketPaths()
	_, err := rig.Client.RegisterVM(testContainerID, ctlSocketPath, ioSocketPath, nil)
	assert.Nil(t, err)

	// Attach to the VM, asking for tokens
	ret, err := rig.Client.AttachVM(testContainerID, &goapi.AttachVMOptions{NumIOStreams: 2})
	assert.Nil(t, err)
	assert.NotNil(t, ret)
	assert.True(t, strings.HasPrefix(ret.IO.URL, "unix://"))
	assert.Equal(t, 2, len(ret.IO.Tokens))

	// Cleanup
	err = rig.Client.UnregisterVM(testContainerID)
	assert.Nil(t, err)

	// This test shouldn't send anything with hyperstart
	msgs := rig.Hyperstart.GetLastMessages()
	assert.Equal(t, 0, len(msgs))

	rig.Stop()
}

func TestConnectShim(t *testing.T) {
	rig := newTestRig(t)
	rig.Start()

	// Register new VM, asking for tokens. We use the assumption the same
	// connection can be used for ConnectShim, which is true in the tests.
	ctlSocketPath, ioSocketPath := rig.Hyperstart.GetSocketPaths()
	ret, err := rig.Client.RegisterVM(testContainerID, ctlSocketPath, ioSocketPath,
		&goapi.RegisterVMOptions{NumIOStreams: 1})
	assert.Nil(t, err)
	assert.Equal(t, 1, len(ret.IO.Tokens))
	token := ret.IO.Tokens[0]

	// Using a bad token should result in an error
	err = rig.Client.ConnectShim("notatoken")
	assert.NotNil(t, err)

	// Register shim with an existing token, all should be good
	err = rig.Client.ConnectShim(token)
	assert.Nil(t, err)

	// Trying to re-use a token that a process has already claimed should
	// result in an error.
	err = rig.Client.ConnectShim(token)
	assert.NotNil(t, err)

	// Cleanup
	err = rig.Client.DisconnectShim()
	assert.Nil(t, err)

	// This test shouldn't send anything to hyperstart.
	msgs := rig.Hyperstart.GetLastMessages()
	assert.Equal(t, 0, len(msgs))

	rig.Stop()
}

// Relocations are thoroughly tested in vm_test.go, this is just to ensure we
// have coverage at a higher level.
func TestHyperSequenceNumberRelocation(t *testing.T) {
	rig := newTestRig(t)
	rig.Start()

	// Register new VM, asking for tokens. We use the assumption the same
	// connection can be used for ConnectShim, which is true in the tests.
	ctlSocketPath, ioSocketPath := rig.Hyperstart.GetSocketPaths()
	ret, err := rig.Client.RegisterVM(
		testContainerID, ctlSocketPath, ioSocketPath,
		&goapi.RegisterVMOptions{NumIOStreams: 1})
	assert.Nil(t, err)
	tokens := ret.IO.Tokens
	assert.Equal(t, 1, len(tokens))

	// Send newcontainer hyper command
	newcontainer := hyperapi.Container{
		Id: testContainerID,
		Process: &hyperapi.Process{
			Args: []string{"/bin/sh"},
		},
	}
	err = rig.Client.HyperWithTokens("newcontainer", tokens, &newcontainer)
	assert.Nil(t, err)

	// Verify hyperstart has received the message with relocation
	msgs := rig.Hyperstart.GetLastMessages()
	assert.Equal(t, 1, len(msgs))
	msg := msgs[0]
	assert.Equal(t, uint32(hyperapi.INIT_NEWCONTAINER), msg.Code)
	payload := hyperapi.Container{}
	err = json.Unmarshal(msg.Message, &payload)
	assert.Nil(t, err)
	assert.NotEqual(t, 0, payload.Process.Stdio)
	assert.NotEqual(t, 0, payload.Process.Stderr)

	rig.Stop()
}
