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
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/clearcontainers/proxy/api"
	"github.com/sirupsen/logrus"
)

const proxyStateFileName = "state.json"
const proxyStateDirPerm = 0750
const proxyStateFilesPerm = 0640

const stateFileFormatVersion = 1

// storeStateDir is populated at link time with the value of:
//   $(LOCALSTATEDIR)/run/clear-containers/proxy/"
var storeStateDir = "/var/run/clear-containers/proxy"

var proxyStateFilePath = filepath.Join(storeStateDir, proxyStateFileName)

// proxyState is used to (re)store proxy state on disk.
// XXX stateFileFormatVersion must be updated in case of any changes in this
// struct.
type proxyState struct {
	Version         uint     `json:"version"`
	SocketPath      string   `json:"socket_path"`
	EnableVMConsole bool     `json:"enable_vm_console"`
	ContainerIDs    []string `json:"container_ids"`
}

// vmState is used to (re)store vm struct on disk
// XXX stateFileFormatVersion must be updated in case of any changes in this
// struct.
type vmState struct {
	RegisterVM api.RegisterVM   `json:"registerVM"`
	IoSessions []ioSessionState `json:"io_sessions"`
	//nextIoBase is restored when ioSessions are restored
}

type ioSessionState struct {
	Token       Token  `json:"token"`
	ContainerID string `json:"container_id"`
	NStreams    int    `json:"n_streams"`
	IoBase      uint64 `json:"io_base"`
}

func logContID(containerID string) *logrus.Entry {
	return proxyLog.WithField("container", containerID)
}

// On success returns nil, otherwise an error string message.
func restoreIoSessions(proxy *proxy, vm *vm, ioSessions []ioSessionState) error {
	if vm == nil {
		return fmt.Errorf("vm parameter is nil")
	}

	for _, ioSes := range ioSessions {
		if ioSes.Token == "" {
			continue
		}

		token, err := vm.AllocateIoSessionAs(ioSes.Token, ioSes.ContainerID,
			ioSes.NStreams, ioSes.IoBase)
		if err != nil {
			return err
		}

		proxy.Lock()
		proxy.tokenToVM[token] = &tokenInfo{
			state: tokenStateAllocated,
			vm:    vm,
		}
		proxy.Unlock()

		session := vm.findSessionByToken(token)
		if session == nil {
			vm.Lock()
			for token := range vm.tokenToSession {
				_ = vm.freeTokenUnlocked(token)
				delete(vm.tokenToSession, token)
			}
			vm.Unlock()
			return fmt.Errorf("unknown token %s", token)
		}

		// Signal that the process is already started
		close(session.processStarted)
	}

	return nil
}

// Returns (false, nil) if it's a clean start (i.e. no state was found).
// Returns (false, error) if the restoring failed.
// Returns (true, nil) if the restoring succeeded.
func restoreAllState(proxy *proxy) (bool, error) {
	if _, err := os.Stat(storeStateDir); os.IsNotExist(err) {
		err := os.MkdirAll(storeStateDir, proxyStateDirPerm)
		if err != nil {
			return false, fmt.Errorf(
				"couldn't create directory %s: %v",
				storeStateDir, err)
		}
		return false, nil
	}

	proxyLog.Info("Restoring proxy's state from: ", proxyStateFilePath)

	fdata, err := ioutil.ReadFile(proxyStateFilePath)
	if err != nil {
		return false, fmt.Errorf("couldn't read state file %s: %v",
			proxyStateFilePath, err)
	}

	var proxyState proxyState

	err = json.Unmarshal(fdata, &proxyState)
	if err != nil {
		return false, fmt.Errorf("couldn't unmarshal %s: %v",
			proxyStateFilePath, err)
	}

	proxyLog.Debugf("proxyState: %+v", proxyState)

	if len(proxyState.ContainerIDs) == 0 {
		return false, fmt.Errorf("containerIDs list is empty")
	}

	if proxyState.Version > stateFileFormatVersion {
		return false, fmt.Errorf("stored state format version (%d) is"+
			" higher than supported (%d). Aborting",
			proxyState.Version, stateFileFormatVersion)
	}

	proxy.socketPath = proxyState.SocketPath
	proxy.enableVMConsole = proxyState.EnableVMConsole

	for _, containerID := range proxyState.ContainerIDs {
		go func(contID string) {
			// ignore failures here but log them inside
			_ = restoreVMState(proxy, contID)
		}(containerID)
	}

	return true, nil
}

