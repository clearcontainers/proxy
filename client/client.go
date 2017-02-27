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
	"fmt"
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

func (client *Client) sendCommand(cmd api.Command, payload interface{}) (*api.Frame, error) {
	var data []byte
	var frame *api.Frame
	var err error

	if payload != nil {
		if data, err = json.Marshal(payload); err != nil {
			return nil, err
		}
	}

	if err := api.WriteCommand(client.conn, cmd, data); err != nil {
		return nil, err
	}

	if frame, err = api.ReadFrame(client.conn); err != nil {
		return nil, err
	}

	if frame.Header.Type != api.TypeResponse {
		return nil, fmt.Errorf("unexepected frame type %v", frame.Header.Type)
	}

	if frame.Header.Opcode != int(cmd) {
		return nil, fmt.Errorf("unexepected opcode %v", frame.Header.Opcode)
	}

	return frame, nil
}

func errorFromResponse(resp *api.Frame) error {
	// We should always have an error with the response, but better safe
	// than sorry.
	if !resp.Header.InError {
		return nil
	}

	decoded := api.ErrorResponse{}
	if err := json.Unmarshal(resp.Payload, &decoded); err != nil {
		return err
	}

	if decoded.Message == "" {
		return errors.New("unknown error")
	}

	return errors.New(decoded.Message)
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

	resp, err := client.sendCommand(api.CmdRegisterVM, &payload)
	if err != nil {
		return nil, err
	}

	if err := errorFromResponse(resp); err != nil {
		return nil, err
	}

	return &RegisterVMReturn{}, nil
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
}

// AttachVM wraps the api.AttachVM payload.
//
// See the api.AttachVM payload description for more details.
func (client *Client) AttachVM(containerID string, options *AttachVMOptions) (*AttachVMReturn, error) {
	payload := api.AttachVM{
		ContainerID: containerID,
	}

	resp, err := client.sendCommand(api.CmdAttachVM, &payload)
	if err != nil {
		return nil, err
	}

	if err := errorFromResponse(resp); err != nil {
		return nil, err
	}

	return &AttachVMReturn{}, nil
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

	resp, err := client.sendCommand(api.CmdHyper, &hyper)
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

	resp, err := client.sendCommand(api.CmdUnregisterVM, &payload)
	if err != nil {
		return err
	}

	return errorFromResponse(resp)
}
