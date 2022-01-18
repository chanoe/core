/*
Copyright 2021 The tKeel Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package statem

// statem: state machine.

import (
	"context"
	"runtime"
	"sort"
	"sync/atomic"

	"github.com/tkeel-io/core/pkg/constraint"
	zfield "github.com/tkeel-io/core/pkg/logger"
	"github.com/tkeel-io/core/pkg/mapper"
	"github.com/tkeel-io/core/pkg/util"
	"github.com/tkeel-io/kit/log"
	"go.uber.org/zap"
)

const (
	// statem runtime-status enumerates.
	StateRuntimeDetached int32 = 0
	StateRuntimeAttached int32 = 1

	// state machine default configurations.
	defaultEnsureConsumeTimes int32 = 3
	defaultStateFlushPeried   int32 = 10
	defaultRetryPutMessageNum int   = 5

	// statem status enumerates.
	SMStatusActive   Status = "active"
	SMStatusInactive Status = "inactive"
	SMStatusDeleted  Status = "deleted"
)

// statem state marchins.
type statem struct {
	// state basic fields.
	Base
	// other state machine property cache.
	cacheProps map[string]map[string]constraint.Node // cache other property.

	// mapper & tentacles.
	mappers   map[string]mapper.Mapper      // key=mapperId
	tentacles map[string][]mapper.Tentacler // key=Sid#propertyKey

	// parse from Configs.
	constraints        map[string]*constraint.Constraint
	searchConstraints  sort.StringSlice
	tseriesConstraints sort.StringSlice

	// state machine mailbox.
	mailbox *mailbox
	// state manager.
	stateManager StateManager

	status             Status
	attached           int32
	nextFlushNum       int32
	ensureComsumeTimes int32
	// state machine message handler.
	msgHandler MessageHandler

	// state Context.
	sCtx   StateContext
	ctx    context.Context
	cancel context.CancelFunc
}

// NewState create an statem object.
func NewState(ctx context.Context, stateManager StateManager, in *Base, msgHandler MessageHandler) (StateMachiner, error) {
	if in.ID == "" {
		in.ID = util.UUID()
	}

	ctx, cancel := context.WithCancel(ctx)

	state := &statem{
		Base: in.Copy(),

		ctx:                ctx,
		cancel:             cancel,
		mailbox:            newMailbox(20),
		status:             SMStatusActive,
		msgHandler:         msgHandler,
		stateManager:       stateManager,
		nextFlushNum:       defaultStateFlushPeried,
		ensureComsumeTimes: defaultEnsureConsumeTimes,
		mappers:            make(map[string]mapper.Mapper),
		cacheProps:         make(map[string]map[string]constraint.Node),
		constraints:        make(map[string]*constraint.Constraint),
	}

	// initialize Properties.
	if nil == state.Base.Properties {
		state.Properties = make(map[string]constraint.Node)
	}

	// initialize Configs.
	if nil == state.Configs {
		state.Configs = make(map[string]constraint.Config)
	}

	// set properties into cacheProps.
	state.cacheProps[in.ID] = state.Properties

	if nil == msgHandler {
		// use default message handler.
		state.msgHandler = state.internelMessageHandler
	}

	return state, nil
}

// GetID returns state ID.
func (s *statem) GetID() string {
	return s.ID
}

// GetBase return state basic info.
func (s *statem) GetBase() *Base {
	return &s.Base
}

// GetStatus returns state machine status.
func (s *statem) GetStatus() Status {
	return s.status
}

// WithContext set state Context.
func (s *statem) WithContext(ctx StateContext) StateMachiner {
	s.sCtx = ctx
	return s
}

// OnMessage recive statem input messages.
func (s *statem) OnMessage(msg Message) bool {
	var attachFlag bool
	switch s.status {
	case SMStatusDeleted:
		log.Info("statem.OnMessage",
			zfield.Eid(s.ID),
			zfield.Status(string(s.status)),
			zfield.Reason("state machine deleted"))
		return false

	default:
		for i := 0; i < defaultRetryPutMessageNum; i++ {
			if err := s.mailbox.Put(msg); nil == err {
				if atomic.CompareAndSwapInt32(&s.attached,
					StateRuntimeDetached, StateRuntimeAttached) {
					attachFlag = true
				}
				break
			}
			runtime.Gosched()
		}
	}

	return attachFlag
}

// HandleLoop run loopHandler.
func (s *statem) HandleLoop() {
	var message Message
	var ensureComsumeTimes = s.ensureComsumeTimes
	log.Debug("actor attached", zfield.ID(s.ID))

	for {
		if s.nextFlushNum == 0 {
			// flush properties.
			if err := s.flush(s.ctx); nil != err {
				log.Error("flush state properties", zfield.ID(s.ID), zap.Error(err))
			}
		}

		// consume message from mailbox.
		if s.mailbox.Empty() {
			if ensureComsumeTimes > 0 {
				ensureComsumeTimes--
				runtime.Gosched()
				continue
			}

			// detach this statem.
			if !atomic.CompareAndSwapInt32(&s.attached, StateRuntimeAttached, StateRuntimeDetached) {
				log.Error("exception occurred, mismatched statem runtime status.",
					zfield.Status(stateRuntimeStatusString(atomic.LoadInt32(&s.attached))))
			}

			// flush properties.
			if err := s.flush(s.ctx); nil != err {
				log.Error("flush state properties failed", zfield.ID(s.ID), zap.Error(err))
			}
			// detaching.
			break
		}

		message = s.mailbox.Get()
		switch msg := message.(type) {
		default:
			// handle message.
			watchKeys := s.msgHandler(msg)
			// active tentacles.
			s.activeTentacle(watchKeys)
		}

		message.Promised(s)

		// reset be sure.
		ensureComsumeTimes = s.ensureComsumeTimes
		s.nextFlushNum = (s.nextFlushNum + defaultStateFlushPeried - 1) % defaultStateFlushPeried
	}

	log.Info("detached statem.", zfield.ID(s.ID), zfield.Type(s.Type))
}

// stateRuntimeStatusString convert actor status.
func stateRuntimeStatusString(statusNum int32) string {
	switch statusNum {
	case StateRuntimeDetached:
		return "detached"
	case StateRuntimeAttached:
		return "attached"
	default:
		return "undefine"
	}
}

type MapperDesc struct {
	Name      string `json:"name"`
	TQLString string `json:"tql"` //nolint
}
