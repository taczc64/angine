// Copyright 2017 ZhongAn Information Technology Services Co.,Ltd.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package trace

import (
	"bytes"
	"fmt"

	"github.com/spf13/viper"
	"go.uber.org/zap"

	"github.com/annchain/angine/types"
	"github.com/annchain/ann-module/lib/go-p2p"
	"github.com/annchain/ann-module/lib/go-wire"
)

const (
	// SpecialOPChannel is for special operation only
	SpecialOPChannel = byte(0x50)

	maxTraceMessageSize = 1048576
)

type channelAttribute struct {
	ValidatorOnly bool
}

type Reactor struct {
	// every reactor implements p2p.BaseReactor
	p2p.BaseReactor

	config *viper.Viper
	logger *zap.Logger

	evsw types.EventSwitch

	// router keeps the state of all tracks
	router *Router

	channelAttributes map[byte]channelAttribute
}

func newChannelAttributes(r *Reactor) map[byte]channelAttribute {
	ret := make(map[byte]channelAttribute)
	ret[SpecialOPChannel] = channelAttribute{
		ValidatorOnly: true,
	}
	return ret
}

func NewTraceReactor(logger *zap.Logger, config *viper.Viper, r *Router) *Reactor {
	tr := &Reactor{
		logger:            logger,
		config:            config,
		router:            r,
		channelAttributes: make(map[byte]channelAttribute),
	}
	tr.BaseReactor = *p2p.NewBaseReactor(logger, "TraceReactor", tr)
	tr.channelAttributes = newChannelAttributes(tr)
	return tr
}

func (tr *Reactor) OnStart() error {
	return tr.BaseReactor.OnStart()
}

func (tr *Reactor) OnStop() {
	tr.BaseReactor.OnStop()
}

// GetChannels returns channel descriptor for all supported channels.
// maybe more in the furture
func (tr *Reactor) GetChannels() []*p2p.ChannelDescriptor {
	return []*p2p.ChannelDescriptor{
		&p2p.ChannelDescriptor{
			ID:                SpecialOPChannel,
			Priority:          1,
			SendQueueCapacity: 100,
		},
	}
}

// AddPeer has nothing to do
func (tr *Reactor) AddPeer(peer *p2p.Peer) {}

// RemovePeer has nothing to do
func (tr *Reactor) RemovePeer(peer *p2p.Peer, reason interface{}) {}

// Receive is the main entrance of handling data flow
func (tr *Reactor) Receive(chID byte, src *p2p.Peer, msgBytes []byte) {
	_, msg, err := DecodeMessage(msgBytes)
	if err != nil {
		tr.logger.Warn("error decoding message", zap.Error(err))
		return
	}

	// different channel different strategy,
	// mainly focus on how to broadcast the message
	switch chID {
	case SpecialOPChannel:
		switch msg := msg.(type) {
		case *traceRequest:
			broadcast, err := tr.router.TraceRequest(HashMessage(msg), src.PubKey[:], msg.Data, tr.channelAttributes[SpecialOPChannel].ValidatorOnly)
			if err != nil {
				// process error
				return
			}
			if broadcast {
				for _, p := range tr.Switch.Peers().List() {
					if !p.Equals(src) {
						p.Send(SpecialOPChannel, struct{ Message }{msg})
					}
				}
			}
		case *traceResponse:
			pk, err := tr.router.TraceRespond(msg.RequestHash, msg.Resp)
			if err != nil {
				// process error
				return
			}
			if pk != nil {
				peer := tr.Switch.Peers().Get(fmt.Sprintf("%X", pk))
				peer.Send(SpecialOPChannel, struct{ Message }{msg})
			}
		}
	default:
		// by default, we have nothing to do
	}
}

func (tr *Reactor) SetEventSwitch(evsw types.EventSwitch) {
	tr.evsw = evsw
}

const (
	msgTypeTraceRequest  = byte(0x11)
	msgTypeTraceResponse = byte(0x12)
)

func DecodeMessage(bz []byte) (msgType byte, msg Message, err error) {
	msgType = bz[0]
	n := new(int)
	r := bytes.NewReader(bz)
	msg = wire.ReadBinary(struct{ Message }{}, r, maxTraceMessageSize, n, &err).(struct{ Message }).Message
	return
}

func HashMessage(o Message) []byte {
	return wire.BinarySha256(o)
}

// TraceMessage is just a wrapper which conforms to the usage of wire.RegisterInterface
type Message interface{}

var _ = wire.RegisterInterface(
	struct{ Message }{},
	wire.ConcreteType{
		O:    &traceRequest{},
		Byte: msgTypeTraceRequest,
	},
	wire.ConcreteType{
		O:    &traceResponse{},
		Byte: msgTypeTraceResponse,
	},
)

type traceRequest struct {
	Data []byte
}

type traceResponse struct {
	RequestHash []byte
	Resp        []byte
}
