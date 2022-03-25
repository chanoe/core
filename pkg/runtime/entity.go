package runtime

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
	v1 "github.com/tkeel-io/core/api/core/v1"
	xerrors "github.com/tkeel-io/core/pkg/errors"
	zfield "github.com/tkeel-io/core/pkg/logger"
	"github.com/tkeel-io/core/pkg/scheme"
	"github.com/tkeel-io/kit/log"
	"github.com/tkeel-io/tdtl"
	"github.com/tkeel-io/tdtl/pkg/json/jsonparser"
	"go.uber.org/zap"
)

// some persistent field enumerate.
const (
	FieldID         string = "id"
	FieldType       string = "type"
	FieldOwner      string = "owner"
	FieldSource     string = "source"
	FieldVersion    string = "version"
	FieldLastTime   string = "last_time"
	FieldTemplate   string = "template_id"
	FieldScheme     string = "scheme"
	FieldProperties string = "properties"
)

type PathConstructor func(pc v1.PathConstructor, destVal, setVal []byte, path string) ([]byte, string, error)

type entity struct {
	id              string
	state           tdtl.Collect
	pathConstructor PathConstructor
}

func DefaultEntity(id string) Entity {
	return &entity{id: id, state: *tdtl.New([]byte(`{}`)), pathConstructor: pathConstructor}
}

func NewEntity(id string, state []byte) (Entity, error) {
	s := tdtl.New(state)
	s.Set("scheme", tdtl.New([]byte("{}")))
	return &entity{id: id, state: *s,
			pathConstructor: pathConstructor},
		errors.Wrap(s.Error(), "new entity")
}

func (e *entity) ID() string {
	return e.id
}

func (e *entity) Get(path string) tdtl.Node {
	return e.state.Get(path)
}

func (e *entity) Handle(ctx context.Context, feed *Feed) *Feed {
	if nil != feed.Err {
		return feed
	}

	pc := feed.Event.Attr(v1.MetaPathConstructor)
	var changes []Patch
	cc := e.state.Copy()
	for _, patch := range feed.Patches {
		switch patch.Op {
		case OpAdd:
			cc.Append(patch.Path, patch.Value)
		case OpCopy:
		case OpMerge:
			res := cc.Get(patch.Path).Merge(patch.Value)
			cc.Set(patch.Path, res)
		case OpRemove:
			cc.Del(patch.Path)
		case OpReplace:
			// construct sub path if not exists.
			pcIns := v1.PathConstructor(pc)
			patchVal, patchPath, err := e.pathConstructor(pcIns, e.state.Raw(), patch.Value.Raw(), patch.Path)
			if nil != err {
				log.L().Error("update entity", zfield.Eid(e.id), zap.Error(err),
					zap.Any("patches", feed.Patches), zfield.Event(feed.Event))
				// in.Patches 处理完毕，丢弃.
				feed.Err = cc.Error()
				feed.Patches = []Patch{}
				feed.State = e.Raw()
				return feed
			}
			cc.Set(patchPath, tdtl.New(patchVal))
		default:
			return &Feed{Err: xerrors.ErrPatchPathInvalid}
		}

		if nil != cc.Error() {
			log.L().Error("update entity", zfield.Eid(e.id), zap.Error(cc.Error()),
				zap.Any("patches", feed.Patches), zfield.Event(feed.Event))
			break
		}

		switch patch.Op {
		case OpMerge:
			patch.Value.Foreach(func(key []byte, value *tdtl.Collect) {
				changes = append(changes, Patch{
					Op: OpReplace, Value: value,
					Path: strings.Join([]string{patch.Path, string(key)}, ".")})
			})
		default:
			changes = append(changes,
				Patch{Op: patch.Op, Path: patch.Path, Value: patch.Value})
		}
	}

	if cc.Error() == nil {
		e.state = *cc
	}

	// in.Patches 处理完毕，丢弃.
	feed.Err = cc.Error()
	feed.Changes = changes
	feed.Patches = []Patch{}
	feed.State = e.Raw()
	return feed
}

