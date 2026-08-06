package launcher

import (
	"log"
	"os/exec"
)

func (m *Manager) configureManagedCommand(cmd *exec.Cmd) {
	if m == nil || m.processControl == nil {
		return
	}
	m.processControl.configure(cmd)
}

func (m *Manager) attachManagedCommand(serviceName string, cmd *exec.Cmd) {
	if m == nil || m.processControl == nil {
		return
	}
	if err := m.processControl.attach(cmd); err != nil {
		log.Printf("WARN: failed to attach managed service to process group service=%s error=%v", serviceName, err)
	}
}

func (m *Manager) closeManagedProcessControl() {
	if m == nil || m.processControl == nil {
		return
	}
	if err := m.processControl.close(); err != nil {
		log.Printf("WARN: failed to close managed process group: %v", err)
	}
}
