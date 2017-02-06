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

// Package api defines the API cc-proxy exposes to clients (processes
// connecting to the proxy AF_UNIX socket).
//
// This package contains the low level definitions of the protocol, the message
// structure and the various payloads that can be sent and received.
//
// The proxy protocol is composed of messages: requests and responses. These
// form a small RPC protocol, requests being similar to a function call and
// responses encoding the result of the call.
//
// Each message is composed of a header and some optional data:
//
//  ┌────────────────┬────────────────┬──────────────────────────────┐
//  │  Data Length   │    Reserved    │  Data (request or response)  │
//  │   (32 bits)    │    (32 bits)   │     (data length bytes)      │
//  └────────────────┴────────────────┴──────────────────────────────┘
//
// - Data Length is in bytes and encoded in network order.
//
// - Reserved is reserved for future use.
//
// - Data is the JSON-encoded request or response data
//
// On top of of this request/response mechanism, the proxy defines payloads,
// which are effectively the various function calls defined in the API.
//
// Requests have 2 fields: the payload id (function name) and its data
// (function argument(s))
//
//  type Request struct {
//      Id    string          `json:"id"`
//      Data *json.RawMessage `json:"data,omitempty"`
//  }
//
// Responses have 3 fields: success, error and data.
//
//  type Response struct {
//      Success bool                   `json:"success"`
//      Error   string                 `json:"error,omitempty"`
//      Data    map[string]interface{} `json:"data,omitempty"`
//  }
//
// Unsurprisingly, the response has the result of a command, with success
// indicating if the request has succeeded for not. If success is true, the
// response can carry additional return values in data. If success if false,
// error will contain an error string suitable for reporting the error to a
// user.
//
// As a concrete example, here is an exchange between a client (runtime) and
// the proxy:
//
//  runtime → proxy
//  {
//    "id": "hello",
//    "data": {
//      "containerId": "foo",
//      "ctlSerial": "/tmp/sh.hyper.channel.0.sock",
//      "ioSerial": "/tmp/sh.hyper.channel.1.sock"
//    }
//  }
//
//  proxy → runtime
//  {
//    "success":true
//  }
//
// - The client starts by calling the hello payload, registered the container
// foo and asking the proxy to connect to hyperstart communication channels
// given
//
// - The proxy answers the function call has succeeded
package api
