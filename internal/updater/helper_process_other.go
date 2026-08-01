//go:build !windows

package updater

import "os/exec"

func configureUpdateHelper(*exec.Cmd) {}
