//go:build !darwin

package notify

func Send(title, message string) error {
	return nil
}
