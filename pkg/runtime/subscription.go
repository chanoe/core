package runtime

import (
	"context"
	"encoding/json"

	daprSDK "github.com/dapr/go-sdk/client"
	"github.com/pkg/errors"
	v1 "github.com/tkeel-io/core/api/core/v1"
	zfield "github.com/tkeel-io/core/pkg/logger"
	"github.com/tkeel-io/core/pkg/repository/dao"
	"github.com/tkeel-io/core/pkg/util/dapr"
	xjson "github.com/tkeel-io/core/pkg/util/json"
	"github.com/tkeel-io/kit/log"
	"github.com/tkeel-io/tdtl"
	"go.uber.org/zap"
)

type SubscriptionMode string

func (sm SubscriptionMode) S() string {
	return string(sm)
}

const (
	SModePeriod    SubscriptionMode = "PERIOD"
	SModeRealtime  SubscriptionMode = "REALTIME"
	SModeOnChanged SubscriptionMode = "ONCHANGED"
)

// 为了订阅实体实现的外部订阅.
func (r *Runtime) handleSubscribe(ctx context.Context, feed *Feed) *Feed {
	log.L().Debug("handle subscribe", zfield.Eid(feed.EntityID), zfield.Event(feed.Event))
	ev, _ := feed.Event.(v1.PatchEvent)

	var err error
	subID := ev.Entity()
	entityID := ev.Attr(v1.MetaSender)
	state, err := r.LoadEntity(subID)
	if nil != err {
		log.L().Error("load entity", zap.Error(err), zfield.Eid(subID))
		feed.Err = err
		return feed
	}

	switch state.Type() {
	case dao.EntityTypeSubscription:
		mode := state.GetProp("mode").String()
		topic := state.GetProp("topic").String()
		pubsubName := state.GetProp("pubsub_name").String()
		log.L().Debug("publish subscription message", zfield.ID(subID), zfield.Event(ev),
			zfield.Eid(entityID), zfield.Topic(topic), zfield.Pubsub(pubsubName), zfield.Mode(mode))

		changes := feed.Patches
		if len(changes) == 0 {
			log.L().Warn("publish empty message", zfield.ID(subID), zfield.Event(ev),
				zfield.Eid(entityID), zfield.Topic(topic), zfield.Pubsub(pubsubName), zfield.Mode(mode))
			return feed
		}

		var payload []byte
		if payload, err = makePayload(ev, changes); nil != err {
			log.L().Error("publish message, make payload", zfield.ID(subID), zfield.Event(ev),
				zfield.Eid(entityID), zfield.Topic(topic), zfield.Pubsub(pubsubName), zfield.Mode(mode))
			return feed
		}

		log.L().Debug("publish message", zfield.ID(subID), zfield.Event(ev), zfield.Payload(payload),
			zfield.Eid(entityID), zfield.Topic(topic), zfield.Pubsub(pubsubName), zfield.Mode(mode))

		switch mode {
		case SModeRealtime.S():
			ctOpts := daprSDK.PublishEventWithContentType("application/json")
			err = dapr.Get().Select().PublishEvent(ctx, pubsubName, topic, payload, ctOpts)
			if nil != err {
				log.L().Error("publish message via dapr", zfield.ID(subID), zfield.Event(ev),
					zfield.Eid(entityID), zfield.Topic(topic), zfield.Pubsub(pubsubName), zfield.Mode(mode))
				return feed
			}
		case SModeOnChanged.S():
		//	dapr.Get().Select().PublishEvent(ctx, pubsubName, topic, nil)
		case SModePeriod.S():
		default:
		}
	default:
		return feed
	}

	return feed
}

func makePayload(ev v1.PatchEvent, changes []Patch) ([]byte, error) {
	basics := map[string]string{
		"id":           ev.Attr(v1.MetaSender),
		"subscribe_id": ev.Entity(),
		"type":         ev.Attr(v1.MetaEntityType),
		"owner":        ev.Attr(v1.MetaOwner),
		"source":       ev.Attr(v1.MetaSource),
	}
	bytes, _ := json.Marshal(basics)

	cc := tdtl.New(`{"properties":{}}`)
	for _, change := range changes {
		switch change.Op {
		case xjson.OpAdd:
			cc.Append(change.Path, change.Value)
		case xjson.OpMerge:
			val := change.Value.Copy()
			val.Merge(cc.Get(change.Path))
			cc.Set(change.Path, val)
		case xjson.OpReplace:
			cc.Set(change.Path, change.Value)
		}

		// check patch.
		if nil != cc.Error() {
			return nil, errors.Wrap(cc.Error(), "patch json")
		}
	}

	payload := tdtl.New(bytes)
	payload.Set(FieldProperties, cc.Get(FieldProperties))
	return payload.Raw(), payload.Error()
}
