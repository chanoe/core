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
package manager

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/pkg/errors"
	v1 "github.com/tkeel-io/core/api/core/v1"
	"github.com/tkeel-io/core/pkg/config"
	"github.com/tkeel-io/core/pkg/dispatch"
	xerrors "github.com/tkeel-io/core/pkg/errors"
	zfield "github.com/tkeel-io/core/pkg/logger"
	"github.com/tkeel-io/core/pkg/manager/holder"
	"github.com/tkeel-io/core/pkg/repository"
	"github.com/tkeel-io/core/pkg/repository/dao"
	"github.com/tkeel-io/core/pkg/types"
	"github.com/tkeel-io/core/pkg/util"
	"github.com/tkeel-io/kit/log"
	"github.com/tkeel-io/tdtl"
	"go.uber.org/zap"
)

const respondFmt = "http://%s:%d/v1/respond"
const (
	sysET = string(v1.ETSystem)
	enET  = string(v1.ETEntity)
)

type apiManager struct {
	holder     holder.Holder
	dispatcher dispatch.Dispatcher
	entityRepo repository.IRepository

	lock   sync.RWMutex
	ctx    context.Context
	cancel context.CancelFunc
}

func New(
	ctx context.Context,
	repo repository.IRepository,
	dispatcher dispatch.Dispatcher) (APIManager, error) {
	ctx, cancel := context.WithCancel(ctx)
	apiManager := &apiManager{
		ctx:        ctx,
		cancel:     cancel,
		entityRepo: repo,
		dispatcher: dispatcher,
		lock:       sync.RWMutex{},
		holder:     holder.New(ctx, 30*time.Second),
	}

	return apiManager, nil
}

func (m *apiManager) OnRespond(ctx context.Context, resp *holder.Response) {
	m.holder.OnRespond(resp)
}

// ------------------------------------APIs-----------------------------.

func (m *apiManager) checkParams(base *Base) error {
	if base.ID == "" {
		base.ID = util.IG().EID()
	}
	return nil
}

func (m *apiManager) callbackAddr() string {
	return fmt.Sprintf(respondFmt, util.ResolveAddr(), config.Get().Proxy.HTTPPort)
}

// CreateEntity create a entity.
func (m *apiManager) CreateEntity(ctx context.Context, en *Base) (*BaseRet, error) {
	var (
		err   error
		bytes []byte
	)

	m.checkParams(en)
	reqID := util.IG().ReqID()
	elapsedTime := util.NewElapsed()
	log.L().Info("entity.CreateEntity", zfield.Eid(en.ID), zfield.Type(en.Type),
		zfield.ReqID(reqID), zfield.Owner(en.Owner), zfield.Source(en.Source), zfield.Base(en.JSON()))

	if bytes, err = en.EncodeJSON(); nil != err {
		log.L().Error("create entity", zfield.Eid(en.ID), zfield.Type(en.Type),
			zfield.ReqID(reqID), zfield.Owner(en.Owner), zfield.Source(en.Source), zfield.Base(en.JSON()))
		return nil, errors.Wrap(err, "create entity")
	}

	// hold request, wait response.
	respWaiter := m.holder.Wait(ctx, reqID)

	// dispatch event.
	if err = m.dispatcher.Dispatch(ctx, &v1.ProtoEvent{
		Id:        util.IG().EvID(),
		Timestamp: time.Now().UnixNano(),
		Callback:  m.callbackAddr(),
		Metadata: map[string]string{
			v1.MetaType:      sysET,
			v1.MetaRequestID: reqID,
			v1.MetaEntityID:  en.ID},
		Data: &v1.ProtoEvent_SystemData{
			SystemData: &v1.SystemData{
				Operator: string(v1.OpCreate),
				Data:     bytes,
			}},
	}); nil != err {
		respWaiter.Cancel()
		log.L().Error("create entity, dispatch event",
			zap.Error(err), zfield.Eid(en.ID), zfield.ReqID(reqID))
		return nil, errors.Wrap(err, "create entity, dispatch event")
	}

	log.L().Debug("holding request, wait response",
		zfield.Eid(en.ID), zfield.ReqID(reqID))

	resp := respWaiter.Wait()
	if resp.Status != types.StatusOK {
		log.L().Error("create entity", zfield.Eid(en.ID), zfield.ReqID(reqID),
			zap.Error(xerrors.New(resp.ErrCode)), zfield.Base(en.JSON()))
		return nil, xerrors.New(resp.ErrCode)
	}

	log.L().Info("processing completed", zfield.Eid(en.ID),
		zfield.ReqID(reqID), zfield.Elapsed(elapsedTime.Elapsed()))

	var baseRet BaseRet
	if err = json.Unmarshal(resp.Data, &baseRet); nil != err {
		log.L().Error("create entity, decode response", zfield.ReqID(reqID),
			zap.Error(err), zfield.Eid(en.ID), zfield.Base(en.JSON()))
		return nil, errors.Wrap(err, "create entity, decode response")
	} else if err = m.addMapper(ctx, &baseRet); nil != err {
		log.L().Error("create entity, decode response, list mapper", zfield.ReqID(reqID),
			zap.Error(err), zfield.Eid(en.ID), zfield.Base(en.JSON()))
		return nil, errors.Wrap(err, "create entity, decode response, list mapper")
	}

	return &baseRet, errors.Wrap(err, "create entity")
}

