// Copyright (c) 2017 Huawei Technologies Duesseldorf GmbH
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
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/clearcontainers/proxy/api"
	"github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
)

func lastLogEq(a *assert.Assertions, lh *test.Hook, msg string) {
	entry := lh.LastEntry()
	if a.NotNil(entry) {
		a.Equal(entry.Message, msg)
	}
}

func TestState_restoreIoSessions(t *testing.T) {
	a := assert.New(t)

	rig := newTestRig(t)
	rig.Start()
	proxy := rig.proxy
	ctlSocketPath, ioSocketPath := rig.Hyperstart.GetSocketPaths()
	tVM := newVM(testContainerID, ctlSocketPath, ioSocketPath)
	a.NotNil(tVM)

	// Test 1: vm == nil
	e := restoreIoSessions(proxy, nil, []ioSessionState{})
	a.EqualError(e, "vm parameter is nil")

	// Test 2: ignores empty list of tokens
	a.Nil(restoreIoSessions(proxy, tVM, []ioSessionState{}))
	a.Equal(len(proxy.tokenToVM), 0)

	// Test 3: registers token1
	a.Nil(restoreIoSessions(proxy, tVM, []ioSessionState{{"token1",
		testContainerID, 2, 2}}))
	a.Equal(proxy.tokenToVM["token1"], &tokenInfo{
		state: tokenStateAllocated,
		vm:    tVM,
	})

	// Test 1: vm == nil
	e = restoreIoSessionsWaitForShim(proxy, nil, &vmState{})
	a.EqualError(e, "failed to restore io sessions []: vm parameter is nil")

	// Test 2: ignores empty list of tokens
	a.Nil(restoreIoSessionsWaitForShim(rig.proxy, &vm{},
		&vmState{api.RegisterVM{}, []ioSessionState{}}))

	// Test 3:
	e = restoreIoSessionsWaitForShim(rig.proxy, &vm{},
		&vmState{api.RegisterVM{}, []ioSessionState{{Token: ""}}})
	a.EqualError(e, "empty token in recovering state")

	// Test 4: success
	proxy.Lock()
	proxy.vms[testContainerID] = tVM
	proxy.Unlock()
	go tVM.AssociateShim(Token("token2"), 1, nil)
	a.Nil(restoreIoSessionsWaitForShim(proxy,
		tVM,
		&vmState{
			api.RegisterVM{
				ContainerID:  testContainerID,
				CtlSerial:    "",
				IoSerial:     "",
				Console:      "",
				NumIOStreams: 1},
			[]ioSessionState{{"token2", testContainerID, 2, 2}}}))
}

func TestState_restoreAllState(t *testing.T) {
	a := assert.New(t)
	rig := newTestRig(t)
	rig.Start()

	// clean up a possible state
	a.Nil(os.RemoveAll(storeStateDir))

	// Test 1: nothing to restore
	restored, err := restoreAllState(rig.proxy)
	a.False(restored)
	a.Nil(err)

	a.Nil(os.MkdirAll(storeStateDir, 0750))

	// Test 2: fails to restore from an inaccessible file (permission denied)
	a.Nil(ioutil.WriteFile(proxyStateFilePath, []byte{' '}, 0000))

	restored, err = restoreAllState(rig.proxy)
	a.False(restored)
	a.EqualError(err, "couldn't unmarshal "+proxyStateFilePath+
		": unexpected end of JSON input")

	a.Nil(os.Remove(proxyStateFilePath))

	// Test 3: fails to restore from an empty file
	a.Nil(ioutil.WriteFile(proxyStateFilePath, []byte(""), 0600))

	restored, err = restoreAllState(rig.proxy)
	a.False(restored)
	a.EqualError(err, "couldn't unmarshal "+proxyStateFilePath+
		": unexpected end of JSON input")

	// Test 4: fails to restore from garbage
	a.Nil(ioutil.WriteFile(proxyStateFilePath, []byte("Hello, World!"), 0600))

	restored, err = restoreAllState(rig.proxy)
	a.False(restored)
	a.EqualError(err, "couldn't unmarshal "+proxyStateFilePath+
		": invalid character 'H' looking for beginning of value")

	// Test 5: fails to restore when ContainerIDs list is empty
	const s = `{ "container_ids": [ ] }`
	a.Nil(ioutil.WriteFile(proxyStateFilePath, []byte(s), 0600))

	restored, err = restoreAllState(rig.proxy)
	a.False(restored)
	a.EqualError(err, "containerIDs list is empty")

	// Test 6: fails to restore when stored Version is higher
	sVer := fmt.Sprintf(`{ "version": %d, "container_ids": [ "09876543210" ] }`,
		stateFileFormatVersion+1)
	a.Nil(ioutil.WriteFile(proxyStateFilePath, []byte(sVer), 0600))

	restored, err = restoreAllState(rig.proxy)
	a.False(restored)
	a.EqualError(err, fmt.Sprintf("stored state format version (%d) is "+
		"higher than supported (%d). Aborting", stateFileFormatVersion+1,
		stateFileFormatVersion))

	a.Nil(os.Remove(proxyStateFilePath))

	// Test 7: success
	rig.RegisterVM()
	rig.Stop()
	rig = newTestRig(t)
	rig.Start()

	restored, err = restoreAllState(rig.proxy)
	a.True(restored)
	a.Nil(err)
}

