package dictionary

import "context"

type Client interface {
	LookupWord(ctx context.Context, word string) (definition string, err error)
}