func (m *apiManager) PatchEntity(ctx context.Context, en *Base, pds []*v1.PatchData, opts ...Option) (out *BaseRet, raw []byte, err error) {
	reqID := util.IG().ReqID()
	elapsedTime := util.NewElapsed()
	log.L().Info("entity.PatchEntity", zfield.Eid(en.ID), zfield.Type(en.Type),
		zfield.ReqID(reqID), zfield.Owner(en.Owner), zfield.Source(en.Source), zfield.Base(en.JSON()))

	// hold request.
	respWaiter := m.holder.Wait(ctx, reqID)

	// setup metadata.
	metadata := Metadata{
		v1.MetaType:      enET,
		v1.MetaEntityID:  en.ID,
		v1.MetaRequestID: reqID}
	// use patch options.
	for _, option := range opts {
		option(metadata)
	}

	// dispatch event.
	if err = m.dispatcher.Dispatch(ctx,
		&v1.ProtoEvent{
			Id:        util.IG().EvID(),
			Metadata:  metadata,
			Timestamp: time.Now().UnixNano(),
			Callback:  m.callbackAddr(),
			Data: &v1.ProtoEvent_Patches{
				Patches: &v1.PatchDatas{Patches: pds}},
		}); nil != err {
		respWaiter.Cancel()
		log.L().Error("patch entity, dispatch event",
			zap.Error(err), zfield.Eid(en.ID), zfield.ReqID(reqID))
		return out, raw, errors.Wrap(err, "patch entity, dispatch event")
	}

	log.L().Debug("holding request, wait response",
		zfield.Eid(en.ID), zfield.ReqID(reqID))

	// wait response.
	resp := respWaiter.Wait()
	if resp.Status != types.StatusOK {
		log.L().Error("patch entity", zfield.Eid(en.ID),
			zap.Error(xerrors.New(resp.ErrCode)), zfield.Base(en.JSON()))
		return out, raw, xerrors.New(resp.ErrCode)
	}

	var baseRet BaseRet
	if err = json.Unmarshal(resp.Data, &baseRet); nil != err {
		log.L().Error("patch entity, decode response",
			zfield.ReqID(reqID), zap.Error(err), zfield.Eid(en.ID),
			zfield.Base(en.JSON()), zfield.Entity(string(resp.Data)))
		return out, raw, errors.Wrap(err, "patch entity, decode response")
	} else if innerErr := m.addMapper(ctx, &baseRet); nil != err {
		log.L().Error("patch entity, decode response, list mapper", zfield.ReqID(reqID),
			zap.Error(innerErr), zfield.Eid(en.ID), zfield.Base(en.JSON()))
	}

	log.L().Info("processing completed", zfield.Eid(en.ID),
		zfield.ReqID(reqID), zfield.Elapsed(elapsedTime.Elapsed()))

	return &baseRet, resp.Data, errors.Wrap(err, "patch entity")
}

