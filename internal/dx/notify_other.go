//go:build !darwin

package dx

import "context"

func Notify(ctx context.Context, title, message string) error {
	return nil
}
