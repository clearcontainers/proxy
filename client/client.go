// Copyright (c) 2016 Intel Corporation
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

package client

import (
	"encoding/json"
	"errors"
	"net"

	"github.com/clearcontainers/proxy/api"
)

// The Client struct can be used to issue proxy API calls with a convenient
// high level API.
type Client struct {
	conn *net.UnixConn
}

// NewClient creates a new client object to communicate with the proxy using
// the connection conn. The user should call Close() once finished with the
// client object to close conn.
func NewClient(conn *net.UnixConn) *Client {
	return &Client{
		conn: conn,
	}
}

// Close a client, closing the underlying AF_UNIX socket.
func (client *Client) Close() {
	client.conn.Close()
}

func (client *Client) sendPayload(id string, payload interface{}) (*api.Response, error) {
	var err error

	req := api.Request{}
	req.ID = id
	if payload != nil {
		if req.Data, err = json.Marshal(payload); err != nil {
			return nil, err
		}
	}

	if err := api.WriteMessage(client.conn, &req); err != nil {
		return nil, err
	}

	resp := api.Response{}
	if err := api.ReadMessage(client.conn, &resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

func errorFromResponse(resp *api.Response) error {
	// We should always have an error with the response, but better safe
	// than sorry.
	if resp.Success == false {
		if resp.Error != "" {
			return errors.New(resp.Error)
		}

		return errors.New("unknown error")
	}

	return nil
}

// RegisterVMOptions holds extra arguments one can pass to the RegisterVM
// function.
//
// See the api.RegisterVM payload for more details.
type RegisterVMOptions struct {
	Console string
}

// RegisterVMReturn contains the return values from RegisterVM.
//
// See the api.RegisterVM and api.RegisterVMResponse payloads.
type RegisterVMReturn struct {
	Version int
}

// RegisterVM wraps the api.RegisterVM payload.
//
// See payload description for more details.
func (client *Client) RegisterVM(containerID, ctlSerial, ioSerial string,
	options *RegisterVMOptions) (*RegisterVMReturn, error) {
	payload := api.RegisterVM{
		ContainerID: containerID,
		CtlSerial:   ctlSerial,
		IoSerial:    ioSerial,
	}

	if options != nil {
		payload.Console = options.Console
	}

	resp, err := client.sendPayload("register", &payload)
	if err != nil {
		return nil, err
	}

	ret := &RegisterVMReturn{}

	val, ok := resp.Data["version"]
	if !ok {
		return nil, errors.New("RegisterVM: no version in response")
	}
	ret.Version = int(val.(float64))

	return ret, errorFromResponse(resp)
}

// AttachVMOptions holds extra arguments one can pass to the AttachVM function.
//
// See the api.AttachVM payload for more details.
type AttachVMOptions struct {
}

// AttachVMReturn contains the return values from AttachVM.
//
// See the api.AttachVM and api.AttachVMResponse payloads.
type AttachVMReturn struct {
	Version int
}

// AttachVM wraps the api.AttachVM payload.
//
// See the api.AttachVM payload description for more details.
func (client *Client) AttachVM(containerID string, options *AttachVMOptions) (*AttachVMReturn, error) {
	payload := api.AttachVM{
		ContainerID: containerID,
	}

	resp, err := client.sendPayload("attach", &payload)
	if err != nil {
		return nil, err
	}

	ret := &AttachVMReturn{}

	val, ok := resp.Data["version"]
	if !ok {
		return nil, errors.New("attach: no version in response")
	}
	ret.Version = int(val.(float64))

	return ret, errorFromResponse(resp)
}

// Hyper wraps the Hyper payload (see payload description for more details)
func (client *Client) Hyper(hyperName string, hyperMessage interface{}) error {
	var data []byte

	if hyperMessage != nil {
		var err error

		data, err = json.Marshal(hyperMessage)
		if err != nil {
			return err
		}
	}

	hyper := api.Hyper{
		HyperName: hyperName,
		Data:      data,
	}

	resp, err := client.sendPayload("hyper", &hyper)
	if err != nil {
		return err
	}

	return errorFromResponse(resp)
}

// UnregisterVM wraps the api.UnregisterVM payload.
//
// See the api.UnregisterVM payload description for more details.
func (client *Client) UnregisterVM(containerID string) error {
	payload := api.UnregisterVM{
		ContainerID: containerID,
	}

	resp, err := client.sendPayload("unregister", &payload)
	if err != nil {
		return err
	}

	return errorFromResponse(resp)
}