// GetProperties returns Base.
func (m *apiManager) GetEntity(ctx context.Context, en *Base) (*BaseRet, error) {
	var err error
	reqID := util.IG().ReqID()
	elapsedTime := util.NewElapsed()
	log.L().Info("entity.GetEntity", zfield.Eid(en.ID), zfield.Type(en.Type),
		zfield.ReqID(reqID), zfield.Owner(en.Owner), zfield.Source(en.Source))

	// hold request.
	respWaiter := m.holder.Wait(ctx, reqID)

	// dispatch event.
	if err = m.dispatcher.Dispatch(ctx,
		&v1.ProtoEvent{
			Id:        util.IG().EvID(),
			Timestamp: time.Now().UnixNano(),
			Callback:  m.callbackAddr(),
			Metadata: map[string]string{
				v1.MetaType:      enET,
				v1.MetaRequestID: reqID,
				v1.MetaEntityID:  en.ID},
			Data: &v1.ProtoEvent_Patches{
				Patches: &v1.PatchDatas{}},
		}); nil != err {
		respWaiter.Cancel()
		log.L().Error("get entity, dispatch event",
			zap.Error(err), zfield.Eid(en.ID), zfield.ReqID(reqID))
		return nil, errors.Wrap(err, "get entity, dispatch event")
	}

	log.L().Debug("holding request, wait response",
		zfield.Eid(en.ID), zfield.ReqID(reqID))

	// wait response.
	resp := respWaiter.Wait()
	if resp.Status != types.StatusOK {
		log.L().Error("get entity", zfield.Eid(en.ID),
			zfield.ReqID(reqID), zap.Error(xerrors.New(resp.ErrCode)))
		return nil, xerrors.New(resp.ErrCode)
	}

	log.L().Info("processing completed", zfield.Eid(en.ID),
		zfield.ReqID(reqID), zfield.Elapsed(elapsedTime.Elapsed()))

	var baseRet BaseRet
	if err = json.Unmarshal(resp.Data, &baseRet); nil != err {
		log.L().Error("get entity, decode response", zfield.ReqID(reqID),
			zap.Error(err), zfield.Eid(en.ID), zfield.Base(en.JSON()))
		return nil, errors.Wrap(err, "create entity, decode response")
	} else if innerErr := m.addMapper(ctx, &baseRet); nil != err {
		log.L().Error("get entity, decode response, list mapper", zfield.ReqID(reqID),
			zap.Error(innerErr), zfield.Eid(en.ID), zfield.Base(en.JSON()))
	}

	return &baseRet, errors.Wrap(err, "get entity")
}

// DeleteEntity delete an entity from manager.
func (m *apiManager) DeleteEntity(ctx context.Context, en *Base) error {
	var err error
	reqID := util.IG().ReqID()
	elapsedTime := util.NewElapsed()
	log.L().Info("entity.DeleteEntity", zfield.Eid(en.ID), zfield.Type(en.Type),
		zfield.ReqID(reqID), zfield.Owner(en.Owner), zfield.Source(en.Source), zfield.Base(en.JSON()))

	// hold request.
	respWaiter := m.holder.Wait(ctx, reqID)

	// dispatch event.
	if err = m.dispatcher.Dispatch(ctx, &v1.ProtoEvent{
		Id:        util.IG().EvID(),
		Timestamp: time.Now().UnixNano(),
		Callback:  m.callbackAddr(),
		Metadata: map[string]string{
			v1.MetaType:      sysET,
			v1.MetaRequestID: reqID,
			v1.MetaEntityID:  en.ID},
		Data: &v1.ProtoEvent_SystemData{
			SystemData: &v1.SystemData{
				Operator: string(v1.OpDelete)},
		}}); nil != err {
		respWaiter.Cancel()
		log.L().Error("delete entity, dispatch event",
			zap.Error(err), zfield.Eid(en.ID), zfield.ReqID(reqID))
		return errors.Wrap(err, "delete entity, dispatch event")
	}

	log.L().Debug("holding request, wait response",
		zfield.Eid(en.ID), zfield.ReqID(reqID))

	// hold request, wait response.

	if resp := respWaiter.Wait(); resp.Status != types.StatusOK {
		log.L().Error("delete entity", zfield.Eid(en.ID),
			zfield.ReqID(reqID), zap.Error(xerrors.New(resp.ErrCode)))
		return xerrors.New(resp.ErrCode)
	}

	log.L().Info("processing completed", zfield.Eid(en.ID),
		zfield.ReqID(reqID), zfield.Elapsed(elapsedTime.Elapsed()))

	return nil
}

// AppendMapper append a mapper into entity.
func (m *apiManager) AppendMapper(ctx context.Context, mp *dao.Mapper) error {
	log.L().Info("entity.AppendMapper",
		zfield.ID(mp.ID), zfield.Eid(mp.EntityID), zfield.Owner(mp.Owner))

	{
		// check TQL & mapper.
		// TODO: 兼容v0.3, 后面去掉.
		if mp.ID == "" && mp.Name != "" {
			mp.ID = mp.Name
		}

		// check mapper.
		if err := checkMapper(mp); nil != err {
			log.L().Error("append mapper", zfield.Eid(mp.EntityID), zap.Error(err))
			return errors.Wrap(err, "check mapper")
		}
	}

	var err error
	var mo *dao.Mapper
	if mo, err = m.entityRepo.GetMapper(ctx, &dao.Mapper{
		ID: mp.ID, Owner: mp.Owner, EntityID: mp.EntityID}); nil != err {
		if !errors.Is(err, xerrors.ErrMapperNotFound) {
			log.L().Error("append mapper", zap.Error(err), zfield.ID(mp.ID), zfield.Eid(mp.EntityID))
			return errors.Wrap(err, "append mapper")
		}
	} else {
		if mo.Owner != mp.Owner || mo.EntityID != mp.EntityID {
			log.L().Error("append mapper, exists.", zfield.ID(mp.ID), zfield.Eid(mp.EntityID))
			return errors.Wrap(err, "append mapper")
		}
	}

	if err = m.entityRepo.PutMapper(ctx, mp); nil != err {
		log.L().Error("append mapper", zap.Error(err), zfield.ID(mp.ID), zfield.Eid(mp.EntityID))
		return errors.Wrap(err, "append mapper")
	}

	return nil
}

