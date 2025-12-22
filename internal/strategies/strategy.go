package strategies

import "context"

type Strategy interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}
