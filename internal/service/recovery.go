package service

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const recoveryTimeout = 5 * time.Second

func recoveryContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), recoveryTimeout)
}

func lifecycleError(cause error, action string, recovery error) error {
	if recovery == nil {
		return cause
	}
	return errors.Join(cause, fmt.Errorf("%s: %w", action, recovery))
}