func TestState_storeProxyState(t *testing.T) {
	a := assert.New(t)
	rig := newTestRig(t)
	rig.Start()
	proxy := rig.proxy
	a.NotNil(proxy)

	// Test 1: success - 0 vm to store, no proxy's state file
	a.Nil(os.RemoveAll(storeStateDir))
	a.Nil(storeProxyState(proxy))

	// Test 2: fails to store a state to a file
	a.Nil(os.MkdirAll(storeStateDir, 0600))
	rig.RegisterVM()
	a.Nil(os.RemoveAll(storeStateDir))
	a.EqualError(storeProxyState(proxy), fmt.Sprintf("couldn't store proxy"+
		" state to file %s: open %s: no such file or directory",
		proxyStateFilePath, proxyStateFilePath))
}

func TestState_storeVMState(t *testing.T) {
	a := assert.New(t)
	rig := newTestRig(t)
	rig.Start()
	proxy := rig.proxy
	a.NotNil(proxy)

	a.Equal(vmStateFilePath(testContainerID), filepath.Join(storeStateDir,
		"vm_"+testContainerID+".json"))

	// clean up a possible state
	a.Nil(os.RemoveAll(storeStateDir))
	a.Nil(os.MkdirAll(storeStateDir, 0750))

	_ = rig.RegisterVM()
	vm := proxy.vms[testContainerID]
	a.NotNil(vm)

	// Test 1: success to store vm's state
	a.Nil(storeVMState(vm))

	// Test 2: fails to write file
	a.Nil(os.RemoveAll(storeStateDir))
	a.EqualError(storeVMState(vm),
		fmt.Sprintf("couldn't store VM state to %s: open %s: no such"+
			" file or directory", vmStateFilePath(testContainerID),
			vmStateFilePath(testContainerID)))
}

func TestState_delVMAndState(t *testing.T) {
	a := assert.New(t)
	rig := newTestRig(t)
	rig.Start()
	proxy := rig.proxy
	a.NotNil(proxy)

	// clean up a possible state
	a.Nil(os.RemoveAll(storeStateDir))
	a.Nil(os.MkdirAll(storeStateDir, proxyStateDirPerm))

	// Test 1: check proxy parameter
	a.EqualError(delVMAndState(nil, nil), "proxy parameter is nil")
	// Test 2: check vm parameter
	a.EqualError(delVMAndState(proxy, nil), "vm parameter is nil")

	// Test 3: check failure to delete a vm state file
	_ = rig.RegisterVM()
	vm := proxy.vms[testContainerID]
	a.Nil(storeVMState(vm))
	a.Nil(os.RemoveAll(storeStateDir))
	a.EqualError(delVMAndState(proxy, vm),
		fmt.Sprintf("couldn't remove file %s: remove %s: no such"+
			" file or directory", vmStateFilePath(testContainerID),
			vmStateFilePath(testContainerID)))

	// Test 4: check successful execution
	a.Nil(os.MkdirAll(storeStateDir, proxyStateDirPerm))
	a.Nil(storeVMState(vm))
	a.Nil(delVMAndState(proxy, vm))
}

