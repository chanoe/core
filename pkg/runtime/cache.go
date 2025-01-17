package runtime

import (
	"context"

	"github.com/pkg/errors"
	"github.com/tkeel-io/core/pkg/repository"
	"github.com/tkeel-io/tdtl"
)

type EntityCache interface {
	Load(ctx context.Context, id string) (Entity, error)
	Snapshot() error
}

type eCache struct {
	entities   map[string]Entity
	repository repository.IRepository
}

func NewCache(repo repository.IRepository) EntityCache {
	return &eCache{repository: repo,
		entities: make(map[string]Entity)}
}

func (ec *eCache) Load(ctx context.Context, id string) (Entity, error) {
	if state, ok := ec.entities[id]; ok {
		return state, nil
	}

	// load from state store.
	cc := tdtl.New([]byte(`{"properties":{}}`))
	cc.Set("id", tdtl.New(id))
	en, err := NewEntity(id, cc.Raw())
	if nil == err {
		// cache entity.
		ec.entities[id] = en
	}
	return en, errors.Wrap(err, "load cache entity")
}

func (ec *eCache) Snapshot() error {
	panic("implement me")
}
