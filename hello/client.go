package hello

import "context"

const (
	systemName          = "HELLO"
	clientComponentName = "hello_client"
)

type Client interface {
	Hello(ctx context.Context, name string) (string, error)
}
