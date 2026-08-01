//go:build !windows

package appshutdown

import "context"

func StartPlatformListener(context.Context) (func(), error) {
	return func() {}, nil
}