// On success returns nil, otherwise an error string message.
func storeProxyState(proxy *proxy) error {
	proxy.Lock()
	defer proxy.Unlock()

	// if there are 0 VMs then remove state from disk
	if (len(proxy.vms)) == 0 {
		if _, err := os.Stat(proxyStateFilePath); os.IsNotExist(err) {
			return nil
		}

		if err := os.Remove(proxyStateFilePath); err != nil {
			return fmt.Errorf("couldn't remove file %s: %v",
				proxyStateFilePath, err)
		}

		return nil
	}

	proxyState := &proxyState{
		Version:         stateFileFormatVersion,
		SocketPath:      proxy.socketPath,
		EnableVMConsole: proxy.enableVMConsole,
	}

	for cID := range proxy.vms {
		proxyState.ContainerIDs = append(proxyState.ContainerIDs, cID)
	}

	data, err := json.MarshalIndent(proxyState, "", "\t")
	if err != nil {
		return fmt.Errorf("couldn't marshal proxy state %+v: %v",
			proxyState, err)
	}

	err = ioutil.WriteFile(proxyStateFilePath, data, proxyStateFilesPerm)
	if err != nil {
		return fmt.Errorf("couldn't store proxy state to file %s: %v",
			proxyStateFilePath, err)
	}

	return nil
}

func vmStateFilePath(id string) string {
	return filepath.Join(storeStateDir, "vm_"+id+".json")
}

// On success returns nil, otherwise an error string message.
func storeVMState(vm *vm) error {
	vm.Lock()
	ioSessions := make([]ioSessionState, 0, len(vm.tokenToSession))
	for _, ioS := range vm.tokenToSession {
		ioSessions = append(ioSessions, ioSessionState{
			ioS.token,
			ioS.containerID,
			ioS.nStreams,
			ioS.ioBase,
		})
	}
	vm.Unlock()

	stVM := vmState{
		RegisterVM: api.RegisterVM{
			ContainerID: vm.containerID,
			CtlSerial:   vm.hyperHandler.GetCtlSockPath(),
			IoSerial:    vm.hyperHandler.GetIoSockPath(),
			Console:     vm.console.socketPath,
		},
		IoSessions: ioSessions,
	}

	o, err := json.MarshalIndent(&stVM, "", "\t")
	if err != nil {
		return fmt.Errorf("couldn't marshal VM state: %v", err)
	}

	stFile := vmStateFilePath(vm.containerID)

	if err := ioutil.WriteFile(stFile, o, proxyStateFilesPerm); err != nil {
		return fmt.Errorf("couldn't store VM state to %s: %v",
			stFile, err)
	}

	return nil
}

// On success returns nil, otherwise an error string message.
func delVMAndState(proxy *proxy, vm *vm) error {
	if proxy == nil {
		return errors.New("proxy parameter is nil")
	}

	if vm == nil {
		return errors.New("vm parameter is nil")
	}

	logContID(vm.containerID).Infof("Removing on-disk state")

	proxy.Lock()
	delete(proxy.vms, vm.containerID)
	proxy.Unlock()

	if err := storeProxyState(proxy); err != nil {
		logContID(vm.containerID).Warnf("Couldn't store proxy's state:"+
			" %v", err)
		// don't fail
	}

	storeFile := vmStateFilePath(vm.containerID)
	if err := os.Remove(storeFile); err != nil {
		return fmt.Errorf("couldn't remove file %s: %v", storeFile, err)
	}

	return nil
}