// DeleteMapper delete mapper from entity.
func (m *apiManager) RemoveMapper(ctx context.Context, mp *dao.Mapper) error {
	log.L().Info("entity.RemoveMapper",
		zfield.ID(mp.ID), zfield.Eid(mp.EntityID), zfield.Owner(mp.Owner))

	// delete mapper.
	var err error
	if err = m.entityRepo.DelMapper(ctx, mp); nil != err {
		log.L().Error("remove mapper", zap.Error(err), zfield.ID(mp.ID), zfield.Eid(mp.EntityID))
		return errors.Wrap(err, "remove mapper")
	}

	return nil
}

func (m *apiManager) GetMapper(ctx context.Context, mp *dao.Mapper) (*dao.Mapper, error) {
	log.L().Info("entity.GetMapper",
		zfield.ID(mp.ID), zfield.Eid(mp.EntityID), zfield.Owner(mp.Owner))

	// delete mapper.
	var err error
	if mp, err = m.entityRepo.GetMapper(ctx, mp); nil != err {
		log.L().Error("get mapper", zap.Error(err), zfield.ID(mp.ID), zfield.Eid(mp.EntityID))
		return mp, errors.Wrap(err, "get mapper")
	}

	return mp, nil
}

func (m *apiManager) ListMapper(ctx context.Context, en *Base) ([]dao.Mapper, error) {
	log.L().Info("entity.GetMapper", zfield.Eid(en.ID), zfield.Owner(en.Owner))

	// delete mapper.
	var err error
	var mps []dao.Mapper
	if mps, err = m.entityRepo.ListMapper(ctx,
		m.entityRepo.GetLastRevision(ctx),
		&dao.ListMapperReq{
			Owner:    en.Owner,
			EntityID: en.ID,
		}); nil != err {
		log.L().Error("list mapper", zap.Error(err), zfield.Eid(en.ID), zfield.Owner(en.Owner))
		return mps, errors.Wrap(err, "list mapper")
	}

	return mps, nil
}

func (m *apiManager) addMapper(ctx context.Context, base *BaseRet) error {
	mappers, err := m.entityRepo.ListMapper(ctx,
		m.entityRepo.GetLastRevision(ctx),
		&dao.ListMapperReq{
			Owner:    base.Owner,
			EntityID: base.ID,
		})
	if nil != err {
		return errors.Wrap(err, "list mapper by entity id.")
	}

	for _, mp := range mappers {
		base.Mappers = append(base.Mappers,
			&v1.Mapper{
				Id:          mp.ID,
				Tql:         mp.TQL,
				Name:        mp.Name,
				Description: mp.Description,
			})
	}

	return nil
}

func checkMapper(m *dao.Mapper) error {
	sep := "."
	FieldProps := "properties"
	if m.ID == "" {
		m.ID = util.UUID("mapper")
	}

	if m.TQL == "" {
		return xerrors.ErrInvalidRequest
	}

	// check tql parse.
	tdtlIns, err := tdtl.NewTDTL(m.TQL, nil)
	if nil != err {
		log.L().Error("check mapper", zap.Error(err), zfield.TQL(m.TQL))
		return errors.Wrap(err, "parse TQL")
	}

	propKeys := make(map[string]string)
	for key := range tdtlIns.Fields() {
		propKeys[" "+key] = " " + strings.Join([]string{FieldProps, key}, sep)
	}

	for _, keys := range tdtlIns.Entities() {
		for _, key := range keys {
			segs := strings.SplitN(key, sep, 2)
			if segs[1] != "*" {
				segs = append(segs[:1], append([]string{FieldProps}, segs[1:]...)...)
			}
			propKeys[key] = strings.Join(segs, sep)
		}
	}

	// sort.
	keys := sort.StringSlice{}
	for key := range propKeys {
		keys = append(keys, key)
	}

	sort.Sort(keys)
	for index := range keys {
		key := keys[keys.Len()-index-1]
		m.TQL = strings.ReplaceAll(m.TQL, key, propKeys[key])
	}

	// check tql parse.
	_, err = tdtl.NewTDTL(m.TQL, nil)
	if nil != err {
		log.L().Error("check mapper", zap.Error(err), zfield.TQL(m.TQL))
		return errors.Wrap(xerrors.ErrInternal, "parse TQL")
	}

	return nil
}
