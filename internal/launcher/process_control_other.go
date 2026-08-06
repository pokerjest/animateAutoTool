//go:build !windows

package launcher

import "os/exec"

type processControl struct{}

func newProcessControl() *processControl {
	return &processControl{}
}

func (*processControl) configure(*exec.Cmd) {}
func (*processControl) attach(*exec.Cmd) error {
	return nil
}
func (*processControl) close() error {
	return nil
}
