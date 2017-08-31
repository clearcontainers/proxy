//
// Copyright (c) 2017 Intel Corporation
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
//

package main

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"
	"syscall"
)

type ksmSetting struct {
	// run describes if we want KSM to be on or off.
	run bool

	// pagesPerScanFactor describes how many pages we want
	// to scan per KSM run.
	// ksmd will san N pages, where N*pagesPerScanFactor is
	// equal to the number of anonymous pages.
	pagesPerScanFactor uint32

	// scanIntervalMS is the KSM scan interval in milliseconds.
	scanIntervalMS uint32
}

func (s ksmSetting) pagesToScan() (string, error) {
	return "10000", nil
}

const (
	ksmInitial    = "initial"
	ksmOff        = "off"
	ksmSlow       = "slow"
	ksmStandard   = "standard"
	ksmAggressive = "aggressive"
)

var ksmSettings = map[string]ksmSetting{
	ksmOff:        {false, 1000, 500}, // Turn KSM off
	ksmSlow:       {true, 500, 100},   // Every 100ms, we scan 1 page for every 500 pages available in the system
	ksmStandard:   {true, 100, 10},    // Every 10ms, we scan 1 page for every 100 pages available in the system
	ksmAggressive: {true, 10, 1},      // Every ms, we scan 1 page for every 10 pages available in the system
}

var defaultKSMRoot = "/sys/kernel/mm/ksm/"
var errKSMUnavailable = errors.New("KSM is unavailable")

const (
	ksmRunFile        = "run"
	ksmPagesToScan    = "pages_to_scan"
	ksmSleepMillisec  = "sleep_millisecs"
	ksmStart          = "1"
	ksmStop           = "0"
	defaultKSMSetting = ksmInitial
)

type sysfsAttribute struct {
	path string
	file *os.File
}

func (attr *sysfsAttribute) open() error {
	file, err := os.OpenFile(attr.path, os.O_RDWR|syscall.O_NONBLOCK, 0660)
	attr.file = file
	return err
}

func (attr *sysfsAttribute) close() error {
	err := attr.file.Close()
	attr.file = nil
	return err
}

func (attr *sysfsAttribute) read() (string, error) {
	_, err := attr.file.Seek(0, os.SEEK_SET)
	if err != nil {
		return "", err
	}

	data, err := ioutil.ReadAll(attr.file)
	if err != nil {
		return "", err
	}

	return string(data), nil
}

func (attr *sysfsAttribute) write(value string) error {
	_, err := attr.file.Seek(0, os.SEEK_SET)
	if err != nil {
		return err
	}
	_, err = attr.file.WriteString(value)

	return err
}

type ksm struct {
	root          string
	run           sysfsAttribute
	pagesToScan   sysfsAttribute
	sleepInterval sysfsAttribute
	initialized   bool

	initialPagesToScan   string
	initialSleepInterval string
	initialKSMRun        string

	sync.Mutex
}

func (k *ksm) isAvailable() error {
	info, err := os.Stat(k.root)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("%s is not available", k.root)
	}

	return nil
}

func newKSM(root string) (*ksm, error) {
	var err error
	var k ksm

	k.initialized = false
	k.root = root

	if root == "" {
		return nil, errors.New("Invalid KSM root")
	}

	if err := k.isAvailable(); err != nil {
		return nil, err
	}

	k.pagesToScan = sysfsAttribute{
		path: filepath.Join(k.root, ksmPagesToScan),
	}

	k.sleepInterval = sysfsAttribute{
		path: filepath.Join(k.root, ksmSleepMillisec),
	}

	k.run = sysfsAttribute{
		path: filepath.Join(k.root, ksmRunFile),
	}

	defer func(err error) {
		if err != nil {
			_ = k.run.close()
			_ = k.sleepInterval.close()
			_ = k.pagesToScan.close()
		}
	}(err)

	if err := k.run.open(); err != nil {
		return nil, err
	}

	if err := k.sleepInterval.open(); err != nil {
		return nil, err
	}

	if err := k.pagesToScan.open(); err != nil {
		return nil, err
	}

	k.initialPagesToScan, err = k.pagesToScan.read()
	if err != nil {
		return nil, err
	}

	k.initialSleepInterval, err = k.sleepInterval.read()
	if err != nil {
		return nil, err
	}

	k.initialKSMRun, err = k.run.read()
	if err != nil {
		return nil, err
	}

	k.initialized = true
	return &k, nil
}

func (k *ksm) restore() error {
	var err error

	k.Lock()
	defer k.Unlock()

	if !k.initialized {
		return errKSMUnavailable
	}

	if err = k.pagesToScan.write(k.initialPagesToScan); err != nil {
		return err
	}

	if err = k.sleepInterval.write(k.initialSleepInterval); err != nil {
		return err
	}

	if err = k.run.write(k.initialKSMRun); err != nil {
		return err
	}

	if err := k.run.close(); err != nil {
		return err
	}

	if err := k.sleepInterval.close(); err != nil {
		return err
	}

	if err := k.pagesToScan.close(); err != nil {
		return err
	}

	k.initialized = false
	return nil
}

func (k *ksm) tune(s ksmSetting) error {
	k.Lock()
	defer k.Unlock()

	if !k.initialized {
		return errKSMUnavailable
	}

	if !s.run {
		return k.run.write(ksmStop)
	}

	newPagesToScan, err := s.pagesToScan()
	if err != nil {
		return err
	}

	if err = k.run.write(ksmStop); err != nil {
		return err
	}

	if err = k.pagesToScan.write(newPagesToScan); err != nil {
		return err
	}

	if err = k.sleepInterval.write(fmt.Sprintf("%v", s.scanIntervalMS)); err != nil {
		return err
	}

	if err = k.run.write(ksmStart); err != nil {
		return err
	}

	return nil
}
