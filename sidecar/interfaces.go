package sidecar

// ProcessManager defines the operations needed to manage background processes.
// Manager implements this interface. Test code can provide mock implementations.
type ProcessManager interface {
	Start(command, name, cwd string, env map[string]string) (*Process, error)
	Stop(id string) (*Process, error)
	List() []*Process
	Get(id string) (*Process, error)
}