func (e *entity) Raw() []byte {
	return e.state.Copy().Raw()
}

func (e *entity) Copy() Entity {
	cp := e.state.Copy()
	return &entity{
		id:    e.id,
		state: *cp,
	}
}

func (e *entity) Basic() *tdtl.Collect {
	basic := e.state.Copy()
	basic.Set("scheme", tdtl.New([]byte("{}")))
	basic.Set("properties", tdtl.New([]byte("{}")))
	return basic
}

func (e *entity) Tiled() tdtl.Node {
	basic := e.state.Copy()
	basic.Del(FieldScheme)
	basic.Del(FieldProperties)
	result := basic.Merge(tdtl.New(e.Properties().Raw()))
	return result
}

func (e *entity) Type() string {
	return e.state.Get(FieldType).String()
}
func (e *entity) Owner() string {
	return e.state.Get(FieldOwner).String()
}
func (e *entity) Source() string {
	return e.state.Get(FieldSource).String()
}
func (e *entity) Version() int64 {
	version := e.state.Get(FieldVersion).String()
	i, _ := strconv.ParseInt(version, 10, 64)
	return i
}
func (e *entity) LastTime() int64 {
	lastTime := e.state.Get(FieldLastTime).String()
	i, _ := strconv.ParseInt(lastTime, 10, 64)
	return i
}
func (e *entity) TemplateID() string {
	return e.state.Get(FieldTemplate).String()
}
func (e *entity) Properties() tdtl.Node {
	return e.state.Get("properties")
}
func (e *entity) Scheme() tdtl.Node {
	return e.state.Get("scheme")
}
func (e *entity) GetProp(key string) tdtl.Node {
	return e.state.Get("properties." + key)
}

func pathConstructor(pc v1.PathConstructor, destVal, setVal []byte, path string) (_ []byte, _ string, err error) {
	switch pc {
	case v1.PCScheme:
		setVal, path, err = makeSubPath(destVal, setVal, path)
		return setVal, path, errors.Wrap(err, "make sub path")
	default:
	}
	return setVal, path, nil
}

func makeSubPath(dest, src []byte, path string) ([]byte, string, error) {
	var index int
	segs := strings.Split(path, ".")
	seg0, segs := segs[0], segs[1:]
	dest = tdtl.New(dest).Get(seg0).Raw()
	for ; index < len(segs); index += 3 {
		if _, _, _, err := jsonparser.Get(dest, segs[:index+1]...); nil != err {
			if errors.Is(err, jsonparser.KeyPathNotFoundError) {
				break
			}
			return nil, path, errors.Wrap(err, "make sub path")
		}
	}

	if index >= len(segs) {
		return src, path, nil
	}

	missSegs := segs[index:]
	if len(missSegs) > 3 {
		path = strings.Join(append([]string{seg0}, segs[:index+1]...), ".")
		return makeScheme(missSegs, src), path, nil
	}

	return src, path, nil
}

func makeScheme(segs []string, data []byte) []byte {
	cfg := &scheme.Config{
		ID:                segs[0],
		Type:              "struct",
		Name:              segs[0],
		Enabled:           true,
		EnabledSearch:     true,
		EnabledTimeSeries: true,
		Define:            map[string]interface{}{},
		LastTime:          time.Now().UnixNano() / 1e6,
	}

	head := cfg
	mids := segs[3 : len(segs)-1]
	for index := 0; index < len(mids); index += 3 {
		curCfg := &scheme.Config{
			ID:                mids[index],
			Type:              "struct",
			Name:              mids[index],
			Enabled:           true,
			EnabledSearch:     true,
			EnabledTimeSeries: true,
			Define:            map[string]interface{}{},
			LastTime:          time.Now().UnixNano() / 1e6,
		}

		head.Define["fields"] = map[string]interface{}{mids[index]: curCfg}
		head = curCfg
	}

	// set last seg.
	var v interface{}
	json.Unmarshal(data, &v)
	head.Define["fields"] =
		map[string]interface{}{
			segs[len(segs)-1]: v}
	bytes, _ := json.Marshal(cfg)

	return bytes
}
