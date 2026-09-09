//go:build !unix

package claude

import "os"

func openAuthFile(path string) (*os.File, error) {
	return os.Open(path)
}