func TestState_readVMState(t *testing.T) {
	a := assert.New(t)
	rig := newTestRig(t)
	rig.Start()
	proxy := rig.proxy
	a.NotNil(proxy)

	// clean up a possible state
	a.Nil(os.RemoveAll(storeStateDir))
	a.Nil(os.MkdirAll(storeStateDir, proxyStateDirPerm))

	// Test 1: containerID parameter must be non-empty
	vmState, err := readVMState("")
	a.Nil(vmState)
	a.EqualError(err, "containerID parameter is empty")

	testContPath := vmStateFilePath(testContainerID)

	// Test 2: read from an inaccessible file
	vmState, err = readVMState(testContainerID)
	a.Nil(vmState)
	a.EqualError(err, fmt.Sprintf("couldn't read %s: open %s: no such file"+
		" or directory", testContPath, testContPath))

	// Test 3: read garbage
	a.Nil(ioutil.WriteFile(testContPath, []byte("Garbage"), 0600))
	vmState, err = readVMState(testContainerID)
	a.Nil(vmState)
	a.EqualError(err, fmt.Sprintf("couldn't unmarshal %s: invalid "+
		"character 'G' looking for beginning of value", testContPath))

	// Test 4: success
	_ = rig.RegisterVM()
	vmState, err = readVMState(testContainerID)
	a.NotNil(vmState)
	a.Nil(err)
}

func TestState_restoreVMState(t *testing.T) {
	a := assert.New(t)
	rig := newTestRig(t)
	rig.Start()
	proxy := rig.proxy
	a.NotNil(proxy)
	lh := test.NewGlobal()

	testContPath := vmStateFilePath(testContainerID)

	// clean up a possible state
	a.Nil(os.RemoveAll(storeStateDir))
	a.Nil(os.MkdirAll(storeStateDir, proxyStateDirPerm))

	// Test 1: proxy == nil
	a.False(restoreVMState(nil, testContainerID))
	lastLogEq(a, lh, "proxy parameter is nil")

	// Test 2: ContainerID is empty
	a.False(restoreVMState(proxy, ""))
	lastLogEq(a, lh, "containerID is empty")

	// Test 3: readVMState() returns an error
	a.False(restoreVMState(proxy, testContainerID))
	lastLogEq(a, lh, fmt.Sprintf("couldn't read %s: open %s: no such file"+
		" or directory", testContPath, testContPath))

	// Test 4: wrong vm parameters
	const t4 = `{ "registerVM": { "containerId": "" } } `
	a.Nil(ioutil.WriteFile(testContPath, []byte(t4), 0600))
	a.False(restoreVMState(proxy, testContainerID))
	lastLogEq(a, lh, fmt.Sprintf("wrong VM parameters: {ContainerID: "+
		"CtlSerial: IoSerial: Console: NumIOStreams:0}"))

	// Test 5: inconsistent container ID
	const t5 = `{ "registerVM": { "containerId": "0", "ctlSerial": "path",
	"ioSerial": "path" } }`
	a.Nil(ioutil.WriteFile(testContPath, []byte(t5), 0600))
	a.False(restoreVMState(proxy, testContainerID))
	lastLogEq(a, lh, fmt.Sprintf("inconsistent container ID: 0"))

	// Test 6: restoreTokens() returns an error
	t6 := fmt.Sprintf(`{ "registerVM": { "containerID": "%s",
		"ctlSerial": "path", "ioSerial": "path" },
		"io_sessions": [ { "token": "" } ] }`, testContainerID)
	a.Nil(ioutil.WriteFile(testContPath, []byte(t6), 0600))
	a.False(restoreVMState(proxy, testContainerID))
	lastLogEq(a, lh, fmt.Sprintf("error restoring tokens: empty token in "+
		"recovering state"))

	// Test 7: container is already registered
	_ = rig.RegisterVM()
	a.False(restoreVMState(proxy, testContainerID))
	lastLogEq(a, lh, fmt.Sprintf("container already registered"))
}
