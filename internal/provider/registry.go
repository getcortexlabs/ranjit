package provider

import (
	"context"
	"fmt"
	"sort"

	"github.com/getcortexlabs/ranjit"
)

// Registry routes each profile to its own credentials and account. Profile names
// are part of persisted institution IDs; renaming one requires a new connection.
type Registry struct{ sources map[string]Source }

func NewRegistry() *Registry { return &Registry{sources: make(map[string]Source)} }

func (r *Registry) Add(name string, source Source) error {
	if _, exists := r.sources[name]; exists || name == "" {
		return fmt.Errorf("duplicate or empty profile")
	}
	r.sources[name] = source
	return nil
}

func (r *Registry) Institutions() []ranjit.Institution {
	items := []ranjit.Institution{}
	for name, source := range r.sources {
		for _, i := range source.Institutions() {
			i.ID = name + ":" + i.ID
			i.Profile = name
			i.Name = name + " · " + i.Name
			items = append(items, i)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items
}

func (r *Registry) Resolve(id string) (Source, string, error) {
	for name, source := range r.sources {
		for _, i := range source.Institutions() {
			if name+":"+i.ID == id {
				return source, i.ID, nil
			}
		}
	}
	return nil, "", ranjit.ErrProviderCredentials
}

func (r *Registry) Fetch(ctx context.Context, c ranjit.Connection) (Snapshot, error) {
	source, id, err := r.Resolve(c.InstitutionID)
	if err != nil {
		return Snapshot{}, err
	}
	c.InstitutionID = id
	return source.Fetch(ctx, c)
}

func (r *Registry) Close() {
	for _, s := range r.sources {
		if c, ok := s.(interface{ Close() }); ok {
			c.Close()
		}
	}
}
