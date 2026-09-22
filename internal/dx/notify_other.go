//go:build !darwin

package dx

func Notify(title, message string) error {
	return nil
}
