package tui

type ControlAction string

const (
	CmdStartCoordinator ControlAction = "start_coordinator"
	CmdStopCoordinator  ControlAction = "stop_coordinator"
	CmdAddWorker        ControlAction = "add_worker"
	CmdRemoveWorker     ControlAction = "remove_worker"
	CmdSetConcurrency   ControlAction = "set_concurrency"
	CmdShutdown         ControlAction = "shutdown"
)

type ControlCmd struct {
	Action   ControlAction
	WorkerID string
}
