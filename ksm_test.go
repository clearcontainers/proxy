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
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

const ksmString = "ksmrules"
const anonPagesMemory = 16777216 // Typically 4096 pages

func ksmTestPrepare() error {
	newKSMRoot, err := ioutil.TempDir("", "cc-ksm-test")
	if err != nil {
		return err
	}

	defaultKSMRoot = newKSMRoot

	memInfoFile, err := ioutil.TempFile("", "cc-ksm-meminfo")
	if err != nil {
		return err
	}

	memInfo = memInfoFile.Name()

	_, err = memInfoFile.WriteString(fmt.Sprintf("AnonPages: %v kB", anonPagesMemory))
	if err != nil {
		return err
	}

	ksmTestRun, err := os.Create(filepath.Join(defaultKSMRoot, ksmRunFile))
	if err != nil {
		return err
	}

	ksmTestPagesToScan, err := os.Create(filepath.Join(defaultKSMRoot, ksmPagesToScan))
	if err != nil {
		return err
	}

	ksmTestSleepMillisec, err := os.Create(filepath.Join(defaultKSMRoot, ksmSleepMillisec))
	if err != nil {
		return err
	}

	defer ksmTestRun.Close()
	defer ksmTestPagesToScan.Close()
	defer ksmTestSleepMillisec.Close()

	return nil
}

func ksmTestCleanup() {
	os.RemoveAll(defaultKSMRoot)
	os.RemoveAll(memInfo)
}

func TestKSMSysfsAttributeOpen(t *testing.T) {
	pagesToScanSysFs := sysfsAttribute{
		path: filepath.Join(defaultKSMRoot, ksmPagesToScan),
	}

	err := pagesToScanSysFs.open()
	defer pagesToScanSysFs.close()

	assert.Nil(t, err)
}

func TestKSMSysfsAttributeOpenNonExistent(t *testing.T) {
	pagesToScanSysFs := sysfsAttribute{
		path: filepath.Join(defaultKSMRoot, "foo"),
	}

	err := pagesToScanSysFs.open()
	defer pagesToScanSysFs.close()

	assert.NotNil(t, err)
}

func TestKSMSysfsAttributeReadWrite(t *testing.T) {
	pagesToScanSysFs := sysfsAttribute{
		path: filepath.Join(defaultKSMRoot, ksmPagesToScan),
	}

	err := pagesToScanSysFs.open()
	defer pagesToScanSysFs.close()

	assert.Nil(t, err)

	err = pagesToScanSysFs.write(ksmString)
	assert.Nil(t, err)

	s, err := pagesToScanSysFs.read()
	assert.Nil(t, err)
	assert.NotNil(t, s)
	assert.Equal(t, s, ksmString, "Wrong sysfs read: %s", s)
}

func initKSM(root string, t *testing.T) *ksm {
	k, err := newKSM(root)
	assert.Nil(t, err)

	return k
}

func TestKSMAvailabilityDummy(t *testing.T) {
	_, err := newKSM("foo")
	assert.NotNil(t, err)
}

func TestKSMAvailability(t *testing.T) {
	k := initKSM(defaultKSMRoot, t)

	err := k.isAvailable()
	assert.Nil(t, err)
}

func TestKSMAnonPages(t *testing.T) {
	pageSize := (int64)(os.Getpagesize())
	expectedAnonPages := (anonPagesMemory * 1024) / pageSize

	anonPages, err := anonPages()
	assert.Nil(t, err)
	assert.Equal(t, expectedAnonPages, anonPages, "Anonymous pages mismatch")
}

func TestKSMPagesToScan(t *testing.T) {
	setting, valid := ksmSettings[ksmAggressive]
	assert.True(t, valid)

	anonPages, err := anonPages()
	assert.Nil(t, err)
	expectedPagesToScan := fmt.Sprintf("%v", anonPages/setting.pagesPerScanFactor)

	pagesToScan, err := setting.pagesToScan()
	assert.Nil(t, err)
	assert.Equal(t, pagesToScan, expectedPagesToScan, "")
}

func TestKSMPagesToScanInvalidSetting(t *testing.T) {
	setting := ksmSetting{
		pagesPerScanFactor: 0,
	}

	_, err := setting.pagesToScan()
	assert.NotNil(t, err)
}
