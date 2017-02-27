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

package api

import (
	"encoding/json"
)

// The RegisterVM payload is issued first after connecting to the proxy socket.
// It is used to let the proxy know about a new container on the system along
// with the paths go hyperstart's command and I/O channels (AF_UNIX sockets).
//
// Console can be used to indicate the path of a socket linked to the VM
// console. The proxy can output this data when asked for verbose output.
//
//  {
//   "containerId": "756535dc6e9ab9b560f84c8...",
//   "ctlSerial": "/tmp/sh.hyper.channel.0.sock",
//   "ioSerial": "/tmp/sh.hyper.channel.1.sock"
//  }
type RegisterVM struct {
	ContainerID string `json:"containerId"`
	CtlSerial   string `json:"ctlSerial"`
	IoSerial    string `json:"ioSerial"`
	Console     string `json:"console,omitempty"`
}

// RegisterVMResponse is the result from a successful RegisterVM.
//
//  {
//  }
type RegisterVMResponse struct {
}

// The AttachVM payload can be used to associate clients to an already known
// VM. AttachVM cannot be issued if a RegisterVM for this container hasn't been
// issued beforehand.
//
//  {
//    "containerId": "756535dc6e9ab9b560f84c8..."
//  }
type AttachVM struct {
	ContainerID string `json:"containerId"`
}

// AttachVMResponse is the result from a successful AttachVM.
//
//  {
//  }
type AttachVMResponse struct {
}

// The UnregisterVM payload does the opposite of what RegisterVM does,
// indicating to the proxy it should release resources created by RegisterVM
// for the container identified by containerId.
//
//  {
//    "containerId": "756535dc6e9ab9b560f84c8..."
//  }
type UnregisterVM struct {
	ContainerID string `json:"containerId"`
}

// The Hyper payload will forward an hyperstart command to hyperstart.
//
//  {
//    "id": "hyper",
//    "data": {
//      "hyperName": "startpod",
//      "data": {
//        "hostname": "clearlinux",
//        "containers": [],
//        "shareDir": "rootfs"
//      }
//    }
//  }
type Hyper struct {
	HyperName string          `json:"hyperName"`
	Data      json.RawMessage `json:"data,omitempty"`
}

// ErrorResponse is the payload send in Responses where the Error flag is set.
type ErrorResponse struct {
	Message string `json:"msg"`
}