func readVMState(containerID string) (*vmState, error) {
	if containerID == "" {
		return nil, fmt.Errorf("containerID parameter is empty")
	}

	vmStateFilePath := vmStateFilePath(containerID)
	fdata, err := ioutil.ReadFile(vmStateFilePath)
	if err != nil {
		return nil, fmt.Errorf("couldn't read %s: %v", vmStateFilePath, err)
	}

	var vmState vmState
	err = json.Unmarshal(fdata, &vmState)
	if err != nil {
		return nil, fmt.Errorf("couldn't unmarshal %s: %v",
			vmStateFilePath, err)
	}

	return &vmState, nil
}

func restoreIoSessionsWaitForShim(proxy *proxy, vm *vm, vmState *vmState) error {
	if err := restoreIoSessions(proxy, vm, vmState.IoSessions); err != nil {
		return fmt.Errorf("failed to restore io sessions %+v: %v",
			vmState.IoSessions, err)
	}

	for _, ioSes := range vmState.IoSessions {
		token := ioSes.Token

		if token == "" {
			return fmt.Errorf("empty token in recovering state")
		}

		session := vm.findSessionByToken(token)
		if session == nil {
			_ = delVMAndState(proxy, vm) // errors are irrelevant here
			return fmt.Errorf("couldn't find a session for token: %s",
				token)
		}

		if err := session.WaitForShim(); err != nil {
			_ = delVMAndState(proxy, vm) // errors are irrelevant here
			return fmt.Errorf("failed to re-connect with shim "+
				"(token = %s): %v", token, err)
		}
	}

	return nil
}

func restoreVMState(proxy *proxy, containerID string) bool {
	if proxy == nil {
		logContID(containerID).Errorf("proxy parameter is nil")
		return false
	}

	if containerID == "" {
		logContID(containerID).Errorf("containerID is empty")
		return false
	}

	vmState, err := readVMState(containerID)
	if err != nil {
		logContID(containerID).Error(err)
		return false
	}
	logContID(containerID).Debugf("restoring vm state: %+v", vmState)

	regVM := vmState.RegisterVM
	if regVM.ContainerID == "" || regVM.CtlSerial == "" ||
		regVM.IoSerial == "" {
		logContID(containerID).Errorf("wrong VM parameters: %+v", regVM)
		return false
	}

	if regVM.ContainerID != containerID {
		logContID(containerID).Errorf("inconsistent container ID: %s",
			regVM.ContainerID)
		return false
	}

	proxy.Lock()
	if _, ok := proxy.vms[regVM.ContainerID]; ok {
		proxy.Unlock()
		logContID(containerID).Errorf("container already registered")
		return false
	}
	vm := newVM(regVM.ContainerID, regVM.CtlSerial, regVM.IoSerial)
	proxy.vms[regVM.ContainerID] = vm
	proxy.Unlock()

	proxyLog.WithFields(logrus.Fields{
		"container":       regVM.ContainerID,
		"control-channel": regVM.CtlSerial,
		"io-channel":      regVM.IoSerial,
		"console":         regVM.Console,
	}).Info("restoring state")

	if regVM.Console != "" && proxy.enableVMConsole {
		vm.setConsole(regVM.Console)
	}

	if err := restoreIoSessionsWaitForShim(proxy, vm, vmState); err != nil {
		logContID(containerID).Errorf("error restoring tokens: %v", err)
		if err := delVMAndState(proxy, vm); err != nil {
			logContID(containerID).Errorf("failed to delete vm's "+
				"state: %v", err)
		}
		return false
	}

	if err := vm.Reconnect(true); err != nil {
		logContID(containerID).Errorf("failed to connect: %v", err)
		if err := delVMAndState(proxy, vm); err != nil {
			logContID(containerID).Errorf("failed to delete vm's "+
				"state: %v", err)
		}
		return false
	}

	// We start one goroutine per-VM to monitor the qemu process
	proxy.wg.Add(1)
	go func() {
		<-vm.OnVMLost()
		vm.Close()
		proxy.wg.Done()
	}()

	return true
}
