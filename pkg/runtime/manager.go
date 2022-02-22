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

package runtime

import (
	"context"
	"sync"

	"github.com/pkg/errors"
	"github.com/tkeel-io/core/pkg/config"
	"github.com/tkeel-io/core/pkg/dispatch"
	xerrors "github.com/tkeel-io/core/pkg/errors"
	"github.com/tkeel-io/core/pkg/inbox"
	zfield "github.com/tkeel-io/core/pkg/logger"
	"github.com/tkeel-io/core/pkg/placement"
	"github.com/tkeel-io/core/pkg/runtime/environment"
	"github.com/tkeel-io/core/pkg/runtime/message"
	"github.com/tkeel-io/core/pkg/runtime/state"
	"github.com/tkeel-io/core/pkg/types"
	"github.com/tkeel-io/kit/log"
	"go.uber.org/zap"
)

type Manager struct {
	containers      map[string]*Container
	actorEnv        environment.IEnvironment
	resourceManager types.ResourceManager
	dispatcher      dispatch.Dispatcher
	inboxes         map[string]inbox.Inboxer

	shutdown chan struct{}
	lock     sync.RWMutex
	ctx      context.Context
	cancel   context.CancelFunc
}

func NewManager(ctx context.Context, resourceManager types.ResourceManager, dispatcher dispatch.Dispatcher) (types.Manager, error) {
	ctx, cancel := context.WithCancel(ctx)
	stateManager := &Manager{
		ctx:             ctx,
		cancel:          cancel,
		inboxes:         make(map[string]inbox.Inboxer),
		actorEnv:        environment.NewEnvironment(),
		containers:      make(map[string]*Container),
		dispatcher:      dispatcher,
		resourceManager: resourceManager,
		lock:            sync.RWMutex{},
	}

	return stateManager, nil
}

func (m *Manager) Start() error {
	log.Info("start runtime manager")
	m.initializeMetadata()
	m.initializeSources()
	return nil
}

func (m *Manager) Shutdown() error {
	m.cancel()
	m.shutdown <- struct{}{}
	return nil
}

func (m *Manager) selectContainer(id string) *Container {
	if _, ok := m.containers[id]; !ok {
		m.containers[id] = NewContainer(m.ctx, id, m)
	}

	return m.containers[id]
}

func (m *Manager) handleMessage(ctx context.Context, msgCtx message.Context) error {
	reqID := msgCtx.Get(message.ExtAPIRequestID)
	entityID := msgCtx.Get(message.ExtEntityID)
	channelID, _ := ctx.Value(inbox.IDKey{}).(string)
	log.Debug("dispose message", zfield.ID(entityID), zfield.Message(msgCtx))

	var flagCreate bool
	container := m.selectContainer(channelID)
	machine, err := container.Load(ctx, entityID)
	if nil != err {
		if !errors.Is(err, xerrors.ErrEntityNotFound) {
			log.Error("undefine error, load state machine", zfield.ReqID(reqID),
				zap.Error(err), zfield.ID(entityID), zfield.Channel(channelID))
			return xerrors.ErrInternal
		}

		flagCreate = true
		// state machine not exists, then create.
		enDao := message.ParseEntityFrom(msgCtx)
		if machine, err = container.MakeMachine(enDao); nil != err {
			log.Error("create state machine", zfield.Channel(channelID),
				zfield.ReqID(reqID), zfield.ID(entityID), zap.Error(err))
			return xerrors.ErrInternal
		}
	}

	log.Debug("handle message",
		zfield.Eid(entityID),
		zfield.ReqID(reqID),
		zfield.Channel(channelID),
		zfield.Header(msgCtx.Attributes()),
		zfield.Message(string(msgCtx.Message())))

	if err = machine.Invoke(ctx, msgCtx); nil != err {
		log.Error("handle message", zap.Error(err),
			zfield.ID(entityID), zfield.ReqID(reqID),
			zfield.Message(string(msgCtx.Message())),
			zfield.Channel(channelID), zfield.Header(msgCtx.Attributes()))
	} else if flagCreate {
		container.Add(machine)
	}

	return errors.Wrap(err, "handle message")
}

// Resource return resource manager.
func (m *Manager) Resource() types.ResourceManager {
	return m.resourceManager
}

func (m *Manager) loadMachine(stateID, stateType string) {
	switch stateType {
	case SMTypeSubscription:
		// TODO: load subscription.
	default:
	}
}

func (m *Manager) reloadMachineEnv(stateIDs []string) {
	for _, stateID := range stateIDs {
		// load state machine.
		queue := placement.Global().Select(stateID)
		container := m.selectContainer(queue.ID)

		log.Debug("reload state machine", zfield.Eid(stateID), zfield.Queue(queue))
		if config.Get().Server.Name != queue.NodeName {
			continue
		}

		// load state machine.
		var has bool
		var machine state.Machiner
		if machine, has = container.Get(stateID); !has {
			log.Debug("load state machine, runtime not found",
				zfield.Queue(queue), zfield.Eid(stateID))
			continue
		}

		// update state machine context.
		stateEnv := m.actorEnv.GetStateEnv(stateID)
		machine.Context().LoadEnvironments(stateEnv)

		// TODO: 初始化新创建的mapper.
	}
}

// Tools.

func (m *Manager) EscapedEntities(expression string) []string {
	return []string{expression}
}
