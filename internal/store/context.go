package store

import (
	"context"
	"fmt"
)

type contextKeyStore struct{}

func ContextWithStore(parent context.Context, dataStore Store) context.Context {
	return context.WithValue(parent, contextKeyStore{}, dataStore)
}

func StoreFromContext(ctx context.Context) Store {
	value := ctx.Value(contextKeyStore{})
	dataStore, ok := value.(Store)
	if !ok || dataStore == nil {
		panic(fmt.Sprintf("store is missing from context: %T", value))
	}
	return dataStore
}

// StoreFromContextSafe returns the store from context, or nil if not present.
func StoreFromContextSafe(ctx context.Context) Store {
	value := ctx.Value(contextKeyStore{})
	dataStore, _ := value.(Store)
	return dataStore
}
