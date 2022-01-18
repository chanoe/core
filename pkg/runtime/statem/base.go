package statem

import (
	"strings"

	"github.com/pkg/errors"
	"github.com/tkeel-io/core/pkg/constraint"
	cerrors "github.com/tkeel-io/core/pkg/errors"
)

// EntityBase statem basic informatinon.
type Base struct {
	ID         string                       `json:"id" msgpack:"id" mapstructure:"id"`
	Type       string                       `json:"type" msgpack:"type" mapstructure:"type"`
	Owner      string                       `json:"owner" msgpack:"owner" mapstructure:"owner"`
	Source     string                       `json:"source" msgpack:"source" mapstructure:"source"`
	Version    int64                        `json:"version" msgpack:"version" mapstructure:"version"`
	LastTime   int64                        `json:"last_time" msgpack:"last_time" mapstructure:"last_time"`
	Mappers    []MapperDesc                 `json:"mappers" msgpack:"mappers" mapstructure:"mappers"`
	Properties map[string]constraint.Node   `json:"properties" msgpack:"properties" mapstructure:"-"`
	Configs    map[string]constraint.Config `json:"configs" msgpack:"-" mapstructure:"-"`
	ConfigFile []byte                       `json:"-" msgpack:"config_file" mapstructure:"-"`
}

func (b *Base) Copy() Base {
	bytes, _ := EncodeBase(b)
	bb, _ := DecodeBase(bytes)
	return *bb
}

func (b *Base) Basic() Base {
	cp := Base{
		ID:         b.ID,
		Type:       b.Type,
		Owner:      b.Owner,
		Source:     b.Source,
		Version:    b.Version,
		LastTime:   b.LastTime,
		Properties: make(map[string]constraint.Node),
		Configs:    make(map[string]constraint.Config),
	}

	cp.Mappers = append(cp.Mappers, b.Mappers...)
	return cp
}

func (b *Base) GetProperty(path string) (constraint.Node, error) {
	if !strings.ContainsAny(path, ".[") {
		if _, has := b.Properties[path]; !has {
			return constraint.NullNode{}, cerrors.ErrPropertyNotFound
		}
		return b.Properties[path], nil
	}

	// patch copy property.
	arr := strings.SplitN(path, ".", 2)
	res, err := constraint.Patch(b.Properties[arr[0]], nil, arr[1], constraint.PatchOpCopy)
	return res, errors.Wrap(err, "patch copy")
}

func (b *Base) GetConfig(path string) (cfg constraint.Config, err error) {
	segs := strings.Split(strings.TrimSpace(path), ".")
	if len(segs) > 1 {
		// check path.
		for _, seg := range segs {
			if strings.TrimSpace(seg) == "" {
				return cfg, constraint.ErrPatchPathInvalid
			}
		}

		rootCfg, ok := b.Configs[segs[0]]
		if !ok {
			return cfg, errors.Wrap(constraint.ErrPatchPathInvalid, "root config not found")
		}

		_, pcfg, err := rootCfg.GetConfig(segs, 1)
		return *pcfg, errors.Wrap(err, "prev config not found")
	} else if len(segs) == 1 {
		if _, ok := b.Configs[segs[0]]; !ok {
			return cfg, cerrors.ErrPropertyNotFound
		}
		return b.Configs[segs[0]], nil
	}
	return cfg, errors.Wrap(constraint.ErrPatchPathInvalid, "copy config")
}
